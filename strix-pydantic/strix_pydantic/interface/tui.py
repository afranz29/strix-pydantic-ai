"""Full Textual TUI with three-panel dashboard for real-time progress feedback."""

import asyncio
from dataclasses import dataclass
from datetime import datetime
from enum import Enum
from typing import Optional

from rich.console import Console
from rich.panel import Panel
from rich.text import Text
from textual.app import App, ComposeResult
from textual.containers import Container, Horizontal, Vertical
from textual.reactive import reactive
from textual.widgets import Static, TextArea


class OperationStatus(str, Enum):
    """Status of current operation."""

    DOCKER_INIT = "Initializing Docker sandbox..."
    HEALTH_CHECK = "Checking sandbox health..."
    AGENT_RECON = "Running reconnaissance agent..."
    AGENT_EXPLOIT = "Running exploitation agent..."
    AGENT_POST = "Running post-exploitation agent..."
    TOOL_EXEC = "Executing security tool..."
    COMPLETE = "Scan complete!"


@dataclass
class AgentState:
    """State of a single agent."""

    name: str
    status: str = "initialized"  # initialized, running, completed
    iterations: int = 0


class AgentsPanel(Static):
    """Left panel showing agent statuses."""

    agents = reactive({})
    current_agent: str = reactive("")

    def render(self) -> Panel:
        """Render agent status list."""
        content = Text()

        agent_order = ["reconnaissance", "exploitation", "post_exploitation"]
        for agent_name in agent_order:
            agent = self.agents.get(agent_name)
            if agent is None:
                continue

            if agent.status == "completed":
                icon = "✓"
                style = "green"
            elif agent.status == "running":
                icon = "→"
                style = "cyan bold"
            else:
                icon = "○"
                style = "dim"

            marker = " " if agent_name != self.current_agent else ">"
            line = f"{marker} {icon} {agent_name}"
            if agent.iterations > 0:
                line += f" (iter {agent.iterations})"

            content.append(line + "\n", style=style)

        return Panel(
            content,
            title="[bold blue]Agents[/bold blue]",
            border_style="blue",
            expand=False,
        )


class ActivityPanel(Static):
    """Middle panel showing current activity."""

    current_status = reactive("")
    current_tools = reactive([])
    elapsed_seconds = reactive(0)

    def render(self) -> Panel:
        """Render current activity."""
        content = Text()

        if self.current_status:
            content.append(self.current_status + "\n\n", style="cyan")

        if self.current_tools:
            content.append("[bold]Active Tools:[/bold]\n", style="yellow")
            for tool in self.current_tools[-5:]:  # Show last 5 tools
                content.append(f"  🔧 {tool}\n", style="yellow")

        elapsed_str = f"{self.elapsed_seconds // 60:02d}:{self.elapsed_seconds % 60:02d}"
        content.append(f"\n[dim]Elapsed: {elapsed_str}[/dim]")

        return Panel(
            content,
            title="[bold yellow]Activity[/bold yellow]",
            border_style="yellow",
            expand=False,
        )


class OutputPanel(Static):
    """Right panel showing results and output."""

    def __init__(self):
        super().__init__()
        self.output_lines = []
        self.max_lines = 100

    def add_line(self, text: str, style: str = "") -> None:
        """Add a line to the output."""
        self.output_lines.append(Text(text, style=style))
        if len(self.output_lines) > self.max_lines:
            self.output_lines.pop(0)
        self.refresh()

    def render(self) -> Panel:
        """Render output panel."""
        content = Text()
        for line in self.output_lines[-20:]:  # Show last 20 lines
            content.append_text(line)
            content.append("\n")

        if not self.output_lines:
            content.append("[dim]Waiting for results...[/dim]")

        return Panel(
            content,
            title="[bold green]Results[/bold green]",
            border_style="green",
            overflow="fold",
        )


