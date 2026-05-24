"""Non-interactive CLI with Go-style output parity."""

import asyncio
import logging
import sys
import uuid
from pathlib import Path
from typing import Any, Optional

import click

from strix_pydantic.agents.pydantic_orchestrator import build_orchestrator_graph
from strix_pydantic.agents.types import RunConfig, StrixDeps, StrixRunState
from strix_pydantic.config.model_config import normalize_model_spec, resolve_model_config
from strix_pydantic.runtime.bootstrap import initialize_sandbox
from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory
from strix_pydantic.tools.tool_registry import ToolRegistry

logger = logging.getLogger(__name__)


def setup_logging(log_file: Optional[Path] = None) -> Path:
    """
    Setup logging with optional file output.

    Logs to current directory by default (./strix_<uuid>.log).

    Returns:
        Path to log file
    """
    if log_file is None:
        # Log to current directory
        log_file = Path.cwd() / f"strix_{uuid.uuid4().hex[:8]}.log"

    handler = logging.FileHandler(log_file)
    handler.setFormatter(
        logging.Formatter(
            "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S",
        )
    )

    root_logger = logging.getLogger()
    root_logger.setLevel(logging.INFO)
    root_logger.addHandler(handler)

    return log_file


@click.command()
@click.option(
    "--target",
    required=True,
    type=str,
    help="Target URL or host to scan",
)
@click.option(
    "--scan-mode",
    type=click.Choice(["quick", "standard", "deep"]),
    default="standard",
    help="Scan intensity level",
)
@click.option(
    "--skills",
    type=str,
    default="",
    help="Comma-separated list of skills to use",
)
@click.option(
    "--model",
    type=str,
    default=None,
    help="Override STRIX_LLM model selection",
)
@click.option(
    "--sandbox-url",
    type=str,
    default=None,
    help="URL of an existing sandbox server. If omitted, Strix creates a Docker sandbox.",
)
@click.option(
    "--timeout",
    type=float,
    default=120.0,
    help="Tool execution timeout in seconds",
)
@click.option(
    "--log-file",
    type=click.Path(),
    default=None,
    help="Log file path (default: ~/.strix/run_<uuid>.log)",
)
@click.option(
    "--verbose",
    is_flag=True,
    help="Enable verbose output",
)
@click.option(
    "--mock-tools",
    is_flag=True,
    help="Use mock tools for testing (no Docker required)",
)
@click.option(
    "--instruction",
    type=str,
    default="",
    help="Custom instruction for the agent (overrides default target/scan-mode prompt)",
)
def scan(
    target: str,
    scan_mode: str,
    skills: str,
    model: Optional[str],
    sandbox_url: str,
    timeout: float,
    log_file: Optional[str],
    verbose: bool,
    mock_tools: bool,
    instruction: str,
) -> None:
    """
    Run a non-interactive Strix security scan.

    Example:
        strix --target https://example.com --scan-mode standard --skills reconnaissance
    """
    # Setup logging
    if log_file:
        log_path = setup_logging(Path(log_file))
    else:
        log_path = setup_logging()

    # Print startup lines
    click.echo(f"📋 Strix Non-Interactive Scanner")
    click.echo(f"   Log file: {log_path}")

    # Resolve model config
    try:
        if model:
            model_spec = normalize_model_spec(model)
            model_display = model
        else:
            model_spec, model_display = resolve_model_config()

        click.echo(f"   Model: {model_display}")
    except Exception as e:
        click.echo(f"❌ Failed to resolve model config: {e}", err=True)
        sys.exit(1)

    # Parse skills
    skill_list = [s.strip() for s in skills.split(",") if s.strip()] if skills else []

    # Create run state
    run_id = f"run_{uuid.uuid4().hex[:12]}"
    state = StrixRunState(
        run_id=run_id,
        target=target,
        scan_mode=scan_mode,
        active_skills=skill_list,
        instruction=instruction,
    )

    # Create run config
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode=scan_mode,
        non_interactive=True,
        execute_timeout=timeout,
    )

    runtime = None
    sandbox_info = None
    sandbox_client = None

    # Build agents and dependencies
    try:
        from strix_pydantic.runtime.sandbox_client import SandboxClient

        tool_registry = ToolRegistry()

        # Register mock tools if requested
        if mock_tools:
            from strix_pydantic.tools.mock_tools import register_mock_tools

            click.echo("📦 Using mock tools (testing mode)")
            register_mock_tools(tool_registry)
            sandbox_url = sandbox_url or "http://127.0.0.1:48081"
            sandbox_client = None
        else:
            sandbox_url, auth_token, runtime, sandbox_info = asyncio.run(
                initialize_sandbox(run_id, sandbox_url)
            )
            sandbox_client = SandboxClient(
                base_url=sandbox_url,
                auth_token=auth_token,
                execute_timeout=run_config.execute_timeout,
            )
            if sandbox_info is None:
                click.echo(f"🐳 Using external sandbox at {sandbox_url}")
            else:
                click.echo(f"🐳 Created Docker sandbox at {sandbox_url}")

        if not mock_tools:
            _register_strix_tools(tool_registry)

        agents = _build_agents(
            model_spec,
            skill_list,
            run_config,
            tool_registry,
            sandbox_client=sandbox_client,
        )

        deps = StrixDeps(
            sandbox_url=sandbox_url,
            tool_registry=tool_registry,
            run_config=run_config,
            agents=agents,
            sandbox_client=sandbox_client,
        )

        click.echo(f"✅ Initialized {len(agents)} agent roles")

    except Exception as e:
        click.echo(f"❌ Failed to initialize agents: {e}", err=True)
        logger.error(f"Agent initialization error: {e}", exc_info=True)
        sys.exit(1)

    # Run orchestrator
    try:
        click.echo(f"\n🚀 Starting scan for {target}")
        _run_orchestrator(state, deps, verbose)
    except Exception as e:
        click.echo(f"❌ Scan failed: {e}", err=True)
        logger.error(f"Orchestrator error: {e}", exc_info=True)
        sys.exit(1)
    finally:
        if runtime and sandbox_info:
            try:
                asyncio.run(runtime.destroy_sandbox(sandbox_info["workspace_id"]))
            except Exception as e:
                logger.warning("Failed to destroy sandbox: %s", e, exc_info=True)

    # Print final summary
    _print_final_summary(state)


