# Plan: Build Textual TUI Client for Backend Service

## Context

User originally wanted to use Textual for an interactive TUI but ran into threading constraints when running Textual in daemon threads. We solved this by creating a FastAPI backend service (Phase 2) that decouples the orchestrator from the UI using HTTP event streaming.

**What We've Built So Far:**
- FastAPI backend service (`service/backend.py`) - runs orchestrator, emits events via HTTP streaming
- Async CLI client (`service/cli_client.py`) - connects to backend, displays events with Rich formatting
- Event schema (`service/events.py`) - typed Pydantic models for scan events

**What's Missing:**
The CLI client is passive (just prints output). We haven't built the **interactive Textual TUI** that was the original goal. The backend architecture now makes this trivial - no threading constraints because the TUI client just consumes HTTP events like the CLI client does.

**Why Build This:**
- Original goal was Textual for interactivity and real-time feedback
- Backend provides perfect foundation (no threading issues)
- TUI can provide scrollable vulnerability list, detail views, keyboard navigation
- Multiple UI clients can connect to same backend simultaneously

## Recommended Approach

Create a **new standalone TUI client** (`service/tui_client.py`) that connects to the existing FastAPI backend service via the same REST + HTTP streaming API as the CLI client.

### Why New Standalone Client (Not Adapting Existing TUI)

**Why not adapt existing `strix_pydantic/interface/tui.py`?**
- Existing TUI (342 lines) is tightly coupled to orchestrator with direct method calls
- Backend uses HTTP streaming with JSON events, not synchronous callbacks
- Standalone client matches architecture where backend runs separately
- Clean separation of concerns

### File Location
```
strix-pydantic/strix_pydantic/service/tui_client.py  (~580 LOC estimated)
strix-pydantic/strix_pydantic/service/assets/tui_styles.tcss  (~100 lines)
```

Entry point in `pyproject.toml`:
```toml
[project.scripts]
strix-tui = "strix_pydantic.service.tui_client:main"
```

## Component Design

### 1. Main Application - `ScanTUIApp(App)` (~150 LOC)

- Three-panel horizontal layout: Agents (25%) | Activity (25%) | Vulnerabilities (50%)
- Reactive properties for thread-safe state management
- Background worker for HTTP streaming (Textual native async, NOT threading)
- Keybindings: q (quit), d (detail), up/down (scroll)

### 2. Widget Components

**AgentsPanel (~60 LOC)** - Shows reconnaissance, exploitation, post_exploitation status with icons (✓/→/○)  
**ActivityPanel (~50 LOC)** - Displays target, scan mode, current status, elapsed timer  
**VulnerabilitiesPanel (~80 LOC)** - Scrollable list of color-coded vulnerability cards  
**VulnerabilityDetailModal (~60 LOC)** - Popup with full vulnerability details  

### 3. Event Stream Handler (~100 LOC)

Background worker that:
1. POSTs to `/scans` to get scan_id
2. Streams events from `/scans/{scan_id}/events` with `timeout=None`
3. Parses newline-delimited JSON
4. Updates reactive properties to trigger UI refresh

**Key Pattern:** Reuses exact HTTP streaming from CLI client (`cli_client.py:81-107`). No threading needed because Textual workers provide async context.

## Implementation Sequence

### Step 1: App Structure & Layout
- Create `tui_client.py` with `ScanTUIApp` class
- Define three panel widgets (AgentsPanel, ActivityPanel, VulnerabilitiesPanel)
- Create `tui_styles.tcss` with layout
- Add CLI entry point with Click
- **Verify:** TUI launches with static panels

### Step 2: Event Streaming
- Implement `start_scan_and_stream()` worker method
- Add event parsing and `handle_event()` dispatcher
- Wire up reactive property updates
- **Verify:** TUI connects to backend and shows real-time updates

### Step 3: Vulnerability Display
- Implement vulnerability card rendering in VulnerabilitiesPanel
- Add severity color coding
- Make panel scrollable with keyboard
- **Verify:** Vulnerabilities display as discovered

### Step 4: Interactive Features
- Implement VulnerabilityDetailModal
- Add keyboard navigation (up/down, Enter for detail, q to quit)
- Add elapsed time counter
- **Verify:** Full interactivity working

### Step 5: Polish & Error Handling
- Add connection error modals with retry
- Improve scan completion summary
- Test with mock backend
- **Verify:** Production-ready TUI

## Key Files to Modify/Create

**New Files:**
- `strix-pydantic/strix_pydantic/service/tui_client.py` (~580 LOC)
- `strix-pydantic/strix_pydantic/service/assets/tui_styles.tcss` (~100 lines)

**Modified Files:**
- `strix-pydantic/pyproject.toml` - add `strix-tui` entry point

**Reference Files (read-only):**
- `strix-pydantic/strix_pydantic/service/cli_client.py` - HTTP streaming pattern
- `strix-pydantic/strix_pydantic/service/events.py` - Event schema
- `strix-pydantic/strix_pydantic/interface/tui.py` - Panel widget patterns
- `strix/interface/tui.py` - Reactive properties and modal patterns

## Reusable Patterns

**From CLI Client (`cli_client.py`):**
- HTTP streaming: `client.stream("GET", url, timeout=None)` + `response.aiter_lines()`
- Event parsing: `json.loads(line)` for newline-delimited JSON
- Severity color map: `{"critical": "red", "high": "bright_red", ...}`

**From Existing TUI (`strix_pydantic/interface/tui.py`):**
- Reactive properties: `agents = reactive({})`
- Panel rendering: Override `render()` returning `Panel(content, ...)`
- Layout composition: `with Horizontal(): yield Panel1(); yield Panel2()`

**From Legacy TUI (`strix/interface/tui.py`):**
- Safe widget checks: `widget.is_mounted` before updates
- Modal screen pattern: Inherit from `ModalScreen`
- Event-driven updates via reactive properties (not polling)

## Verification Steps

1. **Backend Running:**
   ```bash
   cd strix-pydantic
   python -m strix_pydantic.service.backend
   ```

2. **Launch TUI Client:**
   ```bash
   strix-tui --target http://localhost --scan-mode quick --mock-tools
   ```

3. **Expected Behavior:**
   - TUI launches with three-panel layout
   - Agents panel shows three agents with status icons
   - Activity panel shows target, mode, elapsed time
   - Vulnerabilities appear in real-time as discovered
   - Can scroll vulnerabilities with arrow keys
   - Press 'd' on vulnerability to view detail modal
   - Press 'q' to quit

## Success Criteria

✅ TUI launches and connects to backend service  
✅ Three-panel layout renders correctly  
✅ Agent status updates in real-time  
✅ Vulnerabilities appear as discovered  
✅ Color-coded severity levels  
✅ Scrollable vulnerability list  
✅ Detail modal shows complete info  
✅ Keyboard navigation works  
✅ Elapsed time counter updates  
✅ Graceful error handling  
✅ Clean shutdown on 'q' or Ctrl+C  

## Dependencies

All dependencies already in `pyproject.toml`:
- `textual>=1.0.0` ✅
- `rich>=13.0.0` ✅
- `httpx>=0.27.0` ✅
- `click>=8.1.0` ✅

No new dependencies needed!
