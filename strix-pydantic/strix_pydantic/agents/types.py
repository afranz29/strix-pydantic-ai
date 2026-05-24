"""Shared types for graph orchestration and agent execution."""

from dataclasses import dataclass, field
from typing import Any

from pydantic_ai import Agent, ModelMessage


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

    # Resolved in BootstrapRun
    active_skills: list[str] = field(default_factory=list)

    # Per-role message history for Agent.run(message_history=...) continuity
    message_history: dict[str, list[ModelMessage]] = field(default_factory=dict)

    # Agent lifecycle
    agent_statuses: dict[str, str] = field(default_factory=dict)  # role -> status string

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


@dataclass
class StrixDeps:
    """Read-only dependencies injected at graph.run() time."""

    # Do not embed non-serializable objects (Agent instances, asyncio.Event)
    # pydantic-graph owns state serialization; complex objects go in closure context

    sandbox_url: str  # e.g., "http://127.0.0.1:8000"
    tool_registry: Any  # ToolRegistry instance
    run_config: RunConfig
    agents: dict[str, Agent[Any, Any]]  # Pre-constructed Agent[StrixDeps, ...] per role

    # Optional context for tracking
    parent_context: dict[str, Any] = field(default_factory=dict)