def _register_strix_tools(tool_registry) -> None:
    """
    Register strix sandbox tools using stub signatures matching the real tool server.

    Tool signatures are hardcoded to match what the sandbox exposes.
    Execution always dispatches to the Docker sandbox.
    """
    from typing import Any

    # Stubs match the real strix tool signatures exactly so pydantic-ai
    # generates correct tool schemas for the LLM.

    async def terminal_execute(
        command: str,
        is_input: bool = False,
        timeout: float | None = None,
        terminal_id: str | None = None,
        no_enter: bool = False,
    ) -> dict[str, Any]:
        """Execute a shell command in the sandbox terminal. Returns output, exit code, and status."""
        pass  # Replaced by sandbox dispatch wrapper

    tool_registry.register_tool(
        name="terminal_execute",
        description="Execute a shell command in the sandbox terminal. Returns output, exit code, and status.",
        callable_obj=terminal_execute,
        contexts={"parent"},
    )

    click.echo("   Registered 1 strix tool: terminal_execute")
    logger.info("Registered strix tool stubs for sandbox dispatch")


def _build_agents(
    model_spec: str,
    skill_list: list[str],
    run_config: RunConfig,
    tool_registry: Optional[Any] = None,
    sandbox_client: Optional[Any] = None,
) -> dict[str, Any]:
    """
    Build Agent instances for each role with sandbox tool dispatch.

    Args:
        model_spec: Model specification string (e.g., "anthropic:claude-haiku-4-5")
        skill_list: List of skill IDs
        run_config: Run configuration
        tool_registry: Optional ToolRegistry with registered tools
        sandbox_client: SandboxClient instance for tool execution

    Returns:
        Dict of role -> Agent[StrixDeps, Any]
    """
    from pydantic_ai import Agent
    from strix_pydantic.tools.tool_wrapper import build_tools_from_registry

    agents = {}
    roles = ["reconnaissance", "exploitation", "post_exploitation"]

    # Build skill capabilities
    skill_factory = SkillCapabilityFactory()

    for role in roles:
        # Get skills for this role
        role_skills = skill_list if skill_list else []

        # Build skill capability
        skill_build = skill_factory.build_for_role(
            role_skills,
            role=role,
            context="parent",
        )

        # Build registry toolset with sandbox dispatch if tools are registered
        registry_toolset = None
        if tool_registry and len(tool_registry._tools) > 0 and sandbox_client:
            registry_toolset = build_tools_from_registry(
                tool_registry,
                sandbox_client=sandbox_client,
                agent_id=role,
            )
            logger.info(f"Built sandbox toolset for {role} with {len(tool_registry._tools)} tools")

        # Build agent with skill-derived instructions and model spec
        # Include both skill toolset and registry toolset if available
        toolsets = [skill_build.toolset]
        if registry_toolset:
            toolsets.append(registry_toolset)

        agent = Agent(
            model_spec,
            instructions=skill_build.instructions,
            toolsets=toolsets,
        )

        agents[role] = agent
        logger.info(f"Built agent for role: {role} with model {model_spec}")

    return agents


