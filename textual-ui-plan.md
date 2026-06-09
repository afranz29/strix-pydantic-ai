# Plan: Minimal Textual UI Integration for Strix Pydantic

## Context

Strix Pydantic is a security pentesting framework with 3 autonomous agents that discover and exploit web vulnerabilities. Currently, the CLI is non-interactive with minimal progress feedback - users see brief messages at the start of each phase but no indication of progress during long-running operations (LLM reasoning: 10-60s per agent role, Docker sandbox initialization: 2-30s, tool execution: 1-120s).

The legacy Strix implementation has a full 2000+ line Textual TUI with agent trees, streaming chat, modal dialogs, and animated status indicators. However, **the legacy UI was sluggish** - particularly agent tree navigation had noticeable lag. Performance analysis revealed the sluggishness was due to **implementation anti-patterns** (aggressive polling at 19.5 updates/second, O(n²) tree reorganization, cache invalidation on every agent switch, full re-renders) rather than Textual framework limitations.

**Goal**: Create a minimal Textual-based UI enhancement that:
1. Provides real-time progress feedback during long operations
2. Avoids the performance pitfalls of the legacy implementation
3. Uses event-driven updates instead of aggressive polling
4. Has the smallest implementation footprint for evaluation

This will help decide if Textual is worth adopting, or if we should consider alternatives like PyRatatui (Rust-based TUI with Python bindings, but only 3 months old and very immature).

## Recommended Approach

Implement a **hybrid CLI+TUI mode** where the TUI provides live progress feedback during scans but retains the simple CLI interface for input. This gives us real-time visibility into operations without requiring complex interaction patterns.

### Core MVP Features (3 components, ~250-300 LOC)

**1. Live Progress Display** (Primary value-add)
- Show current operation status with spinners
- Display elapsed time and current agent role
- Show tool execution in real-time
- Update scan progress (phase X/3)

**2. Rich Output Formatting**
- Styled panels for vulnerabilities (color-coded severity)
- Syntax highlighting for command output
- Tables for final scan summary
- Status badges with icons

**3. Basic Layout**
- Main content area (scrollable)
- Status bar at bottom (current operation + elapsed time)
- Auto-exit to summary when complete

### What We're NOT Building (Keep Scope Minimal)

- ❌ Chat input / interactive controls
- ❌ Agent tree navigation (legacy had performance issues)
- ❌ Modal dialogs / help screens
- ❌ Splash screen animations
- ❌ Click handlers / mouse interaction
- ❌ Sidebar layouts
- ❌ Background polling loops (use event-driven updates)

This is a display-only enhancement, not a full interactive TUI.

### Performance Strategy (Learning from Legacy Mistakes)

**Anti-patterns to AVOID:**
1. **Aggressive polling** - Legacy used 350ms interval + 60ms animation = 19.5 updates/sec
2. **Repeated DOM queries** - Cache widget references after mount
3. **O(n²) operations** - Avoid nested iterations over large data structures
4. **Full re-renders** - Only update widgets when data actually changes
5. **Synchronous blocking** - Keep UI thread free, use async for heavy operations

**Best practices to FOLLOW:**
1. **Event-driven updates** - Push updates from orchestrator, don't poll
2. **Lazy rendering** - Only render what's visible
3. **Smart caching** - Cache expensive computations with proper invalidation
4. **Debouncing** - Batch rapid updates (e.g., during tool execution)
5. **Reactive properties** - Use Textual's reactive system properly

## Implementation Plan

### Phase 1: Dependencies & Project Setup

**Files to modify:**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/pyproject.toml`

**Changes:**
1. Add `textual>=1.0.0` to dependencies
2. Add optional dependency group: `[ui]` for easy opt-in

### Phase 2: TUI Application Shell

**New file to create:**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/strix_pydantic/interface/tui.py`

**Components:**
1. `StrixProgressApp(App)` - Main Textual application
   - Layout: single `VerticalScroll` for content + `Static` footer for status
   - Methods: `update_status()`, `add_log()`, `show_vulnerability()`, `show_summary()`
   - **Cache widget references** in `on_mount()` - avoid repeated `query_one()` calls
   
2. `ProgressDisplay` widget
   - Spinner + status text + elapsed time
   - **Event-driven updates** via message passing, NOT `set_interval()`

3. `OperationStatus` enum
   - DOCKER_INIT, HEALTH_CHECK, AGENT_RECON, AGENT_EXPLOIT, AGENT_POST, TOOL_EXEC, COMPLETE

**Performance considerations:**
- Use Textual's reactive properties for status updates (not polling)
- Debounce rapid tool execution logs (batch updates every 500ms max)
- Limit scrollback to last 100 entries to prevent memory bloat
- Use `call_later()` to batch multiple updates in single render frame

**References to learn from (but NOT copy directly):**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix/interface/tui.py:1258-1292` - Status display patterns (but avoid their polling approach)
- `/home/afranz/my-stuff/strix-pydantic-ai/strix/interface/tui.py:1356-1430` - Animation logic (but use reactive updates instead of 60ms timer)

### Phase 3: Integration with CLI

**Files to modify:**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/strix_pydantic/interface/cli.py`

**Changes:**

1. Add CLI flag (around line 50):
   ```python
   @click.option(
       "--ui/--no-ui",
       default=True,
       help="Use Textual UI for progress display (default: enabled)"
   )
   ```

2. Modify `scan()` function to conditionally use TUI:
   - If `--ui`: Launch `StrixProgressApp` and update it via callbacks
   - If `--no-ui`: Keep existing `click.echo()` behavior

