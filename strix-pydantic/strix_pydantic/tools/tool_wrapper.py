"""Wrapper to convert registered tools into pydantic-ai toolsets with sandbox dispatch."""

import logging
import uuid
from typing import Any

from pydantic_ai import FunctionToolset

from strix_pydantic.runtime.sandbox_client import SandboxClient

logger = logging.getLogger(__name__)


def build_tools_from_registry(
    tool_registry,
    sandbox_client: SandboxClient,
    agent_id: str,
) -> FunctionToolset:
    """
    Build a pydantic-ai FunctionToolset from registered tools with sandbox dispatch.

    Args:
        tool_registry: ToolRegistry instance with registered tools
        sandbox_client: SandboxClient for tool execution
        agent_id: ID of agent executing the tools

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
        )
        tool_callables.append(wrapped)
        logger.info(f"  ✅ Wrapped tool for sandbox: {tool_name}")

    # Create FunctionToolset with all wrapped tools
    toolset = FunctionToolset(tools=tool_callables)

    logger.info(f"✅ Built toolset with {len(parent_tools)} sandbox-dispatched tools")
    return toolset


def _create_sandbox_wrapper(
    tool_name: str,
    sandbox_client: SandboxClient,
    agent_id: str,
    original_callable: Any,
) -> Any:
    """
    Create a wrapper function that dispatches tool execution through sandbox.

    Args:
        tool_name: Name of the tool
        sandbox_client: SandboxClient instance
        agent_id: ID of agent executing the tool
        original_callable: Original tool function (used for signature/docs)

    Returns:
        Wrapper function that dispatches through sandbox
    """
    # Get docstring and annotations from original for pydantic-ai introspection
    async def sandbox_wrapper(**kwargs) -> Any:
        """Execute tool through sandbox."""
        logger.info(f"🔧 [SANDBOX] Dispatching {tool_name} with args: {list(kwargs.keys())}")

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
            return result.result
        else:
            error_msg = f"Tool error: {result.error_code} - {result.error_message}"
            logger.error(f"❌ [SANDBOX] {tool_name} failed: {error_msg}")
            # Return error info so agent can see what went wrong
            return {"error": result.error_code, "message": result.error_message}

    # Copy metadata from original callable for pydantic-ai introspection
    sandbox_wrapper.__name__ = original_callable.__name__
    sandbox_wrapper.__doc__ = original_callable.__doc__
    if hasattr(original_callable, "__annotations__"):
        sandbox_wrapper.__annotations__ = original_callable.__annotations__

    return sandbox_wrapper
