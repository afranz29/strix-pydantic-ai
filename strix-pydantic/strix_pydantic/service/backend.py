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
    AgentMessageEvent,
    AgentThinkingEvent,
    AgentTokenUsageEvent,
    AgentStartedEvent,
    ScanCompletedEvent,
    ScanConfiguredEvent,
    ScanFailedEvent,
    ScanStartedEvent,
    ToolExecutedEvent,
    VulnerabilityFoundEvent,
    LogMessageEvent,
    ToolStartedEvent,
    ToolOutputEvent,
    GoalUpdatedEvent,
    SandboxStatusEvent,
    CostUpdatedEvent,
)

logger = logging.getLogger(__name__)


def setup_logging() -> None:
    """Configure logging to show all messages in stdout."""
    # Set root logger to DEBUG
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)

    # Remove any existing handlers
    for handler in root_logger.handlers[:]:
        root_logger.removeHandler(handler)

    # Create console handler with DEBUG level
    console_handler = logging.StreamHandler()
    console_handler.setLevel(logging.DEBUG)

    # Create formatter
    formatter = logging.Formatter(
        "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S"
    )
    console_handler.setFormatter(formatter)

    # Add handler to root logger
    root_logger.addHandler(console_handler)

    # Set specific loggers to DEBUG
    for logger_name in [
        "strix_pydantic",
        "strix_pydantic.service.backend",
        "strix_pydantic.agents.pydantic_orchestrator",
    ]:
        logging.getLogger(logger_name).setLevel(logging.DEBUG)


class QueueHandler(logging.Handler):
    """Logging handler that emits messages to an async queue."""

    def __init__(self, queue: asyncio.Queue, min_level: int = logging.INFO):
        """Initialize handler with target queue."""
        super().__init__(min_level)
        self.queue = queue

    def emit(self, record: logging.LogRecord) -> None:
        """Emit log record to queue."""
        try:
            # Convert log level to our event levels
            level_map = {
                logging.DEBUG: "info",
                logging.INFO: "info",
                logging.WARNING: "warning",
                logging.ERROR: "error",
                logging.CRITICAL: "error",
            }
            level = level_map.get(record.levelno, "info")
            message = self.format(record)

            # Put event on queue (non-blocking)
            self.queue.put_nowait({
                "type": "log_message",
                "level": level,
                "message": message,
            })
        except Exception:
            pass

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
    skills: str = ""
    timeout: float = 120.0
    verbose: bool = False


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


# Configure logging on startup
setup_logging()

app = fastapi.FastAPI(title="Strix Backend Service")

logger.debug("Backend initialized")


