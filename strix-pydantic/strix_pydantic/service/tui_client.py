"""Textual TUI client for Strix backend service."""

import asyncio
import json
from datetime import datetime
from typing import Any, Optional

import click
import httpx
from rich.panel import Panel
from rich.text import Text
from textual.app import App, ComposeResult
from textual.containers import Horizontal, Vertical, VerticalScroll
from textual.screen import ModalScreen
from textual.widgets import Button, Label, Static
from textual.reactive import reactive


class AgentsPanel(Static):
    """Display agent status with running/completed/initialized indicators."""

    agents = reactive({})
    current_agent = reactive("")

    def render(self) -> Panel:
        """Render agent status panel (matches original Strix styling)."""
        content = Text()

        agent_roles = ["reconnaissance", "exploitation", "post_exploitation"]
        for role in agent_roles:
            agent_data = self.agents.get(role, {})
            status = agent_data.get("status", "initialized")
            iterations = agent_data.get("iterations", 0)

            # Status indicators (matching original Strix)
            status_indicators = {
                "completed": ("🟢", "green"),
                "running": ("⚪", "cyan"),
                "initialized": ("○", "dim"),
                "failed": ("🔴", "red"),
            }

            icon, style = status_indicators.get(status, ("○", "dim"))

            # Build text line with iteration count
            text_line = f"{icon} {role} (iter {iterations})\n"

            # Apply bold if current agent, otherwise just status style
            if role == self.current_agent:
                content.append(text_line, style=f"{style} bold")
            else:
                content.append(text_line, style=style)

        return Panel(content, title="Agents", border_style="blue", title_align="left")


class ActivityPanel(Static):
    """Display scan activity and progress."""

    target = reactive("")
    scan_mode = reactive("")
    current_status = reactive("initializing")
    elapsed_seconds = reactive(0)

    def render(self) -> Panel:
        """Render activity status panel."""
        content = Text()
        content.append(f"Target: {self.target}\n")
        content.append(f"Mode: {self.scan_mode}\n")
        content.append(f"Status: {self.current_status}\n")
        content.append(f"Elapsed: {self.elapsed_seconds}s\n")

        return Panel(content, title="Activity", border_style="yellow", title_align="left")


class AgentActivityPanel(VerticalScroll):
    """Display agent activity log and reasoning."""

    activity_log = reactive([])

    def compose(self) -> ComposeResult:
        """Compose the activity panel."""
        yield Static(id="activity-content")

    def on_mount(self) -> None:
        """Initialize the panel."""
        self.update_activity()

    def watch_activity_log(self, log: list) -> None:
        """Update when activity log changes."""
        self.update_activity()

    def update_activity(self) -> None:
        """Render activity log."""
        content_widget = self.query_one("#activity-content", Static)
        content = Text()

        if not self.activity_log:
            content.append("Waiting for agent activity...", style="dim")
        else:
            for entry in self.activity_log:
                level = entry.get("level", "info")
                message = entry.get("message", "")

                # Color by level
                level_colors = {
                    "info": "cyan",
                    "success": "green",
                    "warning": "yellow",
                    "error": "red",
                }
                color = level_colors.get(level, "white")

                # Wrap long messages for readability
                lines = message.split("\n")
                for idx, line in enumerate(lines):
                    if line.strip():
                        # Indent continuation lines
                        if idx > 0 and not line.startswith("  "):
                            content.append("  ", style="dim")

                        # Wrap very long lines
                        if len(line) > 80:
                            words = line.split()
                            current_line = ""
                            for word in words:
                                if len(current_line) + len(word) + 1 > 78:
                                    if current_line:
                                        content.append(current_line + "\n", style=color)
                                        content.append("    ", style="dim")
                                        current_line = word
                                    else:
                                        content.append(word + "\n", style=color)
                                        content.append("    ", style="dim")
                                else:
                                    current_line += (" " if current_line else "") + word
                            if current_line:
                                content.append(current_line + "\n", style=color)
                        else:
                            content.append(line + "\n", style=color)

        content_widget.update(content)


