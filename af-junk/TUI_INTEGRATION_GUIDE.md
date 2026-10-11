# Textual TUI Integration Guide

## Overview

The new Textual-based TUI provides a professional three-panel dashboard for real-time visibility into security scans:

```
┌──────────────────┬──────────────────┬──────────────────────────┐
│     AGENTS       │     ACTIVITY     │       RESULTS            │
│                  │                  │                          │
│ ✓ reconnaissance │ Running phase:   │ ✅ Initialized recon     │
│ → exploitation   │ exploitation     │ 🔧 Running gather_info   │
│ ○ post_exploit   │                  │ 🚨 [CRITICAL]            │
│                  │ Active tools:    │    SQL Injection         │
│                  │  • gather_info   │ [CRITICAL] Default...    │
│                  │  • check_vuln    │ 🟡 [HIGH] Weak policy   │
│                  │                  │                          │
│                  │ Elapsed: 00:45   │ [Scrollable, 20 visible] │
└──────────────────┴──────────────────┴──────────────────────────┘
```

## Architecture

### Three Panels

#### **Left Panel: AgentsPanel**
- Shows all three agent roles: `reconnaissance`, `exploitation`, `post_exploitation`
- Status indicators:
  - `✓` = completed (green)
  - `→` = currently running (cyan bold)
  - `○` = initialized/waiting (dim)
- Iteration counter for debugging
- Active agent is marked with `>`

#### **Middle Panel: ActivityPanel**
- Current status message
- List of currently executing tools (last 5 shown)
- Elapsed time counter (MM:SS format)
- Real-time feedback on what's happening

#### **Right Panel: OutputPanel**
- Scrollable results buffer (last 20 visible, 100 total in memory)
- Color-coded by severity:
  - Red: Critical, High severity
  - Yellow: Medium, warnings
  - Green: Success, completion
  - Cyan: Info, activity
- Shows vulnerabilities, notes, and agent findings

### Data Flow

```
CLI (strix_pydantic/interface/cli.py)
  ↓
  StrixTUIApp (wrapper for non-UI mode)
  ↓
  StrixProgressApp (manages Textual app lifecycle)
  ↓
  StrixDashboard (Textual App with 3 panels)
  ↓
  Updates render in real-time
```

## Integration Points

### 1. CLI Updates (Already Done)
```python
# Add to cli.py after TUI initialization
tui_app = StrixTUIApp(use_ui=ui)
if ui:
    tui_app.start()
```

### 2. Orchestrator Integration (TODO)
In `pydantic_orchestrator.py`, add callbacks:

```python
# At start of each agent role
deps.on_agent_start(role)  # Updates left panel

# For each tool execution
deps.on_tool_start(tool_name)  # Adds to middle panel
deps.on_tool_complete(tool_name)  # Removes from middle panel

# On vulnerabilities found
deps.on_vulnerability(vuln)  # Adds to right panel
```

### 3. Background Timer
The elapsed time counter runs in a daemon thread:
```python
def _tick_elapsed(self):
    while self._running:
        time.sleep(1)
        self.tick_elapsed_time()
```

## Usage

### Basic Usage
```bash
./strix.sh --target http://example.com --ui              # TUI enabled
./strix.sh --target http://example.com --no-ui           # TUI disabled
./strix.sh --target http://example.com --mock-tools --ui # Fast test with UI
```

### Keyboard Controls
- `q` - Quit the application
- Arrow keys - Scroll within panels (if implemented)
- Mouse - Click to interact (if implemented)

## Implementation Details

### Thread Safety
- The Textual app runs in a daemon thread separate from the orchestrator
- All updates use reactive properties (thread-safe)
- No blocking operations in the main update loop

### Performance Considerations
- Panels render only when data changes
- Output buffer limited to 100 lines to prevent memory bloat
- Active tools list limited to 5 most recent
- No polling - only event-driven updates

### CSS Styling
```
Agents Panel:      25% width, blue border
Activity Panel:    25% width, yellow border
Results Panel:     50% width, green border
```

The layout adapts to terminal width automatically via Textual's layout system.

## Future Enhancements

### Phase 1 (Current)
- ✅ Three-panel layout
- ✅ Agent status tracking
- ✅ Activity display
- ✅ Results output
- ✅ Elapsed time

### Phase 2 (Possible)
- Click on agent to drill down into details
- Filter/search results
- Export scan results from TUI
- Pause/resume functionality
- Real-time vulnerability severity filtering

### Phase 3 (Advanced)
- Streaming LLM output display
- Tool execution timeline
- Memory/CPU usage graphs
- Network traffic visualization

## Troubleshooting

### TUI doesn't appear
1. Check `--ui` flag is set: `./strix.sh --target X --ui`
2. Verify textual is installed: `pip install textual>=1.0.0`
3. Check terminal supports 256 colors

### Text wrapping issues
- Textual handles terminal width automatically
- Try resizing terminal or maximizing window

### Performance problems
- Limit output to 100 most recent lines (already done)
- Disable verbose logging with `--no-verbose`
- Results panel overflow: fold is enabled

## Testing

Test without full dependencies:
```bash
python3 -c "
from strix_pydantic.interface.tui import StrixTUIApp
app = StrixTUIApp(use_ui=True)
app.start()
app.update_agent_status('reconnaissance', 'running')
app.update_activity('Testing activity panel')
app.add_log('Test output', 'success')
app.show_vulnerability('Test', 'critical', 'Test vulnerability')
import time; time.sleep(5)
app.stop()
"
```

## Architecture Decisions

### Why Textual + Threading?
- Textual provides professional UI with minimal code
- Threading allows scan to continue while UI updates
- Reactive properties make updates thread-safe

### Why Not Full Textual Rewrite?
- Orchestrator uses async/await
- Textual's event loop conflicts with asyncio
- Threading provides clean separation
- Can keep existing CLI logic unchanged

### Why Three Panels?
- Agents: Shows progress through three-phase workflow
- Activity: Immediate feedback on what's running
- Results: Accumulates findings without losing prior output

This separates concerns and makes it easy to understand scan state at a glance.
