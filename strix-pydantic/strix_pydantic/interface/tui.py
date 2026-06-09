"""Minimal TUI using Rich for real-time progress feedback during scans."""

import threading
import time
from enum import Enum
from typing import Optional

from rich.console import Console
from rich.panel import Panel
from rich.text import Text


class OperationStatus(str, Enum):
    """Status of current operation."""

    DOCKER_INIT = "Initializing Docker sandbox..."
    HEALTH_CHECK = "Checking sandbox health..."
    AGENT_RECON = "Running reconnaissance agent..."
    AGENT_EXPLOIT = "Running exploitation agent..."
    AGENT_POST = "Running post-exploitation agent..."
    TOOL_EXEC = "Executing security tool..."
    COMPLETE = "Scan complete!"


class StrixProgressApp:
    """Split-panel display using Rich - phases on left, results on right."""

    def __init__(self):
        """Initialize progress app."""
        self.console = Console()
        self._current_status = "Initializing..."
        self._elapsed = 0
        self._timer_thread: Optional[threading.Thread] = None
        self._running = False
        self._phases: list[str] = []
        self._results: list[str] = []
        self._max_results = 50  # Keep last N results

    def start(self) -> None:
        """Start the progress display."""
        if self._running:
            return

        self._running = True

        # Start elapsed time ticker
        self._timer_thread = threading.Thread(target=self._tick_elapsed, daemon=True)
        self._timer_thread.start()

    def stop(self) -> None:
        """Stop the progress display."""
        self._running = False
        if self._timer_thread:
            self._timer_thread.join(timeout=1)

    def update_status(self, status: OperationStatus) -> None:
        """Update the current operation status."""
        self._current_status = status.value
        # Add to phases list if not already there
        if status.value not in self._phases:
            self._phases.append(status.value)

    def add_log(self, message: str, level: str = "info") -> None:
        """Add a log message to results panel."""
        icon_map = {
            "info": "ℹ",
            "success": "✅",
            "warning": "⚠",
            "error": "❌",
        }
        style_map = {
            "info": "cyan",
            "success": "green",
            "warning": "yellow",
            "error": "red",
        }

        icon = icon_map.get(level, "•")
        style = style_map.get(level, "")
        self.console.print(f"{icon} {message}", style=style)

    def show_vulnerability(self, title: str, severity: str, description: str) -> None:
        """Display a vulnerability in results panel."""
        severity_colors = {
            "critical": "red",
            "high": "bright_red",
            "medium": "yellow",
            "low": "blue",
            "info": "cyan",
        }
        color = severity_colors.get(severity, "white")

        severity_badge = f"[bold {color}]({severity.upper()})[/]"
        content = f"{severity_badge} {title}\n{description}"
        panel = Panel(content, border_style=color, title="[bold]Vulnerability[/]")
        self.console.print(panel)

    def show_summary(self, summary_text: str) -> None:
        """Display final summary."""
        panel = Panel(
            summary_text,
            border_style="green",
            title="[bold green]Scan Summary[/bold green]",
        )
        self.console.print(panel)

    def tick_elapsed_time(self) -> None:
        """Increment elapsed time counter (called internally by timer)."""
        self._elapsed += 1

    def _tick_elapsed(self) -> None:
        """Background timer thread."""
        while self._running:
            time.sleep(1)
            self.tick_elapsed_time()

    @property
    def is_running(self) -> bool:
        """Check if TUI is running."""
        return self._running

    def get_phases_panel(self) -> Panel:
        """Get formatted phases panel."""
        phases_text = Text()
        for phase in self._phases:
            phases_text.append(f"✓ {phase}\n", style="green")

        if self._current_status and self._current_status not in [p for p in self._phases]:
            phases_text.append(f"→ {self._current_status}\n", style="cyan bold")

        elapsed_str = f"{self._elapsed // 60:02d}:{self._elapsed % 60:02d}"
        phases_text.append(f"\nElapsed: {elapsed_str}", style="dim")

        return Panel(phases_text, title="[bold blue]Phases[/bold blue]", expand=False)

    def get_results_panel(self) -> Panel:
        """Get formatted results panel."""
        results_text = Text()
        for result in self._results[-self._max_results :]:
            results_text.append(result + "\n")

        if not self._results:
            results_text.append("Waiting for results...", style="dim")

        return Panel(results_text, title="[bold cyan]Results[/bold cyan]", overflow="fold")


class StrixTUIApp:
    """Wrapper for TUI application - manages lifecycle and callbacks."""

    def __init__(self, use_ui: bool = True):
        """Initialize TUI app."""
        self.use_ui = use_ui
        self._app: Optional[StrixProgressApp] = None

    def start(self) -> None:
        """Start the TUI if enabled."""
        if self.use_ui:
            self._app = StrixProgressApp()
            self._app.start()

    def stop(self) -> None:
        """Stop the TUI."""
        if self._app:
            self._app.stop()

    def update_status(self, status: OperationStatus) -> None:
        """Update status (no-op if UI disabled)."""
        if self._app:
            self._app.update_status(status)

    def add_log(self, message: str, level: str = "info") -> None:
        """Add log message (no-op if UI disabled)."""
        if self._app:
            self._app.add_log(message, level)

    def show_vulnerability(self, title: str, severity: str, description: str) -> None:
        """Show vulnerability (no-op if UI disabled)."""
        if self._app:
            self._app.show_vulnerability(title, severity, description)

    def show_summary(self, summary_text: str) -> None:
        """Show summary (no-op if UI disabled)."""
        if self._app:
            self._app.show_summary(summary_text)

    def tick_elapsed_time(self) -> None:
        """Tick elapsed time (no-op if UI disabled)."""
        if self._app:
            self._app.tick_elapsed_time()
