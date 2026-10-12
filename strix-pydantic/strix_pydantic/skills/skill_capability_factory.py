"""Factory for building skill capabilities from strix/skills content."""

import logging
import os
import re
from dataclasses import dataclass, field
from pathlib import Path

from pydantic_ai import Capability

logger = logging.getLogger(__name__)


@dataclass
class SkillBuild:
    """Skill capability build result."""

    capabilities: list[Capability] = field(default_factory=list)


class SkillCapabilityFactory:
    """Factory for building skill capabilities from strix/skills content."""

    def __init__(self, strix_content_dir: Path | None = None):
        self.strix_content_dir = strix_content_dir or self._resolve_strix_content_dir()
        self.skills_dir = self.strix_content_dir / "skills" if self.strix_content_dir else None
        logger.info(f"SkillCapabilityFactory using content dir: {self.skills_dir}")

    def _resolve_strix_content_dir(self) -> Path | None:
        env_path = os.getenv("STRIX_CONTENT_DIR")
        if env_path and Path(env_path).exists():
            return Path(env_path)

        package_dir = Path(__file__).parent.parent.parent
        sibling_strix = package_dir.parent / "strix"
        if sibling_strix.exists() and (sibling_strix / "skills").exists():
            logger.info(f"Found sibling strix content at {sibling_strix}")
            return sibling_strix

        logger.warning("Could not resolve strix content directory")
        return None

    def build_for_role(self, scan_mode: str, role: str) -> SkillBuild:
        """
        Build skill capabilities for an agent role.

        The scan mode methodology is loaded eagerly (required every turn).
        All other available skills are offered as deferred capabilities so the
        agent can load whichever are relevant based on what it discovers.
        """
        capabilities: list[Capability] = []

        scan_mode_id = f"scan_modes/{scan_mode}"
        scan_mode_cap = self._load_capability(scan_mode_id, defer_loading=False)
        if scan_mode_cap is None:
            raise ValueError(f"Required skill missing: {scan_mode_id}")
        capabilities.append(scan_mode_cap)

        for skill_id in self._enumerate_skills():
            if skill_id.startswith("scan_modes/"):
                continue
            cap = self._load_capability(skill_id, defer_loading=True)
            if cap is not None:
                capabilities.append(cap)

        deferred_count = len(capabilities) - 1
        logger.info(f"Built {len(capabilities)} capabilities for role={role} ({deferred_count} deferred)")
        return SkillBuild(capabilities=capabilities)

    def _enumerate_skills(self) -> list[str]:
        """Return all available skill IDs from the skills directory."""
        if not self.skills_dir or not self.skills_dir.exists():
            return []

        skills = []
        for category_dir in sorted(self.skills_dir.iterdir()):
            if not category_dir.is_dir() or category_dir.name.startswith("_"):
                continue
            for md_file in sorted(category_dir.glob("*.md")):
                skills.append(f"{category_dir.name}/{md_file.stem}")
        return skills

    def _load_capability(self, skill_id: str, defer_loading: bool) -> Capability | None:
        """Load a skill file and return a Capability, or None if not found."""
        if not self.skills_dir:
            return None

        # reject traversal and absolute paths
        if ".." in skill_id or skill_id.startswith("/") or skill_id.startswith("\\"):
            logger.warning(f"Rejected unsafe skill ID: {skill_id!r}")
            return None

        skill_path = (self.skills_dir / f"{skill_id}.md").resolve()

        try:
            skill_path.relative_to(self.skills_dir.resolve())
        except ValueError:
            logger.warning(f"Skill path escapes skills dir: {skill_id!r}")
            return None

        if not skill_path.exists():
            logger.warning(f"Skill not found: {skill_id}")
            return None

        try:
            raw = skill_path.read_text(encoding="utf-8")
        except OSError as e:
            logger.warning(f"Failed to read skill {skill_id}: {e}")
            return None

        metadata, body = self._parse_frontmatter(raw)
        description = metadata.get("description") or skill_id
        cap_id = skill_id.replace("/", ".")

        return Capability(
            id=cap_id,
            description=description,
            instructions=body,
            defer_loading=defer_loading,
        )

    def _parse_frontmatter(self, content: str) -> tuple[dict[str, str], str]:
        """Parse YAML frontmatter and return (metadata, body)."""
        pattern = re.compile(r"^---\s*\n(.*?)\n---\s*\n", re.DOTALL)
        match = pattern.match(content)
        if not match:
            return {}, content

        metadata: dict[str, str] = {}
        for line in match.group(1).splitlines():
            if ":" in line:
                key, _, value = line.partition(":")
                metadata[key.strip()] = value.strip()

        body = content[match.end():].lstrip()
        return metadata, body
