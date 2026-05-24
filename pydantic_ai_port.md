# Implementation Plan - Port to Pydantic AI Graph with Go-Parity Non-Interactive CLI

This port replaces Strix Python's custom orchestration loop with [Pydantic AI](https://github.com/pydantic/pydantic-ai) + [Pydantic Graph](https://pydantic.dev/docs/ai/graph/graph/) for non-interactive runs, while preserving Docker sandbox runtime behavior and aligning with proven lessons from:
- `PYTHON_SKILL_COMPARISON.md`
- `GO_PORT_TOOL_FIX_PLAN.md`
- `strix-go` non-interactive CLI behavior and default model selection

## Decisions Locked In

- Non-interactive flow is the first and only migration target in this phase (`--non-interactive` path).
- Interactive/TUI flow remains on the existing implementation in this phase.
- Tool schema exposure must be context-aware (sandbox vs parent) at prompt-generation time.
- Scope boundary: this plan targets the Python `strix-pydantic` port only.
  - Go findings are reference input for design parity, not in-scope code changes in this phase.
- New port codebase is fully separated under `strix-pydantic/`.
  - No new orchestrator/runtime code is added under `strix/`.
  - `strix/` remains the shared content source for non-code assets (skills, prompt templates, XML schemas, scan-mode markdown, etc.).
- LiteLLM is not used in the new non-interactive port path.
  - New path uses native Pydantic AI model/provider integration directly.
  - Legacy interactive path may continue using existing components until migrated.
- Pydantic target version for this port is `pydantic-ai==2.0.0b1`.
  - Reference release: https://github.com/pydantic/pydantic-ai/releases/tag/v2.0.0b1
  - Build and tests should target v2 beta APIs only.
- Default model behavior must match `strix-go` exactly:
  - If `STRIX_LLM` is unset and `ANTHROPIC_API_KEY` is set: `claude-haiku-4-5`
  - If `STRIX_LLM` is unset and `ANTHROPIC_API_KEY` is not set: `gemini-3.5-flash`

## Required Changes

### 1. Dependencies

#### [MODIFY] [strix-pydantic/pyproject.toml](/home/mdfranz/github/strix-goclient/strix-pydantic/pyproject.toml)
- Pin `pydantic-ai==2.0.0b1`.
- Pin compatible `pydantic-graph` version required by `2.0.0b1`.
- Allow pre-release resolution when generating lockfile (`uv` pre-release mode).
- Add/update lock file from `strix-pydantic` environment after dependency pinning.

### 1.1 Codebase Separation

#### [NEW] `strix-pydantic/` (top-level package root)
Create a standalone Python port package and keep all new implementation there:
- `strix-pydantic/pyproject.toml`
- `strix-pydantic/strix_pydantic/...` (orchestrator, CLI, model resolver, tool wrappers)
- `strix-pydantic/tests/...`

Boundary rule:
- `strix-pydantic` may read shared content from `strix/`.
- `strix-pydantic` should not depend on legacy `strix` Python code modules for orchestration/runtime logic.
- Reuse policy: copying and adapting proven code from `strix/` and `strix-go/` is encouraged (including Docker/runtime pieces), but the resulting maintained implementation should live under `strix-pydantic/`.

### 2.0 Shared Types (Graph State & Deps)

These types must be defined before any graph node or agent construction code. Every node's `run(ctx: GraphRunContext[StrixRunState, StrixDeps])` signature depends on them.

#### `StrixRunState` — mutable graph state, flows through all nodes

