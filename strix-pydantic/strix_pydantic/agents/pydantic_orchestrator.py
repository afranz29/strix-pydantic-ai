"""Pydantic Graph orchestrator for non-interactive scans."""

import json
import logging
import time
from typing import Any

from pydantic_graph import End, GraphBuilder

from .types import RunConfig, StrixDeps, StrixRunState

logger = logging.getLogger(__name__)


def _dump_for_log(value: Any) -> str:
    """Return a stable string representation for debug logs."""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, default=str, ensure_ascii=False)
    except Exception:
        return str(value)


def _log_message_sequence(role: str, messages: list[Any], label: str) -> None:
    """Log full message sequence with all parts for LLM traceability."""
    logger.debug(f"🧾 [LLM TRACE] role={role} {label}: {len(messages)} message(s)")

    for msg_idx, msg in enumerate(messages, 1):
        msg_type = type(msg).__name__
        msg_kind = getattr(msg, "kind", None)
        timestamp = getattr(msg, "timestamp", None)

        logger.debug(
            f"   [msg {msg_idx}] type={msg_type}, kind={msg_kind}, timestamp={timestamp}"
        )

        parts = getattr(msg, "parts", []) or []
        if not parts:
            logger.debug(f"      [no-parts] {_dump_for_log(msg)}")
            continue

        for part_idx, part in enumerate(parts, 1):
            part_kind = getattr(part, "part_kind", None)

            if part_kind == "tool-call":
                tool_name = getattr(part, "tool_name", "unknown")
                args = getattr(part, "args", None)
                logger.debug(
                    f"      [part {part_idx}] tool-call name={tool_name}, args={_dump_for_log(args)}"
                )
            elif part_kind == "tool-return":
                tool_name = getattr(part, "tool_name", "unknown")
                content = getattr(part, "content", None)
                logger.debug(
                    f"      [part {part_idx}] tool-return name={tool_name}, content={_dump_for_log(content)}"
                )
            elif part_kind in {"text", "thinking", "system-prompt", "user-prompt"}:
                content = getattr(part, "content", None)
                logger.debug(f"      [part {part_idx}] {part_kind}: {_dump_for_log(content)}")
            else:
                logger.debug(
                    f"      [part {part_idx}] kind={part_kind}, raw={_dump_for_log(part)}"
                )


def _log_agent_result(role: str, result: Any) -> None:
    """Log token usage and full LLM message sequence."""
    usage = result.usage
    logger.debug(
        f"   Tokens — input: {usage.input_tokens or 0}, "
        f"output: {usage.output_tokens or 0}, "
        f"cache_read: {usage.cache_read_tokens or 0}, "
        f"cache_write: {usage.cache_write_tokens or 0}"
    )

    _log_message_sequence(role, result.all_messages(), label="all_messages")


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
    start_time = time.time()

    # ========== BOOTSTRAP ==========
    logger.info(f"🚀 [BOOTSTRAP] Run {state.run_id} starting")
    logger.info(f"   Target: {state.target}")
    logger.info(f"   Scan Mode: {state.scan_mode}")
    logger.info(f"   Model: {deps.run_config.model_name}")

    # Emit scan started event
    if deps.event_emitter:
        await deps.event_emitter("scan_started", {
            "target": state.target,
            "scan_mode": state.scan_mode,
            "model": deps.run_config.model_name,
        })

    # Validate target
    if not state.target:
        state.error = "Target URL is required"
        logger.error(f"❌ [ABORT] Target is empty")
        if deps.event_emitter:
            await deps.event_emitter("scan_failed", {
                "error": "Target URL is required",
                "duration_seconds": time.time() - start_time,
            })
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
        state.agent_statuses[role] = "running"

        # Track vulnerabilities count before this agent runs
        vuln_count_before = len(state.vulnerabilities)

        # Emit agent started event
        if deps.event_emitter:
            await deps.event_emitter("agent_started", {
                "role": role,
                "iteration": state.iteration,
            })

        # Update UI if callbacks are available
        if deps.ui_update_agent_status:
            deps.ui_update_agent_status(role, "running", state.iteration)

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

        history = state.message_history.get(role, [])
        logger.debug(f"📝 [LLM INPUT] role={role} user_prompt={prompt}")
        _log_message_sequence(role, history, label="message_history_before_run")

        try:
            result = await agent.run(
                user_prompt=prompt,
                message_history=history,
                deps=deps,
            )
        except Exception as e:
            state.agent_statuses[role] = "failed"
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
                vuln_dict = v.model_dump()
                state.vulnerabilities.append(vuln_dict)

                # Emit vulnerability found event
                if deps.event_emitter:
                    await deps.event_emitter("vulnerability_found", {
                        "role": role,
                        "title": v.title,
                        "severity": v.severity,
                        "description": v.description,
                        "cve_id": v.cve_id,
                        "parameter": v.parameter,
                        "poc": v.poc,
                    })

                # Update UI with vulnerability
                if deps.ui_show_vulnerability:
                    deps.ui_show_vulnerability(
                        v.title,
                        v.severity,
                        v.description,
                    )

            logger.info(f"   Extracted {len(output.vulnerabilities)} vulnerabilities")

        if hasattr(output, "notes"):
            for n in output.notes:
                state.notes.append({"content": n, "role": role})

        logger.info(f"✅ [AGENT] {role} completed ({len(summary)} chars)")

        # Calculate vulnerabilities found in this phase only
        vuln_count_after = len(state.vulnerabilities)
        vuln_found_this_phase = vuln_count_after - vuln_count_before

        # Emit agent completed event
        if deps.event_emitter:
            await deps.event_emitter("agent_completed", {
                "role": role,
                "iteration": state.iteration,
                "vulnerabilities_found": vuln_found_this_phase,
            })

        # Update UI with agent completion
        if deps.ui_update_agent_status:
            deps.ui_update_agent_status(role, "completed", state.iteration)

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

    # Emit scan completed event
    duration = time.time() - start_time
    if deps.event_emitter:
        await deps.event_emitter("scan_completed", {
            "duration_seconds": duration,
            "vulnerabilities_count": len(state.vulnerabilities),
            "iterations": state.iteration,
        })

    return End("\n".join(summary_lines))
