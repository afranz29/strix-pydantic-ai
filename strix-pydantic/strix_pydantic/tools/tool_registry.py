"""Tool registry with context-aware filtering for sandbox vs parent execution."""

from dataclasses import dataclass
from typing import Any, Callable, Literal, Optional

from pydantic_ai import AbstractToolset, FilteredToolset, FunctionToolset, RunContext


ToolContext = Literal["sandbox", "parent"]


@dataclass
class ToolDefinition:
    """Definition of a tool with context availability."""

    name: str
    description: str
    callable: Callable[..., Any]
    contexts: set[ToolContext]  # {"sandbox"}, {"parent"}, or {"sandbox", "parent"}
    parameters: Optional[dict[str, Any]] = None


class ToolRegistry:
    """Registry for context-aware tool availability and execution."""

    def __init__(self):
        """Initialize empty tool registry."""
        self._tools: dict[str, ToolDefinition] = {}
        self._sandboxes_tools: set[str] = set()
        self._parent_tools: set[str] = set()

    def register_tool(
        self,
        name: str,
        description: str,
        callable_obj: Callable[..., Any],
        contexts: set[ToolContext],
        parameters: Optional[dict[str, Any]] = None,
    ) -> None:
        """
        Register a tool with context availability.

        Args:
            name: Tool name (used in agent prompts and calls)
            description: Tool description for agent context
            callable_obj: Async or sync callable implementing the tool
            contexts: Set of contexts where tool is available ({"sandbox"}, {"parent"}, or both)
            parameters: Optional schema/parameter definitions
        """
        if name in self._tools:
            raise ValueError(f"Tool '{name}' already registered")

        if not contexts:
            raise ValueError(f"Tool '{name}' must be available in at least one context")

        self._tools[name] = ToolDefinition(
            name=name,
            description=description,
            callable=callable_obj,
            contexts=contexts,
            parameters=parameters,
        )

        if "sandbox" in contexts:
            self._sandboxes_tools.add(name)
        if "parent" in contexts:
            self._parent_tools.add(name)

    def is_tool_available_in_context(self, tool_name: str, context: ToolContext) -> bool:
        """Check if a tool is available in a given context."""
        if tool_name not in self._tools:
            return False
        return context in self._tools[tool_name].contexts

    def get_tools_for_context(self, context: ToolContext) -> dict[str, ToolDefinition]:
        """
        Get all tools available in a context.

        Args:
            context: Execution context ("sandbox" or "parent")

        Returns:
            Dict of tool names to ToolDefinition
        """
        return {
            name: tool
            for name, tool in self._tools.items()
            if context in tool.contexts
        }

    def get_tool_names_for_context(self, context: ToolContext) -> set[str]:
        """Get names of all tools available in a context."""
        return (
            self._sandboxes_tools.copy() if context == "sandbox" else self._parent_tools.copy()
        )

    def get_tools_prompt_for_context(self, context: ToolContext) -> str:
        """
        Generate tool descriptions for agent prompt.

        Args:
            context: Execution context

        Returns:
            Formatted tool list for inclusion in agent system prompt
        """
        tools = self.get_tools_for_context(context)
        if not tools:
            return "(no tools available in this context)"

        lines = []
        for name in sorted(tools.keys()):
            tool = tools[name]
            lines.append(f"- {name}: {tool.description}")

        return "\n".join(lines)

    def build_filtered_toolset(
        self,
        context: ToolContext,
        filter_func: Optional[Callable[[str], bool]] = None,
    ) -> FilteredToolset:
        """
        Build a FilteredToolset for a context with optional additional filtering.

        Args:
            context: Execution context
            filter_func: Optional filter function that takes tool name and returns bool

        Returns:
            FilteredToolset ready for use in Agent construction
        """
        # Create base toolset with all tools for this context
        base = FunctionToolset()
        tools_in_context = self.get_tools_for_context(context)

        for tool_name, tool_def in tools_in_context.items():
            # Register tool with base toolset
            # FunctionToolset expects tools to be registered via decorators or direct calls
            # For now, we'll return a wrapper that handles filtering
            pass

        # Compose with FilteredToolset
        def combined_filter(tool_name: str) -> bool:
            """Combined filter: availability + optional custom filter."""
            if tool_name not in self._tools:
                return False
            if context not in self._tools[tool_name].contexts:
                return False
            if filter_func and not filter_func(tool_name):
                return False
            return True

        return FilteredToolset(
            wrapped=base,
            filter_func=lambda ctx, tool_def: combined_filter(tool_def.name),
        )

    def validate_tool_availability(
        self,
        tool_name: str,
        context: ToolContext,
    ) -> tuple[bool, Optional[str]]:
        """
        Validate tool is available before execution.

        Args:
            tool_name: Name of tool to check
            context: Execution context

        Returns:
            Tuple of (is_available: bool, error_message: Optional[str])
        """
        if tool_name not in self._tools:
            return False, f"Tool '{tool_name}' not found in registry"

        if not self.is_tool_available_in_context(tool_name, context):
            available_contexts = self._tools[tool_name].contexts
            return False, (
                f"Tool '{tool_name}' not available in '{context}' context. "
                f"Available in: {', '.join(sorted(available_contexts))}"
            )

        return True, None
