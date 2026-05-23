# Python Skill Implementation: Detailed Comparison

## Current Python Implementation (strix/tools/python/)

### Architecture

#### 1. **Tool Registration** (`python_actions.py`)
```python
@register_tool  # sandbox_execution=True (default)
def python_action(
    action: PythonAction,
    code: str | None = None,
    timeout: int = 30,
    session_id: str | None = None,
) -> dict[str, Any]:
```

**Key Characteristics:**
- Executes in sandbox (Docker container)
- Session-based with persistent state
- Supports multiple concurrent sessions
- Four actions: `new_session`, `execute`, `close`, `list_sessions`

#### 2. **Session Management** (`python_manager.py`)
```
PythonSessionManager
  ├── _sessions_by_agent: dict[str, dict[str, PythonInstance]]
  │   └── Isolates sessions per agent
  ├── create_session()
  ├── execute_code()
  ├── close_session()
  └── list_sessions()
```

**Per-Agent Isolation:**
- Each agent gets its own session namespace
- Sessions are thread-safe (using `threading.Lock()`)
- Default session ID for backward compatibility

#### 3. **Execution Engine** (`python_instance.py`)
```python
class PythonInstance:
    def __init__(self, session_id: str):
        self.shell = InteractiveShell()  # IPython kernel
        self._setup_proxy_functions()    # Pre-import proxy tools
```

**Capabilities:**
- IPython InteractiveShell for safe code execution
- Output capture (stdout/stderr) with truncation limits
- Automatic proxy function injection
- Session-specific namespace isolation
- Threading support for concurrent execution

#### 4. **Proxy Integration**
Pre-imported into every Python session:
```python
proxy_functions = [
    "list_requests",
    "list_sitemap", 
    "repeat_request",
    "scope_rules",
    "send_request",
    "view_request",
    "view_sitemap_entry",
]
```

**Benefit:** Agents can analyze HTTP traffic directly in Python without additional tool calls

---

## Tool Availability Problem

### Current State: Tools Given but Not Available

**In Sandbox Mode (`STRIX_SANDBOX_MODE=true`):**

Agents receive schemas for these tools but CANNOT use them:
```
❌ load_skill             (sandbox_execution=False)
❌ web_search            (sandbox_execution=False) 
❌ create_todo           (sandbox_execution=False)
❌ update_todo           (sandbox_execution=False)
❌ create_note           (sandbox_execution=False)
❌ create_agent          (sandbox_execution=False)
❌ send_message_to_agent (sandbox_execution=False)
❌ agent_finish          (sandbox_execution=False)
❌ wait_for_message      (sandbox_execution=False)
❌ finish_scan           (sandbox_execution=False)
```

**In Sandbox Mode - CAN Use:**
```
✅ terminal_execute      (sandbox_execution=True)
✅ python_action         (sandbox_execution=True)
✅ browser_action        (if enabled, sandbox_execution=True)
✅ str_replace_editor    (sandbox_execution=True)
✅ list_files           (sandbox_execution=True)
✅ search_files         (sandbox_execution=True)
✅ proxy tools          (sandbox_execution=True)
```

### Root Cause: Schema Distribution

In `registry.py` lines 280-300:
```python
def get_tools_prompt() -> str:
    """Builds tool schemas for agent system prompt"""
    # This includes ALL registered tools
    # No filtering for sandbox_execution mode
    
    for module, module_tools in sorted(tools_by_module.items()):
        section_parts.append(tool_xml)  # ALL tools included
```

**The Problem:**
- Agents get complete tool schema (all ~35+ tools)
- No indication which tools won't work
- Agents optimistically try unavailable tools
- Failure occurs at execution time, not prompt time

---

## Cascade Failure Pattern

### Example 1: Gitea Discovery Agent Failure

