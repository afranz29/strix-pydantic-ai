"""Sandbox bootstrap helpers that can be tested without importing the orchestrator."""

from __future__ import annotations

import os
from typing import Any, Optional


async def initialize_sandbox(
    run_id: str,
    sandbox_url: Optional[str],
) -> tuple[str, str, Any | None, dict[str, Any] | None]:
    """Resolve sandbox connection details, creating a Docker sandbox when needed."""
    if sandbox_url:
        auth_token = os.getenv("STRIX_SANDBOX_TOKEN", "")
        return sandbox_url, auth_token, None, None

    from strix_pydantic.runtime.docker_runtime import DockerRuntime

    runtime = DockerRuntime()
    sandbox_info = await runtime.create_sandbox(run_id)
    return sandbox_info["api_url"], sandbox_info["auth_token"], runtime, sandbox_info