```python
from dataclasses import dataclass, field
from typing import Any
from pydantic_ai import ModelMessage

@dataclass
class StrixRunState:
    # Run identity
    run_id: str
    target: str
    scan_mode: str                    # "quick" | "standard" | "deep"

    # Resolved in BootstrapRun
    active_skills: list[str] = field(default_factory=list)

    # Per-role message history passed to Agent.run(message_history=...) for continuity
    message_history: dict[str, list[ModelMessage]] = field(default_factory=dict)

    # Agent lifecycle
    agent_statuses: dict[str, str] = field(default_factory=dict)  # role -> status string

    # Findings
    vulnerabilities: list[dict[str, Any]] = field(default_factory=list)
    notes: list[dict[str, Any]] = field(default_factory=list)
    tool_observations: list[dict[str, Any]] = field(default_factory=list)

    # Iteration guard (mirrors AgentState.iteration / max_iterations)
    iteration: int = 0
    max_iterations: int = 300

    # Terminal state
    final_summary: str | None = None
    error: str | None = None
```

**Constraint:** Do not embed non-serializable objects (`Agent` instances, `asyncio.Event`) in state. pydantic-graph owns state serialization.

#### `StrixDeps` — injected before `graph.run()`, read-only during execution

```python
from dataclasses import dataclass
from typing import Any
from pydantic_ai import Agent

@dataclass
class StrixDeps:
    sandbox_client: SandboxClient          # Docker runtime (§6.1/6.2)
    tool_registry: ToolRegistry            # Context-aware lookup (§3)
    run_config: RunConfig                  # Model name, timeouts, mode
    agents: dict[str, Agent[StrixDeps, Any]]  # Pre-constructed, keyed by role
```

#### `RunConfig`

```python
@dataclass
class RunConfig:
    model_name: str
    scan_mode: str
    non_interactive: bool
    connect_timeout: float = 5.0
    execute_timeout: float = 120.0
    orchestrator_step_timeout: float = 180.0
```

#### Agent construction ownership

Agents are constructed **in the CLI entrypoint** (§4), before `graph.run()` is called — not inside `BootstrapRun`. `deps` are set at `graph.run()` time and cannot change mid-run; `BootstrapRun` only validates/records in `state`.

Construction sequence in `cli.py`:
1. Resolve model → `model_config.py` (§5)
2. Build base toolsets per role (sandbox / parent)
3. Wrap with `FilteredToolset` (§3)
4. Build `Agent[StrixDeps, Any]` per role with skill-derived instructions
5. Build `StrixDeps(sandbox_client, tool_registry, run_config, agents)`
6. Call `graph.run(state=StrixRunState(...), deps=deps)`

#### Corrected implementation order

1. §1 + §1.1 — package scaffold
2. §5 — `model_config.py` (no deps)
3. §6.1/6.2 — `SandboxClient` Docker interface
4. §3 — `ToolRegistry` with `FilteredToolset` wrapping
5. **§2.0** — `StrixRunState`, `StrixDeps`, `RunConfig`
6. §2 — graph orchestrator nodes
7. §3.2 — `SkillCapabilityFactory` returning `SkillBuild`
8. §4 — CLI (constructs agents + deps, runs graph)
9. §7 — Restate (deferred)

---

### 2. Pydantic Graph Orchestrator

#### [NEW] [pydantic_orchestrator.py](/home/mdfranz/github/strix-goclient/strix-pydantic/strix_pydantic/agents/pydantic_orchestrator.py)
Implement a `pydantic_graph`-driven orchestrator for non-interactive scans with:
- `GraphBuilder` as the control-plane for orchestration stages.
- Typed shared state via graph state model (`GraphRunContext.state`).
- Explicit node transitions using `End` and typed step/edge routing.
- Optional `Graph.iter` support for step-level logging and debugging.
- Model resolution parity with `strix-go/pkg/llm/llm.go`.
- Context-aware tool registration (`sandbox` and `parent`) so an agent only receives executable tools in its prompt.
- Pre-execution tool validation with explicit errors when a tool is unavailable in the current context.
- Tool wrappers that support existing `agent_state`-dependent tools.
- Prompt rendering that removes XML `<tool_usage>` format instructions (Pydantic AI provides tool schema natively).
- No dependency on `strix.llm.LLM`/LiteLLM in this path.

