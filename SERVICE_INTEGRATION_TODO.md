# Service Integration TODO

The basic infrastructure is in place. Here's what needs to be done to fully integrate the orchestrator:

## Phase 1: Event Emitter Integration

### 1.1 Add Event Emitter to StrixDeps (types.py)

```python
@dataclass
class StrixDeps:
    # ... existing fields ...
    
    # Event emitter for backend integration
    event_emitter: Optional[Callable[[str, dict], Awaitable[None]]] = None
```

### 1.2 Update Orchestrator (pydantic_orchestrator.py)

Call event emitter at key points:

```python
async def _run_orchestration(state, deps):
    # Start event
    if deps.event_emitter:
        await deps.event_emitter("scan_started", {
            "target": state.target,
            "scan_mode": state.scan_mode,
            "model": deps.run_config.model_name,
        })
    
    for role in roles:
        # Agent start
        if deps.event_emitter:
            await deps.event_emitter("agent_started", {
                "role": role,
                "iteration": state.iteration,
            })
        
        state.agent_statuses[role] = "running"
        
        # Run agent...
        result = await agent.run(...)
        
        # Process vulnerabilities
        if hasattr(result.output, "vulnerabilities"):
            for v in result.output.vulnerabilities:
                if deps.event_emitter:
                    await deps.event_emitter("vulnerability_found", {
                        "role": role,
                        "title": v.title,
                        "severity": v.severity,
                        "description": v.description,
                        "cve_id": v.cve_id,
                        "parameter": v.parameter,
                        "poc": v.poc,
                    })
                state.vulnerabilities.append(v.model_dump())
        
        state.agent_statuses[role] = "completed"
        
        # Agent complete
        if deps.event_emitter:
            await deps.event_emitter("agent_completed", {
                "role": role,
                "iteration": state.iteration,
                "vulnerabilities_found": len([v for v in state.vulnerabilities if v.get("role") == role]),
            })
    
    # Scan complete
    if deps.event_emitter:
        await deps.event_emitter("scan_completed", {
            "duration_seconds": time.time() - start_time,
            "vulnerabilities_count": len(state.vulnerabilities),
            "iterations": state.iteration,
        })
```

## Phase 2: Backend Integration

### 2.1 Replace Test Orchestrator in backend.py

Current code has a placeholder:
```python
# TODO: Integrate with actual orchestrator
# For now, just emit a test event
```

Replace with actual orchestrator call:

```python
async def _run_scan_background(scan_id: str, request: ScanRequest) -> None:
    """Run scan in background with event streaming."""
    session = scans[scan_id]
    event_queue = scan_queues[scan_id]
    start_time = time.time()
    
    try:
        session.status = "running"
        
        # Create event emitter
        async def emit_event(event_type: str, **kwargs):
            """Emit event to queue."""
            # ... existing code ...
        
        # Import orchestrator components
        from strix_pydantic.config.model_config import normalize_model_spec, resolve_model_config
        from strix_pydantic.runtime.bootstrap import initialize_sandbox
        from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory
        from strix_pydantic.tools.tool_registry import ToolRegistry
        
        # Initialize like CLI does
        if request.model:
            model_spec = normalize_model_spec(request.model)
        else:
            model_spec, _ = resolve_model_config()
        
        # ... setup code similar to cli.py ...
        
        # Create state
        state = StrixRunState(
            run_id=scan_id,
            target=request.target,
            scan_mode=request.scan_mode,
            active_skills=[],
            instruction=request.instruction,
        )
        
        # Create deps with event emitter
        deps = StrixDeps(
            sandbox_url=sandbox_url,
            tool_registry=tool_registry,
            run_config=run_config,
            agents=agents,
            sandbox_client=sandbox_client,
            event_emitter=emit_event,  # ← Pass event emitter
        )
        
        # Run orchestrator
        graph = build_orchestrator_graph()
        await _run_graph_async(graph, state, deps)
        
        # Success case handled by orchestrator emit_event
```

