# Phase 2 Integration - Testing Guide

The orchestrator is now fully integrated with the FastAPI backend service. Here's how to test it end-to-end.

## Quick Start (Mock Mode - Fastest)

This runs with mock tools, no Docker needed:

### Terminal 1: Start Backend Service

```bash
cd strix-pydantic
python -m strix_pydantic.service.backend
```

You should see:
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

### Expected Output

```
📋 Starting scan for http://localhost
✓ Scan started: run_abc123

🚀 Scan started
   Target: http://localhost
   Mode: quick

🤖 Starting reconnaissance (iteration 1)
✅ reconnaissance completed (0 vulnerabilities)

🤖 Starting exploitation (iteration 2)
✅ exploitation completed (0 vulnerabilities)

🤖 Starting post_exploitation (iteration 3)
✅ post_exploitation completed (0 vulnerabilities)

============================================================
✅ Scan completed successfully
   Duration: 45.2s
   Vulnerabilities: 0
   Iterations: 3
============================================================
```

## Testing Different Modes

### Test with Custom Model

```bash
python -m strix_pydantic.service.cli_client \
  --target http://localhost \
  --model claude-haiku-4-5 \
  --mock-tools
```

### Test with Standard Scan Mode

```bash
python -m strix_pydantic.service.cli_client \
  --target http://example.com \
  --scan-mode standard \
  --mock-tools
```

### Test with Deep Scan Mode

```bash
python -m strix_pydantic.service.cli_client \
  --target http://example.com \
  --scan-mode deep \
  --mock-tools
```

## Testing API Directly

### Start Scan via REST

```bash
curl -X POST http://localhost:8000/scans \
  -H "Content-Type: application/json" \
  -d '{
    "target": "http://example.com",
    "scan_mode": "quick",
    "mock_tools": true
  }'
```

Response:
```json
{
  "scan_id": "run_abc123",
  "target": "http://example.com",
  "scan_mode": "quick"
}
```

### Check Scan Status

```bash
curl http://localhost:8000/scans/run_abc123
```

Response:
```json
{
  "scan_id": "run_abc123",
  "status": "running",
  "target": "http://example.com",
  "scan_mode": "quick",
  "start_time": 1234567890.0,
  "duration_seconds": 5.2,
  "error": null,
  "vulnerabilities": 0
}
```

### Stream Events via WebSocket

```bash
# Using websocat
websocat ws://localhost:8000/scans/run_abc123/events

# Or using Python
python -c "
import asyncio
import httpx
import json

async def stream():
    async with httpx.AsyncClient() as client:
        async with client.stream('GET', 'ws://localhost:8000/scans/run_abc123/events') as response:
            async for line in response.aiter_lines():
                print(json.dumps(json.loads(line), indent=2))

asyncio.run(stream())
"
```

## Event Flow Test

Watch events stream through the system:

### Terminal 1: Backend
```bash
python -m strix_pydantic.service.backend
```

### Terminal 2: Monitor Events
```bash
python -c "
import asyncio
import httpx
import json

async def monitor():
    async with httpx.AsyncClient() as client:
        response = await client.post(
            'http://localhost:8000/scans',
            json={'target': 'http://localhost', 'mock_tools': True}
        )
        scan_id = response.json()['scan_id']
        print(f'Scan started: {scan_id}\n')
        
        async with client.stream('GET', f'ws://localhost:8000/scans/{scan_id}/events') as ws:
            async for line in ws.aiter_lines():
                event = json.loads(line)
                print(f'{event[\"type\"]:25} {event.get(\"role\", \"\")}')
                if event['type'] == 'scan_completed':
                    print(f'Duration: {event[\"duration_seconds\"]:.1f}s')
                    break

asyncio.run(monitor())
"
```

### Terminal 3: CLI Client
```bash
python -m strix_pydantic.service.cli_client \
  --target http://localhost \
  --mock-tools
```

## Verification Checklist

After running the tests, verify:

- [ ] Backend starts without errors
- [ ] CLI connects and says "Scan started"
- [ ] Events stream in real-time (no delay)
- [ ] Agent phases show (reconnaissance, exploitation, post_exploitation)
- [ ] Final summary shows correct stats
- [ ] No Python exceptions in either terminal
- [ ] WebSocket connection closes cleanly
- [ ] Status endpoint returns current scan state

## Debugging

### Check Logs

Backend logs will show:
```
INFO [BOOTSTRAP] Run run_abc running
INFO [AGENT] reconnaissance (iteration 1)
INFO ✅ [AGENT] reconnaissance completed
```

### Test Event Emission

Add a quick print to verify events are emitting:

```python
# In service/backend.py, in emit_event function
print(f"Emitting: {event_type}")
```

### Test WebSocket Connection

```bash
# Simple test
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
  http://localhost:8000/scans/run_abc123/events
```

## Common Issues

### "Connection refused"
- Make sure backend is running on port 8000
- Check no firewall is blocking

### "WebSocket connection closed"
- Check backend logs for exceptions
- Verify event_emitter is being called

### "Scan status shows running but no progress"
- Backend might be stuck in orchestrator
- Check it's not waiting for user input
- Try with --mock-tools

## Next Steps

Once Phase 2 is working:

1. Test with **real Docker sandbox** (remove `--mock-tools`)
   - Requires Docker running
   - Will take longer but tests full integration

2. Add **error scenarios**
   - Invalid target
   - Bad model name
   - Sandbox failure

3. Test **concurrent scans**
   - Start multiple scans from different terminals
   - Each should run independently

4. Performance testing
   - Measure event latency
   - Monitor memory usage
   - Test long-running scans

## Status

✅ Phase 2 Implementation Complete
✅ Orchestrator Integration Done
⏳ Manual Testing Pending
⏳ Real Docker Testing Pending

You're ready to test!
