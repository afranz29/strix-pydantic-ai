"""Pydantic Graph orchestrator for non-interactive scans (v1.102.0 compatible)."""

import logging
from typing import Any

from pydantic_graph import End, GraphBuilder

from .types import RunConfig, StrixDeps, StrixRunState

logger = logging.getLogger(__name__)


def build_orchestrator_graph() -> Any:
    """
    Build the orchestration graph using GraphBuilder (v1.102.0 API).

    Single unified orchestrator step handles full workflow loop internally.
    This avoids complex multi-node routing in v1.102.0 GraphBuilder.

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
        while state.iteration < state.max_iterations:
            state.iteration += 1

            # Select agent (use first role as pilot)
            role = list(deps.agents.keys())[0] if deps.agents else None
            if not role:
                state.error = "No agents configured"
                logger.error(f"❌ [ABORT] No agents")
                return End(f"Aborted: No agents configured")

            agent = deps.agents[role]
            logger.info(f"🤖 [AGENT] {role} turn {state.iteration}")

            try:
                # Run agent with message history for continuity
                result = await agent.run(
                    user_prompt=f"Target: {state.target}. Scan mode: {state.scan_mode}.",
                    message_history=state.message_history.get(role, []),
                    deps=deps,
                )

                # Append agent response to history
                state.message_history[role] = result.all_messages()

                # Update agent status
                state.agent_statuses[role] = "completed"

                logger.info(f"✅ [AGENT] {role} completed turn {state.iteration}")
                logger.info(f"   Response length: {len(str(result.response))} chars")

                # In v1.102.0, agents handle tool execution internally
                # One turn is sufficient for demo; multi-turn orchestration can be added later
                logger.info(f"→  [AGENT] Completed one turn (multi-turn loop can be added)")
                break

            except Exception as e:
                logger.error(f"❌ [AGENT] {role} error: {e}")
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

    # Connect start node to orchestrator step
    g.add(g.edge_from(g.start_node).to(orchestrate))

    # Build and return graph (single step = no complex edge routing needed)
    return g.build(validate_graph_structure=False)
