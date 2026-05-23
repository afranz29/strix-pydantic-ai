# LLM Integration Issues - Quick Reference

## 🔴 CRITICAL: Regex-Based XML Parsing (System-Blocking)

**File:** `pkg/tools/registry.go:77-149`

The tool schema loading uses regex instead of proper XML parsing. **13 of 14 tool schemas fail to load**, causing:
- **All agent-accessible tools become unavailable**
- LLM doesn't know what tools exist
- LLM starts hallucinating tool names that don't exist

**Real-world impact from logs** (run_192-168-12-178_1779538701):
```
Iteration 1: LLM tries <function=think> → "Tool not registered"
Iteration 2: LLM tries <function=terminal_execute> → "Tool not registered"  
Iteration 3: LLM tries <function=list_tools> → "Tool not registered"
Iteration 4: LLM tries <function=wait_for_message> → Agent enters wait state
```

The agent quickly exhausts its tool vocabulary and becomes stuck.

**Root Cause:** 13 concurrent schema load failures:
```
Failed to load: agents_graph_actions_schema.xml (line 42)
Failed to load: browser_actions_schema.xml (line 103)
Failed to load: file_edit_actions_schema.xml (line 21)
Failed to load: finish_actions_schema.xml (line 71)
Failed to load: load_skill_actions_schema.xml (line 20)
Failed to load: notes_actions_schema.xml (line 27)
Failed to load: proxy_actions_schema.xml (line 47)
Failed to load: python_actions_schema.xml (line 73)
Failed to load: reporting_actions_schema.xml (line 187)
Failed to load: terminal_actions_schema.xml (line 28 - entity encoding)
Failed to load: thinking_actions_schema.xml (line 21)
Failed to load: todo_actions_schema.xml (line 46)
Failed to load: web_search_actions_schema.xml (line 28)
```

**Fix:** Replace regex parsing with `encoding/xml` package + XML validation.

---

## 🔴 CRITICAL: LLM Has No Tool Visibility

**Location:** `pkg/llm/prompt.go:127-129` (CompileSystemPrompt function)

The system prompt is compiled with `get_tools_prompt()` which injects tool schemas into the LLM's instructions. But:

**When schema loading fails:**
- `toolRegistry` is empty or partial
- `GetToolsPrompt()` returns empty or incomplete `<tools>` block
- LLM has **no knowledge of what tools exist**
- LLM starts inventing/hallucinating tool names

**Evidence from logs:**
```
08:25:14: Tries <function=think>              ← hallucinated, not registered
08:25:14: Tries <function=terminal_execute>   ← hallucinated, not registered
08:25:15: Tries <function=list_tools>         ← hallucinated, not registered
08:25:16: Tries <function=wait_for_message>   ← fallback tool, agent gives up
```

**The cascade:**
```
Schema parse fails (13/14 tools) 
  → toolRegistry empty
  → system prompt has empty <tools> section
  → LLM doesn't know what tools exist
  → LLM guesses tool names
  → All guesses fail
  → Agent exhausts iterations and enters wait state
```

**Fix:** Fix schema parsing (above) + add validation that system prompt contains expected tools.

---

## 🔴 HIGH: Unbounded Chat History Growth

**File:** `pkg/agents/agent.go:156-222`

Agent conversation history grows without limit. After ~100 iterations:
- Each LLM request sends entire history (quadratic token growth)
- Agents become slow and expensive
- Eventually exceed context window and crash

**Current code:**
```go
a.History = append(a.History, llm.Message{...})  // No pruning
```

**Fix:** Implement sliding window history pruning (~80k token limit).

---

## 🔴 HIGH: Poor LLM Error Handling  

**File:** `pkg/agents/agent.go:196-211`

When LLM requests fail:
- 5-second sleep, then retry forever
- No exponential backoff
- No distinction between transient vs permanent errors
- Rate-limited agents spin in tight loops

**Fix:** 
- Distinguish error types
- Implement exponential backoff with max retries
- Fail-fast on permanent errors

---

## 🟡 MEDIUM: No Tool Parameter Validation

**File:** `pkg/agents/agent.go:236-277`

Tool calls are dispatched without validating:
- Required parameters are present
- Types match schema expectations
- Values are non-empty

Invalid calls silently fail or crash handlers.

**Fix:** Add validation before tool execution, return schema-aware errors to LLM.

---

