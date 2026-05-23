# Strix-Go LLM Integration Analysis

## Current Architecture

### Components
1. **LLMClient** (`pkg/llm/llm.go`): Wraps LangChainGo models (Gemini/OpenAI)
2. **Prompt Compilation** (`pkg/llm/prompt.go`): Jinja2 template rendering with skills and tools
3. **Tool Parsing** (`pkg/llm/utils.go`): XML tool invocation extraction
4. **Tool Registry** (`pkg/tools/registry.go`): Regex-based XML schema parsing

### LLM Integration Flow
```
Agent.Run() 
  → CompileSystemPrompt() [uses Jinja2 templates + skill loading]
  → LLMClient.GenerateChatCompletion()
  → ParseToolInvocations() [regex extraction of <function=> blocks]
  → Tool execution & observation loops
```

---

## Issues Identified

### 0. **CASCADING FAILURE: Schema Load → Empty Tool Registry → Blind LLM → Agent Failure**

This is the most critical finding. When schemas fail to load:

1. **Schema parsing fails** (regex can't handle nested XML)
2. **toolRegistry remains empty/partial** 
3. **System prompt has empty `<tools>` section** (no tool definitions)
4. **LLM doesn't know what tools exist**
5. **LLM invents tool names that don't exist**
6. **Tool execution fails with "not registered"**
7. **Agent burns iterations trying fake tools**
8. **Agent enters wait state and becomes stuck**

**Evidence from production logs** (run_192-168-12-178_1779538701):

```
time=08:18:21.018 level=WARN msg="Failed to load schema file" 
  path=/strix/tools/agents_graph/agents_graph_actions_schema.xml
  error="XML syntax error on line 42"
time=08:18:21.016 level=WARN msg="Failed to load schema file" 
  path=/strix/tools/browser/browser_actions_schema.xml
time=08:18:21.017 level=WARN msg="Failed to load schema file" 
  path=/strix/tools/finish/finish_actions_schema.xml
[12 more failures...]

time=08:25:13.091 level=DEBUG msg="Requesting LLM completion" iteration=1
time=08:25:14.123 completion="<function=think>..."
time=08:25:14.124 level=WARN msg="Tool not registered in the system" tool_name=think
  ← LLM invented this tool, it doesn't exist

time=08:25:14.879 completion="<function=terminal_execute>..."
time=08:25:14.879 level=WARN msg="Tool not registered in the system" tool_name=terminal_execute
  ← LLM invented this tool too

time=08:25:15.673 completion="<function=list_tools>..."
time=08:25:15.673 level=WARN msg="Tool not registered in the system" tool_name=list_tools

time=08:25:16.340 completion="<function=wait_for_message>..."
time=08:25:16.340 level=INFO msg="Agent entering wait state for incoming message"
  ← Agent has given up
```

**All 13 production tool schemas fail to load simultaneously**, leaving agents completely blind.

---

### 1. **Tool XML Schema Parsing is Regex-Based (BRITTLE)**

**Location:** `pkg/tools/registry.go:77-149` (LoadSchema function)

**Problem:**
- Uses regex instead of proper XML parsing
- Cannot handle nested structures: `<parameter>` tags with child `<description>` elements
- Fails on special characters that need escaping (e.g., unescaped `&`)
- The regex `(?s)<parameter\s+([^>]+)>` only extracts attributes from opening tags, losing any nested content

**Evidence from Logs:**
```
Failed to load schema file: agents_graph/agents_graph_actions_schema.xml
  error="XML syntax error on line 42: expected attribute name in element"
Failed to load schema file: terminal/terminal_actions_schema.xml  
  error="XML syntax error on line 28: invalid character entity & (no semicolon)"
```

**Impact:**
- Tools fail to register in the registry
- Agents don't know about tools because schemas can't be loaded
- LLM tries to call tools that don't exist → "Tool not registered" errors
- Cascading failures in multi-agent coordination (agents_graph tools unavailable)

**Root Cause Hypothesis:**
The schema files likely contain:
1. Nested `<description>` elements instead of attributes
2. Unescaped special characters in descriptions
3. Complex formatting that the regex doesn't handle

---

### 2. **Tool Parameter Type Normalization Issues**

**Location:** `pkg/tools/registry.go:159-220` (NormalizeArguments function)

**Problem:**
- Parameter descriptions are lost during regex parsing → `xmlTool.Parameters` may be incomplete
- Type coercion logic hardcodes specific parameter names (`tags`, `todo_ids`, `findings`, etc.)
- If new parameters are added to schemas and types change, the hardcoding breaks silently

**Example Issue:**
```go
switch name {
case "tags", "todo_ids", "findings", "final_recommendations":
    // These are hardcoded - if a schema uses a different parameter name, 
    // it won't get JSON/array coercion
}
```

**Impact:**
- Tool arguments may not be converted to the correct types
- String values that should be arrays/JSON objects stay as strings
- Tools fail or behave unexpectedly with type mismatches

---

### 3. **LLM Model Fallback Logic is Unclear**

**Location:** `pkg/llm/llm.go:25-113` (NewLLMClient function)

**Problem:**
- Default to `gemini-3.1-flash-lite` (line 28) without checking if credentials exist
- No explicit fallback on credential failure
- Multiple env var checks (GEMINI_API_KEY, GOOGLE_API_KEY, LLM_API_KEY) but order matters
- Returns errors without clarity on which auth method was attempted

**Current Flow:**
```
STRIX_LLM env var → (defaults to gemini-3.1-flash-lite)
                  → check for various API keys
                  → if Gemini: use googleai client
                  → else: fallback to OpenAI
```

**Issue:**
- If Gemini is set but API key is missing, the error message may be unhelpful
- No logging of which model/auth method is being attempted (only DEBUG level)
- Fallback silently uses OpenAI without clear indication

**Impact:**
- Confusing errors when credentials are misconfigured
- Operators don't know which LLM provider is actually being used
- Hard to troubleshoot in production

---

### 4. **System Prompt Template Dependencies are Fragile**

**Location:** `pkg/llm/prompt.go:107-139` (CompileSystemPrompt function)

**Problem:**
- Hardcoded path to `agents/[agentName]/system_prompt.jinja` (line 114)
- Assumes jinja templates exist and are valid
- If template doesn't exist, error is thrown but agent doesn't gracefully degrade
- Skill loading uses recursive search with `SkipAll` on first match (line 59)
  - If multiple skills have same name, only first one found is used
  - Ordering is filesystem-dependent (non-deterministic)

**Example Failure:**
```
CompileSystemPrompt("StrixAgent", ...) 
  → looks for: strix/agents/StrixAgent/system_prompt.jinja
  → if missing: error, agent fails to start
```

**Impact:**
- Missing template = agent won't start (no fallback)
- Skill loading is non-deterministic (alphabetical order varies by filesystem)
- Cross-system reproducibility issues

---

### 5. **Tool Invocation Parsing Doesn't Validate Format**

**Location:** `pkg/llm/utils.go:32-66` (ParseToolInvocations function)

**Problem:**
- The NormalizeToolFormat function attempts to handle multiple format variations (MiniMax, OpenAI, etc.)
- But if LLM returns malformed XML, regex will silently extract nothing
- No validation that extracted tool calls match expected schema
- Empty parameter values are not validated:
  ```go
  kwargs[paramName] = paramVal  // even if paramVal is ""
  ```

**Issue:**
If LLM returns:
```xml
<function=my_tool>
<parameter=name>
<parameter=value>
</function>
```

The parser extracts parameter names and values but doesn't validate:
- Required parameters are provided
- Parameter types match schema
- Parameter values are non-empty (for required params)

**Impact:**
- Tool calls with missing required parameters are passed to handlers
- Handlers crash or misbehave instead of failing early
- LLM may try same invalid call repeatedly

---

### 6. **Chat History Management Has No Pruning**

**Location:** `pkg/agents/agent.go:156-222`

**Problem:**
- Agent.History grows unbounded: `history = append(history, msg)` (lines 220, 228, 252, 302)
- No context length management or pruning strategy
- For long-running agents (max 300 iterations), history can become huge
- Each LLM call sends full history to model (line 115-131 in llm.go)

**Impact:**
- Token usage grows quadratically (O(n²) total tokens for n iterations)
- LLM calls get slower and more expensive over time
- Eventually hits model context window limits
- No graceful degradation

**Example:**
```
Iteration 1: ~500 tokens in history
Iteration 2: ~1000 tokens (history grows)
Iteration 100: ~50,000 tokens per request
Iteration 300: model context exceeded error
```

---

### 7. **No Validation That Tools Were Successfully Loaded**

**Location:** `pkg/llm/prompt.go:107-139` (CompileSystemPrompt) + `pkg/agents/agent.go:190-194`

**Problem:**
- System prompt compilation doesn't validate that `get_tools_prompt()` returned tools
- If all schemas fail to parse, the system prompt is compiled with empty `<tools>` section
- Agent starts iteration loop with zero visibility into available tools
- No warning or error that tools are missing

**Current code flow:**
```go
systemPrompt, err := llm.CompileSystemPrompt("StrixAgent", a.Skills, ...)
if err != nil {  // Only checks for template compilation errors
    return err
}
// If tools are missing, we get here with a valid systemPrompt that has NO tools
completion, err := a.LLM.GenerateChatCompletion(ctx, systemPrompt, a.History)
```

**What should happen:**
```go
systemPrompt, err := llm.CompileSystemPrompt(...)
if err != nil {
    return err
}
if !strings.Contains(systemPrompt, "<tools>") || strings.TrimSpace(getToolsSection(systemPrompt)) == "" {
    return fmt.Errorf("no tools available - schema loading failed completely")
}
```

**Impact:**
- Agents start blind and discover it only by trying (and failing) tool calls
- Cascades to the "invented tool names" problem above
- No early warning

---

### 8. **LLM Request Error Handling is Insufficient**

**Location:** `pkg/agents/agent.go:196-211`

**Problem:**
- Single 5-second sleep on LLM error: `time.Sleep(5 * time.Second)` (line 209)
- No exponential backoff
- No distinction between:
  - Transient errors (network, rate limiting)
  - Permanent errors (invalid API key, model not found)
- Continues looping on permanent errors forever

**Current Behavior:**
```go
completion, err := a.LLM.GenerateChatCompletion(ctx, systemPrompt, a.History)
if err != nil {
    slog.Error("LLM completion request failed", ...) // Just logs
    time.Sleep(5 * time.Second)                       // Sleeps 5s
    continue                                          // Retries forever
}
```

**Impact:**
- Rate-limited agents spin in tight 5-second loops wasting tokens
- Permanent auth failures (bad API key) never recover
- No distinction in behavior between recoverable/unrecoverable errors

---

### 8. **System Prompt Context Lacks Validation**

**Location:** `pkg/agents/agent.go:74-110` (SpawnAgent function)

**Problem:**
- `systemPromptContext` (map[string]interface{}) is passed unsanitized to LLM prompts
- No schema validation of context values
- In `main.go:226`, context is built from CLI flags without sanitization:
  ```go
  systemPromptContext := buildSystemPromptContext()
  ```
- If context contains special characters or invalid types, LLM prompt becomes malformed

**Impact:**
- Attackers could inject prompt instructions via `--instruction` flag
- LLM prompt structure corruption
- Agents behave unpredictably

---

## Recommendations (by priority)

### 🔴 BLOCKING ISSUES (MUST FIX IMMEDIATELY)

1. **Replace Regex XML Parsing with Proper XML Parser** ← **UNBLOCKS THE ENTIRE SYSTEM**
   - Use Go's `encoding/xml` package to properly parse schemas
   - Handle nested `<description>` elements
   - Validate XML before parsing with clear error messages
   - Add proper error messages with line numbers
   - **Impact:** Currently all 13 tool schemas fail, leaving agents blind. Fixing this is prerequisite for any agent functionality.

2. **Add Tool Visibility Validation in Agent Startup**
   - Before agent starts iteration loop, verify `systemPrompt` contains `<tools>` block
   - Count registered tools and fail early if 0 tools found
   - Log which tools are available as INFO level messages
   - **Impact:** Prevents agents from starting in broken state; gives fast feedback

### HIGH PRIORITY

3. **Add Context Window/History Management**
   - Implement sliding window (keep last N tokens)
   - Prune oldest messages when approaching limits
   - Add metrics for token usage per iteration
   - **Impact:** Prevents agent failure after ~100 iterations

4. **Improve LLM Error Handling**
   - Distinguish between transient and permanent errors
   - Implement exponential backoff
   - Add max retry limits
   - Log which provider/model is being used at INFO level
   - **Impact:** Prevents permanent errors spinning in tight loops

### MEDIUM PRIORITY

5. **Validate Tool Invocations Before Execution**
   - Check required parameters are present
   - Validate parameter types against schema
   - Return clear error to LLM if validation fails

6. **Add Deterministic Skill Loading**
   - Use consistent ordering (alphabetical, explicit paths)
   - Document which skill is loaded if duplicates exist
   - Add logging of loaded skill path

7. **Implement Config Validation**
   - Validate `systemPromptContext` structure
   - Validate LLM model string format
   - Fail fast on invalid config

### NICE TO HAVE

8. **Add LLM Request Logging**
   - Log full system prompt (or hash for large ones)
   - Log model, tokens, latency per request
   - Add request/response caching for debugging

9. **Add Metrics Collection**
   - Tool call success/failure rates
   - Token usage tracking
   - Error rates by type

---

## Test Gaps

Current tests exist for:
- `pkg/llm/utils_test.go`: Tool parsing

Missing tests for:
- Tool schema loading (esp. edge cases)
- System prompt compilation with missing templates
- LLM client initialization with invalid credentials
- Tool parameter type normalization with various input types
- Chat history management over 100+ iterations

---

## Severity Assessment

| Issue | Severity | Impact |
|-------|----------|--------|
| **Cascading failure: Schema load → tool registry → blind LLM** | **CRITICAL** | Complete system failure; agents cannot operate |
| **Regex XML parsing** | **CRITICAL** | 13/14 tool schemas fail; toolRegistry empty |
| **No tool visibility validation** | **CRITICAL** | Agents start blind; LLM invents fake tools |
| History unbounded growth | **HIGH** | Agent slowdown/failure after ~100 iterations |
| LLM error handling | **HIGH** | Permanent errors spin forever; rate limiting |
| Parameter validation | **MEDIUM** | Silent tool failures |
| Skill loading non-determinism | **MEDIUM** | Hard to reproduce issues |
| System prompt injection | **MEDIUM** | Security issue with CLI input |
| Model selection unclear | **LOW** | Confusing troubleshooting |