@app.post("/scans", response_model=ScanResponse)
async def start_scan(request: ScanRequest) -> ScanResponse:
    """Start a new security scan."""
    scan_id = f"run_{uuid.uuid4().hex[:12]}"

    logger.info(f"🚀 [API] Starting scan {scan_id}")
    logger.info(f"   Target: {request.target}")
    logger.info(f"   Scan mode: {request.scan_mode}")
    logger.info(f"   Model: {request.model or 'default'}")
    logger.info(f"   Mock tools: {request.mock_tools}")
    if request.instruction:
        logger.info(f"   Instruction: {request.instruction[:50]}...")

    # Create session
    session = ScanSession(scan_id, request)
    scans[scan_id] = session

    # Create event queue for this scan
    scan_queues[scan_id] = asyncio.Queue()

    # Start scan in background
    asyncio.create_task(_run_scan_background(scan_id, request))

    logger.info(f"✅ [API] Scan {scan_id} queued for background execution")

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
    logger.info(f"📡 [API] Event stream requested for {scan_id}")

    session = scans.get(scan_id)
    if not session:
        logger.warning(f"⚠️  [API] Scan {scan_id} not found. Available: {list(scans.keys())}")
        raise fastapi.HTTPException(status_code=404, detail="Scan not found")

    event_queue = scan_queues.get(scan_id)
    if not event_queue:
        logger.error(f"❌ [API] No event queue for {scan_id}. Available: {list(scan_queues.keys())}")
        raise fastapi.HTTPException(status_code=500, detail="No event queue")

    logger.info(f"✅ [API] Streaming events for {scan_id}")

    async def event_generator() -> AsyncGenerator[str, None]:
        """Generate events as newline-delimited JSON."""
        event_count = 0
        try:
            while True:
                # Get next event from queue
                event = await event_queue.get()
                event_count += 1

                # Handle both dict (from QueueHandler) and event objects (from emit_event)
                if isinstance(event, dict):
                    event_dict = event
                else:
                    event_dict = event.model_dump(mode="json")

                # Send as JSON line
                yield json.dumps(event_dict) + "\n"

                # Log major events
                event_type = event_dict.get("type", "unknown")
                if event_type == "scan_started":
                    logger.info(f"📊 [STREAM] Event {event_count}: scan_started")
                elif event_type == "agent_started":
                    logger.info(f"📊 [STREAM] Event {event_count}: agent_started role={event_dict.get('role')}")
                elif event_type == "agent_completed":
                    logger.info(f"📊 [STREAM] Event {event_count}: agent_completed role={event_dict.get('role')} vulns={event_dict.get('vulnerabilities_found')}")
                elif event_type == "scan_completed":
                    logger.info(f"📊 [STREAM] Event {event_count}: scan_completed ({event_dict.get('vulnerabilities_count')} vulns in {event_dict.get('duration_seconds'):.1f}s)")
                elif event_type == "scan_failed":
                    logger.error(f"📊 [STREAM] Event {event_count}: scan_failed - {event_dict.get('error')}")

                # Stop if scan completed
                if event_type in ("scan_completed", "scan_failed"):
                    logger.info(f"🏁 [STREAM] Stream ended for {scan_id} ({event_count} events)")
                    break

        except Exception as e:
            logger.error(f"❌ [STREAM] Event streaming error for scan {scan_id}: {e}")

    return StreamingResponse(event_generator(), media_type="application/x-ndjson")


