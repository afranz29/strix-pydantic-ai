"""Factory for building skill capabilities from strix/skills content."""

import logging
import os
import re
from dataclasses import dataclass, field
from pathlib import Path

import yaml
from jinja2 import Environment, StrictUndefined
from pydantic_ai import Capability

logger = logging.getLogger(__name__)

_FRONTMATTER_PATTERN = re.compile(r"^---\s*\n(?P<body>.*?)\n---\s*\n", re.DOTALL)

# Only skills with `template: jinja` in their frontmatter are rendered through this;
# every other skill's literal `{{ }}` / `{% %}` payload examples must stay untouched.
_SKILL_TEMPLATES = Environment(
    autoescape=False,  # not HTML; this is LLM prompt content
    undefined=StrictUndefined,
    trim_blocks=True,
    lstrip_blocks=True,
)


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

    def build_for_role(
        self, scan_mode: str, role: str, *, supports_images: bool = True
    ) -> SkillBuild:
        """
        Build skill capabilities for an agent role.

        The scan mode methodology is loaded eagerly (required every turn).
        All other available skills are offered as deferred capabilities so the
        agent can load whichever are relevant based on what it discovers.
        """
        capabilities: list[Capability] = []

        scan_mode_id = f"scan_modes/{scan_mode}"
        scan_mode_cap = self._load_capability(
            scan_mode_id, defer_loading=False, supports_images=supports_images
        )
        if scan_mode_cap is None:
            raise ValueError(f"Required skill missing: {scan_mode_id}")
        capabilities.append(scan_mode_cap)

        for skill_id in self._enumerate_skills():
            if skill_id.startswith("scan_modes/"):
                continue
            cap = self._load_capability(
                skill_id, defer_loading=True, supports_images=supports_images
            )
            if cap is not None:
                capabilities.append(cap)

        deferred_count = len(capabilities) - 1
        logger.info(f"Built {len(capabilities)} capabilities for role={role} ({deferred_count} deferred)")
        return SkillBuild(capabilities=capabilities)

    def validate_required_scan_modes(self) -> None:
        """Fail fast if a scan mode the CLI can select has no backing skill file."""
        missing = [
            mode
            for mode in ("quick", "standard", "deep")
            if self._resolve_skill_path(f"scan_modes/{mode}") is None
        ]
        if missing:
            raise ValueError(f"Required scan mode skill(s) missing: {', '.join(missing)}")

    def resolve_skill_id(self, skill_id: str) -> str | None:
        """
        Resolve a bare (``xss``) or category-qualified (``vulnerabilities/xss``) name to
        its canonical ``category/name`` id.

        Returns None if nothing matches. Raises ValueError if a bare name matches more
        than one category.
        """
        if "/" in skill_id:
            return skill_id if self._resolve_skill_path(skill_id) is not None else None

        matches = [sid for sid in self._enumerate_skills() if sid.rsplit("/", 1)[-1] == skill_id]
        if not matches:
            return None
        if len(matches) > 1:
            raise ValueError(
                f"Ambiguous skill name {skill_id!r}; matches {matches}. "
                "Use a category-qualified name (category/name)."
            )
        return matches[0]

    def validate_requested(self, skill_ids: list[str]) -> None:
        """Raise ValueError with an actionable message if any requested skill can't be resolved."""
        errors: list[str] = []
        for skill_id in skill_ids:
            try:
                resolved = self.resolve_skill_id(skill_id)
            except ValueError as e:
                errors.append(str(e))
                continue
            if resolved is None:
                available = sorted(self._enumerate_skills())
                errors.append(f"Unknown skill {skill_id!r}. Available skills: {available}")
        if errors:
            raise ValueError("; ".join(errors))

    def _enumerate_skills(self) -> list[str]:
        """Return all available skill IDs from the skills directory."""
        if not self.skills_dir or not self.skills_dir.exists():
            return []

        skills = []
        for category_dir in sorted(self.skills_dir.iterdir()):
            if not category_dir.is_dir() or category_dir.name.startswith("_"):
                continue
            for md_file in sorted(category_dir.glob("*.md")):
                if md_file.name == "README.md":
                    continue
                skills.append(f"{category_dir.name}/{md_file.stem}")
        return skills

    def _resolve_skill_path(self, skill_id: str) -> Path | None:
        """Canonicalize skill_id to a path strictly inside skills_dir, or None."""
        if not self.skills_dir:
            return None

        if ".." in Path(skill_id).parts or skill_id.startswith(("/", "\\")):
            logger.warning(f"Rejected unsafe skill ID: {skill_id!r}")
            return None

        skill_path = (self.skills_dir / f"{skill_id}.md").resolve()
        try:
            skill_path.relative_to(self.skills_dir.resolve())
        except ValueError:
            logger.warning(f"Skill path escapes skills dir: {skill_id!r}")
            return None

        return skill_path if skill_path.is_file() else None

    def _load_capability(
        self, skill_id: str, defer_loading: bool, *, supports_images: bool = True
    ) -> Capability | None:
        """Load a skill file and return a Capability, or None if not found."""
        skill_path = self._resolve_skill_path(skill_id)
        if skill_path is None:
            logger.warning(f"Skill not found: {skill_id}")
            return None

        try:
            raw = skill_path.read_text(encoding="utf-8")
        except OSError as e:
            logger.warning(f"Failed to read skill {skill_id}: {e}")
            return None

        metadata, body = self._parse_frontmatter(raw, skill_path)

        if metadata.get("template") == "jinja":
            try:
                body = _SKILL_TEMPLATES.from_string(body).render(supports_images=supports_images)
            except Exception as e:
                logger.warning(f"Failed to render jinja skill {skill_id}: {e}")
                return None

        description = metadata.get("description") or skill_id
        cap_id = skill_id.replace("/", ".")

        return Capability(
            id=cap_id,
            description=description,
            instructions=body,
            defer_loading=defer_loading,
        )

    def _parse_frontmatter(
        self, content: str, source: Path | None = None
    ) -> tuple[dict[str, str], str]:
        """Parse YAML frontmatter and return (metadata, body)."""
        match = _FRONTMATTER_PATTERN.match(content)
        if not match:
            return {}, content.lstrip()

        try:
            parsed = yaml.safe_load(match.group("body"))
        except yaml.YAMLError as e:
            logger.warning(f"Failed to parse skill frontmatter {source or '<content>'}: {e}")
            parsed = None

        body = content[match.end():].lstrip()

        if not isinstance(parsed, dict):
            return {}, body

        metadata = {str(k): "" if v is None else str(v) for k, v in parsed.items()}
        return metadata, body