## 🟡 MEDIUM: Non-Deterministic Skill Loading

**File:** `pkg/llm/prompt.go:42-71`

Recursive skill search uses `filepath.SkipAll` on first match:
- If multiple skills have same name, loading order is filesystem-dependent
- Makes results non-reproducible across systems

**Fix:** Use explicit skill paths or enforce consistent ordering.

---

## 🟡 MEDIUM: System Prompt Injection Vulnerability

**File:** `pkg/agents/agent.go:74-110` + `main.go:226`

`systemPromptContext` is passed unsanitized to LLM prompts:
- User can inject prompt instructions via `--instruction` flag
- No schema validation of context values
- Malformed context corrupts LLM prompt structure

**Fix:** Validate and sanitize `systemPromptContext` structure.

---

## 🟢 LOW: Unclear Model Selection

**File:** `pkg/llm/llm.go:25-113`

Model initialization logic:
- Defaults to `gemini-3.1-flash-lite` silently
- Multiple env var checks in undocumented order
- Falls back to OpenAI without clear logging
- Confusing error messages on auth failure

**Fix:** Add INFO-level logging of provider/model selection.

---

## Cascading Failure Diagram

```
┌─────────────────────────────────────────────────┐
│  13 Tool Schema Files to Load                   │
│  (agents_graph, browser, file_edit, finish,     │
│   load_skill, notes, proxy, python, reporting,  │
│   terminal, thinking, todo, web_search)         │
└──────────────────┬──────────────────────────────┘
                   │
                   ↓ Regex XML parser can't handle nested <description> tags
                   │
┌──────────────────────────────────────────────────┐
│ ✗ All 13 schemas fail to parse                   │
│ toolRegistry remains empty                       │
└──────────────────┬──────────────────────────────┘
                   │
                   ↓ GetToolsPrompt() returns empty <tools> section
                   │
┌──────────────────────────────────────────────────┐
│ ✗ System prompt lacks tool definitions            │
│ LLM doesn't know what tools exist                │
└──────────────────┬──────────────────────────────┘
                   │
      ┌────────────┴────────────┬──────────────────┐
      ↓                         ↓                  ↓
   Iter 1:             Iter 2:             Iter 3:
   Tries <think>      Tries <terminal>    Tries <list_tools>
   ✗ Not registered   ✗ Not registered    ✗ Not registered
      │                  │                  │
      └────────────┬─────┴──────────────────┘
                   ↓
┌──────────────────────────────────────────────────┐
│ ✗ Agent exhausts vocabulary of fake tools        │
│ Gives up and enters wait state                   │
│ Agent becomes stuck, waiting for messages        │
└──────────────────────────────────────────────────┘
```

## Impact Chain

All aspects of the system fail because agents cannot operate:
- Penetration testing = cannot run
- Multi-agent coordination = cannot run  
- Tool execution = cannot run
- Agent graph building = cannot run

---

## 🚨 THIS IS A BLOCKING SYSTEM FAILURE

**Current Status:** Agents cannot operate because:
1. Tool schemas fail to parse
2. Tool registry is empty
3. LLM has no tools to use
4. LLM invents fake tool names
5. Agent exhausts iterations and enters wait state

**To unblock the system, MUST fix in this order:**

### Phase 1: Restore Agent Functionality (2-3 hours)
1. **Replace regex XML parsing** (90 min)
   - Switch from regex to `encoding/xml` + validation
   - Fix all 13 failing schemas
   
2. **Add tool visibility validation** (30 min)
   - Validate system prompt contains tools before agent starts
   - Fail fast with clear error message

### Phase 2: Stabilize Agent Execution (2 hours, can be parallel)
3. **Fix LLM error handling** (45 min)
   - Add exponential backoff
   - Distinguish transient vs permanent errors
   
4. **Add history pruning** (1.5 hours)
   - Implement sliding window before context limits hit

### Phase 3: Improve Robustness (1-2 hours, optional for MVP)
5. **Add parameter validation** (30 min)
6. **Add deterministic skill loading** (30 min)
7. **Config validation** (30 min)

---

## Testing Strategy

Add tests for:
- Schema loading with nested `<description>` elements ✓
- Tool parameter validation edge cases ✓
- LLM history growth over 150+ iterations ✓
- Error classification (transient vs permanent) ✓
- System prompt context validation ✓

See full analysis in: `LLM_INTEGRATION_ANALYSIS.md`
