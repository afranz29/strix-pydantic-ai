"""Textual TUI client for Strix backend service."""

import asyncio
import json
import logging
from datetime import datetime
from pathlib import Path
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

logger = logging.getLogger(__name__)


def setup_logging(log_file: Optional[Path] = None) -> Path:
    """Configure logging to file (console logging is disabled to avoid corrupting the TUI)."""
    if log_file is None:
        logs_dir = Path(__file__).parent.parent.parent / "logs"
        log_file = logs_dir / f"tui_{datetime.now().strftime('%Y%m%d_%H%M%S')}.log"

    # Create logs directory if needed
    log_file.parent.mkdir(parents=True, exist_ok=True)

    # Setup file handler
    file_handler = logging.FileHandler(log_file)
    file_handler.setLevel(logging.DEBUG)
    formatter = logging.Formatter(
        "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S"
    )
    file_handler.setFormatter(formatter)

    # Get root logger, clear existing handlers (like console handlers), and add file handler
    root_logger = logging.getLogger()
    for handler in root_logger.handlers[:]:
        root_logger.removeHandler(handler)
    root_logger.setLevel(logging.DEBUG)
    root_logger.addHandler(file_handler)

    # Set TUI logger to DEBUG
    logging.getLogger("strix_pydantic.service.tui_client").setLevel(logging.DEBUG)

    logger.info(f"TUI logging to {log_file}")
    return log_file


class AgentsPanel(Static):
    """Display agent status and high-level scan goals."""

    goals = reactive({})

    def render(self) -> Text:
        """Render goals status panel."""
        content = Text()

        goal_ids = ["bootstrap", "reconnaissance", "exploitation", "post_exploitation"]
        goal_labels = {
            "bootstrap": "Bootstrap Sandbox",
            "reconnaissance": "Reconnaissance Phase",
            "exploitation": "Exploitation Phase",
            "post_exploitation": "Post-Exploitation Phase",
        }
        for goal in goal_ids:
            status = self.goals.get(goal, "pending")
            label = goal_labels.get(goal, goal)

            # Status indicators
            status_indicators = {
                "completed": ("🟢", "green"),
                "in_progress": ("⚪", "cyan"),
                "pending": ("○", "dim"),
                "failed": ("🔴", "red"),
            }

            icon, style = status_indicators.get(status, ("○", "dim"))
            text_line = f"{icon} {label}\n"

            if status == "in_progress":
                content.append(text_line, style=f"{style} bold")
            else:
                content.append(text_line, style=style)

        return content


class ActivityPanel(Static):
    """Display scan activity and progress."""

    target = reactive("")
    scan_mode = reactive("")
    model_name = reactive("")
    current_status = reactive("initializing")
    elapsed_seconds = reactive(0)
    estimated_cost = reactive(0.0)
    sandbox_status = reactive("unknown")

    def render(self) -> Text:
        """Render activity status panel."""
        content = Text()
        content.append(f"Target: {self.target}\n")
        content.append(f"Mode: {self.scan_mode}\n")
        content.append(f"Model: {self.model_name}\n")
        content.append(f"Status: {self.current_status}\n")

        # Sandbox status rendering
        sandbox_style = {
            "ready": "green",
            "provisioning": "cyan",
            "unreachable": "red",
            "destroyed": "dim",
        }.get(self.sandbox_status, "dim")
        content.append("Sandbox: ")
        content.append(f"{self.sandbox_status}\n", style=sandbox_style)

        content.append(f"Elapsed: {self.elapsed_seconds}s\n")
        content.append(f"Est. Cost: ${self.estimated_cost:.5f}\n", style="yellow")

        return content


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

        # Cap at the last 150 logs to keep the UI extremely snappy
        display_log = self.activity_log[-150:]

        if not display_log:
            content.append("Waiting for agent activity...", style="dim")
        else:
            for entry in display_log:
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
        self.call_later(self.scroll_end, animate=False)


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
        self.call_later(self.scroll_end, animate=False)


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
        content.append(f"{self.vuln.get('title', 'Unknown')}\n\n", style="bold")

        severity = self.vuln.get("severity", "info")
        severity_colors = {
            "critical": "red",
            "high": "bright_red",
            "medium": "yellow",
            "low": "blue",
            "info": "cyan",
        }
        color = severity_colors.get(severity, "white")
        content.append("Severity: ", style=f"bold {color}")
        content.append(f"{severity.upper()}\n\n")

        description = self.vuln.get("description", "")
        content.append("Description:\n", style="bold")
        content.append(f"{description}\n\n")

        if self.vuln.get("cve_id"):
            content.append("CVE: ", style="bold")
            content.append(f"{self.vuln['cve_id']}\n\n")

        if self.vuln.get("parameter"):
            content.append("Parameter: ", style="bold")
            content.append(f"{self.vuln['parameter']}\n\n")

        if self.vuln.get("poc"):
            content.append("POC:\n", style="bold")
            content.append(f"{self.vuln['poc']}\n")

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


