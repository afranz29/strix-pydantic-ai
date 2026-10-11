# Strix Pydantic AI Port - Implementation Status

**Date:** 2026-05-24  
**Branch:** pydantic-v2  
**Target:** pydantic-ai v1.102.0 (stable)

## Summary

Successfully ported Strix's non-interactive orchestrator to **pydantic-ai v1.102.0** with **Pydantic Graph**. The framework is structurally complete and end-to-end functional for orchestration—ready for tool integration.

## What's Working ✅

### Core Framework
- **Model Resolution** — Anthropic (claude-haiku-4-5), OpenAI, Gemini fallback supported
- **Agent Creation** — 3 agent roles (reconnaissance, exploitation, post_exploitation) with skill-derived instructions
- **Graph Orchestrator** — Unified orchestration step handling full scan workflow
- **Logging** — Writes to current directory (e.g., `strix_<uuid>.log`)
- **CLI** — Non-interactive scanner with Go-style output parity

### v1.102.0 Specific
- String-based model format: `"anthropic:claude-haiku-4-5"`
- `toolsets` parameter (not `tools`) for Agent tool registration
- Single-step orchestrator avoids complex multi-node routing in v1 GraphBuilder

### Supporting Infrastructure
- `SandboxClient` — Docker tool execution interface (not yet wired)
- `ToolRegistry` — Context-aware tool filtering (sandbox/parent)
- `SkillCapabilityFactory` — Loads skills from `strix/skills/` markdown
- `content_paths.py` — Resolves shared content via `STRIX_CONTENT_DIR` or sibling `strix/`

## What's Incomplete ❌

### Tool Execution
- **SandboxClient integration** — Not yet wired to orchestrator
- **Tool calls routing** — Agents generate responses but don't execute tools
- **Docker backend** — Requires running sandbox server for actual execution

### Testing & Validation
- Unit tests (pending task #10)
- Integration tests with mock tools
- End-to-end scan with real tool execution

## File Structure

```
strix-pydantic/
├── pyproject.toml                          # v1.102.0 dependencies
├── strix_pydantic/
│   ├── __main__.py                         # Entry point
│   ├── config/
│   │   ├── model_config.py                 # Go-parity model resolution
│   │   └── content_paths.py                # Content directory resolver
│   ├── agents/
│   │   ├── types.py                        # StrixRunState, StrixDeps, RunConfig
│   │   └── pydantic_orchestrator.py        # Graph orchestrator (single step)
│   ├── interface/
│   │   └── cli.py                          # Non-interactive CLI
│   ├── tools/
│   │   └── tool_registry.py                # Context-aware tool lookup
│   ├── skills/
│   │   └── skill_capability_factory.py     # Skill → SkillBuild builder
│   └── runtime/
│       └── sandbox_client.py               # Docker sandbox client interface
└── tests/                                  # (pending)
```

## How to Run

```bash
# Scan with Anthropic API key set
ANTHROPIC_API_KEY=<key> python -m strix_pydantic.interface.cli \
  --target http://192.168.12.187:3000 \
  --scan-mode quick

# Logs appear in current directory (e.g., ./strix_abc123.log)
```

## Key Decisions

1. **v1.102.0 over v2.0.0b1** — Stable APIs, full provider coverage (Gemini fallback works)
2. **Single orchestrator step** — Avoids v1 GraphBuilder's complex multi-node routing
3. **Unified workflow** — Bootstrap → Agent loop → Tool dispatch → Finalize in one step
4. **Logs to cwd** — User preference for visibility during development

## Next Steps

### Phase 1: Tool Integration (High Priority)
1. Fix `AgentRunResult` API compatibility (check for correct tool_calls attribute)
2. Wire `SandboxClient` into orchestrator's tool dispatch phase
3. Implement mock tool execution for testing
4. Add unit tests (task #10)

### Phase 2: Feature Parity
1. Implement skill-aware tool policies (FilteredToolset enforcement)
2. Add message history continuity across agent turns
3. Implement vulnerability/findings collection
4. Support multiple agent roles with round-robin or sequential execution

### Phase 3: Optional
1. Multi-node orchestrator with explicit routing (post v1 GraphBuilder mastery)
2. Restate integration for durable execution (task #7, deferred)
3. Interactive/TUI mode (currently skipped)

## References

- [pydantic-ai v1.102.0](https://github.com/pydantic/pydantic-ai/releases/tag/v1.102.0)
- [Pydantic Graph GraphBuilder API](https://pydantic.dev/docs/ai/api/pydantic_graph/beta_graph_builder/)
- [PYDANTIC_V1_PIVOT.md](./PYDANTIC_V1_PIVOT.md) — Pivot decision details
- [pydantic_ai_port.md](./pydantic_ai_port.md) — Original implementation plan

## Known Issues

- `AgentRunResult.tool_calls` attribute name differs from v2; needs investigation
- Graph validation suppressed (validate_graph_structure=False) due to single-step design
- No actual tool execution without Docker sandbox backend

---

**Status:** Framework complete, ready for tool integration and testing.
