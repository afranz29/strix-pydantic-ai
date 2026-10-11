# Strix Service Architecture

## Overview

Strix now uses a **distributed service architecture** with a FastAPI backend and async CLI client. This decouples the orchestrator from the UI, enabling multiple clients, better testability, and cleaner code.

```
┌────────────────────────────────────────────────┐
│   FastAPI Backend Service                      │
│   (strix_pydantic/service/backend.py)          │
│                                                 │
│   • Manages scan lifecycle                     │
│   • Runs orchestrator                          │
│   • Streams events via WebSocket               │
└────────────────────────────────────────────────┘
         ↑                          ↑
    REST API              WebSocket Events
         |                          |
  ┌──────┴──────────────────────────┴──────┐
  │  CLI Client (async)                     │
  │  (strix_pydantic/service/cli_client.py) │
  │                                         │
  │  • Connects to backend                 │
  │  • Streams events                      │
  │  • Renders Rich console UI             │
  └─────────────────────────────────────────┘
```

## Architecture Components

### 1. Event Schema (`service/events.py`)

Strongly-typed Pydantic models for all scan events:

```python
# Event types
- ScanStartedEvent
- AgentStartedEvent
- AgentCompletedEvent
- VulnerabilityFoundEvent
- ToolExecutedEvent
- LogMessageEvent
- ScanCompletedEvent
- ScanFailedEvent
```

All events include:
- `type`: Event type (enum)
- `timestamp`: UTC timestamp
- `scan_id`: Which scan this event belongs to
- Type-specific fields (e.g., `role`, `title`, `severity`)

### 2. FastAPI Backend (`service/backend.py`)

Async backend service that:

**Endpoints:**
- `POST /scans` - Start a new scan
  - Request: `{target, scan_mode, model, mock_tools, confirm, instruction}`
  - Response: `{scan_id, target, scan_mode}`
  
- `GET /scans/{id}` - Get scan status
  - Response: Status, progress, vulnerabilities count
  
- `GET /scans/{id}/events` - WebSocket endpoint for event streaming
  - Streams events as JSON lines

**Features:**
- In-memory scan sessions (`ScanSession` class)
- Async event queues per scan (allows multiple concurrent scans)
- Non-blocking scan execution (background tasks)
- Event emitter callback pattern

**Health Check:**
- `GET /health` - Service availability

### 3. Async CLI Client (`service/cli_client.py`)

Async CLI that:

**Features:**
- Connects to backend service
- Makes REST call to start scan
- Opens WebSocket for event streaming
- Updates Rich console UI based on events
- Tracks agent status, vulnerabilities, output

**CLI Options:**
```bash
strix-client \
  --target http://example.com \
  --scan-mode standard \
  --model gpt-4o \
  --mock-tools \
  --backend-url http://localhost:8000 \
  --ui/--no-ui
```

## Event Flow

### Starting a Scan

```
1. CLI: POST /scans
   → Backend creates ScanSession
   → Starts background orchestrator task
   → Returns scan_id

2. CLI: WebSocket connect /scans/{id}/events
   → Backend accepts connection
   → Starts streaming events

3. Backend: Orchestrator runs
   → Emits: scan_started
   → Emits: agent_started (reconnaissance)
   → Emits: vulnerability_found (x N)
   → Emits: agent_completed
   → Emits: agent_started (exploitation)
   → ... more events ...
   → Emits: scan_completed

4. CLI: Receives events
   → Updates state
   → Renders to console
   → Closes connection on scan_completed
```

### Example Event Stream

```json
{"type": "scan_started", "scan_id": "run_abc", "target": "localhost", "scan_mode": "standard"}
{"type": "agent_started", "scan_id": "run_abc", "role": "reconnaissance", "iteration": 1}
{"type": "vulnerability_found", "scan_id": "run_abc", "role": "reconnaissance", "title": "SQL Injection", "severity": "critical"}
{"type": "vulnerability_found", "scan_id": "run_abc", "role": "reconnaissance", "title": "XSS", "severity": "high"}
{"type": "agent_completed", "scan_id": "run_abc", "role": "reconnaissance", "vulnerabilities_found": 2}
{"type": "agent_started", "scan_id": "run_abc", "role": "exploitation", "iteration": 2}
{"type": "scan_completed", "scan_id": "run_abc", "duration_seconds": 45, "vulnerabilities_count": 2}
```

## Running the Service

### Terminal 1: Start Backend Service

```bash
# With uv
uv run --project strix-pydantic python -m strix_pydantic.service.backend

# Or with pip
cd strix-pydantic
python -m strix_pydantic.service.backend
```

Service will start on `http://localhost:8000`

