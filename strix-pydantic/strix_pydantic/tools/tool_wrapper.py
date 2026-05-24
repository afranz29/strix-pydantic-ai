"""Wrapper to convert registered tools into pydantic-ai toolsets."""

import logging
from typing import Any

from pydantic_ai import FunctionToolset

logger = logging.getLogger(__name__)


def build_tools_from_registry(tool_registry) -> FunctionToolset:
    """
    Build a pydantic-ai FunctionToolset from registered tools.

    Args:
        tool_registry: ToolRegistry instance with registered tools

    Returns:
        FunctionToolset with all parent-context tools
    """
    logger.info("Building toolset from registry")

    # Get all tools available in parent context
    parent_tools = tool_registry.get_tools_for_context("parent")

    # Collect tool callables for FunctionToolset initialization
    tool_callables = []
    for tool_name, tool_def in parent_tools.items():
        tool_callables.append(tool_def.callable)
        logger.info(f"  ✅ Collected tool: {tool_name}")

    # Create FunctionToolset with all tools
    toolset = FunctionToolset(tools=tool_callables)

    logger.info(f"✅ Built toolset with {len(parent_tools)} tools")
    return toolset