def _run_orchestrator(
    state: StrixRunState,
    deps: StrixDeps,
    verbose: bool = False,
) -> None:
    """
    Run the Pydantic Graph orchestrator.

    Args:
        state: Run state
        deps: Dependencies
        verbose: Enable verbose logging
    """
    if verbose:
        logging.getLogger().setLevel(logging.DEBUG)

    # Build graph
    graph = build_orchestrator_graph()

    # Run graph (async wrapper)
    asyncio.run(_run_graph_async(graph, state, deps))


async def _run_graph_async(
    graph: Any,
    state: StrixRunState,
    deps: StrixDeps,
) -> None:
    """
    Run the graph asynchronously.

    Args:
        graph: Pydantic Graph instance
        state: Run state
        deps: Dependencies
    """
    try:
        # Run graph with state and deps
        # The exact API depends on pydantic-graph implementation
        result = graph.run(state=state, deps=deps)

        # For async compatibility, handle both sync and async returns
        if hasattr(result, "__await__"):
            await result
        else:
            # Sync result, already done
            pass

        logger.info(f"Graph completed: {state.final_summary}")

    except Exception as e:
        logger.error(f"Graph execution error: {e}", exc_info=True)
        raise


def _print_final_summary(state: StrixRunState) -> None:
    """
    Print final summary in Go-style format.

    Args:
        state: Run state with findings
    """
    click.echo("\n" + "=" * 60)
    click.echo(f"🤖 [AGENT STATUSES]")
    for role, status in state.agent_statuses.items():
        click.echo(f"   {role}: {status}")

    if state.agent_responses:
        click.echo(f"\n💬 [AGENT RESPONSES]")
        for role, response in state.agent_responses.items():
            click.echo(f"\n--- {role} ---")
            click.echo(response)

    if state.vulnerabilities:
        click.echo(f"\n🚨 [VULNERABILITIES IDENTIFIED ({len(state.vulnerabilities)})]")
        for i, vuln in enumerate(state.vulnerabilities, 1):
            title = vuln.get("title", "Unknown")
            severity = vuln.get("severity", "unknown")
            click.echo(f"   {i}. {title} ({severity})")

    if state.notes:
        click.echo(f"\n📝 [NOTES & FINDINGS ({len(state.notes)})]")
        for i, note in enumerate(state.notes, 1):
            content = note.get("content", "Unknown")
            click.echo(f"   {i}. {content}")

    click.echo("\n" + "=" * 60)
    if state.error:
        click.echo(f"❌ Run Failed: {state.error}")
    else:
        click.echo(f"✅ Scan completed successfully")
        click.echo(f"   Run ID: {state.run_id}")
        click.echo(f"   Total iterations: {state.iteration}")


if __name__ == "__main__":
    scan()
