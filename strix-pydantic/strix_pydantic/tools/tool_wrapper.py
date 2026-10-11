"""Wrapper to convert registered tools into pydantic-ai toolsets with sandbox dispatch."""

import inspect
import json
import logging
import uuid
from typing import Any, Callable

from pydantic_ai import FunctionToolset, RunContext

from strix_pydantic.runtime.sandbox_client import SandboxClient

logger = logging.getLogger(__name__)


def _dump_for_log(value: Any) -> str:
    """Return a stable string representation for debug logs."""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, default=str, ensure_ascii=False)
    except Exception:
        return str(value)


def build_tools_from_registry(
    tool_registry,
    sandbox_client: SandboxClient,
    agent_id: str,
    on_tool_event: Callable[[dict[str, Any]], None] | None = None,
) -> FunctionToolset:
    """
    Build a pydantic-ai FunctionToolset from registered tools with sandbox dispatch.

    Args:
        tool_registry: ToolRegistry instance with registered tools
        sandbox_client: SandboxClient for tool execution
        agent_id: ID of agent executing the tools
        on_tool_event: Optional callback fired after each tool execution

    Returns:
        FunctionToolset with all parent-context tools wrapped for sandbox dispatch
    """
    logger.info("Building toolset from registry with sandbox dispatch")

    # Get all tools available in parent context
    parent_tools = tool_registry.get_tools_for_context("parent")

    # Collect wrapped tool callables for FunctionToolset initialization
    tool_callables = []
    for tool_name, tool_def in parent_tools.items():
        wrapped = _create_sandbox_wrapper(
            tool_name,
            sandbox_client,
            agent_id,
            tool_def.callable,
            on_tool_event=on_tool_event,
        )
        tool_callables.append(wrapped)
        logger.info(f"  ✅ Wrapped tool for sandbox: {tool_name}")

    # Create FunctionToolset with all wrapped tools
    toolset = FunctionToolset(tools=tool_callables)

    logger.info(f"✅ Built toolset with {len(parent_tools)} sandbox-dispatched tools")
    return toolset


def build_direct_tools_from_registry(tool_registry) -> FunctionToolset:
    """
    Build a pydantic-ai FunctionToolset for mock execution with event wrappers.

    Args:
        tool_registry: ToolRegistry instance with registered tools

    Returns:
        FunctionToolset with mock tools wrapped for event emission
    """
    logger.info("Building mock toolset from registry with event wrappers")

    parent_tools = tool_registry.get_tools_for_context("parent")

    tool_callables = []
    for tool_name, tool_def in parent_tools.items():
        wrapped = _create_direct_wrapper(tool_name, tool_def.callable)
        tool_callables.append(wrapped)
        logger.info(f"  ✅ Wrapped mock tool: {tool_name}")

    return FunctionToolset(tools=tool_callables)


def _create_direct_wrapper(
    tool_name: str,
    original_callable: Any,
) -> Any:
    """Create a wrapper for direct (mock) tools that injects RunContext and emits events."""

    async def direct_wrapper(ctx: RunContext[Any], **kwargs) -> Any:
        role = ctx.agent.name if hasattr(ctx, "agent") and ctx.agent else "unknown"

        # Emit tool started
        if ctx.deps.event_emitter:
            cmd = f"{tool_name}(" + ", ".join(f"{k}={v}" for k, v in kwargs.items()) + ")"
            await ctx.deps.event_emitter("tool_started", {
                "tool_name": tool_name,
                "command": cmd,
                "role": role,
            })

        # Run original tool
        result = await original_callable(**kwargs)

        # Emit tool output
        if ctx.deps.event_emitter:
            output_str = json.dumps(result, default=str)
            await ctx.deps.event_emitter("tool_output", {
                "tool_name": tool_name,
                "output": output_str,
                "role": role,
            })

        return result

    direct_wrapper.__name__ = original_callable.__name__
    direct_wrapper.__doc__ = original_callable.__doc__

    # Prepend RunContext to signature
    sig = inspect.signature(original_callable)
    params = list(sig.parameters.values())
    ctx_param = inspect.Parameter(
        name="ctx",
        kind=inspect.Parameter.POSITIONAL_OR_KEYWORD,
        annotation=RunContext[Any],
    )
    direct_wrapper.__signature__ = sig.replace(parameters=[ctx_param] + params)

    # Prepend RunContext to annotations
    annotations = getattr(original_callable, "__annotations__", {}).copy()
    annotations["ctx"] = RunContext[Any]
    direct_wrapper.__annotations__ = annotations

    return direct_wrapper