class VulnerabilitiesPanel(VerticalScroll):
    """Display discovered vulnerabilities in scrollable list."""

    vulnerabilities = reactive([])

    def compose(self) -> ComposeResult:
        """Compose the vulnerabilities panel."""
        yield Static(id="vulns-content")

    def on_mount(self) -> None:
        """Initialize the panel."""
        self.update_vulnerabilities()

    def watch_vulnerabilities(self, vulns: list) -> None:
        """Update when vulnerabilities change."""
        self.update_vulnerabilities()

    def update_vulnerabilities(self) -> None:
        """Render vulnerability cards."""
        content_widget = self.query_one("#vulns-content", Static)
        content = Text()

        if not self.vulnerabilities:
            content.append("No vulnerabilities found yet...", style="dim")
        else:
            for idx, vuln in enumerate(self.vulnerabilities):
                severity = vuln.get("severity", "info")
                title = vuln.get("title", "Unknown")
                description = vuln.get("description", "")

                # Severity colors
                severity_colors = {
                    "critical": "red",
                    "high": "bright_red",
                    "medium": "yellow",
                    "low": "blue",
                    "info": "cyan",
                }
                color = severity_colors.get(severity, "white")

                # Render vulnerability card
                content.append(f"({severity.upper()}) ", style=f"bold {color}")
                content.append(f"{title}\n", style="bold")
                content.append(f"{description}\n")
                content.append("-" * 60 + "\n", style="dim")

        content_widget.update(content)


class VulnerabilityDetailModal(ModalScreen):
    """Modal screen showing detailed vulnerability information."""

    DEFAULT_CSS = """
    VulnerabilityDetailModal {
        align: center middle;
    }

    VulnerabilityDetailModal > Static {
        width: 80%;
        height: auto;
        border: solid $accent;
        background: $surface;
    }
    """

    def __init__(self, vuln: dict):
        """Initialize with vulnerability data."""
        super().__init__()
        self.vuln = vuln

    def compose(self) -> ComposeResult:
        """Compose the detail modal."""
        content = Text()
        content.append(f"[bold]{self.vuln.get('title', 'Unknown')}[/bold]\n\n")

        severity = self.vuln.get("severity", "info")
        severity_colors = {
            "critical": "red",
            "high": "bright_red",
            "medium": "yellow",
            "low": "blue",
            "info": "cyan",
        }
        color = severity_colors.get(severity, "white")
        content.append(f"[bold {color}]Severity:[/bold {color}] {severity.upper()}\n\n")

        description = self.vuln.get("description", "")
        content.append(f"[bold]Description:[/bold]\n{description}\n\n")

        if self.vuln.get("cve_id"):
            content.append(f"[bold]CVE:[/bold] {self.vuln['cve_id']}\n\n")

        if self.vuln.get("parameter"):
            content.append(f"[bold]Parameter:[/bold] {self.vuln['parameter']}\n\n")

        if self.vuln.get("poc"):
            content.append(f"[bold]POC:[/bold] {self.vuln['poc']}\n")

        yield Static(Panel(content, title="Vulnerability Details", border_style=color))
        yield Button("Close [dim](ESC)[/dim]", id="close-button")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        """Close the modal."""
        if event.button.id == "close-button":
            self.app.pop_screen()

    def on_key(self, event) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            self.app.pop_screen()


