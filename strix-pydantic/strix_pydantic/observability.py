"""Logfire observability setup. No-ops when neither LOGFIRE_TOKEN nor linked project credentials exist."""

import os
from pathlib import Path


def logfire_enabled() -> bool:
    """True if LOGFIRE_TOKEN is set or `logfire projects use` linked a project."""
    if os.getenv("LOGFIRE_TOKEN"):
        return True
    creds_dir = Path(os.getenv("LOGFIRE_CREDENTIALS_DIR", ".logfire"))
    return (creds_dir / "logfire_credentials.json").is_file()


def configure_logfire() -> None:
    if not logfire_enabled():
        return

    import logfire

    logfire.configure()
    logfire.instrument_pydantic_ai()
    logfire.instrument_httpx()
