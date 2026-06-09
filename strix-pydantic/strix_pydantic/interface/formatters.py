"""Output formatters for enhanced TUI display."""

from typing import Any, Optional

from rich.panel import Panel
from rich.syntax import Syntax
from rich.table import Table
from rich.text import Text

from strix_pydantic.agents.types import Vulnerability


def format_vulnerability_panel(vuln: Vulnerability) -> Panel:
    """
    Format a vulnerability as a styled Rich Panel.

    Color-coded by severity with title, description, and metadata.
    """
    severity_colors = {
        "critical": "red",
        "high": "light_red",
        "medium": "yellow",
        "low": "blue",
        "info": "cyan",
    }
    color = severity_colors.get(vuln.severity, "white")

    # Build content
    lines = []
    lines.append(f"[bold {color}]{vuln.severity.upper()}[/bold {color}]")
    lines.append(f"\n{vuln.description}")

    if vuln.cve_id:
        lines.append(f"\n[dim]CVE: {vuln.cve_id}[/dim]")
    if vuln.parameter:
        lines.append(f"[dim]Parameter: {vuln.parameter}[/dim]")
    if vuln.poc:
        lines.append(f"[dim]PoC available[/dim]")

    content = Text.from_markup("".join(lines))

    return Panel(
        content,
        title=f"[bold]{vuln.title}[/bold]",
        border_style=color,
        expand=False,
    )


def format_tool_output(
    command: str,
    output: str,
    exit_code: int,
    max_lines: int = 20,
) -> Syntax:
    """
    Format tool execution output with syntax highlighting.

    Truncates long output and shows exit code as status badge.
    """
    # Truncate if needed
    lines = output.split("\n")
    if len(lines) > max_lines:
        truncated = "\n".join(lines[:max_lines])
        truncated += f"\n... ({len(lines) - max_lines} more lines)"
    else:
        truncated = output

    # Syntax highlight shell output
    return Syntax(truncated, "bash", theme="monokai", line_numbers=False)


def format_scan_summary(
    run_id: str,
    target: str,
    vulnerabilities: list[dict[str, Any]],
    duration_seconds: float,
    agent_iterations: int,
    tools_executed: int,
    error: Optional[str] = None,
) -> Table:
    """
    Format scan summary as a Rich Table.

    Shows statistics, vulnerability counts by severity, and run metadata.
    """
    table = Table(title="Scan Summary", show_header=True, header_style="bold blue")

    table.add_column("Metric", style="cyan")
    table.add_column("Value", style="green")

    # Basic stats
    table.add_row("Run ID", run_id)
    table.add_row("Target", target)
    table.add_row("Duration", f"{duration_seconds:.1f}s")
    table.add_row("Agent Iterations", str(agent_iterations))
    table.add_row("Tools Executed", str(tools_executed))

    # Vulnerability counts by severity
    severity_counts = {
        "critical": 0,
        "high": 0,
        "medium": 0,
        "low": 0,
        "info": 0,
    }

    for vuln in vulnerabilities:
        severity = vuln.get("severity", "info").lower()
        if severity in severity_counts:
            severity_counts[severity] += 1

    total_vulns = sum(severity_counts.values())
    table.add_row("Total Vulnerabilities", str(total_vulns))

    for severity in ["critical", "high", "medium", "low", "info"]:
        count = severity_counts[severity]
        if count > 0:
            severity_color = {
                "critical": "red",
                "high": "light_red",
                "medium": "yellow",
                "low": "blue",
                "info": "cyan",
            }[severity]
            table.add_row(
                f"  {severity.capitalize()}",
                f"[{severity_color}]{count}[/{severity_color}]",
            )

    # Error status if present
    if error:
        table.add_row("[bold red]Error[/bold red]", error)
    else:
        table.add_row("[bold green]Status[/bold green]", "Completed")

    return table


def format_agent_start(role: str, iteration: int) -> Text:
    """Format agent start message."""
    return Text(f"🤖 Starting {role} agent (iteration {iteration})", style="cyan bold")


def format_tool_start(tool_name: str) -> Text:
    """Format tool execution start message."""
    return Text(f"🔧 Executing {tool_name}...", style="yellow")


def format_tool_complete(tool_name: str, exit_code: int) -> Text:
    """Format tool execution complete message."""
    if exit_code == 0:
        return Text(f"✅ {tool_name} completed", style="green")
    else:
        return Text(f"⚠ {tool_name} exited with code {exit_code}", style="yellow")


def format_sandbox_created() -> Text:
    """Format sandbox creation message."""
    return Text("🐳 Docker sandbox initialized", style="green")


def format_health_check() -> Text:
    """Format health check message."""
    return Text("✓ Sandbox health check passed", style="green")