def _create_sandbox_wrapper(
    tool_name: str,
    sandbox_client: SandboxClient,
    agent_id: str,
    original_callable: Any,
    on_tool_event: Callable[[dict[str, Any]], None] | None = None,
) -> Any:
    """
    Create a wrapper function that dispatches tool execution through sandbox.

    Args:
        tool_name: Name of the tool
        sandbox_client: SandboxClient instance
        agent_id: ID of agent executing the tool
        original_callable: Original tool function (used for signature/docs)
        on_tool_event: Optional callback fired after each tool execution

    Returns:
        Wrapper function that dispatches through sandbox
    """

    def _emit_tool_event(event: dict[str, Any]) -> None:
        if on_tool_event is None:
            return
        try:
            on_tool_event(event)
        except Exception as e:
            logger.debug(f"Failed to record tool event for {tool_name}: {e}")

    # Get docstring and annotations from original for pydantic-ai introspection
    async def sandbox_wrapper(ctx: RunContext[Any], **kwargs) -> Any:
        """Execute tool through sandbox."""
        logger.info(f"🔧 [SANDBOX] Dispatching {tool_name} kwargs={kwargs}")

        role = ctx.agent.name if hasattr(ctx, "agent") and ctx.agent else agent_id

        # Emit tool started
        if ctx.deps.event_emitter:
            cmd = kwargs.get("command") or f"{tool_name}(" + ", ".join(f"{k}={v}" for k, v in kwargs.items()) + ")"
            await ctx.deps.event_emitter("tool_started", {
                "tool_name": tool_name,
                "command": cmd,
                "role": role,
            })

        # Unique call ID prevents parallel tool calls from cancelling each other.
        # The sandbox cancels previous tasks per agent_id, so each call needs a unique id.
        call_id = f"{agent_id}/{tool_name}/{uuid.uuid4().hex[:8]}"
        result = await sandbox_client.execute_tool(
            agent_id=call_id,
            tool_name=tool_name,
            kwargs=kwargs,
        )

        if result.ok:
            logger.info(f"✅ [SANDBOX] {tool_name} succeeded")
            logger.debug(f"🔧 [SANDBOX] {tool_name} return={_dump_for_log(result.result)}")
            _emit_tool_event(
                {
                    "agent_id": agent_id,
                    "tool_name": tool_name,
                    "kwargs": kwargs,
                    "ok": True,
                    "result": result.result,
                }
            )

            # Emit tool output
            if ctx.deps.event_emitter:
                output_str = ""
                if isinstance(result.result, dict):
                    if "content" in result.result:
                        output_str = str(result.result["content"])
                    else:
                        output_str = json.dumps(result.result, default=str)
                else:
                    output_str = str(result.result)

                await ctx.deps.event_emitter("tool_output", {
                    "tool_name": tool_name,
                    "output": output_str,
                    "role": role,
                })

            return result.result

        error_msg = f"Tool error: {result.error_code} - {result.error_message}"
        logger.error(f"❌ [SANDBOX] {tool_name} failed: {error_msg}")
        logger.debug(
            f"🔧 [SANDBOX] {tool_name} failure_payload="
            f"{_dump_for_log({'error_code': result.error_code, 'error_message': result.error_message})}"
        )
        _emit_tool_event(
            {
                "agent_id": agent_id,
                "tool_name": tool_name,
                "kwargs": kwargs,
                "ok": False,
                "error_code": result.error_code,
                "error_message": result.error_message,
            }
        )

        # Emit tool output error
        if ctx.deps.event_emitter:
            await ctx.deps.event_emitter("tool_output", {
                "tool_name": tool_name,
                "output": f"ERROR: {result.error_code} - {result.error_message}",
                "role": role,
            })

        # Return error info so agent can see what went wrong
        return {"error": result.error_code, "message": result.error_message}

    # Prepend RunContext to signature
    sig = inspect.signature(original_callable)
    params = list(sig.parameters.values())
    ctx_param = inspect.Parameter(
        name="ctx",
        kind=inspect.Parameter.POSITIONAL_OR_KEYWORD,
        annotation=RunContext[Any],
    )
    sandbox_wrapper.__signature__ = sig.replace(parameters=[ctx_param] + params)

    # Prepend RunContext to annotations
    annotations = getattr(original_callable, "__annotations__", {}).copy()
    annotations["ctx"] = RunContext[Any]
    sandbox_wrapper.__annotations__ = annotations

    return sandbox_wrapper
