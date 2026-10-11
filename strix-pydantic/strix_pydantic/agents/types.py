"""Shared types for graph orchestration and agent execution."""

from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable, Literal, Optional

from pydantic import BaseModel
from pydantic_ai import Agent, ModelMessage


class Vulnerability(BaseModel):
    title: str
    severity: Literal["critical", "high", "medium", "low", "info"]
    description: str
    cve_id: str | None = None
    parameter: str | None = None
    poc: str | None = None


class AgentOutput(BaseModel):
    summary: str
    vulnerabilities: list[Vulnerability] = []
    notes: list[str] = []


@dataclass
class RunConfig:
    """Configuration for a run."""

    model_name: str
    scan_mode: str  # "quick" | "standard" | "deep"
    non_interactive: bool
    connect_timeout: float = 5.0
    execute_timeout: float = 120.0
    orchestrator_step_timeout: float = 180.0


@dataclass
class StrixRunState:
    """Mutable graph state flowing through all nodes."""

    # Run identity
    run_id: str
    target: str
    scan_mode: str  # "quick" | "standard" | "deep"
    instruction: str = ""  # Custom instruction for the agent

    # Resolved in BootstrapRun
    active_skills: list[str] = field(default_factory=list)

    # Per-role message history for Agent.run(message_history=...) continuity
    message_history: dict[str, list[ModelMessage]] = field(default_factory=dict)

    # Agent lifecycle
    agent_statuses: dict[str, str] = field(default_factory=dict)  # role -> status string

    # Agent response text, keyed by role
    agent_responses: dict[str, str] = field(default_factory=dict)

    # Findings
    vulnerabilities: list[dict[str, Any]] = field(default_factory=list)
    notes: list[dict[str, Any]] = field(default_factory=list)
    tool_observations: list[dict[str, Any]] = field(default_factory=list)

    # Iteration guard (mirrors AgentState.iteration / max_iterations)
    iteration: int = 0
    max_iterations: int = 300

    # Terminal state
    final_summary: str | None = None
    error: str | None = None
    total_cost: float = 0.0


@dataclass
class StrixDeps:
    """Read-only dependencies injected at graph.run() time."""

    # Do not embed non-serializable objects (Agent instances, asyncio.Event)
    # pydantic-graph owns state serialization; complex objects go in closure context

    sandbox_url: str  # e.g., "http://127.0.0.1:8000"
    tool_registry: Any  # ToolRegistry instance
    run_config: RunConfig
    agents: dict[str, Agent[Any, Any]]  # Pre-constructed Agent[StrixDeps, ...] per role
    sandbox_client: Any = None  # SandboxClient instance, initialized in orchestrator

    # Called between roles: (completed_role, next_role, summary_snippet, vuln_count) -> bool
    confirm_proceed: Optional[Callable[[str, str, str, int], bool]] = None

    # Optional TUI callbacks for real-time dashboard updates
    ui_update_agent_status: Optional[Callable[[str, str, int], None]] = None  # (role, status, iterations)
    ui_add_output: Optional[Callable[[str, str], None]] = None  # (message, style)
    ui_show_vulnerability: Optional[Callable[[str, str, str], None]] = None  # (title, severity, desc)

    # Optional event emitter for backend service integration
    event_emitter: Optional[Callable[[str, dict[str, Any]], Awaitable[None]]] = None  # (event_type, payload)

    # Optional context for tracking
    parent_context: dict[str, Any] = field(default_factory=dict)