Note:
- This module is owned by `strix-pydantic/` only.

#### Graph Topology (Initial)
- `BootstrapRun` -> resolve targets, run config, model, and execution context.
- `RunAgentTurn` -> execute one model turn and collect tool intents.
- `DispatchToolCalls` -> validate context, execute tools, append observations.
- `WaitOrResume` -> handle wait states/messages/timeouts.
- `FinalizeRun` -> produce final summary payload and terminal status.
- `AbortRun` -> deterministic failure state with structured error output.

### 3. Tool Availability Architecture (Lessons Applied)

#### [NEW] [tool_registry.py](/home/mdfranz/github/strix-goclient/strix-pydantic/strix_pydantic/tools/tool_registry.py)
Implement explicit context views for tool schemas and lookups:
- `get_tools_for_context(context: Literal["sandbox", "parent"])`
- `get_tools_prompt_for_context(context: Literal["sandbox", "parent"])`
- `is_tool_available_in_context(tool_name: str, context: str) -> bool`

Behavior requirements:
- Sandbox agents must not receive schemas for parent-only tools.
- Parent agents may receive all tools.
- Prompt generation uses context-specific tool lists only (no global unfiltered tool prompt).

### 3.1 Shared Content Reuse (Go-style)

`strix-pydantic` should implement content-path resolution equivalent to Go port behavior:
- Prefer sibling/local candidates first (for dev monorepo usage), then fallback to configured path.
- Suggested env var override: `STRIX_CONTENT_DIR`.
- Candidate resolution should verify required content directories exist (`tools/`, `skills/`, `agents/` templates).

Minimum reused content from `strix/`:
- `skills/` markdown corpus
- `tools/*/*_schema.xml` tool schema files
- `agents/*/system_prompt.jinja` prompt templates
- scan-mode and coordination markdown used by prompts

Allowed code migration examples:
- Docker sandbox lifecycle helpers and client wrappers.
- Tool argument normalization/validation patterns.
- Non-interactive summary formatting and run-directory conventions.

### 3.2 Capabilities Strategy for `strix/skills`

For `pydantic-ai==2.0.0b1`, skills are wired as instructions + toolset wrappers rather than abstract capability objects.

#### [NEW] [skill_capability_factory.py](/home/mdfranz/github/strix-goclient/strix-pydantic/strix_pydantic/skills/skill_capability_factory.py)
Build a factory that converts selected `strix/skills` content into a `SkillBuild` per agent role:
- Input: selected skill IDs, agent role, execution context (`sandbox` or `parent`), and run mode.
- Output: `SkillBuild` containing instructions list + composed toolset.
- Reuse source: markdown and metadata from `strix/skills` content directory (no orchestration logic imports from `strix` package code).

#### `SkillBuild` return type

```python
from dataclasses import dataclass
from typing import Any, Callable
from pydantic_ai import AbstractToolset, RunContext

@dataclass
class SkillBuild:
    instructions: list[str | Callable[[RunContext[StrixDeps]], str]]
    toolset: AbstractToolset[StrixDeps]   # FilteredToolset layered
```

#### Skill concept → pydantic-ai construct mapping

| Skill concept | pydantic-ai construct | Notes |
|---|---|---|
| Instruction | `Agent(instructions=[str, Callable])` | Static `str` → `dynamic=False` → Anthropic cache-eligible; `Callable` → `dynamic=True` |
| Tool policy | `FilteredToolset(wrapped, filter_func)` | `filter_func(RunContext[StrixDeps], ToolDefinition) -> bool` |
| Hook | `prepare` param on `@agent.tool` | Per-tool pre-execution callback; use `AbstractCapability.wrap_node_run` for cross-cutting hooks |

#### Toolset composition (innermost → outermost, applied left-to-right on call path)

