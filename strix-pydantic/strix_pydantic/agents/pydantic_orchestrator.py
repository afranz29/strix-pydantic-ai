"""Pydantic Graph orchestrator for non-interactive scans."""

import logging
from typing import Any

from pydantic_graph import End, GraphBuilder

from .types import RunConfig, StrixDeps, StrixRunState

logger = logging.getLogger(__name__)


def _log_agent_result(role: str, result: Any) -> None:
    """Log token usage at INFO and full message sequence at DEBUG."""
    usage = result.usage
    logger.debug(
        f"   Tokens — input: {usage.input_tokens or 0}, "
        f"output: {usage.output_tokens or 0}, "
        f"cache_read: {usage.cache_read_tokens or 0}, "
        f"cache_write: {usage.cache_write_tokens or 0}"
    )

    for msg in result.all_messages():
        for part in getattr(msg, "parts", []):
            kind = getattr(part, "part_kind", None)
            if kind == "tool-call":
                logger.debug(f"   [tool-call] {part.tool_name}({str(part.args)[:200]})")
            elif kind == "tool-return":
                logger.debug(f"   [tool-return] {part.tool_name} → {str(part.content)[:200]}")
            elif kind == "text" and part.content:
                logger.debug(f"   [text] {part.content[:300]}")
            elif kind == "thinking" and part.content:
                logger.debug(f"   [thinking] {part.content[:300]}")


def build_orchestrator_graph() -> Any:
    """
    Build the orchestration graph using GraphBuilder.

    Single unified orchestrator step handles full workflow loop internally.

    Returns:
        Configured Graph ready for execution with graph.run(state=..., deps=...)
    """
    g = GraphBuilder(
        state_type=StrixRunState,
        deps_type=StrixDeps,
        output_type=str,
    )

    @g.step
    async def orchestrate(ctx) -> End[str]:
        """
        Execute full scan orchestration workflow.

        Handles bootstrap, agent turns, tool dispatch, and finalization
        in a single unified step.
        """
        state = ctx.state
        deps = ctx.deps

        # Initialize sandbox client for tool execution
        if deps.sandbox_client:
            await deps.sandbox_client.__aenter__()
            logger.info(f"✅ [BOOTSTRAP] Initialized sandbox client")

        try:
            return await _run_orchestration(state, deps)
        finally:
            # Cleanup sandbox client
            if deps.sandbox_client:
                await deps.sandbox_client.__aexit__(None, None, None)
                logger.info(f"✅ [CLEANUP] Closed sandbox client")

    # Connect start node to orchestrator step
    g.add(g.edge_from(g.start_node).to(orchestrate))

    # Build and return graph
    return g.build(validate_graph_structure=False)


async def _run_orchestration(state: StrixRunState, deps: StrixDeps) -> End[str]:
    """Run the main orchestration workflow with proper error handling."""
    # ========== BOOTSTRAP ==========
    logger.info(f"🚀 [BOOTSTRAP] Run {state.run_id} starting")
    logger.info(f"   Target: {state.target}")
    logger.info(f"   Scan Mode: {state.scan_mode}")
    logger.info(f"   Model: {deps.run_config.model_name}")

    # Validate target
    if not state.target:
        state.error = "Target URL is required"
        logger.error(f"❌ [ABORT] Target is empty")
        return End(f"Aborted: Target URL is required")

    # Initialize message history for all agent roles
    for role in deps.agents.keys():
        if role not in state.message_history:
            state.message_history[role] = []
        state.agent_statuses[role] = "initialized"

    logger.info(f"✅ [BOOTSTRAP] Initialized {len(deps.agents)} agent roles")

    # ========== AGENT LOOP ==========
    roles = list(deps.agents.keys())
    if not roles:
        state.error = "No agents configured"
        logger.error("❌ [ABORT] No agents")
        return End("Aborted: No agents configured")

    prior_outputs: dict[str, str] = {}

    for role in roles:
        state.iteration += 1
        agent = deps.agents[role]
        logger.info(f"🤖 [AGENT] {role} (iteration {state.iteration})")

        # Build prompt — inject prior role outputs as context
        base = f"Target: {state.target}. " + (
            state.instruction if state.instruction else f"Scan mode: {state.scan_mode}."
        )
        if prior_outputs:
            findings_block = "\n\n".join(
                f"=== {r} findings ===\n{out}" for r, out in prior_outputs.items()
            )
            prompt = f"{base}\n\nPrevious agent findings:\n{findings_block}"
        else:
            prompt = base

        try:
            result = await agent.run(
                user_prompt=prompt,
                message_history=state.message_history.get(role, []),
                deps=deps,
            )
        except Exception as e:
            logger.error(f"❌ [AGENT] {role} error: {e}", exc_info=True)
            state.error = f"Agent execution error: {e}"
            return End(f"Aborted: {state.error}")

        _log_agent_result(role, result)

        output = result.output
        summary = output.summary if hasattr(output, "summary") else str(output)

        state.message_history[role] = result.all_messages()
        state.agent_responses[role] = summary
        state.agent_statuses[role] = "completed"
        prior_outputs[role] = summary

        if hasattr(output, "vulnerabilities"):
            for v in output.vulnerabilities:
                state.vulnerabilities.append(v.model_dump())
            logger.info(f"   Extracted {len(output.vulnerabilities)} vulnerabilities")

        if hasattr(output, "notes"):
            for n in output.notes:
                state.notes.append({"content": n, "role": role})

        logger.info(f"✅ [AGENT] {role} completed ({len(summary)} chars)")

        # Between-phase confirmation gate
        next_role_index = roles.index(role) + 1
        if deps.confirm_proceed is not None and next_role_index < len(roles):
            next_role = roles[next_role_index]
            vuln_count = len(state.vulnerabilities)
            snippet = summary[:300].rstrip()
            if not deps.confirm_proceed(role, next_role, snippet, vuln_count):
                logger.info(f"⏹  [CONFIRM] User stopped scan after {role}")
                break

    # ========== FINALIZE ==========
    logger.info(f"📊 [FINALIZE] Generating summary")

    # Build final summary
    summary_lines = [
        f"Run {state.run_id} completed",
        f"Target: {state.target}",
        f"Scan mode: {state.scan_mode}",
        f"Iterations: {state.iteration}/{state.max_iterations}",
    ]

    if state.vulnerabilities:
        summary_lines.append(f"Vulnerabilities found: {len(state.vulnerabilities)}")

    if state.notes:
        summary_lines.append(f"Notes: {len(state.notes)}")

    state.final_summary = "\n".join(summary_lines)

    logger.info(f"✅ [FINALIZE] Run complete")

    return End("\n".join(summary_lines))
