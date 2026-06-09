"""Non-interactive CLI with Go-style output parity."""

import asyncio
import logging
import signal
import sys
import uuid
from pathlib import Path
from typing import Any, Optional

import click

from strix_pydantic.agents.pydantic_orchestrator import build_orchestrator_graph
from strix_pydantic.agents.types import RunConfig, StrixDeps, StrixRunState
from strix_pydantic.config.model_config import normalize_model_spec, resolve_model_config
from strix_pydantic.interface.tui import StrixTUIApp, OperationStatus
from strix_pydantic.runtime.bootstrap import initialize_sandbox
from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory
from strix_pydantic.tools.tool_registry import ToolRegistry

logger = logging.getLogger(__name__)


class _StrixFilter(logging.Filter):
    """Pass only records from strix_pydantic.* loggers to suppress third-party noise."""

    def filter(self, record: logging.LogRecord) -> bool:
        return record.name.startswith("strix_pydantic")


def setup_logging(log_file: Optional[Path] = None, verbose: bool = False) -> Path:
    """
    Setup logging with file output and live console output.

    Logs to current directory by default (./strix_<uuid>.log).

    Returns:
        Path to log file
    """
    if log_file is None:
        log_file = Path.cwd() / f"strix_{uuid.uuid4().hex[:8]}.log"

    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)  # capture everything; handlers filter by level

    file_handler = logging.FileHandler(log_file)
    file_handler.setLevel(logging.DEBUG)  # log file always gets full detail
    file_handler.setFormatter(
        logging.Formatter(
            "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
            datefmt="%Y-%m-%d %H:%M:%S",
        )
    )
    root_logger.addHandler(file_handler)

    console_handler = logging.StreamHandler()
    console_handler.setFormatter(logging.Formatter("%(message)s"))
    console_handler.addFilter(_StrixFilter())
    console_handler.setLevel(logging.DEBUG)  # DEBUG by default
    root_logger.addHandler(console_handler)

    return log_file


@click.command(no_args_is_help=True)
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
@click.option(
    "--confirm",
    is_flag=True,
    help="Pause and ask for confirmation between agent phases",
)
@click.option(
    "--ui/--no-ui",
    default=True,
    help="Use Textual UI for progress display (default: enabled)",
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
    confirm: bool,
    ui: bool,
) -> None:
    """
    Run a non-interactive Strix security scan.

    Example:
        strix --target https://example.com --scan-mode standard --skills reconnaissance
    """
    # Setup logging
    if log_file:
        log_path = setup_logging(Path(log_file), verbose=verbose)
    else:
        log_path = setup_logging(verbose=verbose)

    # Initialize TUI if requested
    tui_app = StrixTUIApp(use_ui=ui)
    if ui:
        tui_app.start()

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
            run_state=state,
        )

        deps = StrixDeps(
            sandbox_url=sandbox_url,
            tool_registry=tool_registry,
            run_config=run_config,
            agents=agents,
            sandbox_client=sandbox_client,
            confirm_proceed=_make_confirm_callback() if confirm else None,
        )

        click.echo(f"✅ Initialized {len(agents)} agent roles")

    except Exception as e:
        click.echo(f"❌ Failed to initialize agents: {e}", err=True)
        logger.error(f"Agent initialization error: {e}", exc_info=True)
        if runtime is not None:
            runtime.cleanup()
        sys.exit(1)

    interrupted = False
    exit_code = 0

    # Register signal handler for emergency cleanup before starting the scan
    if runtime is not None:
        def _signal_cleanup(signum, frame):
            runtime.cleanup()
            raise KeyboardInterrupt

        signal.signal(signal.SIGTERM, _signal_cleanup)
        signal.signal(signal.SIGINT, _signal_cleanup)

    # Run orchestrator
    try:
        click.echo(f"\n🚀 Starting scan for {target}")
        _run_orchestrator(state, deps, verbose)
    except KeyboardInterrupt:
        interrupted = True
        state.error = state.error or "Interrupted by user"
        click.echo("\n⏹  Scan interrupted. Summarizing findings collected so far...")
        logger.info("Scan interrupted by user")
    except Exception as e:
        click.echo(f"❌ Scan failed: {e}", err=True)
        logger.error(f"Orchestrator error: {e}", exc_info=True)
        state.error = state.error or f"Scan failed: {e}"
        exit_code = 1
    finally:
        if runtime and sandbox_info:
            try:
                asyncio.run(runtime.destroy_sandbox(sandbox_info["workspace_id"]))
            except Exception as e:
                logger.warning("Failed to destroy sandbox: %s", e, exc_info=True)

    # Print final summary and TUI display
    _print_final_summary(state, tui_app)
    tui_app.stop()
    if interrupted:
        sys.exit(130)
    if exit_code:
        sys.exit(exit_code)