```python
from pydantic_ai import FilteredToolset, FunctionToolset

def build_toolset(skill_ids: list[str], run_config: RunConfig) -> AbstractToolset[StrixDeps]:
    base = FunctionToolset()          # all tools for this context registered here
    return FilteredToolset(
        wrapped=base,
        filter_func=skill_tool_policy_filter(skill_ids),
    )
```

#### Instruction construction (skills → `Agent(instructions=...)`)

```python
def build_instructions(skill_ids: list[str], role: str) -> list[str | Callable]:
    static_parts = [load_skill_instruction_text(sid) for sid in skill_ids]
    dynamic_part = lambda ctx: f"Role: {role}. Target: {ctx.deps.run_config.target}"
    return static_parts + [dynamic_part]
```

Pass to `Agent(instructions=build_instructions(active_skills, role), ...)`.

#### Graph Integration
- `BootstrapRun` resolves active skills for each role and records in `state.active_skills`.
- CLI entrypoint calls `SkillCapabilityFactory` per role and passes `SkillBuild` into agent construction.
- `RunAgentTurn` calls the pre-constructed agent; toolset wrappers enforce policy inline.

#### Test Requirements
- Skill instructions are present when capability is enabled and absent when disabled.
- `FilteredToolset` removes skill-denied tools from schema and execution path.

### 4. Non-Interactive CLI Parity with Go

#### [NEW] [cli.py](/home/mdfranz/github/strix-goclient/strix-pydantic/strix_pydantic/interface/cli.py)
Replace the non-interactive execution path to use the new Pydantic orchestrator and output in Go-style sequence:
- Print startup lines for log destinations and active model.
- Stream concise emoji/status activity lines for agent/tool lifecycle.
- Print final summary sections matching Go headings:
  - `🤖 [AGENT STATUSES]`
  - `🚨 [VULNERABILITIES IDENTIFIED (N)]`
  - `📝 [NOTES & FINDINGS (N)]`
- Non-interactive CLI should call the graph orchestrator entrypoint only (no LiteLLM route).
- CLI for the new port should be owned by `strix-pydantic` package entrypoint.

### 5. Model Resolution Parity

#### [NEW] [model_config.py](/home/mdfranz/github/strix-goclient/strix-pydantic/strix_pydantic/config/model_config.py)
Add resolver logic compatible with Go behavior:
- Read `STRIX_LLM` first.
- If unset, fallback exactly as Go (`claude-haiku-4-5` with Anthropic key, else `gemini-3.5-flash`).
- Preserve provider-prefix normalization (`openai/`, `anthropic/`, `gemini/`, `googleai/`, `azure/`, `bedrock/`) so routing and model names are stable.
- Return Pydantic AI model/provider configuration directly for the new orchestrator path.

### 6. Graph Handling Fixes (From Python/Go Review)

The new graph orchestrator must explicitly avoid current implementation pitfalls:
- Synchronize graph state mutations/reads (node registry, edges, message queues) with a single concurrency strategy; no unsynchronized global dict/list access across threads.
- Handle unknown sender IDs safely when converting inter-agent messages into prompt content (never assume sender lookup exists).
- Validate spawn hooks before mutating graph topology to avoid orphaned `running` nodes when spawn initialization fails.
- Make wait/timeout behavior configurable and consistent with run mode (non-interactive vs interactive), not hard-coded.
- Add regression tests for the above in `strix-pydantic` test suite.

### 6.1 Orchestrator <-> Docker Tool Interface (Port-Critical)

For the `strix-pydantic` port, the sandbox interface contract must include:
- Request shape parity:
  - `agent_id`, `tool_name`, `kwargs` payload to `/execute`.
- Agent context propagation:
  - Ensure per-agent tool context is set before each tool execution.
- Deterministic cancellation semantics:
  - Avoid "task cancelled but side effect still running" behavior.
  - Cancellation/timeout must terminate underlying execution path or mark it as detached and non-overlapping.
- Explicit network exposure policy:
  - Bind tool-server ports to loopback-only host bindings.