class ScanTUIApp(App):
    """Main Textual application for Strix TUI client."""

    CSS_PATH = "assets/tui_styles.tcss"

    # Reactive state
    target = reactive("")
    scan_mode = reactive("")
    scan_status = reactive("initializing")
    current_agent = reactive("")
    agents = reactive({})
    vulnerabilities = reactive([])
    elapsed = reactive(0)
    activity_log = reactive([])

    BINDINGS = [
        ("q", "quit", "Quit"),
        ("d", "show_detail", "Detail"),
    ]

    def __init__(
        self,
        target: str,
        scan_mode: str,
        backend_url: str,
        model: Optional[str] = None,
        mock_tools: bool = False,
    ):
        """Initialize the TUI app."""
        super().__init__()
        self.target = target
        self.scan_mode = scan_mode
        self.backend_url = backend_url
        self.model = model
        self.mock_tools = mock_tools
        self.current_vuln_idx = 0

    def compose(self) -> ComposeResult:
        """Compose the layout with four panels."""
        with Horizontal():
            with Vertical():
                with Horizontal():
                    yield AgentsPanel(id="agents-panel")
                    yield ActivityPanel(id="activity-panel")
                yield AgentActivityPanel(id="agent-activity-panel")
            yield VulnerabilitiesPanel(id="vulnerabilities-panel")

    def on_mount(self) -> None:
        """Initialize the app and start background scan."""
        # Initialize agent statuses
        for role in ["reconnaissance", "exploitation", "post_exploitation"]:
            self.agents[role] = {"status": "initialized", "iterations": 0}

        # Wire up reactive updates to widgets
        activity_log_panel = self.query_one("#agent-activity-panel", AgentActivityPanel)
        activity_log_panel.activity_log = self.activity_log

        agents_panel = self.query_one("#agents-panel", AgentsPanel)
        agents_panel.agents = self.agents
        agents_panel.current_agent = self.current_agent

        activity_panel = self.query_one("#activity-panel", ActivityPanel)
        activity_panel.target = self.target
        activity_panel.scan_mode = self.scan_mode
        activity_panel.current_status = self.scan_status

        vulns_panel = self.query_one("#vulnerabilities-panel", VulnerabilitiesPanel)
        vulns_panel.vulnerabilities = self.vulnerabilities

        # Start scan and event streaming in background
        self.run_worker(self.start_scan_and_stream(), exclusive=True)

        # Start elapsed time timer
        self.set_interval(1.0, self.update_elapsed_time)

    def watch_agents(self, agents: dict) -> None:
        """Update agents panel when agents change."""
        try:
            panel = self.query_one("#agents-panel", AgentsPanel)
            panel.agents = agents
        except Exception:
            pass

    def watch_current_agent(self, agent: str) -> None:
        """Update current agent highlight."""
        try:
            panel = self.query_one("#agents-panel", AgentsPanel)
            panel.current_agent = agent
        except Exception:
            pass

    def watch_scan_status(self, status: str) -> None:
        """Update scan status display."""
        try:
            panel = self.query_one("#activity-panel", ActivityPanel)
            panel.current_status = status
        except Exception:
            pass

    def watch_vulnerabilities(self, vulns: list) -> None:
        """Update vulnerabilities panel."""
        try:
            panel = self.query_one("#vulnerabilities-panel", VulnerabilitiesPanel)
            panel.vulnerabilities = vulns
        except Exception:
            pass

    def watch_activity_log(self, log: list) -> None:
        """Update activity log panel."""
        try:
            panel = self.query_one("#agent-activity-panel", AgentActivityPanel)
            panel.activity_log = log
        except Exception:
            pass

    def watch_elapsed(self, seconds: int) -> None:
        """Update elapsed time display."""
        try:
            panel = self.query_one("#activity-panel", ActivityPanel)
            panel.elapsed_seconds = seconds
        except Exception:
            pass

    def update_elapsed_time(self) -> None:
        """Increment elapsed time counter."""
        self.elapsed += 1

    async def start_scan_and_stream(self) -> None:
        """Start scan and stream events from backend."""
        try:
            async with httpx.AsyncClient() as client:
                # 1. Start scan
                self.scan_status = "starting scan..."
                response = await client.post(
                    f"{self.backend_url}/scans",
                    json={
                        "target": self.target,
                        "scan_mode": self.scan_mode,
                        "model": self.model,
                        "mock_tools": self.mock_tools,
                    },
                    timeout=10.0,
                )
                response.raise_for_status()
                scan_data = response.json()
                scan_id = scan_data["scan_id"]

                # 2. Stream events
                self.scan_status = "streaming events..."
                async with client.stream(
                    "GET",
                    f"{self.backend_url}/scans/{scan_id}/events",
                    timeout=None,
                ) as stream:
                    if stream.status_code != 200:
                        self.scan_status = f"error: HTTP {stream.status_code}"
                        return

                    async for line in stream.aiter_lines():
                        if not line:
                            continue

                        try:
                            event = json.loads(line)
                            self.handle_event(event)
                        except json.JSONDecodeError:
                            continue

        except httpx.ConnectError:
            self.scan_status = "error: backend not available"
        except Exception as e:
            self.scan_status = f"error: {e}"

    def handle_event(self, event: dict) -> None:
        """Handle incoming event from backend stream."""
        event_type = event.get("type")

        if event_type == "scan_started":
            self.scan_status = "running"
            self.elapsed = 0

        elif event_type == "agent_started":
            role = event.get("role", "")
            iteration = event.get("iteration", 0)
            self.current_agent = role

            # Update agent status
            agents = dict(self.agents)
            agents[role] = {"status": "running", "iterations": iteration}
            self.agents = agents

            # Log agent start
            log = list(self.activity_log)
            log.append({
                "level": "info",
                "message": f"Starting {role} agent (iteration {iteration})",
            })
            self.activity_log = log

        elif event_type == "tool_executed":
            # Add tool execution to activity log
            tool_name = event.get("tool_name", "unknown")
            log = list(self.activity_log)
            log.append({
                "level": "info",
                "message": f"  → Executed: {tool_name}",
            })
            self.activity_log = log

        elif event_type == "agent_message":
            # Add agent reasoning/output to activity log
            role = event.get("role", "")
            message = event.get("message", "")
            log = list(self.activity_log)
            log.append({
                "level": "info",
                "message": message,
            })
            self.activity_log = log

        elif event_type == "agent_completed":
            role = event.get("role", "")
            iteration = event.get("iteration", 0)

            # Update agent status
            agents = dict(self.agents)
            agents[role] = {"status": "completed", "iterations": iteration}
            self.agents = agents

            # Log agent completion
            log = list(self.activity_log)
            vuln_found = event.get("vulnerabilities_found", 0)
            log.append({
                "level": "success",
                "message": f"✅ {role} completed ({vuln_found} vulnerabilities found)",
            })
            self.activity_log = log

        elif event_type == "log_message":
            # Add log message to activity panel
            log = list(self.activity_log)
            log.append({
                "level": event.get("level", "info"),
                "message": event.get("message", ""),
            })
            self.activity_log = log

        elif event_type == "vulnerability_found":
            # Add vulnerability to list
            vulns = list(self.vulnerabilities)
            vulns.append(event)
            self.vulnerabilities = vulns

        elif event_type == "scan_completed":
            duration = event.get("duration_seconds", 0)
            vuln_count = event.get("vulnerabilities_count", 0)
            self.scan_status = f"completed ({vuln_count} vulnerabilities in {duration:.1f}s)"

        elif event_type == "scan_failed":
            error = event.get("error", "Unknown error")
            self.scan_status = f"failed: {error}"

    def action_quit(self) -> None:
        """Quit the application."""
        self.exit()

    def action_show_detail(self) -> None:
        """Show detail modal for selected vulnerability."""
        if self.current_vuln_idx < len(self.vulnerabilities):
            vuln = self.vulnerabilities[self.current_vuln_idx]
            self.push_screen(VulnerabilityDetailModal(vuln))


@click.command()
@click.option("--target", required=True, help="Target URL to scan")
@click.option(
    "--scan-mode",
    type=click.Choice(["quick", "standard", "deep"]),
    default="standard",
    help="Scan intensity level",
)
@click.option("--model", type=str, default=None, help="Override LLM model")
@click.option(
    "--backend-url",
    default="http://localhost:8000",
    help="Backend service URL",
)
@click.option(
    "--mock-tools",
    is_flag=True,
    help="Use mock tools for testing (no Docker required)",
)
def main(
    target: str,
    scan_mode: str,
    model: Optional[str],
    backend_url: str,
    mock_tools: bool,
) -> None:
    """Launch Textual TUI client connected to backend service."""
    app = ScanTUIApp(
        target=target,
        scan_mode=scan_mode,
        backend_url=backend_url,
        model=model,
        mock_tools=mock_tools,
    )
    app.run()


if __name__ == "__main__":
    main()