3. Create callback interface for progress updates:
   - `on_docker_init()`, `on_health_check()`, `on_agent_start(role)`, `on_tool_exec(tool, status)`, `on_complete(summary)`

4. Wire callbacks into existing orchestration flow:
   - `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/strix_pydantic/agents/pydantic_orchestrator.py:_run_orchestration()` - Add callbacks at key points
   - `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/strix_pydantic/runtime/docker_runtime.py:_wait_for_tool_server()` - Add progress callback

### Phase 4: Enhanced Output Formatting

**New file to create:**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix-pydantic/strix_pydantic/interface/formatters.py`

**Functions:**
1. `format_vulnerability_panel(vuln: Vulnerability) -> Panel`
   - Color-coded by severity (red=critical, orange=high, yellow=medium, blue=low)
   - Shows title, description, affected endpoint
   - Rich Panel with border styling

2. `format_tool_output(observation: ToolObservation) -> Syntax`
   - Syntax highlighted command/output
   - Truncate long output (configurable, default 20 lines)
   - Show exit code with status badge

3. `format_scan_summary(state: StrixRunState) -> Table`
   - Rich Table with run statistics
   - Vulnerability counts by severity
   - Duration, agent iterations, tools executed

**References to reuse from legacy:**
- `/home/afranz/my-stuff/strix-pydantic-ai/strix/interface/tui.py:594-639` - Vulnerability panel styling
- `/home/afranz/my-stuff/strix-pydantic-ai/strix/tui_components/terminal_renderer.py` - Terminal output rendering

### Phase 5: Testing & Refinement

**Verification steps:**

1. **Test with mock tools** (fast iteration):
   ```bash
   strix-pydantic --target http://example.com --mock-tools --ui
   ```
   - Verify: Status updates appear in real-time
   - Verify: Vulnerabilities display in styled panels
   - Verify: Summary table appears at end

2. **Test with real Docker sandbox**:
   ```bash
   strix-pydantic --target http://testphp.vulnweb.com --ui
   ```
   - Verify: Docker initialization spinner shows
   - Verify: Tool executions appear in real-time
   - Verify: Can still use `--no-ui` to disable

3. **Test confirm mode**:
   ```bash
   strix-pydantic --target http://example.com --confirm --ui
   ```
   - Verify: TUI doesn't break confirmation prompts
   - May need to pause TUI for `click.confirm()` or replace with Textual modal

4. **Edge cases**:
   - Very long tool output (>1000 lines)
   - No vulnerabilities found
   - Agent timeout/error scenarios
   - Narrow terminal (<80 cols)

## Key Files to Modify

**New files** (~250 LOC total):
- `strix-pydantic/strix_pydantic/interface/tui.py` (~150 LOC)
- `strix-pydantic/strix_pydantic/interface/formatters.py` (~100 LOC)

**Modified files** (~50 LOC changes):
- `strix-pydantic/pyproject.toml` (dependencies)
- `strix-pydantic/strix_pydantic/interface/cli.py` (add --ui flag, wire callbacks)
- `strix-pydantic/strix_pydantic/agents/pydantic_orchestrator.py` (add progress callbacks)
- `strix-pydantic/strix_pydantic/runtime/docker_runtime.py` (add health check callback)

## Success Criteria

This MVP is successful if:

✅ **Real-time visibility**: Users can see what's happening during long operations  
✅ **Minimal code**: <300 LOC for TUI implementation  
✅ **Non-breaking**: Existing CLI behavior preserved with `--no-ui`  
✅ **Better UX**: Styled output is more readable than plain text  
✅ **Performance**: No noticeable slowdown from UI updates  

After this evaluation, we can decide whether to:
1. Keep it minimal (current plan)
2. Expand to full interactive TUI (agent tree, chat input, modals)
3. Revert to plain CLI (if Textual adds too much complexity)

## Alternatives Considered

### Why Not Just Rich?

We could use Rich's Live and Progress APIs without Textual. However:
- **Rich Live** requires manual layout management and refresh handling
- **Textual** provides built-in layout, scrolling, and event loop
- **Legacy code** already uses Textual patterns we can learn from (and mistakes to avoid)
- **Evaluation goal** is specifically to assess Textual's value-add

Since the goal is to evaluate Textual (not just better output), we should use it for the parts where it shines: layout management, auto-scrolling, status bar, structured content display.

### Why Not PyRatatui?

PyRatatui (https://github.com/pyratatui/pyratatui) is Python bindings for the Ratatui Rust TUI library, offering:
- **Rust-speed rendering** - compiled Rust core with PyO3 bindings
- **Immediate mode rendering** - explicit frame-based drawing like game engines
- **35+ widgets** including charts, gauges, tables, trees, QR codes
- **TachyonFX effects engine** for animations

**However, PyRatatui is too immature:**
- Created March 2026 (only 3 months old!)
- v0.2.9 with 120 stars, small community
- Potential breaking changes as API stabilizes
- Limited production use cases and documentation
- Single-maintainer risk

**Performance isn't the bottleneck** - the legacy Textual UI's sluggishness was due to implementation anti-patterns (19.5 updates/sec polling, O(n²) operations, aggressive cache invalidation), not framework limitations. Textual is capable of 60 FPS when used correctly.

**Verdict**: Re-evaluate PyRatatui in 6-12 months once it matures. Stick with Textual for now - it's proven, stable, and performant when used properly.
