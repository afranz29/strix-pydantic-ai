"""Tests for SkillCapabilityFactory loader hardening. No network/model access required."""

import pytest

from strix_pydantic.skills.skill_capability_factory import SkillCapabilityFactory


@pytest.fixture
def skills_root(tmp_path):
    """Build a minimal fixture skills tree covering the loader's edge cases."""
    content_dir = tmp_path / "strix"
    skills_dir = content_dir / "skills"

    scan_modes = skills_dir / "scan_modes"
    scan_modes.mkdir(parents=True)
    for mode in ("quick", "standard", "deep"):
        (scan_modes / f"{mode}.md").write_text(
            f"---\ndescription: {mode} scan methodology\n---\nRun a {mode} scan.\n",
            encoding="utf-8",
        )

    vulns = skills_dir / "vulnerabilities"
    vulns.mkdir()
    (vulns / "xss.md").write_text(
        "---\ndescription: XSS testing\n---\n"
        "Try payloads like `{{ 7*7 }}` and `{% if true %}` literally.\n",
        encoding="utf-8",
    )

    tooling = skills_dir / "tooling"
    tooling.mkdir()
    (tooling / "xss.md").write_text(
        "---\ndescription: duplicate bare name across categories\n---\nAmbiguous.\n",
        encoding="utf-8",
    )
    (tooling / "templated.md").write_text(
        "---\ndescription: templated skill\ntemplate: jinja\n---\n"
        "{% if supports_images %}Use screenshots.{% else %}Text only.{% endif %}\n",
        encoding="utf-8",
    )

    return content_dir


def test_literal_jinja_syntax_untouched_without_template_flag(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    cap = factory._load_capability("vulnerabilities/xss", defer_loading=True)
    assert cap is not None
    instructions = cap.get_instructions()[0]
    assert "{{ 7*7 }}" in instructions
    assert "{% if true %}" in instructions


def test_jinja_template_renders_with_supports_images_true(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    cap = factory._load_capability("tooling/templated", defer_loading=True, supports_images=True)
    assert cap is not None
    instructions = cap.get_instructions()[0]
    assert "Use screenshots." in instructions
    assert "{%" not in instructions


def test_jinja_template_renders_with_supports_images_false(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    cap = factory._load_capability("tooling/templated", defer_loading=True, supports_images=False)
    assert cap is not None
    assert "Text only." in cap.get_instructions()[0]


def test_bare_name_resolves_when_unambiguous(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    assert factory.resolve_skill_id("templated") == "tooling/templated"


def test_qualified_name_resolves_directly(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    assert factory.resolve_skill_id("vulnerabilities/xss") == "vulnerabilities/xss"


def test_ambiguous_bare_name_raises(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    with pytest.raises(ValueError, match="Ambiguous skill name"):
        factory.resolve_skill_id("xss")


def test_unknown_skill_resolves_to_none(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    assert factory.resolve_skill_id("does_not_exist") is None


def test_validate_requested_raises_actionable_error_for_unknown_skill(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    with pytest.raises(ValueError, match="Unknown skill"):
        factory.validate_requested(["does_not_exist"])


def test_validate_requested_passes_for_known_skills(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    factory.validate_requested(["vulnerabilities/xss", "templated"])


def test_path_traversal_rejected(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    assert factory._resolve_skill_path("../../etc/passwd") is None
    assert factory._resolve_skill_path("/etc/passwd") is None


def test_build_for_role_fails_fast_when_scan_mode_missing(tmp_path):
    content_dir = tmp_path / "strix"
    (content_dir / "skills" / "scan_modes").mkdir(parents=True)
    factory = SkillCapabilityFactory(strix_content_dir=content_dir)
    with pytest.raises(ValueError, match="Required skill missing"):
        factory.build_for_role(scan_mode="quick", role="reconnaissance")


def test_validate_required_scan_modes_fails_fast_when_missing(tmp_path):
    content_dir = tmp_path / "strix"
    (content_dir / "skills" / "scan_modes").mkdir(parents=True)
    factory = SkillCapabilityFactory(strix_content_dir=content_dir)
    with pytest.raises(ValueError, match="Required scan mode skill"):
        factory.validate_required_scan_modes()


def test_validate_required_scan_modes_passes_when_present(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    factory.validate_required_scan_modes()


def test_build_for_role_loads_scan_mode_eagerly_and_rest_deferred(skills_root):
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    build = factory.build_for_role(scan_mode="quick", role="reconnaissance")

    by_id = {cap.id: cap for cap in build.capabilities}
    assert by_id["scan_modes.quick"].defer_loading is False
    assert by_id["vulnerabilities.xss"].defer_loading is True
    assert by_id["tooling.templated"].defer_loading is True


def test_readme_not_treated_as_skill(skills_root):
    (skills_root / "skills" / "vulnerabilities" / "README.md").write_text(
        "not a skill", encoding="utf-8"
    )
    factory = SkillCapabilityFactory(strix_content_dir=skills_root)
    assert "vulnerabilities/README" not in factory._enumerate_skills()
