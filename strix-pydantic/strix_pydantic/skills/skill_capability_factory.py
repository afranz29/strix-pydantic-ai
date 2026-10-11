"""Factory for building skill-aware agent capabilities from strix/skills content."""

import logging
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Optional

from pydantic_ai import AbstractToolset, FilteredToolset, FunctionToolset, RunContext

logger = logging.getLogger(__name__)


@dataclass
class SkillBuild:
    """Skill capability build with instructions and toolset."""

    instructions: list[str | Callable[[RunContext[Any]], str]]
    toolset: AbstractToolset[Any]


class SkillCapabilityFactory:
    """Factory for building skill capabilities from strix/skills content."""

    def __init__(self, strix_content_dir: Optional[Path] = None):
        """
        Initialize the skill factory.

        Args:
            strix_content_dir: Path to strix content directory (default: sibling strix/)
        """
        self.strix_content_dir = strix_content_dir or self._resolve_strix_content_dir()
        self.skills_dir = self.strix_content_dir / "skills" if self.strix_content_dir else None
        logger.info(f"SkillCapabilityFactory using content dir: {self.skills_dir}")

    def _resolve_strix_content_dir(self) -> Optional[Path]:
        """
        Resolve strix content directory with priority:
        1. STRIX_CONTENT_DIR env var
        2. Sibling ../strix/ relative to strix-pydantic
        3. Configured fallback path
        """
        import os

        # Check env var
        env_path = os.getenv("STRIX_CONTENT_DIR")
        if env_path and Path(env_path).exists():
            return Path(env_path)

        # Check sibling strix/ from current package location
        package_dir = Path(__file__).parent.parent.parent  # strix-pydantic/
        sibling_strix = package_dir.parent / "strix"
        if sibling_strix.exists() and (sibling_strix / "skills").exists():
            logger.info(f"Found sibling strix content at {sibling_strix}")
            return sibling_strix

        logger.warning("Could not resolve strix content directory")
        return None

    def build_for_role(
        self,
        skill_ids: list[str],
        role: str,
        context: str = "parent",
    ) -> SkillBuild:
        """
        Build skill capabilities for an agent role.

        Args:
            skill_ids: List of skill identifiers to load
            role: Agent role (e.g., "reconnaissance", "exploitation")
            context: Execution context ("sandbox" or "parent")

        Returns:
            SkillBuild with instructions and filtered toolset
        """
        instructions = self._build_instructions(skill_ids, role, context)
        toolset = self._build_toolset(skill_ids, context)

        return SkillBuild(instructions=instructions, toolset=toolset)

    def _build_instructions(
        self,
        skill_ids: list[str],
        role: str,
        context: str,
    ) -> list[str | Callable[[RunContext[Any]], str]]:
        """
        Build instruction list from selected skills.

        Returns:
            List of static strings and/or dynamic callables for agent instructions
        """
        instructions: list[str | Callable[[RunContext[Any]], str]] = []

        # Load static skill content
        for skill_id in skill_ids:
            content = self._load_skill_content(skill_id)
            if content:
                instructions.append(content)

        # Add role-specific instruction
        role_instruction = f"You are the {role} agent. Execute your assigned tasks for the target system."
        instructions.append(role_instruction)

        # Add context-specific instruction
        if context == "sandbox":
            instructions.append(
                "You are running in a restricted sandbox environment. "
                "Only use tools available to you in this context."
            )

        logger.info(f"Built {len(instructions)} instructions for role={role}, context={context}")

        return instructions

    def _build_toolset(
        self,
        skill_ids: list[str],
        context: str,
    ) -> AbstractToolset[Any]:
        """
        Build a toolset filtered by skill policies.

        Returns:
            FilteredToolset with skill-aware filtering applied
        """
        # Create base toolset (empty for now; real implementation registers tools)
        base = FunctionToolset()

        # Build filter function based on skill policies
        filter_func = self._skill_tool_policy_filter(skill_ids, context)

        # Wrap with FilteredToolset
        filtered = FilteredToolset(
            wrapped=base,
            filter_func=filter_func,
        )

        logger.info(f"Built toolset for {len(skill_ids)} skills in {context} context")

        return filtered

    def _skill_tool_policy_filter(
        self,
        skill_ids: list[str],
        context: str,
    ) -> Callable:
        """
        Create a filter function for skill-aware tool availability.

        Args:
            skill_ids: Selected skill IDs
            context: Execution context

        Returns:
            Filter function: (tool_name: str) -> bool
        """

        def filter_func(tool_name: str) -> bool:
            """
            Determine if a tool is allowed by skill policies.

            Placeholder: returns True for all tools. Real implementation would:
            - Check skill-specific tool allowlists
            - Enforce context-aware restrictions
            - Apply role-specific policies
            """
            return True

        return filter_func

    def _load_skill_content(self, skill_id: str) -> Optional[str]:
        """
        Load markdown content from a skill file.

        Args:
            skill_id: Skill identifier (e.g., "reconnaissance" or "tooling/nmap")

        Returns:
            Markdown content without frontmatter, or None if not found
        """
        if not self.skills_dir:
            logger.warning(f"Skills dir not available; cannot load {skill_id}")
            return None

        # Try to find the skill file
        skill_path = None

        if "/" in skill_id:
            skill_path = self.skills_dir / f"{skill_id}.md"
        else:
            # Search by category
            for category_dir in self.skills_dir.iterdir():
                if category_dir.is_dir() and (category_dir / f"{skill_id}.md").exists():
                    skill_path = category_dir / f"{skill_id}.md"
                    break

            # Also check root
            if not skill_path and (self.skills_dir / f"{skill_id}.md").exists():
                skill_path = self.skills_dir / f"{skill_id}.md"

        if not skill_path or not skill_path.exists():
            logger.warning(f"Skill not found: {skill_id}")
            return None

        try:
            content = skill_path.read_text(encoding="utf-8")
            # Strip frontmatter (YAML between --- markers)
            import re

            pattern = re.compile(r"^---\s*\n.*?\n---\s*\n", re.DOTALL)
            content = pattern.sub("", content).lstrip()

            logger.info(f"Loaded skill content: {skill_id}")
            return content

        except (FileNotFoundError, OSError, ValueError) as e:
            logger.warning(f"Failed to load skill {skill_id}: {e}")
            return None
