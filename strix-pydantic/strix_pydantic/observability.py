"""Logfire observability setup. No-ops when LOGFIRE_TOKEN is unset."""

import os


def configure_logfire() -> None:
    if not os.getenv("LOGFIRE_TOKEN"):
        return

    import logfire

    logfire.configure()
    logfire.instrument_pydantic_ai()
    logfire.instrument_httpx()
