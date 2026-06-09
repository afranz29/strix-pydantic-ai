"""FastAPI backend service for orchestrating Strix scans."""

import asyncio
import json
import logging
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, AsyncGenerator, Optional

import fastapi
from fastapi.responses import StreamingResponse
from pydantic import BaseModel

from strix_pydantic.agents.pydantic_orchestrator import build_orchestrator_graph
from strix_pydantic.agents.types import RunConfig, StrixDeps, StrixRunState
from strix_pydantic.config.model_config import normalize_model_spec, resolve_model_config
from strix_pydantic.runtime.bootstrap import initialize_sandbox
from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory
from strix_pydantic.tools.tool_registry import ToolRegistry
from strix_pydantic.runtime.sandbox_client import SandboxClient
from strix_pydantic.service.events import (
    AgentCompletedEvent,
    AgentStartedEvent,
    ScanCompletedEvent,
    ScanFailedEvent,
    ScanStartedEvent,
    VulnerabilityFoundEvent,
)

logger = logging.getLogger(__name__)

# In-memory scan storage
scans: dict[str, "ScanSession"] = {}
scan_queues: dict[str, asyncio.Queue] = {}


class ScanRequest(BaseModel):
    """Request to start a scan."""

    target: str
    scan_mode: str = "standard"
    model: Optional[str] = None
    mock_tools: bool = False
    confirm: bool = False
    instruction: str = ""
    sandbox_url: Optional[str] = None


class ScanResponse(BaseModel):
    """Response when scan is started."""

    scan_id: str
    target: str
    scan_mode: str


class ScanSession:
    """In-memory session for a single scan."""

    def __init__(self, scan_id: str, request: ScanRequest):
        self.scan_id = scan_id
        self.request = request
        self.start_time = time.time()
        self.state: Optional[StrixRunState] = None
        self.status = "initialized"  # initialized, running, completed, failed
        self.error: Optional[str] = None

    def to_dict(self) -> dict:
        """Convert session to dict for API responses."""
        return {
            "scan_id": self.scan_id,
            "status": self.status,
            "target": self.request.target,
            "scan_mode": self.request.scan_mode,
            "start_time": self.start_time,
            "duration_seconds": time.time() - self.start_time,
            "error": self.error,
            "vulnerabilities": len(self.state.vulnerabilities) if self.state else 0,
        }


app = fastapi.FastAPI(title="Strix Backend Service")


@app.post("/scans", response_model=ScanResponse)
async def start_scan(request: ScanRequest) -> ScanResponse:
    """Start a new security scan."""
    scan_id = f"run_{uuid.uuid4().hex[:12]}"

    # Create session
    session = ScanSession(scan_id, request)
    scans[scan_id] = session

    # Create event queue for this scan
    scan_queues[scan_id] = asyncio.Queue()

    # Start scan in background
    asyncio.create_task(_run_scan_background(scan_id, request))

    return ScanResponse(
        scan_id=scan_id,
        target=request.target,
        scan_mode=request.scan_mode,
    )


@app.get("/scans/{scan_id}")
async def get_scan_status(scan_id: str):
    """Get scan status and results."""
    session = scans.get(scan_id)
    if not session:
        raise fastapi.HTTPException(status_code=404, detail="Scan not found")

    return session.to_dict()


@app.get("/scans/{scan_id}/events")
async def stream_events(scan_id: str):
    """Stream scan events as newline-delimited JSON."""
    logger.info(f"Event stream requested for {scan_id}, available scans: {list(scans.keys())}")

    session = scans.get(scan_id)
    if not session:
        raise fastapi.HTTPException(status_code=404, detail="Scan not found")

    event_queue = scan_queues.get(scan_id)
    if not event_queue:
        logger.error(f"No event queue for {scan_id}, available queues: {list(scan_queues.keys())}")
        raise fastapi.HTTPException(status_code=500, detail="No event queue")

    async def event_generator() -> AsyncGenerator[str, None]:
        """Generate events as newline-delimited JSON."""
        try:
            while True:
                # Get next event from queue
                event = await event_queue.get()

                # Send as JSON line
                yield json.dumps(event.model_dump(mode="json")) + "\n"

                # Stop if scan completed
                if event.type in ("scan_completed", "scan_failed"):
                    break

        except Exception as e:
            logger.error(f"Event streaming error for scan {scan_id}: {e}")

    return StreamingResponse(event_generator(), media_type="application/x-ndjson")


