
"""Event schema for backend service event streaming."""

from datetime import datetime
from typing import Any, Literal, Optional

from pydantic import BaseModel


class ScanEvent(BaseModel):
    """Base event emitted during scan execution."""

    type: Literal[
        "scan_started",
        "scan_configured",
        "scan_completed",
        "scan_failed",
        "agent_started",
        "agent_completed",
        "agent_message",
        "agent_thinking",
        "agent_token_usage",
        "vulnerability_found",
        "tool_executed",
        "log_message",
    ]
    timestamp: datetime
    scan_id: str


class ScanStartedEvent(ScanEvent):
    """Emitted when scan begins."""

    type: Literal["scan_started"]
    target: str
    scan_mode: str
    model: str


class ScanConfiguredEvent(ScanEvent):
    """Emitted after tools and skills are configured."""

    type: Literal["scan_configured"]
    tools_count: int
    skills: list[str] = []
    sandbox_url: str
    mock_tools: bool


class AgentStartedEvent(ScanEvent):
    """Emitted when agent phase begins."""

    type: Literal["agent_started"]
    role: str
    iteration: int


class AgentMessageEvent(ScanEvent):
    """Emitted with agent reasoning/output during execution."""

    type: Literal["agent_message"]
    role: str
    iteration: int
    message: str


class AgentCompletedEvent(ScanEvent):
    """Emitted when agent phase completes."""

    type: Literal["agent_completed"]
    role: str
    iteration: int
    vulnerabilities_found: int


class VulnerabilityFoundEvent(ScanEvent):
    """Emitted when vulnerability is discovered."""

    type: Literal["vulnerability_found"]
    role: str
    title: str
    severity: str
    description: str
    cve_id: Optional[str] = None
    parameter: Optional[str] = None
    poc: Optional[str] = None


class AgentThinkingEvent(ScanEvent):
    """Emitted with agent thinking/reasoning from LLM response."""

    type: Literal["agent_thinking"]
    role: str
    iteration: int
    thinking: str


class AgentTokenUsageEvent(ScanEvent):
    """Emitted with LLM token usage statistics."""

    type: Literal["agent_token_usage"]
    role: str
    iteration: int
    input_tokens: int
    output_tokens: int
    cache_read_tokens: int = 0
    cache_write_tokens: int = 0


class ToolExecutedEvent(ScanEvent):
    """Emitted when tool is executed."""

    type: Literal["tool_executed"]
    tool_name: str
    status: Literal["started", "completed", "failed"]
    duration_ms: Optional[float] = None
    exit_code: Optional[int] = None
    error: Optional[str] = None


class LogMessageEvent(ScanEvent):
    """Emitted for log messages."""

    type: Literal["log_message"]
    level: Literal["info", "success", "warning", "error"]
    message: str


class ScanCompletedEvent(ScanEvent):
    """Emitted when scan completes successfully."""

    type: Literal["scan_completed"]
    duration_seconds: float
    vulnerabilities_count: int
    iterations: int


class ScanFailedEvent(ScanEvent):
    """Emitted when scan fails."""

    type: Literal["scan_failed"]
    error: str
    duration_seconds: float


# Union type for all events
AnyEvent = (
    ScanStartedEvent
    | ScanConfiguredEvent
    | AgentStartedEvent
    | AgentMessageEvent
    | AgentThinkingEvent
    | AgentTokenUsageEvent
    | AgentCompletedEvent
    | VulnerabilityFoundEvent
    | ToolExecutedEvent
    | LogMessageEvent
    | ScanCompletedEvent
    | ScanFailedEvent
)