- Error contract stability:
  - Normalize server transport/auth/timeout/tool errors into typed orchestrator errors.
- Timeout layering:
  - Separate and tune connect timeout, tool execution timeout, and orchestrator step timeout.

#### Docker Image Constraint
- The port does not change Docker images or image tags.
- Reuse current sandbox image contract as-is; only orchestrator/client-side code changes are in scope.

### 6.2 Interface Contract Draft (For `strix-pydantic`)

#### Request Model (`POST /execute`)
```python
from typing import Any
from pydantic import BaseModel, Field

class ToolExecutionRequest(BaseModel):
    agent_id: str = Field(min_length=1)
    tool_name: str = Field(min_length=1)
    kwargs: dict[str, Any] = Field(default_factory=dict)
```

#### Response Model
```python
from typing import Any, Literal
from pydantic import BaseModel

ToolErrorCode = Literal[
    "auth_error",
    "tool_not_found",
    "invalid_arguments",
    "tool_timeout",
    "tool_cancelled",
    "tool_runtime_error",
    "transport_error",
]

class ToolError(BaseModel):
    code: ToolErrorCode
    message: str
    retriable: bool = False
    details: dict[str, Any] | None = None

class ToolExecutionResponse(BaseModel):
    ok: bool
    result: Any | None = None
    error: ToolError | None = None
```

#### Orchestrator-Side Normalized Result
```python
from dataclasses import dataclass
from typing import Any

@dataclass
class SandboxToolResult:
    ok: bool
    result: Any | None
    error_code: str | None
    error_message: str | None
    retriable: bool
```

#### Timeout Contract
- `connect_timeout`: short (network reachability/auth path).
- `execute_timeout`: per-tool wall clock timeout enforced by server.
- `orchestrator_step_timeout`: outer bound for a graph step (includes retries/backoff).
- Rule: each layer reports a distinct error code; do not collapse all timeout classes into one string.

#### Cancellation Contract
- Cancellation must be explicit at execution backend level (not only coroutine/task cancellation).
- If hard-stop is not possible for a tool backend, mark execution as `detached` and prevent overlapping runs for the same `(agent_id, tool_name)` slot until completion/expiry.

#### Wire Sequence (Single Tool Call)
1. Orchestrator validates tool availability in execution context.
2. Orchestrator builds `ToolExecutionRequest(agent_id, tool_name, kwargs)`.
3. Orchestrator sends authenticated `POST /execute` to sandbox server.
4. Tool server sets per-agent context and executes tool.
5. Tool server returns `ToolExecutionResponse`.
6. Orchestrator maps response to `SandboxToolResult`.
7. Orchestrator appends normalized observation to graph state/history.
8. If `retriable=True`, retry policy is applied at orchestrator layer; otherwise fail step.

### 7. Durable Execution Evaluation (Restate)

Restate is a strong fit for the reliability challenges observed so far, especially for non-interactive scans and review/HITL workflows.

What it directly helps with:
- Crash/restart recovery:
  - Execution is journaled and replayed; completed steps are skipped on recovery.
- Duplicate side-effect prevention:
  - External/tool operations wrapped in durable steps are replay-safe.
- Long-running + pause/resume:
  - Supports approval workflows that suspend and resume across restarts.
- Multi-agent/tool orchestration:
  - Complex tool logic can be extracted into remote durable workflows.

Integration model (from current Restate + Pydantic guidance):
- Run agent inside a Restate service HTTP handler.
- Wrap agent with `RestateAgent`.
- In tools, wrap non-deterministic operations via `restate_context().run_typed(...)`.
- Keep deterministic replay discipline:
  - Non-deterministic work must be wrapped as durable steps.
  - Avoid nested Restate-context operations inside `ctx.run` bodies.

Tradeoffs / adoption costs:
- Requires additional runtime infrastructure (Restate server + service deployment shape).
- Requires explicit durable-step boundaries in tool wrappers.
- Timeout settings must be tuned for long LLM/tool calls (inactivity/abort policies).