**Available at:**
- REST API: `http://localhost:8000`
- API Docs: `http://localhost:8000/docs`
- Events: `ws://localhost:8000/scans/{id}/events`

### Terminal 2: Run CLI Client

```bash
# With uv
uv run --project strix-pydantic python -m strix_pydantic.service.cli_client \
  --target http://example.com \
  --scan-mode standard \
  --backend-url http://localhost:8000

# Or with pip
cd strix-pydantic
python -m strix_pydantic.service.cli_client --target http://example.com
```

## API Documentation

### Start Scan

```
POST /scans
Content-Type: application/json

{
  "target": "http://example.com",
  "scan_mode": "standard",
  "model": "claude-haiku-4-5",
  "mock_tools": false,
  "confirm": false,
  "instruction": ""
}

Response (200):
{
  "scan_id": "run_abc123",
  "target": "http://example.com",
  "scan_mode": "standard"
}
```

### Get Scan Status

```
GET /scans/run_abc123

Response (200):
{
  "scan_id": "run_abc123",
  "status": "running",
  "target": "http://example.com",
  "scan_mode": "standard",
  "start_time": 1718000000.0,
  "duration_seconds": 45.2,
  "error": null,
  "vulnerabilities": 3
}
```

### Stream Events (WebSocket)

```
WebSocket Connection: ws://localhost:8000/scans/run_abc123/events

Incoming (JSON-formatted events):
{"type": "scan_started", ...}
{"type": "agent_started", ...}
{"type": "vulnerability_found", ...}
...
{"type": "scan_completed", ...}
```

## Implementation Notes

### Async/Await Usage

- **Backend**: Fully async with FastAPI
  - Event emission: `await emit_event(...)`
  - Orchestrator integration: `await orchestrator(...)`
  
- **CLI**: Async/await for WebSocket
  - Event streaming: `async for event in ws: ...`
  - Clean, linear flow

### Thread Safety

No threading needed! Design eliminates threading complexity:
- Backend runs orchestrator in async context (no threads)
- CLI uses async/await (no threads)
- Events communicated via WebSocket (no shared state)

### Scalability

Current design supports:
- ✅ Multiple concurrent scans (one per session)
- ✅ Multiple CLI clients connecting to same backend
- ✅ Historical scan storage (in-memory, can add DB)
- ✅ Easy to extend with more event types

### Error Handling

- **Backend**: Emits `scan_failed` event with error message
- **CLI**: Catches WebSocket errors, displays to user
- **Graceful**: Clients can reconnect mid-scan

## Future Enhancements

### Phase 2: Persistent Storage
- SQLite database for historical scans
- Resume interrupted scans
- Scan comparison and trending

### Phase 3: Web UI
- React/Vue frontend connecting to same backend
- Real-time dashboard
- Scan visualization

### Phase 4: Advanced Features
- Multi-target scanning
- Scan scheduling
- Concurrent agent execution
- Custom agent plugins

## File Structure

```
strix-pydantic/
├── strix_pydantic/
│   └── service/
│       ├── __init__.py
│       ├── events.py           # Event schema
│       ├── backend.py          # FastAPI backend
│       └── cli_client.py       # Async CLI client
├── pyproject.toml              # Dependencies (fastapi, uvicorn)
└── SERVICE_ARCHITECTURE.md     # This file
```

## Debugging

### View API Docs

```
http://localhost:8000/docs
```

Swagger UI for interactive API testing.

### Check Service Health

```bash
curl http://localhost:8000/health
```

### Monitor Events

```bash
# In Python
import httpx
import json

async def monitor_scan(scan_id):
    async with httpx.AsyncClient() as client:
        async with client.stream("GET", f"ws://localhost:8000/scans/{scan_id}/events") as ws:
            async for line in ws.aiter_lines():
                event = json.loads(line)
                print(event)

import asyncio
asyncio.run(monitor_scan("run_abc123"))
```

## Testing

### Test Backend Service

```bash
# With pytest
cd strix-pydantic
pytest tests/service/test_backend.py -v

# Or manually
python -c "
import asyncio
from strix_pydantic.service.cli_client import ScanClient

async def test():
    client = ScanClient()
    await client.run_scan('http://localhost', mock_tools=True)

asyncio.run(test())
"
```

## Migration from Old TUI

The old in-process TUI (`strix_pydantic/interface/tui.py`) is now **deprecated**. New code uses the service architecture:

**Old:** `strix-pydantic --target http://example.com --ui`  
**New:** 
```bash
# Terminal 1: Start backend
python -m strix_pydantic.service.backend

# Terminal 2: Run client
python -m strix_pydantic.service.cli_client --target http://example.com
```

The legacy CLI is still available for backward compatibility but is not being enhanced.
