"""FastAPI backend service for orchestrating Strix scans."""

import asyncio
import logging
import time
import uuid
from datetime import datetime
from typing import Optional

import fastapi
from fastapi import WebSocket, WebSocketDisconnect
from pydantic import BaseModel

from strix_pydantic.agents.pydantic_orchestrator import build_orchestrator_graph
from strix_pydantic.agents.types import RunConfig, StrixDeps, StrixRunState
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

    return {
        "scan_id": scan_id,
        "status": session.status,
        "target": session.request.target,
        "scan_mode": session.request.scan_mode,
        "start_time": session.start_time,
        "duration_seconds": time.time() - session.start_time,
        "error": session.error,
        "vulnerabilities": (
            len(session.state.vulnerabilities) if session.state else 0
        ),
    }


@app.websocket("/scans/{scan_id}/events")
async def websocket_events(websocket: WebSocket, scan_id: str):
    """WebSocket endpoint for streaming scan events."""
    session = scans.get(scan_id)
    if not session:
        await websocket.close(code=404)
        return

    event_queue = scan_queues.get(scan_id)
    if not event_queue:
        await websocket.close(code=500)
        return

    await websocket.accept()

    try:
        while True:
            # Get next event from queue
            event = await event_queue.get()

            # Send to client
            await websocket.send_json(event.model_dump(mode="json"))

            # Break if scan completed
            if event.type in ("scan_completed", "scan_failed"):
                break

    except WebSocketDisconnect:
        logger.info(f"Client disconnected from scan {scan_id}")
    except Exception as e:
        logger.error(f"WebSocket error for scan {scan_id}: {e}")
        try:
            await websocket.close(code=1011)
        except Exception:
            pass


async def _run_scan_background(scan_id: str, request: ScanRequest) -> None:
    """Run scan in background with event streaming."""
    session = scans[scan_id]
    event_queue = scan_queues[scan_id]

    try:
        session.status = "running"

        # Create event emitter
        async def emit_event(event_type: str, **kwargs):
            """Emit event to queue."""
            timestamp = datetime.utcnow()
            kwargs["scan_id"] = scan_id
            kwargs["timestamp"] = timestamp
            kwargs["type"] = event_type

            # Create appropriate event class
            if event_type == "scan_started":
                event = ScanStartedEvent(**kwargs)
            elif event_type == "agent_started":
                event = AgentStartedEvent(**kwargs)
            elif event_type == "agent_completed":
                event = AgentCompletedEvent(**kwargs)
            elif event_type == "vulnerability_found":
                event = VulnerabilityFoundEvent(**kwargs)
            elif event_type == "scan_completed":
                event = ScanCompletedEvent(**kwargs)
            elif event_type == "scan_failed":
                event = ScanFailedEvent(**kwargs)
            else:
                return

            await event_queue.put(event)

        # Emit scan started
        await emit_event(
            "scan_started",
            target=request.target,
            scan_mode=request.scan_mode,
            model=request.model or "auto",
        )

        # TODO: Integrate with actual orchestrator
        # For now, just emit a test event
        await emit_event("agent_started", role="reconnaissance", iteration=1)
        await asyncio.sleep(1)
        await emit_event("agent_completed", role="reconnaissance", iteration=1, vulnerabilities_found=0)

        # Emit scan completed
        duration = time.time() - session.start_time
        await emit_event(
            "scan_completed",
            duration_seconds=duration,
            vulnerabilities_count=0,
            iterations=1,
        )

        session.status = "completed"

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


@app.get("/health")
async def health_check():
    """Health check endpoint."""
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)
