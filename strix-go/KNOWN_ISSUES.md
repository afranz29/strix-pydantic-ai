# Known Issues - Strix Go Port

Last Updated: 2026-05-23

## Critical Issues

### 1. Fuzzing Agent Deadlock
**Severity**: Critical  
**Status**: Open  
**First Observed**: 2026-05-23 15:56:47

**Description**:
Fuzzing Agent (`agent_b1e6efac`) enters `wait_for_message` state waiting for long-running `ffuf` command to complete but never resumes execution. The agent remains stuck even after the command finishes, blocking the root agent and preventing scan completion.

**Log Evidence**:
```
time=2026-05-23T15:56:47.046Z level=INFO msg="Agent entering wait state for incoming message" 
  agent_id=agent_b1e6efac reason="Waiting for ffuf to complete and for subagents to report findings."
```

**Impact**: 
- Blocks entire penetration test workflow
- Root agent waits indefinitely
- Scan never completes

**Root Cause**:
Long-running terminal commands don't notify waiting agents on completion. The wait_for_message mechanism expects messages from other agents, not command completion events.

---

### 2. Missing Tool: `create_vulnerability_report`
**Severity**: High  
**Status**: Open  
**First Observed**: 2026-05-23

**Description**:
Multiple agents attempt to invoke `create_vulnerability_report` tool but it's not registered in the sandbox tool registry, causing tool execution failures.

**Affected Agents**:
- `agent_190f0461` (Web Recon Agent)
- `agent_6e4c9a39` (Token Exposure Reporting Agent)
- `agent_2047b2d0` (Timing Attack Reporting Agent)

**Log Evidence**:
```
time=2026-05-23T15:50:17.254Z level=ERROR 
  msg="Sandbox tool server reported execution error" 
  tool_name=create_vulnerability_report 
  execution_error="Tool execution error: Tool 'create_vulnerability_report' not found"
```

**Occurrences**: 3 failures across different agents

**Impact**:
- Agents cannot document/report discovered vulnerabilities
- Findings may be lost or incompletely reported
- Agents must work around missing tool

**Resolution Options**:
1. Implement the `create_vulnerability_report` tool in sandbox
2. Remove tool from agent capabilities/prompts
3. Map to alternative reporting mechanism (notes, finish tool, etc.)

---

## Performance & UX Issues

### 3. Missing Wordlists in Sandbox Docker Image
**Severity**: Medium  
**Status**: Open  
**First Observed**: 2026-05-23

**Description**:
Agents repeatedly fail to find common wordlist files at expected system paths (e.g., `/usr/share/wordlists/dirb/common.txt`), forcing them to manually download wordlists during scans.

**Log Evidence**:
```
Encountered error(s): 1 errors occurred.
  * stat /usr/share/wordlists/dirb/common.txt: no such file or directory
```

**Impact**:
- Scan delays (agents must download wordlists mid-execution)
- Redundant network requests across multiple agents
- Inefficient use of agent iteration budget
- Dependency on external GitHub repositories during scans

**Workaround**:
Agents download wordlists to `/workspace/common.txt` using curl:
```bash
curl -s https://raw.githubusercontent.com/danielmiessler/SecLists/master/Discovery/Web-Content/common.txt -o /workspace/common.txt
```

**Recommended Fix**:
Pre-install SecLists or commonly-used wordlists in sandbox Docker image at standard paths:
- `/usr/share/wordlists/dirb/`
- `/usr/share/wordlists/seclists/`
- `/usr/share/wordlists/rockyou.txt`

---

### 4. Inefficient Long-Running Command Handling
**Severity**: Medium  
**Status**: Open  
**First Observed**: 2026-05-23

**Description**:
Long-running commands (30+ seconds) like `nuclei` and `ffuf` scans require agents to repeatedly poll for completion using empty `terminal_execute("")` commands. This is inefficient and wastes agent iterations.

**Log Evidence**:
```
time=2026-05-23T15:57:38.501Z level=INFO msg="Tool executed successfully" 
  content="[Command still running after 30.0s - showing output so far] 
  exit_code:<nil> status:running"
```

**Polling Pattern Observed**:
1. Agent runs long command (nuclei, ffuf, etc.)
2. After 30s, gets "still running" status
3. Agent issues empty `terminal_execute("")` to check status
4. Repeats every 30s until command completes

**Impact**:
- Wastes agent LLM iterations on polling
- Increases latency between command completion and agent resumption
- Creates verbose, repetitive logs

**Recommended Improvements**:
1. Implement async command tracking with automatic completion notifications
2. Add webhook/callback mechanism when commands finish
3. Consider background command execution with status queries
4. Integrate with agent message inbox for completion events

---

### 5. Terminal Command Cancellation
**Severity**: Low  
**Status**: Needs Investigation  
**First Observed**: 2026-05-23 15:49:03

**Description**:
One instance of terminal command cancellation with error "Cancelled by newer request".

**Log Evidence**:
```
time=2026-05-23T15:49:03.766Z level=ERROR 
  msg="Sandbox tool server reported execution error" 
  tool_name=terminal_execute 
  execution_error="Cancelled by newer request"
```

**Context**:
Occurred during agent `agent_190f0461` execution with high iteration count (79-81).

**Questions to Investigate**:
- Is this intentional preemption behavior (newer command cancels older)?
- Does it indicate a race condition in terminal multiplexing?
- Should agents be prevented from issuing commands while previous ones run?
- Is there a terminal command queue that overflows?

**Impact**: 
- Minimal (single occurrence)
- May indicate deeper concurrency issues if pattern repeats

---

## Observations & Patterns

### Agent Recovery Behavior
Agents demonstrate good error recovery when tools fail:
- Token Exposure Reporting Agent discovered missing tool through exploration
- Recon agents successfully downloaded alternative wordlists
- Agents adapt and find workarounds

### Successful Components
- **Agent spawning**: Multiple child agents launched successfully
- **Tool proxying**: Sandbox tool execution works reliably (except missing tools)
- **Logging**: Comprehensive structured logs enable debugging
- **LLM integration**: Gemini responses consistent and appropriate
- **Agent completion**: Agents properly use `agent_finish` when done
- **Parent notification**: Report-to-parent mechanism works correctly

### Areas Working Well
- Nuclei Scanner Agent completed successfully (no vulnerabilities found)
- Recon & Mapping Agent completed with useful findings
- Directory structure and per-run isolation working correctly
- Notes and TODO persistence functioning properly

---

## Recommendations

### Immediate Actions
1. **Fix Fuzzing Agent deadlock**: Implement command completion notifications or revise wait strategy
2. **Add vulnerability report tool**: Either implement or remove from agent expectations
3. **Update Docker image**: Include common wordlists in sandbox container

### Architecture Improvements
1. Implement async command execution with completion callbacks
2. Add agent message types for non-agent events (command completion, timeouts)
3. Consider command cancellation policy and terminal multiplexing strategy
4. Add tool availability validation before agent dispatch

### Monitoring Enhancements
1. Track agent wait states and detect deadlocks
2. Alert on repeated tool-not-found errors
3. Monitor command execution times and polling frequency
4. Log agent state transitions more explicitly
