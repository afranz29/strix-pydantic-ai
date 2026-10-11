# Quick Start Guide - Go Strix TUI

## One-Minute Setup

```bash
cd go-client

# Build
go build -o bin/strix-tui ./cmd/strix-tui

# Run (ensure backend is running on localhost:8000)
./bin/strix-tui --target localhost --mode quick --mock-tools
```

## What You'll See

The TUI displays a 4-panel dashboard:

```
┌─────────────────────┬─────────────────────┬─────────────────────┐
│     Agents          │     Activity        │  Vulnerabilities    │
│                     │                     │                     │
│ ⚪ reconnaissance    │ Target: localhost   │ No vulnerabilities  │
│ ○ exploitation      │ Mode: quick         │ found yet...        │
│ ○ post_exploitation │ Status: running     │                     │
│                     │ Elapsed: 12s        │                     │
├─────────────────────────────────────────────────────────────────┤
│                         Events Log                              │
│                                                                 │
│ ✅ Scan started                                                 │
│ 🤖 reconnaissance started                                       │
│ 💭 Thinking: Analyzing target for potential vulnerabilities... │
│ 📊 Tokens → in: 1024, out: 256                                 │
│ 🔧 Tool: nmap_scan started                                     │
│ ✅ reconnaissance completed                                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## CLI Options

```bash
./bin/strix-tui --help

Flags:
      --backend string       Backend API URL (default "http://localhost:8000")
      --instruction string   Custom instruction (default "Penetration testing engagement")
      --mock-tools           Use mock tools instead of real ones
      --mode string          Scan mode (quick, standard, deep) (default "quick")
      --model string         LLM model to use (optional)
      --skills string        Comma-separated skills
  -t, --target string        Scan target (default "localhost")
      --timeout float        Scan timeout in seconds (default 120)
      --verbose              Enable verbose logging
```

## Common Tasks

### Quick scan with mock tools
```bash
./bin/strix-tui --target localhost --mode quick --mock-tools
```

### Standard scan against a real target
```bash
./bin/strix-tui --target example.com --mode standard
```

### Deep scan with specific skills
```bash
./bin/strix-tui --target example.com --mode deep --skills "reconnaissance,exploitation"
```

### Custom LLM model
```bash
./bin/strix-tui --target localhost --model claude-opus
```

## Keyboard Controls

- **q** or **Ctrl+C**: Exit the application

## Logs

Debug logs are written to timestamped files in the current directory:

```
tui_20240610_153045.log
```

Check these for event details and troubleshooting.

## Troubleshooting

**"Connection refused" error**
- Ensure the Strix backend is running: `cd ../strix-pydantic && uv run python -m strix_pydantic.service.backend`
- Check backend is on `http://localhost:8000` (or use `--backend` flag to specify different URL)

**Terminal size issues**
- Ensure your terminal is at least 40x15 characters
- The TUI will show "Terminal too small" if below minimum

**No events appearing**
- Check the log file for errors: `tail -f tui_*.log`
- Verify mock tools are enabled: `--mock-tools`
- Check backend logs for scan errors

## Next Steps

- Compare side-by-side with Python TUI: `cd ../strix-pydantic && ./start-tui.sh`
- Customize colors/styling in `internal/tui/styles.go`
- Add new features in `internal/tui/panels.go`
