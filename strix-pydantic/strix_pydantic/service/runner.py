"""Runner that starts backend service and launches TUI client together."""

import asyncio
import signal
import subprocess
import sys
import time
from typing import Optional

import click
import httpx


async def wait_for_backend(backend_url: str, timeout: int = 10) -> bool:
    """Wait for backend to be ready, with timeout."""
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            async with httpx.AsyncClient() as client:
                response = await client.get(f"{backend_url}/health", timeout=1.0)
                if response.status_code == 200:
                    return True
        except Exception:
            pass
        await asyncio.sleep(0.5)
    return False


def run_backend(
    backend_url: str = "http://0.0.0.0:8000",
    stdout=None,
    stderr=None,
) -> subprocess.Popen:
    """Start the backend service in a subprocess."""
    # Parse host and port
    url_parts = backend_url.replace("http://", "").split(":")
    host = url_parts[0] if url_parts[0] else "0.0.0.0"
    port = int(url_parts[1]) if len(url_parts) > 1 else 8000

    # Start uvicorn
    process = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "uvicorn",
            "strix_pydantic.service.backend:app",
            "--host",
            host,
            "--port",
            str(port),
            "--log-level",
            "debug",
        ],
        stdout=stdout,
        stderr=stderr,
        text=True,
    )
    return process


@click.command()
@click.option(
    "-t",
    "--target",
    required=True,
    type=str,
    multiple=True,
    help="Target URL to scan (can be specified multiple times)",
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
    help="Run in non-interactive mode (ignored in TUI runner)",
)
@click.option(
    "--scope-mode",
    type=click.Choice(["auto", "diff", "full"]),
    default="auto",
    help="Scope mode for code targets (ignored in TUI runner)",
)
@click.option(
    "--diff-base",
    type=str,
    default=None,
    help="Target branch or commit to compare against (ignored in TUI runner)",
)
@click.option(
    "--config",
    type=click.Path(exists=True),
    default=None,
    help="Path to a custom config file (ignored in TUI runner)",
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
    """Start backend service and launch Textual TUI client."""
    import os

    if instruction and instruction_file:
        raise click.UsageError("Cannot specify both --instruction and --instruction-file. Use one or the other.")

    if instruction_file:
        try:
            with open(instruction_file, "r", encoding="utf-8") as f:
                instruction = f.read().strip()
        except Exception as e:
            raise click.ClickException(f"Failed to read instruction file '{instruction_file}': {e}")

    # Redirect backend output to log file to avoid flashing on top of TUI
    logs_dir = os.path.join(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))), "logs")
    os.makedirs(logs_dir, exist_ok=True)
    log_path = os.path.join(logs_dir, "strix_backend.log")
    backend_log = None
    try:
        backend_log = open(log_path, "w", encoding="utf-8")
        stdout_stream = backend_log
        stderr_stream = backend_log
    except Exception as e:
        click.echo(f"⚠️  Could not open backend log file ({e}), redirecting to DEVNULL", err=True)
        stdout_stream = subprocess.DEVNULL
        stderr_stream = subprocess.DEVNULL

    # Start backend
    click.echo("🚀 Starting backend service...", err=True)
    backend_process = run_backend(backend_url, stdout=stdout_stream, stderr=stderr_stream)

    try:
        # Wait for backend to be ready
        click.echo("⏳ Waiting for backend to be ready...", err=True)
        ready = asyncio.run(wait_for_backend(backend_url))

        if not ready:
            click.echo("❌ Backend failed to start or didn't respond in time", err=True)
            if backend_log:
                click.echo(f"ℹ️  Check backend logs for details: {log_path}", err=True)
            backend_process.terminate()
            sys.exit(1)

        click.echo("✅ Backend is ready!", err=True)

        overall_exit_code = 0

        # Run TUI client sequentially for each target
        for idx, single_target in enumerate(target, 1):
            if len(target) > 1:
                click.echo(f"\n🎨 Launching TUI client for target {idx}/{len(target)}: {single_target}...\n", err=True)
            else:
                click.echo("🎨 Launching TUI client...\n", err=True)

            tui_args = [
                sys.executable,
                "-m",
                "strix_pydantic.service.tui_client",
                "--target",
                single_target,
                "--scan-mode",
                scan_mode,
                "--backend-url",
                backend_url,
            ]
            if model:
                tui_args.extend(["--model", model])
            if mock_tools:
                tui_args.append("--mock-tools")
            if instruction:
                tui_args.extend(["--instruction", instruction])
            if skills:
                tui_args.extend(["--skills", skills])
            tui_args.extend(["--timeout", str(timeout)])
            if verbose:
                tui_args.append("--verbose")

            tui_process = subprocess.run(tui_args)
            if tui_process.returncode != 0:
                overall_exit_code = tui_process.returncode

        # Exit with accumulated exit code
        sys.exit(overall_exit_code)

    except KeyboardInterrupt:
        click.echo("\n\n⏹  Interrupted by user", err=True)

    finally:
        # Cleanup: terminate backend
        click.echo("🛑 Stopping backend service...", err=True)
        backend_process.terminate()
        try:
            backend_process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            backend_process.kill()

        # Close log file if opened
        if backend_log:
            try:
                backend_log.close()
            except Exception:
                pass
        click.echo("✅ Cleanup complete", err=True)


if __name__ == "__main__":
    main()
