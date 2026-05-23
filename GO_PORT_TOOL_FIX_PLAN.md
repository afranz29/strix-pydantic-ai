# Go Port: Tool System Fix Plan

## Executive Summary

The Python implementation has a critical design flaw: **agents receive tool schemas for tools they cannot use**. This causes cascading failures where agents exhaust themselves trying unavailable tools.

The Go port must fix this by filtering tool schemas at generation time based on execution context.

---

## The Bug in Current Python Implementation

### Root Cause
**File:** `strix/tools/registry.py:280-300`

```python
def get_tools_prompt() -> str:
    """Builds ALL tool schemas regardless of execution context"""
    # BUG: No filtering for sandbox_execution mode
    
    for tool in all_tools:  # includes load_skill, web_search, etc.
        xml_sections.append(tool.xml_schema)  # ALL sent to agent
    
    return "\n\n".join(xml_sections)
```

**Impact:**
- Sandbox agents receive ~20 non-functional tool schemas
- They try to use them (because schemas say they should)
- Tools fail silently or with cryptic errors
- Agents either recover with degradation or give up

### Example Error Sequence

```
Agent in Sandbox receives:
  ✅ 10 working tools (terminal, python, files, proxy)
  ❌ 20 broken tools (load_skill, web_search, todos, notes, agents, etc.)

Agent thinks: "I have 30 powerful tools!"
Agent calls: load_skill()
Sandbox says: "Tool 'load_skill' not found"
Agent thinks: "My tool is broken, let me try something else..."
Agent repeats: Creates circular logic when no fallback exists
```

---

## Required Fixes for Go Port

### Fix 1: Separate Tool Registries by Execution Context

**Current (Python) - Single Registry:**
```python
@register_tool(sandbox_execution=False)
def load_skill(...):
    pass

@register_tool  # sandbox_execution=True
def python_action(...):
    pass
```

**Required (Go) - Dual Registry:**

```go
type ToolRegistry struct {
    SandboxTools  map[string]Tool     // Executable in container
    ParentTools   map[string]Tool     // Executable in parent only
    AllTools      map[string]Tool     // For reference
}

// Usage
registry := NewToolRegistry()

// Register with context
registry.RegisterSandbox("terminal_execute", terminalTool)
registry.RegisterSandbox("python_action", pythonTool)
registry.RegisterParent("load_skill", loadSkillTool)
registry.RegisterParent("web_search", webSearchTool)

// Get schema for context
sandboxSchema := registry.GetSchemasForContext("sandbox")  // Only sandbox tools
parentSchema := registry.GetSchemasForContext("parent")    // All tools
```

**Benefit:** Agents never see unavailable tools in their prompt

### Fix 2: Dynamic Schema Generation

**Current (Python) - Static Generation:**
```python
# Same schema sent to all agents
schemas = get_tools_prompt()
```

**Required (Go) - Dynamic Generation:**

```go
type AgentContext struct {
    AgentID    string
    ExecutionMode string  // "sandbox" or "parent"
    AvailableTools []string
}

func (a *Agent) GenerateSystemPrompt(ctx AgentContext) string {
    // Only include tools relevant to execution context
    tools := registry.GetSchemasForContext(ctx.ExecutionMode)
    
    systemPrompt := fmt.Sprintf(
        "You have access to these tools:\n%s\n",
        formatToolSchemas(tools),
    )
    return systemPrompt
}
```

**Benefit:** Each agent knows exactly what tools it can use

### Fix 3: Tool Availability Validation

**Add Pre-Execution Check:**

```go
type ToolValidator struct {
    registry *ToolRegistry
}

func (v *ToolValidator) ValidateToolCall(
    toolName string, 
    executionContext string,
) error {
    
    // Check 1: Tool exists
    tool, exists := v.registry.AllTools[toolName]
    if !exists {
        return fmt.Errorf("tool '%s' does not exist", toolName)
    }
    
    // Check 2: Tool available in context
    available := v.registry.IsAvailableInContext(
        toolName, 
        executionContext,
    )
    if !available {
        return fmt.Errorf(
            "tool '%s' not available in %s context. Available tools: %v",
            toolName,
            executionContext,
            v.registry.GetAvailableTools(executionContext),
        )
    }
    
    return nil
}
```

**Usage:**
```go
// When processing agent response
if err := validator.ValidateToolCall(toolName, "sandbox"); err != nil {
    // Fail fast with clear message
    // Instead of silently failing in sandbox
    return fmt.Errorf("invalid tool call: %w", err)
}
```

**Benefit:** Clear, immediate feedback instead of cryptic "tool not found"

### Fix 4: Execution Context Propagation

**Modify Agent Initialization:**

```go
type Agent struct {
    ID             string
    ExecutionMode  string  // "sandbox" or "parent"
    AvailableTools []string
    // ...
}

func (a *Agent) Initialize(mode ExecutionMode) {
    a.ExecutionMode = string(mode)
    
    // Load only available tools for this agent's context
    a.AvailableTools = toolRegistry.GetAvailableTools(mode)
    
    // Generate prompt with correct tool list
    a.SystemPrompt = a.generateSystemPrompt()
}

func (a *Agent) executeToolCall(toolName string, params map[string]interface{}) error {
    // Check tool availability BEFORE execution
    if !slices.Contains(a.AvailableTools, toolName) {
        return fmt.Errorf(
            "agent %s running in %s mode cannot use tool '%s'. Available: %v",
            a.ID,
            a.ExecutionMode,
            toolName,
            a.AvailableTools,
        )
    }
    // ... execute tool
}
```

