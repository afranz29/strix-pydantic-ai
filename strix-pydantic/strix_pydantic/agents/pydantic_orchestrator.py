"""Pydantic Graph orchestrator for non-interactive scans."""

import logging
from typing import Any

from pydantic_graph import End, GraphBuilder

from .types import RunConfig, StrixDeps, StrixRunState

logger = logging.getLogger(__name__)


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
    max_turns_per_role = 3  # Prevent infinite loops per role
    role_turns = {}  # Track turns per role

    while state.iteration < state.max_iterations:
        state.iteration += 1

        # Select agent (use first role as pilot for now)
        role = list(deps.agents.keys())[0] if deps.agents else None
        if not role:
            state.error = "No agents configured"
            logger.error(f"❌ [ABORT] No agents")
            return End(f"Aborted: No agents configured")

        # Check if this role has exceeded turn limit
        role_turns[role] = role_turns.get(role, 0) + 1
        if role_turns[role] > max_turns_per_role:
            logger.info(f"⏸  [AGENT] {role} reached max turns ({max_turns_per_role})")
            break

        agent = deps.agents[role]
        logger.info(f"🤖 [AGENT] {role} turn {state.iteration} (role turn {role_turns[role]})")

        try:
            # Run agent with message history for continuity
            prompt = state.instruction if state.instruction else f"Target: {state.target}. Scan mode: {state.scan_mode}."
            result = await agent.run(
                user_prompt=prompt,
                message_history=state.message_history.get(role, []),
                deps=deps,
            )

            # Append agent response to history
            state.message_history[role] = result.all_messages()

            # Store response text for display
            state.agent_responses[role] = str(result.output)

            # Update agent status
            state.agent_statuses[role] = "completed"

            logger.info(f"✅ [AGENT] {role} completed turn {state.iteration}")
            logger.info(f"   Response length: {len(str(result.output))} chars")

            # Check if this is the final turn (usually agent signals completion in response)
            response_text = str(result.output).lower()
            if any(word in response_text for word in ["complete", "done", "finished", "summary"]):
                logger.info(f"→  [AGENT] {role} signaled completion")
                break

            # For demo: do 1 turn per role, then move to finalize
            if role_turns[role] >= 1:
                logger.info(f"→  [AGENT] Completed turn for {role}, moving to finalize")
                break

        except Exception as e:
            logger.error(f"❌ [AGENT] {role} error: {e}", exc_info=True)
            state.error = f"Agent execution error: {e}"
            logger.error(f"❌ [ABORT] Agent failed")
            return End(f"Aborted: {state.error}")

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
