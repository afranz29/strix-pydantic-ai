# Tool Context Fix: Summary of Changes

## Problem Fixed

**Tool Availability Mismatch:** Agents in sandbox environments were receiving tool schemas for tools they couldn't use (e.g., `load_skill`, `web_search`, `create_todo`), causing cascading failures.

### Root Cause
- Agents received ALL tool schemas regardless of execution context
- No validation that tools were available before agents tried to use them
- Failures occurred at execution time, not prompt time
- Models gave up when they couldn't access expected tools

### Impact from Last Run
- **Recon Agent**: Tried `load_skill` → failed gracefully with terminal fallback → recovered ✓
- **Gitea Discovery Agent**: Tried security tools → failed → model gave up with safety refusal → failed ✗
- **Root Agent**: Tried `web_search` → failed → degraded to notes instead of research → failed partially ✗

---

## Solution Implemented

### 1. Execution Context Types (`strix-go/pkg/tools/registry.go`)

Added `ExecutionContext` enum:
```go
type ExecutionContext string

const (
    ExecutionContextSandbox ExecutionContext = "sandbox"
    ExecutionContextParent  ExecutionContext = "parent"
)
```

This allows the system to understand where each tool can run:
- **Sandbox tools** (sandbox_execution=true): terminal, python, files, proxy, browser
- **Parent tools** (sandbox_execution=false): load_skill, web_search, todos, notes, agents, think, finish, reporting

### 2. Context-Aware Tool Availability (`strix-go/pkg/tools/registry.go`)

Added methods to `ToolDefinition`:
```go
// IsAvailableInContext returns true if this tool can be executed in the given context
func (td ToolDefinition) IsAvailableInContext(ctx ExecutionContext) bool
```

New registry functions:
```go
// GetToolsPromptForContext returns tool schemas filtered by execution context
func GetToolsPromptForContext(ctx ExecutionContext) string

// GetAvailableTools returns a list of tool names available in the given context
func GetAvailableTools(ctx ExecutionContext) []string

// ValidateToolCallInContext checks if a tool can be executed in the given context
func ValidateToolCallInContext(toolName string, ctx ExecutionContext) error
```

### 3. Context-Aware System Prompt Generation (`strix-go/pkg/llm/prompt.go`)

Updated `CompileSystemPrompt`:
```go
// New context-aware version
func CompileSystemPromptWithContext(
    agentName string,
    skillNames []string,
    scanMode string,
    interactive bool,
    systemPromptContext map[string]interface{},
    executionContext tools.ExecutionContext,
) (string, error)
```

Now generates schemas only for available tools:
```go
"get_tools_prompt": func() string {
    return tools.GetToolsPromptForContext(executionContext)
}
```

### 4. Agent Context Tracking (`strix-go/pkg/agents/agent.go`)

Added fields to `Agent` struct:
```go
type Agent struct {
    // ... existing fields ...
    ExecutionContext tools.ExecutionContext
    AvailableTools   []string
}
```

Agent now knows:
- Its execution context (sandbox vs parent)
- Which tools it can actually use
- Gets system prompt with only available tools

### 5. Pre-Execution Validation (`strix-go/pkg/agents/agent.go`)

Added validation before tool execution:
```go
// Validate tool is available in this execution context
if err := tools.ValidateToolCallInContext(call.ToolName, a.ExecutionContext); err != nil {
    // Clear error message telling agent why tool failed
    obs := fmt.Sprintf(
        "<observation>\nError: %s\n\nAvailable tools in %s context: %v\n</observation>",
        err.Error(),
        a.ExecutionContext,
        a.AvailableTools,
    )
    a.History = append(a.History, llm.Message{Role: "user", Content: obs})
    continue
}
```

### 6. Comprehensive Tests (`strix-go/pkg/tools/registry_test.go`)

Added test coverage for:
- `TestToolDefinitionIsAvailableInContext` - 4 test cases
- `TestGetToolsPromptForContext` - Validates context filtering
- `TestGetAvailableTools` - Validates tool list filtering
- `TestValidateToolCallInContext` - Validates 6 scenarios including error cases

All tests pass ✓

---

## Behavior Changes

### Before (Python Implementation)
```
Agent in Sandbox
├─ Receives: all ~30 tools in schema
├─ Tries: load_skill
├─ Error: "Tool 'load_skill' not found"
├─ Fallback: tries alternative
├─ Outcome: Depends on fallback availability
└─ Model may give up due to frustration
```

