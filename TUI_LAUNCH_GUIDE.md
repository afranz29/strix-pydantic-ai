# Textual TUI Client - Launch Guide

## Quick Start (Recommended)

The easiest way to run the TUI is with a single command that automatically starts the backend and launches the client:

```bash
cd strix-pydantic
.venv/bin/python -m strix_pydantic.service.runner \
  --target http://localhost \
  --scan-mode quick \
  --mock-tools
```

This command:
- ✅ Starts the FastAPI backend service on port 8000
- ✅ Waits for the backend to be ready
- ✅ Launches the Textual TUI client
- ✅ Cleans up both processes when you exit

## Manual Approach (Two Terminals)

If you want to run the backend and client separately for debugging:

**Terminal 1 - Start the backend:**
```bash
cd strix-pydantic
.venv/bin/python -m strix_pydantic.service.backend
```

You should see:
```
INFO:     Application startup complete
INFO:     Uvicorn running on http://0.0.0.0:8000
```

**Terminal 2 - Launch the TUI client:**
```bash
cd strix-pydantic
.venv/bin/python -m strix_pydantic.service.tui_client \
  --target http://localhost \
  --scan-mode quick \
  --mock-tools
```

## Command Options

Both methods support these options:

- `--target` (required) - Target URL to scan (e.g., `http://localhost`, `http://example.com`)
- `--scan-mode` - Scan intensity: `quick` (fast), `standard` (default), `deep` (thorough)
- `--model` - Override LLM model (optional)
- `--backend-url` - Backend service URL (default: `http://localhost:8000`)
- `--mock-tools` - Use mock tools for testing (no Docker required)

## TUI Controls

Once the TUI launches, you'll see four panels arranged in a dashboard:

**Left Column:**
- **Top-Left: Agents Panel** [50% height]: Shows agent phase status
  - `○` = initialized
  - `⚪` = running
  - `🟢` = completed
  - Iteration counter for each agent
  
- **Top-Right: Activity Panel** [50% height]: Shows scan progress
  - Target URL
  - Scan mode
  - Current status
  - Elapsed time counter

- **Bottom-Left: Agent Activity Panel**: Real-time agent reasoning and tool execution
  - Agent planning and decisions
  - Tool calls executed (e.g., "Executed: scan_nmap")
  - Completion status with vulnerability counts
  - Color-coded by message level (info/success/warning/error)
  - Scrollable history

**Right Panel:**
- **Vulnerabilities**: Real-time vulnerability discoveries
  - Color-coded by severity (critical/high/medium/low/info)
  - Scrollable list
  - Full details on each finding

**Keyboard Shortcuts:**
- `q` - Quit the TUI
- `↑/↓` - Scroll vulnerabilities
- `d` - Show vulnerability details (coming soon)

## Examples

### Quick scan with mock tools (no Docker):
```bash
.venv/bin/python -m strix_pydantic.service.runner \
  --target http://localhost \
  --scan-mode quick \
  --mock-tools
```

### Standard scan with custom model:
```bash
.venv/bin/python -m strix_pydantic.service.runner \
  --target http://example.com \
  --scan-mode standard \
  --model claude-sonnet-4-6
```

### Deep scan with real Docker sandbox:
```bash
.venv/bin/python -m strix_pydantic.service.runner \
  --target http://testphp.vulnweb.com \
  --scan-mode deep
```

## Troubleshooting

### "Backend failed to start"
- Make sure port 8000 is not in use: `lsof -i :8000`
- Check Python dependencies are installed: `pip install -e .`

### "Connection refused"
- Verify backend is running and listening on the correct port
- Check firewall settings if running remotely

### TUI looks garbled or has display issues
- Try resizing your terminal window
- Ensure terminal supports 256 colors

### Text is hard to read
- The TUI uses your terminal's color scheme
- Try a terminal with better contrast (darker background preferred)

## Architecture

```
┌─────────────────────────────────────────┐
│   strix-tui-standalone (runner.py)      │
│   ↓                                     │
│   Starts backend  →  Waits for ready    │
│   ↓                                     │
│   Launches TUI client                   │
│   ↓                                     │
└─────────────────────────────────────────┘
         ↓
┌──────────────────────────────────────────────────┐
│         FastAPI Backend (port 8000)              │
│  ┌────────────────────────────────────────┐     │
│  │  Orchestrator + Agent Execution        │     │
│  │  ↓                                     │     │
│  │  Events (agent_message, tool_executed) │     │
│  │  ↓                                     │     │
│  │  HTTP Streaming (NDJSON)               │     │
│  └────────────────────────────────────────┘     │
└──────────────────────────────────────────────────┘
         ↑
         │ REST + HTTP Streaming
         ↓
┌──────────────────────────────────────────────────────────────┐
│         Textual TUI Client                                  │
│  ┌────────────────────┬────────────┐  ┌──────────────────┐  │
│  │    Agents          │  Activity  │  │                  │  │
│  │                    │            │  │                  │  │
│  ├────────────────────┴────────────┤  │ Vulnerabilities  │  │
│  │                                  │  │                  │  │
│  │    Agent Activity (Reasoning)    │  │                  │  │
│  │                                  │  │                  │  │
│  └────────────────────────────────┘  └──────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

## Next Steps

The TUI is fully functional for monitoring scans. Future enhancements could include:
- Interactive vulnerability detail modal
- Filter/search vulnerabilities
- Export scan results
- Connection retry logic
- Performance metrics display
