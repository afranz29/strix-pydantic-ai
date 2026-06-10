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


def run_backend(backend_url: str = "http://0.0.0.0:8000") -> subprocess.Popen:
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
            "info",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return process


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
    """Start backend service and launch Textual TUI client."""

    # Start backend
    click.echo("🚀 Starting backend service...", err=True)
    backend_process = run_backend(backend_url)

    try:
        # Wait for backend to be ready
        click.echo("⏳ Waiting for backend to be ready...", err=True)
        ready = asyncio.run(wait_for_backend(backend_url))

        if not ready:
            click.echo("❌ Backend failed to start or didn't respond in time", err=True)
            backend_process.terminate()
            sys.exit(1)

        click.echo("✅ Backend is ready!", err=True)
        click.echo("🎨 Launching TUI client...\n", err=True)

        # Launch TUI client
        tui_args = [
            sys.executable,
            "-m",
            "strix_pydantic.service.tui_client",
            "--target",
            target,
            "--scan-mode",
            scan_mode,
            "--backend-url",
            backend_url,
        ]
        if model:
            tui_args.extend(["--model", model])
        if mock_tools:
            tui_args.append("--mock-tools")

        tui_process = subprocess.run(tui_args)

        # Exit with TUI exit code
        sys.exit(tui_process.returncode)

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
        click.echo("✅ Cleanup complete", err=True)


if __name__ == "__main__":
    main()