async def _run_scan_background(scan_id: str, request: ScanRequest) -> None:
    """Run scan in background with event streaming."""
    session = scans[scan_id]
    event_queue = scan_queues[scan_id]
    start_time = time.time()

    logger.info(f"🔄 [BACKGROUND] Starting background scan {scan_id}")

    try:
        session.status = "running"

        # Setup logging handler to emit log messages to the event queue
        handler = QueueHandler(event_queue, min_level=logging.INFO)
        handler.setFormatter(logging.Formatter("%(message)s"))

        # Add handler to relevant loggers
        for logger_name in ["strix_pydantic.agents.pydantic_orchestrator", "strix_pydantic"]:
            module_logger = logging.getLogger(logger_name)
            module_logger.addHandler(handler)

        logger.info(f"✅ [BACKGROUND] Logging handlers configured for {scan_id}")

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
            elif event_type == "scan_configured":
                event = ScanConfiguredEvent(**payload)
            elif event_type == "agent_started":
                event = AgentStartedEvent(**payload)
            elif event_type == "agent_message":
                event = AgentMessageEvent(**payload)
            elif event_type == "agent_thinking":
                event = AgentThinkingEvent(**payload)
            elif event_type == "agent_token_usage":
                event = AgentTokenUsageEvent(**payload)
            elif event_type == "agent_completed":
                event = AgentCompletedEvent(**payload)
            elif event_type == "tool_executed":
                event = ToolExecutedEvent(**payload)
            elif event_type == "vulnerability_found":
                event = VulnerabilityFoundEvent(**payload)
            elif event_type == "log_message":
                event = LogMessageEvent(**payload)
            elif event_type == "scan_completed":
                event = ScanCompletedEvent(**payload)
            elif event_type == "scan_failed":
                event = ScanFailedEvent(**payload)
            elif event_type == "tool_started":
                event = ToolStartedEvent(**payload)
            elif event_type == "tool_output":
                event = ToolOutputEvent(**payload)
            elif event_type == "goal_updated":
                event = GoalUpdatedEvent(**payload)
            elif event_type == "sandbox_status":
                event = SandboxStatusEvent(**payload)
            elif event_type == "cost_updated":
                event = CostUpdatedEvent(**payload)
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
            await emit_event("goal_updated", {"goal_id": "bootstrap", "status": "in_progress"})
            if request.mock_tools:
                await emit_event("sandbox_status", {"status": "provisioning"})
                from strix_pydantic.tools.mock_tools import register_mock_tools
                sandbox_url = "http://127.0.0.1:48081"
                sandbox_client = None
                tool_registry = ToolRegistry()
                register_mock_tools(tool_registry)
                runtime = None
                sandbox_info = None
                await emit_event("sandbox_status", {"status": "ready", "sandbox_url": sandbox_url})
                await emit_event("goal_updated", {"goal_id": "bootstrap", "status": "completed"})
            else:
                run_id = scan_id
                await emit_event("sandbox_status", {"status": "provisioning"})
                sandbox_url, auth_token, runtime, sandbox_info = await initialize_sandbox(run_id, request.sandbox_url or None)
                sandbox_client = SandboxClient(
                    base_url=sandbox_url,
                    auth_token=auth_token,
                    execute_timeout=120.0,
                )
                tool_registry = ToolRegistry()
                await emit_event("sandbox_status", {"status": "ready", "sandbox_url": sandbox_url})
                await emit_event("goal_updated", {"goal_id": "bootstrap", "status": "completed"})

        except Exception as e:
            try:
                await emit_event("sandbox_status", {"status": "unreachable", "error": str(e)})
            except Exception:
                pass
            try:
                await emit_event("goal_updated", {"goal_id": "bootstrap", "status": "failed"})
            except Exception:
                pass
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

            if request.mock_tools:
                logger.info(f"📦 [TOOLS] Using mock tools (no Docker)")
            else:
                logger.info(f"📦 [TOOLS] Registering real tools to {sandbox_url}")
                _register_strix_tools(tool_registry)

            logger.info(f"✅ [TOOLS] Tool registry initialized with {len(tool_registry._tools)} tools")

            # Parse skills
            skill_list = []
            if request.skills:
                skill_list = [s.strip() for s in request.skills.split(",") if s.strip()]
                logger.info(f"🎯 [SKILLS] Active skills: {', '.join(skill_list)}")
            else:
                logger.info(f"🎯 [SKILLS] No specific skills requested")

            # Emit scan configured event
            await emit_event("scan_configured", {
                "tools_count": len(tool_registry._tools),
                "skills": skill_list,
                "sandbox_url": sandbox_url,
                "mock_tools": request.mock_tools,
            })

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
            logger.info(f"🤖 [BACKGROUND] Starting orchestrator for scan {scan_id}")
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
            # Count agent phases completed (typically 3: reconnaissance, exploitation, post_exploitation)
            iterations = len(state.agent_statuses) if state else 0

            logger.info(f"✅ [BACKGROUND] Scan {scan_id} completed successfully")
            logger.info(f"   Duration: {duration:.1f}s")
            logger.info(f"   Vulnerabilities: {vuln_count}")
            logger.info(f"   Iterations: {iterations}")

            await emit_event("scan_completed", {
                "duration_seconds": duration,
                "vulnerabilities_count": vuln_count,
                "iterations": iterations,
            })

        except Exception as e:
            logger.error(f"❌ [BACKGROUND] Scan {scan_id} orchestrator error: {e}", exc_info=True)
            session.status = "failed"
            session.error = str(e)

            duration = time.time() - start_time
            logger.error(f"   Duration before failure: {duration:.1f}s")
            await emit_event("scan_failed", {
                "error": str(e),
                "duration_seconds": duration,
            })

        finally:
            # Remove logging handler
            try:
                for logger_name in ["strix_pydantic.agents.pydantic_orchestrator", "strix_pydantic"]:
                    module_logger = logging.getLogger(logger_name)
                    module_logger.removeHandler(handler)
            except Exception:
                pass

            # Cleanup
            if not request.mock_tools and runtime and sandbox_info:
                try:
                    await emit_event("sandbox_status", {"status": "destroyed"})
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
