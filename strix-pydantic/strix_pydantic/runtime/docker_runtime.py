"""Docker runtime for bootstrapping the shared Strix sandbox image."""

from __future__ import annotations

import secrets
import socket
import time
import uuid
from typing import TypedDict
from urllib.parse import urlparse

import docker
import httpx
from docker.errors import DockerException, ImageNotFound, NotFound
from requests.exceptions import ConnectionError as RequestsConnectionError
from requests.exceptions import Timeout as RequestsTimeout

DEFAULT_IMAGE = "ghcr.io/usestrix/strix-sandbox:0.1.13"
CONTAINER_TOOL_SERVER_PORT = 48081
CONTAINER_CAIDO_PORT = 48080
HOST_GATEWAY_HOSTNAME = "host.docker.internal"
DOCKER_TIMEOUT = 60


class SandboxInitializationError(Exception):
    """Raised when the Docker sandbox cannot be initialized."""


class SandboxInfo(TypedDict):
    workspace_id: str
    api_url: str
    auth_token: str
    tool_server_port: int
    caido_port: int
    agent_id: str


class DockerRuntime:
    """Create and clean up the shared sandbox container used by the pydantic port."""

    def __init__(self) -> None:
        try:
            self.client = docker.from_env(timeout=DOCKER_TIMEOUT)
        except (DockerException, RequestsConnectionError, RequestsTimeout) as exc:
            raise SandboxInitializationError(
                "Docker is not available. Ensure Docker is installed and running."
            ) from exc

        self._container_id: str | None = None
        self._tool_server_port: int | None = None
        self._caido_port: int | None = None
        self._tool_server_token: str | None = None

    def _find_available_port(self) -> int:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            return int(sock.getsockname()[1])

    def _resolve_docker_host(self) -> str:
        docker_host = __import__("os").environ.get("DOCKER_HOST", "")
        if docker_host:
            parsed = urlparse(docker_host)
            if parsed.scheme in {"tcp", "http", "https"} and parsed.hostname:
                return parsed.hostname
        return "127.0.0.1"

    def _ensure_image(self, image_name: str) -> None:
        try:
            self.client.images.get(image_name)
            return
        except ImageNotFound:
            pass
        except DockerException as exc:
            raise SandboxInitializationError(f"Failed to inspect Docker image {image_name}: {exc}") from exc

        try:
            self.client.images.pull(image_name)
        except DockerException as exc:
            raise SandboxInitializationError(f"Failed to pull Docker image {image_name}: {exc}") from exc

    def _wait_for_tool_server(self, host: str) -> None:
        if self._tool_server_port is None:
            raise SandboxInitializationError("Sandbox tool server port was not assigned.")

        health_url = f"http://{host}:{self._tool_server_port}/health"
        time.sleep(2)

        for attempt in range(30):
            try:
                with httpx.Client(trust_env=False, timeout=5) as client:
                    response = client.get(health_url)
                    if response.status_code == 200 and response.json().get("status") == "healthy":
                        return
            except (httpx.RequestError, ValueError):
                pass
            time.sleep(min(0.5 * (2**attempt), 5))

        raise SandboxInitializationError(f"Sandbox tool server did not become healthy at {health_url}")

    async def _register_agent(self, api_url: str, agent_id: str, token: str) -> None:
        try:
            async with httpx.AsyncClient(trust_env=False, timeout=30) as client:
                response = await client.post(
                    f"{api_url}/register_agent",
                    params={"agent_id": agent_id},
                    headers={"Authorization": f"Bearer {token}"},
                )
                response.raise_for_status()
        except httpx.RequestError:
            return

    async def create_sandbox(self, agent_id: str) -> SandboxInfo:
        image_name = __import__("os").environ.get("STRIX_IMAGE", DEFAULT_IMAGE)
        execution_timeout = __import__("os").environ.get("STRIX_SANDBOX_EXECUTION_TIMEOUT", "120")
        self._ensure_image(image_name)

        self._tool_server_port = self._find_available_port()
        self._caido_port = self._find_available_port()
        self._tool_server_token = secrets.token_urlsafe(32)
        container_name = f"strix-pydantic-{agent_id[:32]}-{uuid.uuid4().hex[:8]}"

        try:
            container = self.client.containers.run(
                image_name,
                command="sleep infinity",
                detach=True,
                name=container_name,
                hostname=container_name,
                ports={
                    f"{CONTAINER_TOOL_SERVER_PORT}/tcp": ("127.0.0.1", self._tool_server_port),
                    f"{CONTAINER_CAIDO_PORT}/tcp": ("127.0.0.1", self._caido_port),
                },
                cap_add=["NET_ADMIN", "NET_RAW"],
                labels={"strix-scan-id": agent_id},
                environment={
                    "PYTHONUNBUFFERED": "1",
                    "TOOL_SERVER_PORT": str(CONTAINER_TOOL_SERVER_PORT),
                    "TOOL_SERVER_TOKEN": self._tool_server_token,
                    "STRIX_SANDBOX_EXECUTION_TIMEOUT": str(execution_timeout),
                    "HOST_GATEWAY": HOST_GATEWAY_HOSTNAME,
                },
                extra_hosts={HOST_GATEWAY_HOSTNAME: "host-gateway"},
                tty=True,
            )
        except (DockerException, RequestsConnectionError, RequestsTimeout) as exc:
            raise SandboxInitializationError(f"Failed to create sandbox container: {exc}") from exc

        self._container_id = container.id
        if self._container_id is None:
            raise SandboxInitializationError("Sandbox container did not return an id.")

        host = self._resolve_docker_host()
        api_url = f"http://{host}:{self._tool_server_port}"
        try:
            self._wait_for_tool_server(host)
            await self._register_agent(api_url, agent_id, self._tool_server_token)
        except Exception:
            await self.destroy_sandbox(self._container_id)
            raise

        return {
            "workspace_id": self._container_id,
            "api_url": api_url,
            "auth_token": self._tool_server_token,
            "tool_server_port": self._tool_server_port,
            "caido_port": self._caido_port,
            "agent_id": agent_id,
        }

    async def destroy_sandbox(self, container_id: str) -> None:
        try:
            container = self.client.containers.get(container_id)
            container.stop(timeout=5)
            container.remove(force=True)
        except (NotFound, DockerException):
            pass
        finally:
            self._container_id = None
            self._tool_server_port = None
            self._caido_port = None
            self._tool_server_token = None
