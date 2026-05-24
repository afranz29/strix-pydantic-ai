"""Docker sandbox client for tool execution with timeout and error handling."""

from dataclasses import dataclass
from typing import Any, Literal, Optional

import httpx
from pydantic import BaseModel, Field


ToolErrorCode = Literal[
    "auth_error",
    "tool_not_found",
    "invalid_arguments",
    "tool_timeout",
    "tool_cancelled",
    "tool_runtime_error",
    "transport_error",
]


class ToolError(BaseModel):
    """Tool execution error with retriable flag."""

    code: ToolErrorCode
    message: str
    retriable: bool = False
    details: Optional[dict[str, Any]] = None


class ToolExecutionRequest(BaseModel):
    """Request to execute a tool in sandbox."""

    agent_id: str = Field(min_length=1)
    tool_name: str = Field(min_length=1)
    kwargs: dict[str, Any] = Field(default_factory=dict)


class ToolExecutionResponse(BaseModel):
    """Response from tool execution."""

    ok: bool
    result: Any | None = None
    error: Optional[ToolError] = None


@dataclass
class SandboxToolResult:
    """Normalized result from sandbox tool execution."""

    ok: bool
    result: Any | None
    error_code: Optional[str]
    error_message: Optional[str]
    retriable: bool


class SandboxClient:
    """Client for Docker sandbox tool execution."""

    def __init__(
        self,
        base_url: str,
        connect_timeout: float = 5.0,
        execute_timeout: float = 120.0,
        orchestrator_step_timeout: float = 180.0,
    ):
        """
        Initialize sandbox client.

        Args:
            base_url: Base URL of sandbox server (e.g., http://127.0.0.1:8000)
            connect_timeout: Network connection timeout in seconds
            execute_timeout: Per-tool execution timeout in seconds
            orchestrator_step_timeout: Orchestrator step outer bound in seconds
        """
        self.base_url = base_url.rstrip("/")
        self.connect_timeout = connect_timeout
        self.execute_timeout = execute_timeout
        self.orchestrator_step_timeout = orchestrator_step_timeout
        self._client: Optional[httpx.AsyncClient] = None

    async def __aenter__(self):
        """Async context manager entry."""
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=httpx.Timeout(self.connect_timeout),
            verify=True,
        )
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        """Async context manager exit."""
        if self._client:
            await self._client.aclose()

    def _ensure_client(self):
        """Ensure client is initialized."""
        if self._client is None:
            raise RuntimeError(
                "SandboxClient not initialized. Use 'async with SandboxClient(...)' "
                "or call __aenter__() first."
            )

    async def execute_tool(
        self,
        agent_id: str,
        tool_name: str,
        kwargs: dict[str, Any],
    ) -> SandboxToolResult:
        """
        Execute a tool in the sandbox.

        Args:
            agent_id: ID of the agent executing the tool
            tool_name: Name of the tool to execute
            kwargs: Tool arguments

        Returns:
            SandboxToolResult with normalized success/error state
        """
        self._ensure_client()

        req = ToolExecutionRequest(agent_id=agent_id, tool_name=tool_name, kwargs=kwargs)

        try:
            response = await self._client.post(
                "/execute",
                json=req.model_dump(),
                timeout=httpx.Timeout(self.execute_timeout),
            )
            response.raise_for_status()
        except httpx.ConnectError as e:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="transport_error",
                error_message=f"Failed to connect to sandbox: {e}",
                retriable=True,
            )
        except httpx.TimeoutException as e:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="tool_timeout",
                error_message=f"Sandbox connection timeout: {e}",
                retriable=True,
            )
        except httpx.HTTPStatusError as e:
            return self._handle_http_error(e)
        except httpx.RequestError as e:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="transport_error",
                error_message=str(e),
                retriable=True,
            )

        try:
            resp = ToolExecutionResponse.model_validate_json(response.content)
        except Exception as e:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="transport_error",
                error_message=f"Invalid response from sandbox: {e}",
                retriable=False,
            )

        if resp.ok:
            return SandboxToolResult(
                ok=True,
                result=resp.result,
                error_code=None,
                error_message=None,
                retriable=False,
            )

        if resp.error:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code=resp.error.code,
                error_message=resp.error.message,
                retriable=resp.error.retriable,
            )

        return SandboxToolResult(
            ok=False,
            result=None,
            error_code="tool_runtime_error",
            error_message="Unknown error from sandbox",
            retriable=False,
        )

    def _handle_http_error(self, error: httpx.HTTPStatusError) -> SandboxToolResult:
        """Handle HTTP errors from sandbox."""
        status_code = error.response.status_code

        if status_code == 401 or status_code == 403:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="auth_error",
                error_message=f"Authentication failed: {status_code}",
                retriable=False,
            )

        if status_code == 404:
            try:
                resp = ToolExecutionResponse.model_validate_json(error.response.content)
                if resp.error and resp.error.code == "tool_not_found":
                    return SandboxToolResult(
                        ok=False,
                        result=None,
                        error_code="tool_not_found",
                        error_message=resp.error.message,
                        retriable=False,
                    )
            except Exception:
                pass

            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="tool_not_found",
                error_message="Tool not found in sandbox",
                retriable=False,
            )

        if status_code == 408 or status_code == 504:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="tool_timeout",
                error_message=f"Sandbox timeout: {status_code}",
                retriable=True,
            )

        if status_code >= 500:
            return SandboxToolResult(
                ok=False,
                result=None,
                error_code="transport_error",
                error_message=f"Sandbox server error: {status_code}",
                retriable=True,
            )

        return SandboxToolResult(
            ok=False,
            result=None,
            error_code="transport_error",
            error_message=f"HTTP {status_code}",
            retriable=False,
        )