**What Happened:**
1. Agent spawned with skills `gospider, katana, nuclei, zaproxy`
2. Agent receives schema for 35+ tools (including unavailable ones)
3. Agent tries to use security tools (which don't exist in sandbox)
4. Agent model sees it can't access web tools
5. Model falls back to safety policy: "refuse security testing requests"
6. Task fails with no recovery path

**Why It Failed:**
```
Expected Tools    │ Actual Tools
─────────────────┼──────────────
gospider          │ ❌ Not in strix
katana            │ ❌ Not in strix  
nuclei            │ ❌ Not in strix
zaproxy           │ ❌ Not in strix
terminal_execute  │ ✅ Available
python_action     │ ✅ Available
```

### Example 2: Recon Agent Success (with Degradation)

**What Happened:**
1. Agent called `load_skill(skills="nmap,httpx")`
2. Got error: "Tool 'load_skill' not found"
3. Agent recovered: Used `terminal_execute` instead
4. Success: `nmap` and `httpx` executed directly via terminal

**Why It Recovered:**
- Agent had fallback option (terminal)
- Both nmap and httpx were available
- No model-level safety restrictions

---

## Go Port Implications

### What The Go Port Should Fix

#### 1. **Dynamic Schema Filtering**
```go
// Current Python approach (wrong for sandbox)
GetToolSchemas() // Returns ALL tools

// Go port should do (correct)
GetToolSchemasForContext(context) {
    if context == SANDBOX {
        return FilterTools(allTools, IsAvailableInSandbox)
    }
    return allTools
}
```

#### 2. **Separate Tool Registries**

**Python (current):**
- One global registry
- Tools marked with `sandbox_execution` flag
- Filtering happens at execution time

**Go (should be):**
- Two separate registries
- `SandboxToolRegistry` - only sandbox-safe tools
- `ParentToolRegistry` - all tools
- Filtering at schema time (not execution time)

#### 3. **Session-Based Tool Context**

```go
type SandboxSession {
    AvailableTools []string
    Context        map[string]interface{}
    PythonSession  PythonInstance
    // ...
}
```

Each sandbox session knows exactly which tools it can use.

#### 4. **Early Validation**

Instead of failing at execution:
```python
# Current: Fail at execution time
agent_calls_load_skill()
→ sandbox.execute()
→ "Tool not found" error

# Go should: Fail early (or prevent)
if "load_skill" in agent_response:
    validate_tool_exists_in_sandbox()
    → error before proxying to sandbox
```

---

## Python Skill Implementation Best Practices

### For the Go Port

#### 1. **Use Sessions for State Management**
```go
type PythonSession struct {
    ID          string
    AgentID     string
    IPythonShell InteractiveShell  // or equivalent
    Namespace   map[string]interface{}
}
```

#### 2. **Pre-Import Common Functions**
Like Python does with proxy tools, pre-import security-relevant functions:
```go
preImportedFunctions := []string{
    // HTTP tools
    "list_requests",
    "view_request",
    "send_request",
    // ... others as appropriate
}
```

#### 3. **Output Management**
- Capture stdout/stderr separately
- Truncate large outputs (10k stdout, 5k stderr)
- Preserve execution results

#### 4. **Concurrent Session Support**
- Use agent_id for session isolation
- Support multiple session_ids per agent
- Thread-safe operations (locks/channels)

#### 5. **Sandbox vs Parent Split**
```go
// In sandbox container
AvailableTools: [terminal, python, file_edit, proxy]

// In parent process
AvailableTools: [load_skill, web_search, agents, todos, notes, reporting]
```

---

## Key Takeaways for Gitea Agent Failure

1. **Not a model safety issue** - Agent couldn't use the tools it thought it had
2. **Tool schema mismatch** - Given skills it couldn't load (load_skill unavailable)
3. **No fallback path** - Web security tools don't have terminal equivalents
4. **Cascading failure** - Model gave up when tools failed

**Fix:** Filter tool schemas based on execution context BEFORE sending to agent.
