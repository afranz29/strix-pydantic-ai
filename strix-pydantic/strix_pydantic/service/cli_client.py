"""Async CLI client that connects to backend service."""

import asyncio
import json
import sys
from typing import Optional

import click
import httpx
from rich.console import Console
from rich.panel import Panel
from rich.text import Text


class ScanClient:
    """Async client for Strix backend service."""

    def __init__(self, base_url: str = "http://localhost:8000", use_ui: bool = True):
        """Initialize scan client."""
        self.base_url = base_url
        self.use_ui = use_ui
        self.console = Console()

        # Agent status tracking
        self.agents_status = {
            "reconnaissance": "initialized",
            "exploitation": "initialized",
            "post_exploitation": "initialized",
        }
        self.agents_iterations = {}
        self.vulnerabilities = []
        self.total_vulnerabilities = 0

    async def run_scan(
        self,
        target: str,
        scan_mode: str = "standard",
        model: Optional[str] = None,
        mock_tools: bool = False,
        confirm: bool = False,
        instruction: str = "",
    ) -> None:
        """Run a scan and stream events."""
        async with httpx.AsyncClient() as client:
            # 1. Start scan
            self.console.print(f"📋 Starting scan for {target}")

            try:
                response = await client.post(
                    f"{self.base_url}/scans",
                    json={
                        "target": target,
                        "scan_mode": scan_mode,
                        "model": model,
                        "mock_tools": mock_tools,
                        "confirm": confirm,
                        "instruction": instruction,
                    },
                    timeout=10.0,
                )
                response.raise_for_status()
                scan_data = response.json()
                scan_id = scan_data["scan_id"]

            except httpx.ConnectError:
                self.console.print(
                    "[red]❌ Failed to connect to backend service at {self.base_url}[/red]"
                )
                self.console.print(
                    "[dim]Make sure to start the backend with: python -m strix_pydantic.service.backend[/dim]"
                )
                sys.exit(1)
            except Exception as e:
                self.console.print(f"[red]❌ Failed to start scan: {e}[/red]")
                sys.exit(1)

            self.console.print(f"[green]✓[/green] Scan started: {scan_id}\n")

            # 2. Connect to WebSocket and stream events
            try:
                async with client.stream(
                    "GET",
                    f"{self.base_url}/scans/{scan_id}/events",
                ) as response:
                    async for line in response.aiter_lines():
                        if not line:
                            continue

                        try:
                            event = json.loads(line)
                            await self._handle_event(event)
                        except json.JSONDecodeError:
                            continue

            except Exception as e:
                self.console.print(f"[red]❌ Connection error: {e}[/red]")
                sys.exit(1)

    async def _handle_event(self, event: dict) -> None:
        """Handle incoming event."""
        event_type = event.get("type")

        if event_type == "scan_started":
            self.console.print(f"🚀 Scan started")
            self.console.print(f"   Target: {event.get('target')}")
            self.console.print(f"   Mode: {event.get('scan_mode')}\n")

        elif event_type == "agent_started":
            role = event.get("role")
            iteration = event.get("iteration", 0)
            self.agents_status[role] = "running"
            self.agents_iterations[role] = iteration
            self.console.print(
                f"🤖 [cyan]Starting {role} (iteration {iteration})[/cyan]"
            )

        elif event_type == "agent_completed":
            role = event.get("role")
            self.agents_status[role] = "completed"
            vuln_count = event.get("vulnerabilities_found", 0)
            self.console.print(
                f"✅ [green]{role} completed ({vuln_count} vulnerabilities)[/green]\n"
            )

        elif event_type == "vulnerability_found":
            title = event.get("title", "Unknown")
            severity = event.get("severity", "info")
            description = event.get("description", "")
            role = event.get("role", "unknown")

            self.vulnerabilities.append(
                {
                    "title": title,
                    "severity": severity,
                    "description": description,
                    "role": role,
                }
            )

            severity_colors = {
                "critical": "red",
                "high": "bright_red",
                "medium": "yellow",
                "low": "blue",
                "info": "cyan",
            }
            color = severity_colors.get(severity, "white")

            severity_badge = f"[bold {color}]({severity.upper()})[/bold {color}]"
            content = f"{severity_badge} {title}\n{description}"
            panel = Panel(content, border_style=color, title="[bold]Vulnerability[/]")
            self.console.print(panel)

        elif event_type == "scan_completed":
            duration = event.get("duration_seconds", 0)
            vuln_count = event.get("vulnerabilities_count", 0)
            iterations = event.get("iterations", 0)

            self.console.print("\n" + "=" * 60)
            self.console.print(
                f"[green]✅ Scan completed successfully[/green]"
            )
            self.console.print(f"   Duration: {duration:.1f}s")
            self.console.print(f"   Vulnerabilities: {vuln_count}")
            self.console.print(f"   Iterations: {iterations}")
            self.console.print("=" * 60)

        elif event_type == "scan_failed":
            error = event.get("error", "Unknown error")
            duration = event.get("duration_seconds", 0)

            self.console.print("\n" + "=" * 60)
            self.console.print(f"[red]❌ Scan failed[/red]")
            self.console.print(f"   Error: {error}")
            self.console.print(f"   Duration: {duration:.1f}s")
            self.console.print("=" * 60)


@click.command()
@click.option("--target", required=True, help="Target URL or host to scan")
@click.option(
    "--scan-mode",
    type=click.Choice(["quick", "standard", "deep"]),
    default="standard",
    help="Scan intensity level",
)
@click.option("--model", type=str, default=None, help="Override LLM model")
@click.option(
    "--mock-tools",
    is_flag=True,
    help="Use mock tools for testing (no Docker required)",
)
@click.option(
    "--confirm",
    is_flag=True,
    help="Pause for confirmation between agent phases",
)
@click.option(
    "--instruction",
    type=str,
    default="",
    help="Custom instruction for the agent",
)
@click.option(
    "--backend-url",
    type=str,
    default="http://localhost:8000",
    help="Backend service URL",
)
@click.option(
    "--ui/--no-ui",
    default=True,
    help="Enable/disable UI (default: enabled)",
)
def scan(
    target: str,
    scan_mode: str,
    model: Optional[str],
    mock_tools: bool,
    confirm: bool,
    instruction: str,
    backend_url: str,
    ui: bool,
) -> None:
    """Run a security scan via the backend service."""
    async def _run():
        client = ScanClient(base_url=backend_url, use_ui=ui)

        try:
            await client.run_scan(
                target=target,
                scan_mode=scan_mode,
                model=model,
                mock_tools=mock_tools,
                confirm=confirm,
                instruction=instruction,
            )
        except KeyboardInterrupt:
            print("\n⏹  Scan interrupted by user")
            sys.exit(130)
        except Exception as e:
            print(f"\n❌ Error: {e}")
            sys.exit(1)

    try:
        asyncio.run(_run())
    except KeyboardInterrupt:
        print("\n⏹  Scan interrupted by user")
        sys.exit(130)


def main():
    """Entry point for CLI."""
    scan()


if __name__ == "__main__":
    main()
