from unittest.mock import MagicMock, patch

from strix_pydantic.agents.types import RunConfig
from strix_pydantic.interface.cli import _build_agents


def test_build_agents_injects_scan_mode_skill():
    model_spec = "openai:gpt-4o-mini"
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode="quick",
        non_interactive=True
    )

    mock_build = MagicMock()
    mock_build.capabilities = []

    with patch("strix_pydantic.interface.cli.SkillCapabilityFactory") as mock_factory_cls:
        mock_factory = mock_factory_cls.return_value
        mock_factory.build_for_role.return_value = mock_build
        with patch("pydantic_ai.Agent"):
            _build_agents(
                model_spec=model_spec,
                skill_list=[],
                run_config=run_config
            )

            for call in mock_factory.build_for_role.call_args_list:
                _, kwargs = call
                assert kwargs["scan_mode"] == "quick"


def test_build_agents_sets_reasoning_effort_quick():
    model_spec = "openai:gpt-4o-mini"
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode="quick",
        non_interactive=True
    )

    mock_build = MagicMock()
    mock_build.capabilities = []

    with patch("strix_pydantic.interface.cli.SkillCapabilityFactory") as mock_factory_cls:
        mock_factory_cls.return_value.build_for_role.return_value = mock_build
        with patch("pydantic_ai.Agent") as mock_agent_cls:
            _build_agents(
                model_spec=model_spec,
                skill_list=[],
                run_config=run_config
            )

            for call in mock_agent_cls.call_args_list:
                kwargs = call.kwargs
                assert kwargs["model_settings"]["reasoning_effort"] == "medium"


def test_build_agents_sets_reasoning_effort_deep():
    model_spec = "openai:gpt-4o-mini"
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode="deep",
        non_interactive=True
    )

    mock_build = MagicMock()
    mock_build.capabilities = []

    with patch("strix_pydantic.interface.cli.SkillCapabilityFactory") as mock_factory_cls:
        mock_factory_cls.return_value.build_for_role.return_value = mock_build
        with patch("pydantic_ai.Agent") as mock_agent_cls:
            _build_agents(
                model_spec=model_spec,
                skill_list=[],
                run_config=run_config
            )

            for call in mock_agent_cls.call_args_list:
                kwargs = call.kwargs
                assert kwargs["model_settings"]["reasoning_effort"] == "high"