### After (Go Port with Fix)
```
Agent in Sandbox
├─ Receives: only ~10 sandbox tools in schema
├─ Tries: load_skill
├─ Error: "tool 'load_skill' not available in sandbox context (available: terminal, python, ...)"
├─ Clear guidance: knows exactly what's available
├─ Fallback: uses alternative from available tools
└─ Model doesn't give up - has clear options
```

---

## Files Modified

1. **strix-go/pkg/tools/registry.go**
   - Added `ExecutionContext` enum
   - Added `IsAvailableInContext()` method to ToolDefinition
   - Added `GetToolsPromptForContext()`
   - Added `GetAvailableTools()`
   - Added `ValidateToolCallInContext()`
   - Kept backward compatibility: `GetToolsPrompt()` still works (calls parent context version)

2. **strix-go/pkg/llm/prompt.go**
   - Added `CompileSystemPromptWithContext()`
   - Updated context passed to templates
   - Kept `CompileSystemPrompt()` for backward compatibility

3. **strix-go/pkg/agents/agent.go**
   - Added `ExecutionContext` field to Agent
   - Added `AvailableTools` field to Agent
   - Updated agent initialization to set context based on sandbox availability
   - Updated system prompt generation to use context-aware version
   - Added pre-execution validation of tool calls
   - Improved error messages for unavailable tools

4. **strix-go/pkg/tools/registry_test.go**
   - Added `TestToolDefinitionIsAvailableInContext()`
   - Added `TestGetToolsPromptForContext()`
   - Added `TestGetAvailableTools()`
   - Added `TestValidateToolCallInContext()`

---

## Backward Compatibility

✅ **Fully Backward Compatible**
- `GetToolsPrompt()` still works (returns parent context by default)
- `CompileSystemPrompt()` still works (uses parent context by default)
- Existing code continues to work as-is
- New context-aware functions are opt-in

---

## Expected Outcomes

### Gitea Discovery Agent (Previously Failed)
**Before:**
- Tried to use unavailable web security tools
- Model gave up with "safety constraints" refusal
- Task failed

**After:**
- Receives only ~10 available tools in schema
- Sees `terminal_execute` and `python_action` available
- Can fall back to Python scripts for analysis
- Task has better chance of completion

### Recon Agent (Previously Recovered)
**Before:**
- Tried `load_skill` → got cryptic "not found" error
- Used terminal as fallback
- Completed with workaround

**After:**
- Never receives `load_skill` in schema
- Never tries unavailable tools
- Uses `terminal_execute` directly
- Cleaner execution path

### Root Agent (Previously Degraded)
**Before:**
- Tried `web_search` → got "not found"
- Fell back to creating notes
- Lost research capability

**After:**
- Never tries web_search in sandbox context
- Has clear understanding of available tools
- Can optimize for terminal/python-based alternatives
- Better resource utilization

---

## Testing

Run the tests to verify the implementation:

```bash
go test ./pkg/tools -v
go test ./pkg/agents -v
```

All tests pass:
- Registry tests: ✓ (11 tests)
- Agent tests: ✓ (3 tests)
- Integration: ✓ (Production schema files load correctly)

---

## Documentation

For implementers and maintainers:

1. **When registering tools**: Use `tools.Register(name, isSandbox, handler)`
   - `isSandbox = true`: Tool runs in Docker sandbox
   - `isSandbox = false`: Tool runs in parent process

2. **When spawning agents**: 
   - Sandbox agents automatically get `ExecutionContextSandbox`
   - Parent agents automatically get `ExecutionContextParent`
   - System prompts filter tools accordingly

3. **When agents try tools**:
   - Validation happens BEFORE sandbox execution
   - Clear error messages list available alternatives
   - No more cryptic "tool not found" from sandbox

---

## Next Steps (Recommended)

1. **Run end-to-end test**: Execute a scan similar to the last run
   - Monitor agent tool selection
   - Verify no "tool not found" errors for documented tools
   - Check Gitea agent completes successfully

2. **Add contextual guidance**: Include in system prompt
   - "You are running in SANDBOX context with these tools: ..."
   - Guide agents toward appropriate tools for their task

3. **Monitor and log**: Track tool validation
   - Log when agents avoid unavailable tools
   - Monitor tool selection patterns
   - Identify remaining edge cases

4. **Consider skill context**: Extend fix to skills
   - Some skills may only work with certain tools
   - Could pre-filter skills based on available tools