class ScanSummaryModal(ModalScreen):
    """Modal screen showing the scan summary report."""

    DEFAULT_CSS = """
    ScanSummaryModal {
        align: center middle;
    }

    ScanSummaryModal > Vertical {
        width: 85%;
        height: 85%;
        border: solid $accent;
        background: $surface;
        padding: 1 2;
    }

    #summary-scroll {
        height: 1fr;
        margin: 1 0;
    }

    #summary-close-btn {
        align-horizontal: center;
        width: 20;
        margin-top: 1;
    }

    #summary-header {
        width: 100%;
        text-align: center;
        text-style: bold;
        background: $boost;
        color: $accent;
        padding: 1;
        margin-bottom: 1;
    }
    """

    def __init__(
        self,
        target: str,
        scan_mode: str,
        model_name: str,
        scan_status: str,
        elapsed_seconds: int,
        estimated_cost: float,
        vulnerabilities: list,
        agent_responses: dict,
    ):
        super().__init__()
        self.target = target
        self.scan_mode = scan_mode
        self.model_name = model_name
        self.scan_status = scan_status
        self.elapsed_seconds = elapsed_seconds
        self.estimated_cost = estimated_cost
        self.vulnerabilities = vulnerabilities
        self.agent_responses = agent_responses

    def compose(self) -> ComposeResult:
        # Format elapsed time nicely
        minutes, seconds = divmod(self.elapsed_seconds, 60)
        time_str = f"{minutes}m {seconds}s" if minutes > 0 else f"{seconds}s"

        content = Text()
        content.append("📊 SCAN SUMMARY REPORT\n", style="bold underline yellow")
        content.append("=" * 60 + "\n\n")

        # Basic Stats
        content.append("Overview\n", style="bold")
        content.append("• Target: ")
        content.append(f"{self.target}\n", style="cyan")
        content.append("• Scan Mode: ")
        content.append(f"{self.scan_mode}\n", style="cyan")
        content.append("• Model: ")
        content.append(f"{self.model_name}\n", style="cyan")
        content.append("• Status: ")
        content.append(f"{self.scan_status}\n", style="cyan")
        content.append("• Duration: ")
        content.append(f"{time_str}\n", style="cyan")
        content.append("• Estimated Cost: ")
        content.append(f"${self.estimated_cost:.5f}\n\n", style="yellow")

        # Vulnerabilities section
        content.append("Discovered Vulnerabilities\n", style="bold")
        if not self.vulnerabilities:
            content.append("No vulnerabilities found during the scan.\n\n", style="dim")
        else:
            for idx, vuln in enumerate(self.vulnerabilities, 1):
                severity = vuln.get("severity", "info").upper()
                severity_colors = {
                    "CRITICAL": "red",
                    "HIGH": "bright_red",
                    "MEDIUM": "yellow",
                    "LOW": "blue",
                    "INFO": "cyan",
                }
                color = severity_colors.get(severity, "white")
                title = vuln.get("title", "Unknown")
                description = vuln.get("description", "")
                cve = f" ({vuln['cve_id']})" if vuln.get("cve_id") else ""

                content.append(f"{idx}. [{severity}]", style=f"bold {color}")
                content.append(f" {title}{cve}\n", style="bold")
                content.append(f"   {description}\n")
                if vuln.get("poc"):
                    content.append("   PoC: ", style="bold")
                    content.append(f"{vuln['poc']}\n")
                content.append("\n")

        # Agent comments/summaries
        content.append("AI Agent Insights\n", style="bold")
        roles_order = ["reconnaissance", "exploitation", "post_exploitation"]
        has_agents_content = False
        for role in roles_order:
            if role in self.agent_responses and self.agent_responses[role]:
                has_agents_content = True
                role_pretty = role.replace("_", " ").title()
                content.append(f"🤖 {role_pretty} Agent Summary:\n", style="bold green")
                indented_response = "\n".join("   " + line for line in self.agent_responses[role].split("\n"))
                content.append(f"{indented_response}\n\n")
        
        if not has_agents_content:
            content.append("No agent reports or summaries captured yet.\n\n", style="dim")

        with Vertical():
            yield Label("Strix Scan Summary", id="summary-header")
            with VerticalScroll(id="summary-scroll"):
                yield Static(content)
            yield Button("Close [dim](ESC)[/dim]", id="summary-close-btn")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "summary-close-btn":
            self.app.pop_screen()

    def on_key(self, event) -> None:
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
    estimated_cost = reactive(0.0)
    sandbox_status = reactive("unknown")
    goals = reactive({})
    agent_responses = reactive({})
    model_name = reactive("")

    BINDINGS = [
        ("q", "quit", "Quit"),
        ("d", "show_detail", "Detail"),
        ("s", "show_summary", "Summary"),
    ]

    def __init__(
        self,
        target: str,
        scan_mode: str,
        backend_url: str,
        model: Optional[str] = None,
        mock_tools: bool = False,
        instruction: str = "",
        skills: str = "",
        timeout: float = 120.0,
        verbose: bool = False,
    ):
        """Initialize the TUI app."""
        super().__init__()
        self.target = target
        self.scan_mode = scan_mode
        self.backend_url = backend_url
        self.model = model
        self.mock_tools = mock_tools
        self.instruction = instruction
        self.skills = skills
        self.timeout = timeout
        self.verbose = verbose
        self.current_vuln_idx = 0
        self.agent_responses = {}
        self.model_name = model or "default"

    def compose(self) -> ComposeResult:
        """Compose the layout with four panels."""
        with Vertical():
            with Horizontal():
                # Agents panel
                with Vertical(id="agents-container"):
                    yield Label("[bold]Agents[/bold]", id="agents-title")
                    yield AgentsPanel(id="agents-panel")

                # Activity panel
                with Vertical(id="activity-container"):
                    yield Label("[bold]Activity[/bold]", id="activity-title")
                    yield ActivityPanel(id="activity-panel")

                # Vulnerabilities panel
                with Vertical(id="vulnerabilities-container"):
                    yield Label("[bold]Vulnerabilities[/bold]", id="vulnerabilities-title")
                    yield VulnerabilitiesPanel(id="vulnerabilities-panel")

            # Events panel spans full width
            with Vertical(id="events-container"):
                yield Label("[bold]Events[/bold]", id="events-title")
                yield AgentActivityPanel(id="agent-activity-panel")

            # Button container at the bottom
            with Horizontal(id="buttons-container"):
                yield Button("Show Summary [dim](S)[/dim]", id="summary-button")
                yield Button("Quit [dim](Q)[/dim]", id="quit-button")

    def on_mount(self) -> None:
        """Initialize the app and start background scan."""
        # Initialize agent statuses and goals
        for role in ["reconnaissance", "exploitation", "post_exploitation"]:
            self.agents[role] = {"status": "initialized", "iterations": 0}

        self.goals = {
            "bootstrap": "pending",
            "reconnaissance": "pending",
            "exploitation": "pending",
            "post_exploitation": "pending",
        }

        # Wire up reactive updates to widgets
        activity_log_panel = self.query_one("#agent-activity-panel", AgentActivityPanel)
        activity_log_panel.activity_log = self.activity_log

        agents_panel = self.query_one("#agents-panel", AgentsPanel)
        agents_panel.goals = self.goals

        activity_panel = self.query_one("#activity-panel", ActivityPanel)
        activity_panel.target = self.target
        activity_panel.scan_mode = self.scan_mode
        activity_panel.model_name = self.model_name
        activity_panel.current_status = self.scan_status
        activity_panel.sandbox_status = self.sandbox_status
        activity_panel.estimated_cost = self.estimated_cost

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

    def watch_goals(self, goals: dict) -> None:
        """Update goals panel when goals change."""
        try:
            panel = self.query_one("#agents-panel", AgentsPanel)
            panel.goals = goals
        except Exception:
            pass

    def watch_model_name(self, model_name: str) -> None:
        """Update model name in activity panel."""
        try:
            panel = self.query_one("#activity-panel", ActivityPanel)
            panel.model_name = model_name
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

    def watch_sandbox_status(self, status: str) -> None:
        """Update sandbox status in activity panel."""
        try:
            panel = self.query_one("#activity-panel", ActivityPanel)
            panel.sandbox_status = status
        except Exception:
            pass

    def watch_estimated_cost(self, cost: float) -> None:
        """Update estimated cost in activity panel."""
        try:
            panel = self.query_one("#activity-panel", ActivityPanel)
            panel.estimated_cost = cost
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
                        "instruction": self.instruction,
                        "skills": self.skills,
                        "timeout": self.timeout,
                        "verbose": self.verbose,
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
        logger.debug(f"📨 Received event: {event_type}")

        if event_type == "scan_started":
            logger.info("✅ Scan started")
            self.scan_status = "running"
            self.elapsed = 0
            self.model_name = event.get("model", self.model_name)

        elif event_type == "tool_started":
            tool_name = event.get("tool_name", "unknown")
            command = event.get("command", "")
            role = event.get("role", "unknown")

            log = list(self.activity_log)
            msg = f"  🔧 [{role}] Tool Started: {tool_name}"
            if command:
                cmd_preview = command[:80] + "..." if len(command) > 80 else command
                msg += f"\n     Command: {cmd_preview}"
            log.append({
                "level": "info",
                "message": msg,
            })
            self.activity_log = log

        elif event_type == "tool_output":
            tool_name = event.get("tool_name", "unknown")
            output = event.get("output", "")
            role = event.get("role", "unknown")

            log = list(self.activity_log)
            msg = f"  📥 [{role}] Tool Output: {tool_name}"
            if output:
                lines = output.strip().split("\n")
                if len(lines) > 5:
                    lines = lines[:4] + [f"... ({len(lines) - 4} more lines) ..."]
                out_preview = "\n     ".join(lines)
                msg += f"\n     Result:\n     {out_preview}"
            log.append({
                "level": "info",
                "message": msg,
            })
            self.activity_log = log

        elif event_type == "goal_updated":
            goal_id = event.get("goal_id", "")
            status = event.get("status", "pending")
            if goal_id:
                goals = dict(self.goals)
                goals[goal_id] = status
                self.goals = goals

        elif event_type == "sandbox_status":
            status = event.get("status", "unknown")
            self.sandbox_status = status

            error = event.get("error")
            log = list(self.activity_log)
            msg = f"📦 Sandbox status: {status}"
            if error:
                msg += f" (Error: {error})"
            log.append({
                "level": "warning" if status in ("unreachable", "destroyed") else "info",
                "message": msg,
            })
            self.activity_log = log

        elif event_type == "cost_updated":
            cumulative_cost = event.get("cumulative_cost", 0.0)
            self.estimated_cost = cumulative_cost

        elif event_type == "scan_configured":
            # Log scan configuration
            tools_count = event.get("tools_count", 0)
            skills = event.get("skills", [])
            mock_tools = event.get("mock_tools", False)
            logger.info(f"⚙️  Scan configured: {tools_count} tools, skills={skills}, mock={mock_tools}")

            log = list(self.activity_log)
            config_msg = f"⚙️  Configuration: {tools_count} tools"
            if skills:
                config_msg += f", skills: {', '.join(skills)}"
            if mock_tools:
                config_msg += " (mock mode)"
            log.append({
                "level": "info",
                "message": config_msg,
            })
            self.activity_log = log

        elif event_type == "agent_started":
            role = event.get("role", "")
            iteration = event.get("iteration", 0)
            logger.info(f"🤖 Agent started: {role} (iteration {iteration})")
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

        elif event_type == "agent_token_usage":
            # Add token usage to activity log
            input_tokens = event.get("input_tokens", 0)
            output_tokens = event.get("output_tokens", 0)
            cache_read = event.get("cache_read_tokens", 0)
            cache_write = event.get("cache_write_tokens", 0)
            log = list(self.activity_log)
            tokens_summary = f"Tokens → in: {input_tokens}, out: {output_tokens}"
            if cache_read or cache_write:
                tokens_summary += f", cache_read: {cache_read}, cache_write: {cache_write}"
            log.append({
                "level": "info",
                "message": tokens_summary,
            })
            self.activity_log = log

        elif event_type == "agent_thinking":
            # Add agent thinking to activity log
            thinking = event.get("thinking", "")
            if thinking:
                log = list(self.activity_log)
                log.append({
                    "level": "info",
                    "message": f"💭 Thinking: {thinking[:200]}...",
                })
                self.activity_log = log

        elif event_type == "tool_executed":
            # Add tool execution to activity log
            tool_name = event.get("tool_name", "unknown")
            log = list(self.activity_log)

            message = f"  → Tool: {tool_name}"

            # Add command if available
            if event.get("command"):
                command = event["command"]
                # Truncate long commands
                if len(command) > 100:
                    command = command[:97] + "..."
                message += f"\n     Command: {command}"

            # Add output if available
            if event.get("output"):
                output = event["output"]
                if len(output) > 100:
                    output = output[:97] + "..."
                message += f"\n     Result: {output}"

            log.append({
                "level": "info",
                "message": message,
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

            # Store in agent responses for the final summary page
            if role:
                responses = dict(self.agent_responses)
                responses[role] = message
                self.agent_responses = responses

        elif event_type == "agent_completed":
            role = event.get("role", "")
            iteration = event.get("iteration", 0)
            vuln_found = event.get("vulnerabilities_found", 0)
            logger.info(f"✅ Agent completed: {role} ({vuln_found} vulnerabilities)")

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
            logger.info(f"🏁 Scan completed: {vuln_count} vulnerabilities in {duration:.1f}s")
            self.scan_status = f"completed ({vuln_count} vulnerabilities in {duration:.1f}s)"

        elif event_type == "scan_failed":
            error = event.get("error", "Unknown error")
            logger.error(f"❌ Scan failed: {error}")
            self.scan_status = f"failed: {error}"

    def action_quit(self) -> None:
        """Quit the application."""
        self.exit()

    def action_show_detail(self) -> None:
        """Show detail modal for selected vulnerability."""
        if self.current_vuln_idx < len(self.vulnerabilities):
            vuln = self.vulnerabilities[self.current_vuln_idx]
            self.push_screen(VulnerabilityDetailModal(vuln))

    def action_show_summary(self) -> None:
        """Show summary modal."""
        self.push_screen(ScanSummaryModal(
            target=self.target,
            scan_mode=self.scan_mode,
            model_name=self.model_name,
            scan_status=self.scan_status,
            elapsed_seconds=self.elapsed,
            estimated_cost=self.estimated_cost,
            vulnerabilities=self.vulnerabilities,
            agent_responses=self.agent_responses,
        ))

    def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button pressed events."""
        if event.button.id == "summary-button":
            self.action_show_summary()
        elif event.button.id == "quit-button":
            self.action_quit()


@click.command()
@click.option(
    "-t",
    "--target",
    required=True,
    type=str,
    multiple=True,
    help="Target URL to scan (first target will be scanned)",
)
@click.option(
    "-m",
    "--scan-mode",
    type=click.Choice(["quick", "standard", "deep"]),
    default="deep",
    help="Scan intensity level (default: deep)",
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
@click.option(
    "--instruction",
    type=str,
    default="",
    help="Custom instruction for the agent",
)
@click.option(
    "--instruction-file",
    type=click.Path(exists=True, dir_okay=False, readable=True),
    default=None,
    help="Path to a file containing detailed custom instructions for the penetration test.",
)
@click.option(
    "--skills",
    type=str,
    default="",
    help="Comma-separated list of skills to use",
)
@click.option(
    "--timeout",
    type=float,
    default=120.0,
    help="Tool execution timeout in seconds",
)
@click.option(
    "--verbose",
    is_flag=True,
    help="Enable verbose output",
)
@click.option(
    "-n",
    "--non-interactive",
    is_flag=True,
    help="Run in non-interactive mode (ignored in TUI)",
)
@click.option(
    "--scope-mode",
    type=click.Choice(["auto", "diff", "full"]),
    default="auto",
    help="Scope mode for code targets (ignored in TUI)",
)
@click.option(
    "--diff-base",
    type=str,
    default=None,
    help="Target branch or commit to compare against (ignored in TUI)",
)
@click.option(
    "--config",
    type=click.Path(exists=True),
    default=None,
    help="Path to a custom config file (ignored in TUI)",
)
@click.version_option(version="0.1.0-pydantic")
def main(
    target: tuple[str, ...],
    scan_mode: str,
    model: Optional[str],
    backend_url: str,
    mock_tools: bool,
    instruction: str,
    instruction_file: Optional[str],
    skills: str,
    timeout: float,
    verbose: bool,
    non_interactive: bool,
    scope_mode: str,
    diff_base: Optional[str],
    config: Optional[str],
) -> None:
    """Launch Textual TUI client connected to backend service."""
    if instruction and instruction_file:
        raise click.UsageError("Cannot specify both --instruction and --instruction-file. Use one or the other.")

    if instruction_file:
        try:
            with open(instruction_file, "r", encoding="utf-8") as f:
                instruction = f.read().strip()
        except Exception as e:
            raise click.ClickException(f"Failed to read instruction file '{instruction_file}': {e}")

    # Setup logging
    log_file = setup_logging()

    single_target = target[0] if target else ""

    logger.info("=" * 80)
    logger.info("🎨 Starting Strix TUI Client")
    logger.info(f"   Backend URL: {backend_url}")
    logger.info(f"   Target: {single_target}")
    logger.info(f"   Scan Mode: {scan_mode}")
    logger.info(f"   Model: {model or 'default'}")
    logger.info(f"   Mock Tools: {mock_tools}")
    if instruction:
        logger.info(f"   Instruction: {instruction[:60]}...")
    if skills:
        logger.info(f"   Skills: {skills}")
    logger.info("=" * 80)

    app = ScanTUIApp(
        target=single_target,
        scan_mode=scan_mode,
        backend_url=backend_url,
        model=model,
        mock_tools=mock_tools,
        instruction=instruction,
        skills=skills,
        timeout=timeout,
        verbose=verbose,
    )

    logger.info("🚀 Launching TUI app")
    app.run()
    logger.info("✅ TUI closed")


if __name__ == "__main__":
    main()
