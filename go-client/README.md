# Strix Go TUI Client

A terminal user interface (TUI) client for the Strix security scanner, written in Go using Bubble Tea and Lipgloss.

## Features

- **Real-time scan monitoring** with 4-panel dashboard
- **Event streaming** via HTTP NDJSON from backend
- **Vulnerability tracking** with severity coloring
- **Agent activity log** with token usage and tool execution details
- **Structured logging** to timestamped files for debugging

## Panels

- **Agents**: Shows status (🟢 completed, ⚪ running, ○ initialized, 🔴 failed) and iteration count for three roles (reconnaissance, exploitation, post_exploitation)
- **Activity**: Displays current target, scan mode, status, and elapsed time
- **Vulnerabilities**: Scrollable list of discovered vulnerabilities with severity-based coloring
- **Events**: Scrollable event log showing agent thinking, tool execution, token usage, and messages

## Build

```bash
cd go-client
go build -o bin/strix-tui ./cmd/strix-tui
```

## Usage

```bash
./bin/strix-tui \
  --target localhost \
  --mode quick \
  --backend http://localhost:8000 \
  --instruction "Penetration testing engagement" \
  --mock-tools
```

### CLI Flags

- `--target`, `-t`: Scan target (default: "localhost")
- `--mode`: Scan mode - quick, standard, or deep (default: "quick")
- `--backend`: Backend API URL (default: "http://localhost:8000")
- `--model`: LLM model to use (optional)
- `--mock-tools`: Use mock tools instead of real ones
- `--instruction`: Custom instruction for the scan
- `--skills`: Comma-separated list of skills to enable
- `--timeout`: Scan timeout in seconds (default: 120)
- `--verbose`: Enable verbose logging

### Keyboard Shortcuts

- `q` or `Ctrl+C`: Quit the application

## Logging

The TUI writes structured debug logs to timestamped files in the current directory:

```
tui_20240610_153045.log
```

Logs include all events received from the backend with timestamps and structured fields for debugging.

## Architecture

### Project Structure

```
go-client/
├── cmd/strix-tui/
│   └── main.go           # CLI entry point with cobra
├── internal/
│   ├── api/
│   │   ├── client.go     # HTTP client + event streaming
│   │   └── types.go      # Request/response types
│   ├── tui/
│   │   ├── model.go      # Bubble Tea model + event handling
│   │   ├── messages.go   # Custom Bubble Tea messages
│   │   ├── view.go       # Layout rendering
│   │   ├── panels.go     # Panel render functions
│   │   └── styles.go     # Lipgloss styles
│   └── logger/
│       └── logger.go     # Structured logging setup
├── go.mod
└── go.sum
```

### Event Streaming

The client uses Go channels to handle HTTP NDJSON streaming:

1. **StartScan** - POST `/scans` endpoint to initialize scan
2. **StreamEvents** - GET `/scans/{id}/events` returns NDJSON stream
3. **Channel-based reading** - Each event becomes a Bubble Tea message
4. **Recursive commands** - After handling each event, command returns to read next event

### State Management

Uses Bubble Tea's Elm Architecture:

- **Model**: Immutable state containing scan config, agents, vulnerabilities, logs
- **Update**: Message handlers that return new model + commands
- **View**: Pure rendering function that generates layout from current state

All events update the model immutably, triggering re-renders automatically.

## Supported Events

The TUI handles all 13 event types from the backend:

1. `scan_started` - Scan initialization
2. `scan_configured` - Tools and skills configured
3. `agent_started` - Agent phase begins
4. `agent_message` - Agent output
5. `agent_thinking` - LLM thinking process
6. `agent_token_usage` - Token consumption stats
7. `agent_completed` - Agent phase ends
8. `vulnerability_found` - New vulnerability discovered
9. `tool_executed` - Tool invocation result
10. `log_message` - Backend log message
11. `scan_completed` - Scan finished successfully
12. `scan_failed` - Scan encountered error

## Comparison with Python TUI

The Go TUI aims for feature parity with the Python Textual implementation:

- ✅ Same 4-panel layout (3 columns top, 1 full-width bottom)
- ✅ Identical event handling and data display
- ✅ Same CLI flags and parameters
- ✅ Matching color scheme and styling
- ✅ Same keyboard shortcuts
- ✅ Structured logging for debugging

Main advantages of Go version:

- Single static binary (no runtime dependencies)
- Lower memory footprint
- Faster startup and response times

## Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - Terminal styling
- `github.com/charmbracelet/bubbles` - Pre-built components (viewport)
- `github.com/spf13/cobra` - CLI framework
- `go.uber.org/zap` - Structured logging

## Testing

Run the TUI against the backend:

```bash
# Terminal 1: Start backend
cd ../strix-pydantic
uv run python -m strix_pydantic.service.backend

# Terminal 2: Run Go TUI
cd ../go-client
./bin/strix-tui --target localhost --mode quick --mock-tools
```

Expected behavior:

1. TUI displays initialization message
2. Agents panel shows reconnaissance/exploitation/post_exploitation roles
3. Activity panel shows target and scan status
4. Events panel shows real-time scan progress
5. Vulnerabilities panel populates as findings are discovered
6. Scan completes and shows total time + vulnerability count

## Future Enhancements

- [ ] Save/export scan results
- [ ] Filter and search vulnerabilities
- [ ] Replay mode for saved scans
- [ ] Multi-scan management
- [ ] Custom color themes
- [ ] Mouse support for panel interaction
