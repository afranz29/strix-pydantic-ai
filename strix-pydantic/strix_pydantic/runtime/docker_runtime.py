"""Docker runtime for bootstrapping the shared Strix sandbox image."""

from __future__ import annotations

import contextlib
import os
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

        self._container = None  # docker Container object
        self._tool_server_port: int | None = None
        self._caido_port: int | None = None
        self._tool_server_token: str | None = None

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _container_name(self, run_id: str) -> str:
        return f"strix-pydantic-{run_id[:24]}"

    def _find_available_port(self) -> int:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            return int(sock.getsockname()[1])

    def _resolve_docker_host(self) -> str:
        docker_host = os.environ.get("DOCKER_HOST", "")
        if docker_host:
            parsed = urlparse(docker_host)
            if parsed.scheme in {"tcp", "http", "https"} and parsed.hostname:
                return parsed.hostname
        return "127.0.0.1"

    def _get_extra_hosts(self) -> dict[str, str]:
        extra_hosts = {HOST_GATEWAY_HOSTNAME: "host-gateway"}
        configured = os.environ.get("STRIX_SANDBOX_EXTRA_HOSTS", "")
        if not configured:
            return extra_hosts
        for raw_entry in configured.split(","):
            entry = raw_entry.strip()
            if not entry:
                continue
            parts = [p.strip() for p in entry.split("=", 1)]
            if len(parts) != 2 or not all(parts):
                raise SandboxInitializationError(
                    f"STRIX_SANDBOX_EXTRA_HOSTS: invalid entry '{entry}', use hostname=address format"
                )
            extra_hosts[parts[0]] = parts[1]
        return extra_hosts

    def _ensure_image(self, image_name: str) -> None:
        try:
            self.client.images.get(image_name)
            return
        except ImageNotFound:
            pass
        except DockerException as exc:
            raise SandboxInitializationError(
                f"Failed to inspect Docker image {image_name}: {exc}"
            ) from exc
        try:
            self.client.images.pull(image_name)
        except DockerException as exc:
            raise SandboxInitializationError(
                f"Failed to pull Docker image {image_name}: {exc}"
            ) from exc

    def _recover_container_state(self, container) -> None:
        """Read token and ports back from an existing container's runtime state."""
        for env_var in container.attrs["Config"]["Env"]:
            if env_var.startswith("TOOL_SERVER_TOKEN="):
                self._tool_server_token = env_var.split("=", 1)[1]
                break
        port_bindings = container.attrs.get("NetworkSettings", {}).get("Ports", {})
        ts_key = f"{CONTAINER_TOOL_SERVER_PORT}/tcp"
        if port_bindings.get(ts_key):
            self._tool_server_port = int(port_bindings[ts_key][0]["HostPort"])
        caido_key = f"{CONTAINER_CAIDO_PORT}/tcp"
        if port_bindings.get(caido_key):
            self._caido_port = int(port_bindings[caido_key][0]["HostPort"])

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
        raise SandboxInitializationError(
            f"Sandbox tool server did not become healthy at {health_url}"
        )

    # ------------------------------------------------------------------
    # Container lifecycle
    # ------------------------------------------------------------------

    def _create_container(
        self,
        run_id: str,
        name: str,
        image_name: str,
        execution_timeout: str,
        max_retries: int = 2,
    ):
        """Create a new sandbox container, retrying up to max_retries times."""
        last_error: Exception | None = None
        for attempt in range(max_retries + 1):
            try:
                with contextlib.suppress(NotFound, DockerException):
                    old = self.client.containers.get(name)
                    old.stop(timeout=5)
                    old.remove(force=True)
                    time.sleep(1)

                self._tool_server_port = self._find_available_port()
                self._caido_port = self._find_available_port()
                self._tool_server_token = secrets.token_urlsafe(32)

                container = self.client.containers.run(
                    image_name,
                    command="sleep infinity",
                    detach=True,
                    name=name,
                    hostname=name,
                    ports={
                        f"{CONTAINER_TOOL_SERVER_PORT}/tcp": ("127.0.0.1", self._tool_server_port),
                        f"{CONTAINER_CAIDO_PORT}/tcp": ("127.0.0.1", self._caido_port),
                    },
                    cap_add=["NET_ADMIN", "NET_RAW"],
                    labels={"strix-run-id": run_id},
                    environment={
                        "PYTHONUNBUFFERED": "1",
                        "TOOL_SERVER_PORT": str(CONTAINER_TOOL_SERVER_PORT),
                        "TOOL_SERVER_TOKEN": self._tool_server_token,
                        "STRIX_SANDBOX_EXECUTION_TIMEOUT": execution_timeout,
                        "HOST_GATEWAY": HOST_GATEWAY_HOSTNAME,
                    },
                    extra_hosts=self._get_extra_hosts(),
                    tty=True,
                )
                self._container = container

                host = self._resolve_docker_host()
                self._wait_for_tool_server(host)
                return container

            except (DockerException, RequestsConnectionError, RequestsTimeout) as exc:
                last_error = exc
                self._tool_server_port = None
                self._caido_port = None
                self._tool_server_token = None
                if attempt < max_retries:
                    time.sleep(2**attempt)

        raise SandboxInitializationError(
            f"Failed to create sandbox container after {max_retries + 1} attempts: {last_error}"
        ) from last_error

    def _get_or_create_container(
        self, run_id: str, image_name: str, execution_timeout: str
    ):
        """Return a running container for this run, reusing an existing one if possible."""
        name = self._container_name(run_id)

        # 1. In-memory reference still running?
        if self._container is not None:
            try:
                self._container.reload()
                if self._container.status == "running":
                    return self._container
            except NotFound:
                pass
            self._container = None
            self._tool_server_port = None
            self._caido_port = None
            self._tool_server_token = None

        # 2. Container exists by name?
        try:
            container = self.client.containers.get(name)
            container.reload()
            if container.status != "running":
                container.start()
                time.sleep(2)
            self._container = container
            self._recover_container_state(container)
            return container
        except NotFound:
            pass

        # 3. Container exists by label?
        try:
            candidates = self.client.containers.list(
                all=True, filters={"label": f"strix-run-id={run_id}"}
            )
            if candidates:
                container = candidates[0]
                if container.status != "running":
                    container.start()
                    time.sleep(2)
                self._container = container
                self._recover_container_state(container)
                return container
        except DockerException:
            pass

        # 4. Create fresh
        return self._create_container(run_id, name, image_name, execution_timeout)

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def create_sandbox(self, agent_id: str) -> SandboxInfo:
        image_name = os.environ.get("STRIX_IMAGE", DEFAULT_IMAGE)
        execution_timeout = os.environ.get("STRIX_SANDBOX_EXECUTION_TIMEOUT", "120")
        self._ensure_image(image_name)

        container = self._get_or_create_container(agent_id, image_name, execution_timeout)

        if container.id is None:
            raise SandboxInitializationError("Sandbox container did not return an id.")

        host = self._resolve_docker_host()
        api_url = f"http://{host}:{self._tool_server_port}"

        try:
            await self._register_agent(api_url, agent_id, self._tool_server_token)
        except Exception:
            await self.destroy_sandbox(container.id)
            raise

        return {
            "workspace_id": container.id,
            "api_url": api_url,
            "auth_token": self._tool_server_token,
            "tool_server_port": self._tool_server_port,
            "caido_port": self._caido_port,
            "agent_id": agent_id,
        }

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

    async def destroy_sandbox(self, container_id: str) -> None:
        try:
            container = self.client.containers.get(container_id)
            container.stop(timeout=5)
            container.remove(force=True)
        except (NotFound, DockerException):
            pass
        finally:
            self._container = None
            self._tool_server_port = None
            self._caido_port = None
            self._tool_server_token = None

    def cleanup(self) -> None:
        """Synchronous emergency cleanup — safe to call from signal handlers."""
        container = self._container
        self._container = None
        self._tool_server_port = None
        self._caido_port = None
        self._tool_server_token = None
        if container is not None:
            with contextlib.suppress(NotFound, DockerException):
                container.stop(timeout=5)
                container.remove(force=True)
