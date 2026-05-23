# Strix Go Orchestrator Package Implementation Details (`strix-go/pkg`)

This document captures the detailed architecture, structure, and code design of the Golang rewrite of the Strix Multi-Agent Orchestrator located under [strix-go/pkg](file:///home/mfranz/github/strix-goclient/strix-go/pkg).

---

## 1. Agent Runtime (`pkg/agents`)

Located in [strix-go/pkg/agents](file:///home/mfranz/github/strix-goclient/strix-go/pkg/agents).

### [agent.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/agents/agent.go)
Declares the core agent structure and manages the autonomous execution loop.

* **Core Structs**:
  * `Agent`: Holds agent state including unique `ID`, `Name`, task description, active `Skills`, conversation `History`, LLM connection, `Sandbox` container bindings, `ExecutionContext` (sandbox vs parent), and iteration counters.
  * `RunConfig`: Configures CLI arguments (scan mode, interactivity, target contexts).
* **Key Functions**:
  * `SpawnAgent`: Initializes a new agent node, establishes LLM clients, compiles the context-aware system prompt, and starts the asynchronous execution loop.
  * `agentLoop`: The main iterative scan loop (caps at 300 cycles). Handles:
    1. Compiling system prompts.
    2. Dispatching prompts to the LLM.
    3. Parsing XML-based tool call actions.
    4. Executing local or sandbox tools.
    5. Appending responses to conversation histories.
* **Important Design Decisions**:
  * **Exponential Backoff**: Implements `llmBackoffDelay` (exponential backoff starting at 2s up to 60s) to gracefully retry on API failures, aborting after 6 consecutive errors.
  * **Tool Failure Loop Detection**: Monitors consecutive identical tool invocation failures. If an agent repeats the same error, it breaks tool execution immediately and injects a strong instruction nudge into the history to force recovery.

---

## 2. User Interface (`pkg/interface`)

Located in [strix-go/pkg/interface](file:///home/mfranz/github/strix-goclient/strix-go/pkg/interface).

### [log_handler.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/interface/log_handler.go)
Implements a thread-safe custom `slog.Handler` directing multi-layered diagnostic outputs to the CLI and log files.
* Directs standard logs to `strix.log`.
* Directs agent execution records (LLM prompts, raw outputs, backoffs) to `agent.log`.

### [tui.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/interface/tui.go)
An interactive terminal UI built using Charm's `bubbletea`, `lipgloss`, and `glamour`.
* **Pane Structure**:
  * **Left Column** (exactly 1/3 width, minimum 30 columns):
    * **AGENTS GRAPH** (50% height, minimum 3 lines): Displays agent hierarchy and active states.
    * **TODO TASKS** (50% height, minimum 2 lines): Displays scan checklists.
  * **Right Column** (remaining width):
    * **FINDINGS** (2/3 height): Renders markdown notes.
    * **LOG STREAM (TAILING)** (1/3 height): Displays tailing console logs.
* **Fixed-Height Padding Layout**: To prevent panels from collapsing upward when empty or short, all visible lines are padded using empty strings (`""`) to match their calculated dimensions, keeping dividers locked in place.
* **Scrolling & Focus Constraints**: Focus is constrained only to **FINDINGS** and **LOG STREAM**. TAB toggles focus between these two panels, and arrow scrolls only modify the active right-side panel, keeping the left panels static.

---

## 3. Language Model Integration (`pkg/llm`)

Located in [strix-go/pkg/llm](file:///home/mfranz/github/strix-goclient/strix-go/pkg/llm).

### [llm.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/llm/llm.go)
Integrates LLM connections using `langchaingo`. Supports Google AI (Gemini), OpenAI, and Azure OpenAI, with fallback configurations for deployment names and endpoints.

### [bedrock.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/llm/bedrock.go)
Implements custom client wrappers for Amazon Bedrock, resolving AWS credentials and token authentications for Anthropic Claude and Amazon Titan models.

### [prompt.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/llm/prompt.go)
Compiles Jinja system prompts using `flosch/pongo2`. Looks up skill instructions in target playbooks recursively and injects XML tool schemas based on the agent's context.

---

## 4. Sandbox Isolation Runtime (`pkg/runtime`)

Located in [strix-go/pkg/runtime](file:///home/mfranz/github/strix-goclient/strix-go/pkg/runtime).

### [docker.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/runtime/docker.go)
Orchestrates secure target container sandboxes using the Docker SDK.
* Pulls container images (`ghcr.io/usestrix/strix-sandbox`).
* Generates secure authentication tokens for tool servers.
* Allocates random host ports for tool execution APIs and Caido proxies.
* Configures sandbox capabilities (`NET_ADMIN`, `NET_RAW`) and DNS.
* Implements directory copying (`copyDirToContainer`) by converting local files into tar streams and unpacking them inside `/workspace` in the container.

### [client.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/runtime/client.go)
HTTP client proxying tools execution requests (`/execute`) to the sandbox. Incorporates authorization bearer tokens and maps execution responses.

---

## 5. Context-Aware Tool Registry (`pkg/tools`)

Located in [strix-go/pkg/tools](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools).

### [registry.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/registry.go)
Manages the validation and compilation of system schemas.
* **Execution Context Isolation**: Exposes two contexts:
  * `ExecutionContextParent`: Host machine. Can access coordinates (`create_agent`, `load_skill`, `todo`, `notes`, `reporting`, `finish`).
  * `ExecutionContextSandbox`: Target container. Can only access security commands (`terminal_execute`, `python_action`, `browser_action`, file editors).
* Dynamically filters tool prompts (`GetToolsPromptForContext`) so sandbox agents never see schemas for tools they lack permission to use, preventing LLM reasoning stalls.

### Standard Tool Implementations:
* **[agents_graph/graph.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/agents_graph/graph.go)**: 
  * Spawns sub-agents concurrently in background goroutines. Collects updates asynchronously using message passings through inbox channels. Thread-safe using `sync.RWMutex`.
  * **Agent-Level Review Mode**: When `ReviewAgents` is active, intercepts sub-agent creation (`CreateAgent`) and completion (`AgentFinish`) to prompt the operator for approval, rejection with feedback (rework), or instruction revisions. Reads from stdin and serializes prompts across goroutines via a global `StepReviewMutex`.
* **[notes/notes.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/notes/notes.go)**: Manages markdown notes for findings.
* **[todo/todo.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/todo/todo.go)**: Implements task checklist operations.
* **[reporting/reporting.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/reporting/reporting.go)**: Formats vulnerabilities into reports.
* **[thinking/thinking.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/thinking/thinking.go)**: Allows logging intermediate reasoning steps.
* **[finish/finish.go](file:///home/mfranz/github/strix-goclient/strix-go/pkg/tools/finish/finish.go)**: Validates target tasks and terminates execution cycles.