### 2.2 Import Needed Modules in backend.py

```python
# At the top of backend.py
from strix_pydantic.agents.pydantic_orchestrator import (
    build_orchestrator_graph,
    _run_graph_async,
)
from strix_pydantic.config.model_config import (
    normalize_model_spec,
    resolve_model_config,
)
from strix_pydantic.runtime.bootstrap import initialize_sandbox
from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory
from strix_pydantic.tools.tool_registry import ToolRegistry
from strix_pydantic.runtime.sandbox_client import SandboxClient
```

## Phase 3: Tool Integration (Optional for MVP)

For MVP, keep mock tools. For full integration:

- Wire up `SandboxClient` in backend
- Handle tool execution with event emissions
- Stream tool output via `tool_executed` events

## Phase 4: Error Handling

Add proper error handling:

```python
except Exception as e:
    logger.error(f"Scan {scan_id} failed: {e}", exc_info=True)
    session.status = "failed"
    session.error = str(e)
    
    duration = time.time() - session.start_time
    await event_queue.put(
        ScanFailedEvent(
            scan_id=scan_id,
            timestamp=datetime.utcnow(),
            error=str(e),
            duration_seconds=duration,
        )
    )
finally:
    # Cleanup
    if runtime and sandbox_info:
        try:
            await runtime.destroy_sandbox(sandbox_info["workspace_id"])
        except Exception as e:
            logger.warning(f"Failed to cleanup sandbox: {e}")
```

## Testing the Integration

### Manual Testing

```bash
# Terminal 1: Start backend
python -m strix_pydantic.service.backend

# Terminal 2: Run test scan
python -m strix_pydantic.service.cli_client \
  --target http://example.com \
  --scan-mode quick \
  --mock-tools
```

### Expected Output

```
📋 Starting scan for http://example.com
✓ Scan started: run_abc123

🚀 Scan started
   Target: http://example.com
   Mode: quick

🤖 Starting reconnaissance (iteration 1)
✅ reconnaissance completed (0 vulnerabilities)

🤖 Starting exploitation (iteration 2)
✅ exploitation completed (0 vulnerabilities)

🤖 Starting post_exploitation (iteration 3)
✅ post_exploitation completed (0 vulnerabilities)

============================================================
✅ Scan completed successfully
   Duration: 45.1s
   Vulnerabilities: 0
   Iterations: 3
============================================================
```

## File Changes Required

### Modified Files
- `strix_pydantic/agents/types.py` - Add event_emitter to StrixDeps
- `strix_pydantic/agents/pydantic_orchestrator.py` - Emit events
- `strix_pydantic/service/backend.py` - Integrate orchestrator

### New Functionality
- Event emission calls (~50-100 LOC)
- Orchestrator setup in backend (~100-150 LOC)

## Implementation Order

1. ✅ Create service scaffold (DONE)
2. ✅ Define event schema (DONE)
3. ✅ Create backend structure (DONE)
4. ✅ Create CLI client (DONE)
5. ⏳ **Add event emitter to orchestrator** (START HERE)
6. ⏳ **Integrate orchestrator in backend**
7. ⏳ **Test end-to-end**
8. ⏳ Add persistence (future)
9. ⏳ Add web UI (future)

## Success Criteria

When integration is complete:

✅ Run `strix-service` in terminal 1  
✅ Run `strix-client --target http://example.com --mock-tools` in terminal 2  
✅ See real-time events streaming from backend to CLI  
✅ See vulnerabilities displayed as they're found  
✅ See final summary with stats  
✅ No errors, no threading issues, no race conditions  

## Notes

- The basic structure is sound and ready for orchestrator integration
- No breaking changes needed to existing code
- Event emitter is a clean callback pattern (already used for confirm_proceed)
- WebSocket streaming handles real-time delivery efficiently
- Design supports scaling to web UI or remote backends

Next step: Add event emissions to orchestrator!
