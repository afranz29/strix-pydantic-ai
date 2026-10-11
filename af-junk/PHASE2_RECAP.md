# Phase 2: Backend Service Architecture - Recap

## The Goal

You wanted to improve Strix's UI with Textual, but ran into threading constraints. Rather than wrestling with Textual in daemon threads, we pivoted to a completely different architecture: **decouple the UI from the orchestrator using a backend service**.

## What We Built - Phase 2 Architecture

### 1. FastAPI Backend Service (`service/backend.py`)

- **Runs on port 8000** as a headless API (no web UI)
- **Receives scan requests** via `POST /scans`
- **Orchestrator runs in a background task** (no threading - pure async)
- **Events are emitted into queues** as they occur
- **Provides HTTP streaming endpoint** `GET /scans/{id}/events` for real-time event consumption
- **In-memory scan storage** for session management

### 2. Async CLI Client (`service/cli_client.py`)

- **Connects to backend** via REST + HTTP streaming
- **Starts scan** with `POST /scans` → gets `scan_id`
- **Streams events** with `GET /scans/{id}/events` (newline-delimited JSON)
- **Renders everything beautifully** with Rich panels:
  - Color-coded severity badges (red=critical, orange=high, yellow=medium, blue=low)
  - Vulnerability titles and descriptions in formatted panels
  - Final scan summary with statistics

### 3. Event Schema (`service/events.py`)

- **Type-safe Pydantic models** for each event type
- Events include: `ScanStartedEvent`, `AgentStartedEvent`, `VulnerabilityFoundEvent`, `AgentCompletedEvent`, `ScanCompletedEvent`, `ScanFailedEvent`
- **Validates all events** before sending
- Ensures data consistency across the service layer

## Why No Output at localhost:8000

The backend is intentionally **headless** - it's just an API service, not a web server with a UI. There's no web page to visit because:

- **The CLI client is the consumer** of the API
- **You could build other UIs later** if desired (React web app, Textual TUI wrapper, etc.)
- **Multiple clients can connect** to the same backend simultaneously
- **Separation of concerns** - backend orchestrates, client displays

## Vulnerability Reporting

The vulnerabilities are clean and specific. Each vulnerability shows:

```
╭─────────────────────────────── Vulnerability ────────────────────────────────╮
│ (CRITICAL) Default Credentials (admin:admin)                                 │
│ Admin panel accessible with default credentials admin:admin. No credentials  │
│ change enforced on initial setup.                                            │
╰──────────────────────────────────────────────────────────────────────────────╯
```

- **Severity badge** with color coding
- **Title** of the vulnerability
- **Detailed description** of the issue


## The Real Achievement

✅ **Eliminated threading entirely** by moving to a pure async architecture with a backend service.

**Before**: Textual UI trying to run in a daemon thread → signal handler constraints → complexity  
**After**: Async orchestrator running in background task + async CLI client consuming events → clean, simple, no threading

## Key Technical Decisions

| Decision | Reasoning |
|----------|-----------|
| HTTP streaming instead of WebSocket | Simpler with httpx, more compatible, no ws library needed |
| Backend completely headless | API-first design allows multiple UI clients later |
| In-memory queues for events | Fast, simple for MVP; can add persistence layer later |
| Newline-delimited JSON format | Standard for streaming, easy to parse line-by-line |
| No web page at :8000 | Not needed - CLI client is primary consumer |

## How to Test

### Terminal 1: Start Backend
```bash
cd strix-pydantic
python -m strix_pydantic.service.backend
```

Should see:
```
INFO:     Application startup complete
INFO:     Uvicorn running on http://0.0.0.0:8000
```

### Terminal 2: Run CLI Client
```bash
cd strix-pydantic
python -m strix_pydantic.service.cli_client \
  --target http://localhost \
  --scan-mode quick \
  --mock-tools \
  --backend-url http://localhost:8000
```

Should see:
- Scan starts
- Events stream in real-time
- Vulnerabilities render in colored panels
- Final summary with statistics

## What's Working ✅

- FastAPI backend service on port 8000
- HTTP streaming endpoint for events (newline-delimited JSON)
- Async CLI client connecting and consuming events
- Real-time vulnerability discovery and display with Rich formatting
- Scan completion with statistics
- Mock tools support (no Docker needed for testing)

## Known Limitations

- No concurrent scan support yet (should work but untested)
- No error recovery if backend crashes mid-scan
- No database persistence (scans stored in-memory only)
- No web UI (intentional - API first)

## Next Steps

1. Test with real Docker sandbox (remove `--mock-tools`)
2. Test concurrent scans from multiple clients
3. Add error scenario testing
4. Consider building web UI client if needed
5. Add persistence layer if scans need to survive restarts

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    FastAPI Backend (port 8000)              │
├─────────────────────────────────────────────────────────────┤
│  POST /scans              → Start scan, return scan_id       │
│  GET /scans/{id}          → Get scan status                  │
│  GET /scans/{id}/events   → Stream events (HTTP)             │
└─────────────────────────────────────────────────────────────┘
         ▲
         │ REST + HTTP Streaming
         │
┌─────────────────────────────────────────────────────────────┐
│                    CLI Client (httpx)                       │
├─────────────────────────────────────────────────────────────┤
│  1. POST to start scan                                       │
│  2. Stream events via HTTP GET                               │
│  3. Render with Rich (panels, colors, formatting)            │
└─────────────────────────────────────────────────────────────┘
         ▲
         │ Terminal Output
         │
    [ User Terminal ]
```

## Why This Design Wins

1. **No threading complexity** - pure async/await throughout
2. **Decoupled UI from orchestrator** - build multiple clients later
3. **Real-time feedback** - events stream as they happen
4. **Extensible** - easy to add web UI, Textual TUI, metrics dashboard
5. **Maintainable** - clear separation: backend logic vs. client display
6. **Testable** - can test backend and client independently