Current repo status:
- `restate` / `restate.ext.pydantic` are not installed in local `.venv` yet.

Recommended path:
- Phase 1 (current): complete Pydantic Graph port without Restate dependency.
- Phase 2 (pilot): add optional Restate runner for non-interactive mode behind a feature flag.
- Phase 3: graduate Restate path after parity + recovery tests pass.

## Local Venv API Notes (Verified)

Installed versions detected from local `.venv`:
- `pydantic_ai==2.0.0b1`
- `pydantic_graph` installed (module import available)

Version note:
- These local API checks are from the v2 beta line.
- Final implementation should remain pinned and validated against `pydantic-ai==2.0.0b1`.

Implementation constraints from installed API surface:
- Use `GraphBuilder` API for new orchestration graph.
  - `GraphBuilder(...).build()` returns `pydantic_graph.graph_builder.Graph`.
  - Builder graph `run` signature is `run(*, state=..., deps=..., inputs=...)`.
- Do not base new design on importing `Graph` from `pydantic_graph` root.
  - Local package emits a deprecation warning for the old `BaseNode` runner import path.
- Use `GraphBuilder` control constructs for orchestration:
  - `edge_from(...).to(...)`
  - `edge_from(...).map(...)` for iterable fan-out
  - `join(...)` for reduction/merge
  - `decision(...)` / `match(...)` for branch routing
- Use `Agent.run(..., event_stream_handler=...)` or `Agent.iter(...)` for non-interactive progress logging and tool lifecycle hooks.
- Use typed `RunContext` deps for injecting Strix runtime state into tools instead of ad-hoc globals.
- Use `UsageLimits` to enforce request/tool-call/token caps in addition to scan iteration guardrails.

## Verification Plan

### Manual Verification

1. Install dependencies:
   ```bash
   uv sync
   uv pip show pydantic-ai
   ```
   - Validate installed version is `pydantic-ai==2.0.0b1`.
2. Run non-interactive scan:
   ```bash
   uv run strix --target https://example.com --non-interactive
   ```
3. Confirm CLI parity behavior:
   - Startup lines include log files and selected model.
   - Activity lines are streamed sequentially with status symbols.
   - Final summary sections print with the three Go-style headings.
4. Confirm tool-context safety:
   - A sandbox agent prompt excludes parent-only tools (e.g. `load_skill`).
   - Calling a context-forbidden tool fails early with a clear validation error.
5. Confirm skill capability behavior:
   - Selected skills inject expected instruction/policy behavior.
   - Skill-denied tools are not visible in tool schemas for that run.
6. Confirm default model parity:
   - `STRIX_LLM` unset + `ANTHROPIC_API_KEY` set => `claude-haiku-4-5`
   - `STRIX_LLM` unset + no `ANTHROPIC_API_KEY` => `gemini-3.5-flash`
   - `STRIX_LLM` set => exact configured model is used.
7. Confirm LiteLLM bypass in new path:
   - Running with `--non-interactive` does not instantiate `strix.llm.LLM`.
   - Provider/model calls are issued through Pydantic AI model classes/providers.
8. Confirm graph race/edge-case fixes:
   - Concurrent message delivery does not corrupt graph state.
   - Unknown sender IDs do not crash message processing.
   - Spawn hook failures do not leave orphaned running nodes.
   - Wait behavior follows configured timeout policy.
9. Confirm durable-execution pilot behavior (if Restate feature flag enabled):
   - Agent run resumes after process restart without repeating completed steps.
   - Durable tool-wrapped side effects are not duplicated on retry/recovery.
10. Confirm code/content boundary:
   - New non-interactive execution uses code from `strix-pydantic/` only.
   - Skills/schemas/prompts are loaded from `strix/` content path.
   - Running without `strix/` code imports still succeeds when content path is valid.