async def _run_scan_background(scan_id: str, request: ScanRequest) -> None:
    """Run scan in background with event streaming."""
    session = scans[scan_id]
    event_queue = scan_queues[scan_id]
    start_time = time.time()

    try:
        session.status = "running"

        # Create event emitter
        async def emit_event(event_type: str, payload: dict[str, Any]) -> None:
            """Emit event to queue."""
            timestamp = datetime.now(timezone.utc).replace(tzinfo=None)
            payload["scan_id"] = scan_id
            payload["timestamp"] = timestamp
            payload["type"] = event_type

            # Create appropriate event class
            if event_type == "scan_started":
                event = ScanStartedEvent(**payload)
            elif event_type == "agent_started":
                event = AgentStartedEvent(**payload)
            elif event_type == "agent_completed":
                event = AgentCompletedEvent(**payload)
            elif event_type == "vulnerability_found":
                event = VulnerabilityFoundEvent(**payload)
            elif event_type == "scan_completed":
                event = ScanCompletedEvent(**payload)
            elif event_type == "scan_failed":
                event = ScanFailedEvent(**payload)
            else:
                return

            await event_queue.put(event)

        # Setup orchestrator like CLI does
        try:
            if request.model:
                model_spec = normalize_model_spec(request.model)
            else:
                model_spec, _ = resolve_model_config()

        except Exception as e:
            await emit_event("scan_failed", {
                "error": f"Failed to resolve model: {e}",
                "duration_seconds": time.time() - start_time,
            })
            session.status = "failed"
            session.error = str(e)
            return

        # Initialize sandbox
        try:
            if request.mock_tools:
                from strix_pydantic.tools.mock_tools import register_mock_tools
                sandbox_url = "http://127.0.0.1:48081"
                sandbox_client = None
                tool_registry = ToolRegistry()
                register_mock_tools(tool_registry)
                runtime = None
                sandbox_info = None
            else:
                run_id = scan_id
                sandbox_url, auth_token, runtime, sandbox_info = await initialize_sandbox(run_id, request.sandbox_url or None)
                sandbox_client = SandboxClient(
                    base_url=sandbox_url,
                    auth_token=auth_token,
                    execute_timeout=120.0,
                )
                tool_registry = ToolRegistry()

        except Exception as e:
            await emit_event("scan_failed", {
                "error": f"Failed to initialize sandbox: {e}",
                "duration_seconds": time.time() - start_time,
            })
            session.status = "failed"
            session.error = str(e)
            return

        try:
            # Register strix tools
            from strix_pydantic.interface.cli import _register_strix_tools, _build_agents
            if not request.mock_tools:
                _register_strix_tools(tool_registry)

            # Parse skills
            skill_list = []

            # Create run state
            state = StrixRunState(
                run_id=scan_id,
                target=request.target,
                scan_mode=request.scan_mode,
                instruction=request.instruction,
                active_skills=skill_list,
            )

            # Create run config
            run_config = RunConfig(
                model_name=model_spec,
                scan_mode=request.scan_mode,
                non_interactive=True,
                execute_timeout=120.0,
            )

            # Build agents
            agents = _build_agents(
                model_spec,
                skill_list,
                run_config,
                tool_registry,
                sandbox_client=sandbox_client,
                run_state=state,
            )

            # Create deps with event emitter
            deps = StrixDeps(
                sandbox_url=sandbox_url,
                tool_registry=tool_registry,
                run_config=run_config,
                agents=agents,
                sandbox_client=sandbox_client,
                event_emitter=emit_event,  # ← Pass event emitter
                confirm_proceed=None,  # No interactive confirmation in service
            )

            # Run orchestrator
            logger.info(f"Starting orchestrator for scan {scan_id}")
            graph = build_orchestrator_graph()
            result = graph.run(state=state, deps=deps)

            # Handle both sync and async returns
            if hasattr(result, "__await__"):
                await result
            else:
                pass  # Sync result, already done

            session.state = state
            session.status = "completed"

            # Emit completion event
            duration = time.time() - start_time
            vuln_count = len(state.vulnerabilities) if state else 0
            iterations = len([r for r in state.runs]) if state else 0
            await emit_event("scan_completed", {
                "duration_seconds": duration,
                "vulnerabilities_count": vuln_count,
                "iterations": iterations,
            })

        except Exception as e:
            logger.error(f"Scan {scan_id} orchestrator error: {e}", exc_info=True)
            session.status = "failed"
            session.error = str(e)

            duration = time.time() - start_time
            await emit_event("scan_failed", {
                "error": str(e),
                "duration_seconds": duration,
            })

        finally:
            # Cleanup
            if not request.mock_tools and runtime and sandbox_info:
                try:
                    await runtime.destroy_sandbox(sandbox_info["workspace_id"])
                except Exception as e:
                    logger.warning(f"Failed to cleanup sandbox: {e}")

    except Exception as e:
        logger.error(f"Scan {scan_id} background error: {e}", exc_info=True)
        session.status = "failed"
        session.error = str(e)


@app.get("/health")
async def health_check():
    """Health check endpoint."""
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)