**Benefit:** Agents know their constraints from initialization

### Fix 5: Schema Filtering Logic

**Implementation in Registry:**

```go
func (r *ToolRegistry) GetSchemasForContext(ctx string) string {
    var schemas []string
    
    var tools map[string]Tool
    switch ctx {
    case "sandbox":
        tools = r.SandboxTools
    case "parent":
        tools = r.AllTools
    default:
        return ""
    }
    
    for name, tool := range tools {
        schemas = append(schemas, tool.Schema)
    }
    
    sort.Strings(schemas)  // Consistent ordering
    return strings.Join(schemas, "\n\n")
}
```

---

## Concrete Implementation Checklist for Go Port

### 1. Tool Registry Architecture
- [ ] Create `ToolRegistry` struct with sandbox/parent separation
- [ ] Implement `RegisterSandbox()` and `RegisterParent()` methods
- [ ] Implement `GetSchemasForContext()` method
- [ ] Implement `IsAvailableInContext()` method

### 2. Execution Context
- [ ] Add `ExecutionMode` enum (Sandbox, Parent)
- [ ] Add `ExecutionContext` struct with mode, agent_id, available_tools
- [ ] Propagate context through agent initialization

### 3. Schema Management
- [ ] Modify agent prompt generation to use context-aware schemas
- [ ] Only include tools in schema that are available in execution context
- [ ] Add clear comments about which tools are available where

### 4. Validation
- [ ] Add `ToolValidator` with context-aware checks
- [ ] Implement pre-execution validation of tool calls
- [ ] Provide clear error messages for unavailable tools
- [ ] Test error paths

### 5. Documentation
- [ ] Document which tools are available in sandbox vs parent
- [ ] Document execution contexts and how they affect tool availability
- [ ] Add examples for agents (sandbox agents should use terminal/python, not load_skill)
- [ ] Add troubleshooting guide for "tool not found" errors

---

## Mapping Python Tools to Go Port

### Sandbox-Executable Tools (Available in Container)

```go
// File operations
terminal_execute      ✅ Shell command execution
python_action        ✅ Python code execution (with IPython)
str_replace_editor   ✅ File editing
list_files          ✅ Directory listing
search_files        ✅ File search

// Proxy/Web (if running with proxy)
list_requests       ✅ Query HTTP history
view_request        ✅ View request details
send_request        ✅ Send HTTP request
repeat_request      ✅ Replay request
scope_rules         ✅ Manage proxy scope
list_sitemap        ✅ View discovered URLs
view_sitemap_entry  ✅ View sitemap item

// Browser (if enabled)
browser_action      ✅ Browser automation
```

### Parent-Only Tools (Not in Container)

```go
// Agent control
create_agent        ❌ Create subagent (parent only)
send_message_to_agent  ❌ Inter-agent messaging (parent only)
agent_finish        ❌ Agent completion (parent only)
wait_for_message    ❌ Wait for response (parent only)
view_agent_graph    ❌ View agent relationships (parent only)

// Data management
create_todo        ❌ Todo system (parent only)
update_todo        ❌ Todo updates (parent only)
list_todos         ❌ Todo listing (parent only)
create_note        ❌ Notes system (parent only)
update_note        ❌ Note updates (parent only)
list_notes         ❌ Note listing (parent only)

// Special tools
load_skill         ❌ Runtime skill loading (parent only)
web_search         ❌ Web search (parent only, requires API)
think              ❌ Extended thinking (parent only)
finish_scan        ❌ Scan completion (parent only)
create_vulnerability_report  ❌ Reporting (parent only)
```

---

## Impact on Agent Behavior

### Before Fix (Current Python)
```
Recon Agent                  Gitea Discovery Agent
├─ See all 30 tools         ├─ See all 30 tools
├─ Try load_skill ❌        ├─ Try gospider ❌
├─ Recover with terminal ✓  ├─ Try katana ❌
├─ Success: scan completes  ├─ Try nuclei ❌
└─ Report findings ✓        ├─ Model gives up
                            └─ Fail: "safety constraints"
```

### After Fix (Go Port)
```
Recon Agent                  Gitea Discovery Agent
├─ See only 10 tools        ├─ See only 10 tools
├─ Has terminal ✓           ├─ Has terminal ✓
├─ Has python ✓             ├─ Has python ✓
├─ No load_skill            ├─ No load_skill
├─ Success: scan completes  ├─ Uses alternatives
└─ Report findings ✓        └─ Partial success
```

---

## Testing Strategy

### Unit Tests
```go
func TestSandboxToolAvailability(t *testing.T) {
    // Verify only sandbox tools are in sandbox schema
}

func TestParentToolAvailability(t *testing.T) {
    // Verify all tools available to parent agents
}

func TestToolValidation(t *testing.T) {
    // Test that unavailable tools are rejected
}
```

### Integration Tests
```go
func TestAgentInSandbox(t *testing.T) {
    // Spawn agent in sandbox context
    // Verify only 10 tools available
    // Verify terminal execution works
    // Verify python execution works
}

func TestAgentInParent(t *testing.T) {
    // Spawn agent in parent context
    // Verify all 30 tools available
}
```

---

## Success Criteria

✅ Sandbox agents receive only tools they can use
✅ Parent agents receive full tool set
✅ Tool unavailability detected at schema time (not execution time)
✅ Clear error messages for unavailable tools
✅ No more "tool not found" errors for documented tools
✅ Gitea agent can fall back to available tools
✅ Agents complete with higher success rate
