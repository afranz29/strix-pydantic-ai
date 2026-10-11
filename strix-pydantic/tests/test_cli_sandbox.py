from __future__ import annotations

import pytest

from strix_pydantic.runtime import bootstrap


@pytest.mark.asyncio
async def test_initialize_sandbox_uses_external_url(monkeypatch):
    monkeypatch.setenv("STRIX_SANDBOX_TOKEN", "token-123")

    sandbox_url, auth_token, runtime, sandbox_info = await bootstrap.initialize_sandbox(
        "run_123", "http://127.0.0.1:48081"
    )

    assert sandbox_url == "http://127.0.0.1:48081"
    assert auth_token == "token-123"
    assert runtime is None
    assert sandbox_info is None


@pytest.mark.asyncio
async def test_initialize_sandbox_creates_docker_runtime(monkeypatch):
    created = {}

    class FakeRuntime:
        async def create_sandbox(self, run_id):
            created["run_id"] = run_id
            return {
                "workspace_id": "container-1",
                "api_url": "http://127.0.0.1:48081",
                "auth_token": "generated-token",
                "tool_server_port": 48081,
                "caido_port": 48080,
                "agent_id": run_id,
            }

    import strix_pydantic.runtime.docker_runtime as docker_runtime_module

    monkeypatch.setattr(docker_runtime_module, "DockerRuntime", FakeRuntime)

    sandbox_url, auth_token, runtime, sandbox_info = await bootstrap.initialize_sandbox(
        "run_456", None
    )

    assert created["run_id"] == "run_456"
    assert sandbox_url == "http://127.0.0.1:48081"
    assert auth_token == "generated-token"
    assert isinstance(runtime, FakeRuntime)
    assert sandbox_info["workspace_id"] == "container-1"