def _make_confirm_callback():
    """Return a callback that prints a phase summary and prompts the user to continue."""

    def confirm_proceed(completed_role: str, next_role: str, snippet: str, vuln_count: int) -> bool:
        click.echo(f"\n{'─' * 60}")
        click.echo(f"✅ {completed_role} complete — {vuln_count} vulnerabilities found so far")
        if snippet:
            click.echo(f"\n{snippet}{'...' if len(snippet) == 300 else ''}")
        click.echo("")
        return click.confirm(f"Proceed to {next_role}?", default=True)

    return confirm_proceed


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
    run_state: Optional[StrixRunState] = None,
) -> dict[str, Any]:
    """
    Build Agent instances for each role with sandbox tool dispatch.

    Args:
        model_spec: Model specification string (e.g., "anthropic:claude-haiku-4-5")
        skill_list: List of skill IDs
        run_config: Run configuration
        tool_registry: Optional ToolRegistry with registered tools
        sandbox_client: SandboxClient instance for tool execution
        run_state: Mutable run state for incremental tool-observation capture

    Returns:
        Dict of role -> Agent[StrixDeps, Any]
    """
    from pydantic_ai import Agent
    from strix_pydantic.agents.types import AgentOutput
    from strix_pydantic.tools.tool_wrapper import build_tools_from_registry

    agents = {}
    roles = ["reconnaissance", "exploitation", "post_exploitation"]

    # Build skill capabilities
    skill_factory = SkillCapabilityFactory()

    for role in roles:
        # Get skills for this role
        role_skills = skill_list.copy() if skill_list else []

        # Automatically append the selected scan mode methodology as a skill
        # (parity with core strix/llm/llm.py)
        scan_mode_skill = f"scan_modes/{run_config.scan_mode}"
        if scan_mode_skill not in role_skills:
            role_skills.append(scan_mode_skill)

        # Build skill capability
        skill_build = skill_factory.build_for_role(
            role_skills,
            role=role,
            context="parent",
        )

        # Build registry toolset — direct callables for mock mode, sandbox dispatch for real mode
        registry_toolset = None
        if tool_registry and len(tool_registry._tools) > 0:

            def _record_tool_event(event: dict[str, Any], current_role: str = role) -> None:
                if run_state is None:
                    return

                kwargs = event.get("kwargs") if isinstance(event.get("kwargs"), dict) else {}
                result_payload = event.get("result")

                observation: dict[str, Any] = {
                    "role": current_role,
                    "tool": event.get("tool_name", "unknown"),
                    "ok": bool(event.get("ok")),
                }

                if "command" in kwargs:
                    observation["command"] = kwargs.get("command")

                if isinstance(result_payload, dict):
                    for field in ("status", "exit_code", "terminal_id", "working_dir"):
                        if field in result_payload:
                            observation[field] = result_payload[field]

                    content = result_payload.get("content")
                    if isinstance(content, str) and content.strip():
                        observation["content_preview"] = content.strip()[:300]

                    result_error = result_payload.get("error")
                    if result_error:
                        observation["error"] = str(result_error)

                if not observation["ok"]:
                    observation["error_code"] = event.get("error_code")
                    if event.get("error_message"):
                        observation["error"] = event.get("error_message")

                # Keep memory bounded for long-running scans.
                if len(run_state.tool_observations) >= 500:
                    run_state.tool_observations.pop(0)
                run_state.tool_observations.append(observation)

            if sandbox_client:
                registry_toolset = build_tools_from_registry(
                    tool_registry,
                    sandbox_client=sandbox_client,
                    agent_id=role,
                    on_tool_event=_record_tool_event,
                )
                logger.info(f"Built sandbox toolset for {role} with {len(tool_registry._tools)} tools")
            else:
                from pydantic_ai import FunctionToolset

                parent_tools = tool_registry.get_tools_for_context("parent")
                registry_toolset = FunctionToolset(tools=[td.callable for td in parent_tools.values()])
                logger.info(f"Built direct toolset for {role} with {len(parent_tools)} mock tools")

        # Build agent with skill-derived instructions and model spec
        # Include both skill toolset and registry toolset if available
        toolsets = [skill_build.toolset]
        if registry_toolset:
            toolsets.append(registry_toolset)

        # Configure settings for the model
        model_settings = {}

        # Set reasoning effort based on scan mode (parity with core strix)
        if run_config.scan_mode == "quick":
            model_settings["reasoning_effort"] = "medium"
        else:
            model_settings["reasoning_effort"] = "high"

        # Configure safety settings for Gemini models to prevent refusals during security scans
        if "google" in model_spec or "gemini" in model_spec:
            model_settings["google_safety_settings"] = [
                {"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
                {"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
                {"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
                {"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
                {"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "BLOCK_NONE"},
            ]

        agent = Agent(
            model_spec,
            output_type=AgentOutput,
            instructions=skill_build.instructions,
            toolsets=toolsets,
            model_settings=model_settings,
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


def _print_final_summary(state: StrixRunState, tui_app: Optional[StrixTUIApp] = None) -> None:
    """
    Print final summary in Go-style format and TUI display.

    Args:
        state: Run state with findings
        tui_app: Optional TUI app to display summary
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
            description = vuln.get("description", "")
            click.echo(f"   {i}. {title} ({severity})")
            if tui_app:
                tui_app.show_vulnerability(title, severity, description)

    if state.tool_observations:
        total_observations = len(state.tool_observations)
        max_to_show = 20
        observations_to_show = state.tool_observations[-max_to_show:]

        click.echo(f"\n🔧 [TOOL OBSERVATIONS ({total_observations})]")
        if total_observations > max_to_show:
            click.echo(f"   Showing last {max_to_show} observations")

        start_index = total_observations - len(observations_to_show) + 1
        for idx, obs in enumerate(observations_to_show, start=start_index):
            role = obs.get("role", "unknown")
            tool = obs.get("tool", "unknown")
            status = obs.get("status") or ("ok" if obs.get("ok") else "error")
            exit_code = obs.get("exit_code")

            line = f"   {idx}. [{role}] {tool} status={status}"
            if exit_code is not None:
                line += f", exit_code={exit_code}"
            click.echo(line)

            command = obs.get("command")
            if command:
                click.echo(f"      cmd: {str(command)[:180]}")

            preview = obs.get("content_preview")
            if preview:
                click.echo(f"      out: {str(preview).replace(chr(10), ' ')[:180]}")

            error = obs.get("error")
            if error:
                click.echo(f"      err: {str(error)[:180]}")

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

    if tui_app:
        summary = f"Run: {state.run_id}\nIterations: {state.iteration}\nVulnerabilities: {len(state.vulnerabilities)}"
        tui_app.show_summary(summary)
if __name__ == "__main__":
    scan()