class StrixDashboard(App):
    """Main Textual dashboard app."""

    CSS = """
    Screen {
        layout: horizontal;
        background: $surface;
    }

    #agents-panel {
        width: 25%;
        border: solid $primary;
    }

    #activity-panel {
        width: 25%;
        border: solid $accent;
    }

    #output-panel {
        width: 50%;
        border: solid $success;
    }
    """

    BINDINGS = [("q", "quit", "Quit")]

    def __init__(self):
        super().__init__()
        self.agents_panel: Optional[AgentsPanel] = None
        self.activity_panel: Optional[ActivityPanel] = None
        self.output_panel: Optional[OutputPanel] = None

    def compose(self) -> ComposeResult:
        """Create the three-panel layout."""
        self.agents_panel = AgentsPanel(id="agents-panel")
        self.activity_panel = ActivityPanel(id="activity-panel")
        self.output_panel = OutputPanel(id="output-panel")

        with Horizontal():
            yield self.agents_panel
            yield self.activity_panel
            yield self.output_panel

    def update_agent_status(self, agent_name: str, status: str, iterations: int = 0) -> None:
        """Update an agent's status."""
        if self.agents_panel:
            agents = dict(self.agents_panel.agents)
            agents[agent_name] = AgentState(name=agent_name, status=status, iterations=iterations)
            self.agents_panel.agents = agents
            if status == "running":
                self.agents_panel.current_agent = agent_name

    def update_activity(self, status: str, tools: Optional[list[str]] = None) -> None:
        """Update current activity."""
        if self.activity_panel:
            self.activity_panel.current_status = status
            if tools is not None:
                self.activity_panel.current_tools = tools

    def add_output(self, text: str, style: str = "white") -> None:
        """Add text to output panel."""
        if self.output_panel:
            self.output_panel.add_line(text, style=style)

    def tick_elapsed(self) -> None:
        """Increment elapsed time."""
        if self.activity_panel:
            self.activity_panel.elapsed_seconds += 1


class StrixProgressApp:
    """Wrapper for the Textual app - manages lifecycle and callbacks."""

    def __init__(self):
        """Initialize progress app."""
        self.app: Optional[StrixDashboard] = None
        self._running = False
        self._app_task: Optional[asyncio.Task] = None

    def start(self) -> None:
        """Start the TUI application."""
        if self._running:
            return

        self._running = True
        self.app = StrixDashboard()

        # Run the app in a separate thread since the orchestrator is async
        import threading

        def run_app():
            try:
                self.app.run()
            except Exception as e:
                print(f"TUI Error: {e}")

        self._thread = threading.Thread(target=run_app, daemon=True)
        self._thread.start()

    def stop(self) -> None:
        """Stop the TUI application gracefully."""
        if self.app:
            self.app.exit()
        self._running = False

    def update_agent_status(
        self, agent_name: str, status: str, iterations: int = 0
    ) -> None:
        """Update agent status in the dashboard."""
        if self.app and self.app.is_mounted:
            self.app.update_agent_status(agent_name, status, iterations)

    def update_activity(self, status: str, tools: Optional[list[str]] = None) -> None:
        """Update current activity."""
        if self.app and self.app.is_mounted:
            self.app.update_activity(status, tools)

    def add_output(self, text: str, style: str = "white") -> None:
        """Add text to output panel."""
        if self.app and self.app.is_mounted:
            self.app.add_output(text, style)

    def tick_elapsed(self) -> None:
        """Tick elapsed time."""
        if self.app and self.app.is_mounted:
            self.app.tick_elapsed()

    @property
    def is_running(self) -> bool:
        """Check if TUI is running."""
        return self._running


class StrixTUIApp:
    """Wrapper for backward compatibility with CLI."""

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

    def update_status(self, status: "OperationStatus") -> None:
        """Update status (no-op if UI disabled)."""
        if self._app:
            self._app.update_activity(status.value)

    def add_log(self, message: str, level: str = "info") -> None:
        """Add log message (no-op if UI disabled)."""
        if self._app:
            style_map = {"info": "cyan", "success": "green", "warning": "yellow", "error": "red"}
            style = style_map.get(level, "white")
            self._app.add_output(message, style)

    def show_vulnerability(self, title: str, severity: str, description: str) -> None:
        """Show vulnerability (no-op if UI disabled)."""
        if self._app:
            severity_colors = {
                "critical": "red",
                "high": "bright_red",
                "medium": "yellow",
                "low": "blue",
                "info": "cyan",
            }
            color = severity_colors.get(severity, "white")
            self._app.add_output(
                f"[{color}]({severity.upper()})[/{color}] {title}: {description}", color
            )

    def show_summary(self, summary_text: str) -> None:
        """Show summary (no-op if UI disabled)."""
        if self._app:
            self._app.add_output(f"[green]Summary:[/green] {summary_text}", "green")

    def tick_elapsed_time(self) -> None:
        """Tick elapsed time (no-op if UI disabled)."""
        if self._app:
            self._app.tick_elapsed()

    def update_agent_status(self, agent_name: str, status: str, iterations: int = 0) -> None:
        """Update agent status."""
        if self._app:
            self._app.update_agent_status(agent_name, status, iterations)

    def update_activity(self, status: str, tools: Optional[list[str]] = None) -> None:
        """Update current activity."""
        if self._app:
            self._app.update_activity(status, tools)
