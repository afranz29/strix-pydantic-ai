# Pivot to pydantic-ai v1.102.0

## Rationale
- **v2.0.0b1**: Beta APIs, only Anthropic + OpenAI providers, Gemini unsupported
- **v1.102.0**: Stable APIs, all providers supported (Anthropic, OpenAI, Gemini, Bedrock, etc.), simpler model string format

## Changes Made

### 1. Dependencies (pyproject.toml)
- ✅ `pydantic-ai==2.0.0b1` → `pydantic-ai==1.102.0`
- ✅ `pydantic-graph==2.0.0b1` → `pydantic-graph>=1.0`

### 2. Model Configuration (model_config.py)
- ✅ Switched from provider class instantiation to string-based format
- ✅ Returns `(model_spec: str, display_name: str)` tuple
- ✅ Model spec format: `"provider:model"` (e.g., `"anthropic:claude-haiku-4-5"`)
- ✅ Supports Go-parity fallback:
  1. `STRIX_LLM` override
  2. `ANTHROPIC_API_KEY` set → `"anthropic:claude-haiku-4-5"`
  3. Fallback → `"gemini:gemini-2.0-flash"` (now works!)
- ✅ Added `normalize_model_spec()` for flexible input handling

### 3. CLI (cli.py)
- ✅ Updated to use new model_config return type
- ✅ Passes model_spec string directly to Agent()
- ✅ Updated _build_agents() signature

### 4. Orchestrator (pydantic_orchestrator.py)
- ⚠️ **Status: Verify compatibility**
  - v1 supports BaseNode approach we're using
  - StepContext API should be similar to our GraphRunContext
  - Return type annotations determine edges (no explicit edge declarations)

## Remaining Work

1. **Verify GraphBuilder API** in pydantic_orchestrator.py:
   - Check StepContext imports and signature
   - Verify BaseNode usage works in v1
   - Test explicit edge declarations (may need adjustment)

2. **Test model resolution**:
   - Verify `Agent("anthropic:claude-haiku-4-5")` works
   - Verify `Agent("gemini:gemini-2.0-flash")` works
   - Verify `Agent("openai:gpt-4o")` works

3. **Integration testing**:
   - Full end-to-end scan with v1.102.0 APIs
   - Tool registration and FilteredToolset compatibility
   - Agent.run() message history handling

## References

- [pydantic-ai v1.102.0 GraphBuilder API](https://pydantic.dev/docs/ai/api/pydantic_graph/beta_graph_builder/)
- [StepContext/BaseNode](https://ai.pydantic.dev/api/pydantic_graph/beta_step/)
