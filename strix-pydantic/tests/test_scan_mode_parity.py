import pytest
from unittest.mock import MagicMock, patch
from strix_pydantic.interface.cli import _build_agents
from strix_pydantic.agents.types import RunConfig

def test_build_agents_injects_scan_mode_skill():
    model_spec = "openai:gpt-4o-mini"
    skill_list = ["reconnaissance"]
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode="quick",
        non_interactive=True
    )
    
    # Mock dependencies that _build_agents uses
    with patch("strix_pydantic.interface.cli.SkillCapabilityFactory") as mock_factory_cls:
        mock_factory = mock_factory_cls.return_value
        # Mock Agent to avoid real initialization and API key checks
        with patch("pydantic_ai.Agent") as mock_agent_cls:
            _build_agents(
                model_spec=model_spec,
                skill_list=skill_list,
                run_config=run_config
            )
            
            # Check that build_for_role was called with "scan_modes/quick"
            for call in mock_factory.build_for_role.call_args_list:
                args, kwargs = call
                passed_skills = args[0]
                assert "scan_modes/quick" in passed_skills
                assert "reconnaissance" in passed_skills

def test_build_agents_sets_reasoning_effort_quick():
    model_spec = "openai:gpt-4o-mini"
    run_config = RunConfig(
        model_name=model_spec,
        scan_mode="quick",
        non_interactive=True
    )
    
    with patch("pydantic_ai.Agent") as mock_agent_cls:
        _build_agents(
            model_spec=model_spec,
            skill_list=[],
            run_config=run_config
        )
        
        # Check that Agent was instantiated with reasoning_effort="medium"
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
    
    with patch("pydantic_ai.Agent") as mock_agent_cls:
        _build_agents(
            model_spec=model_spec,
            skill_list=[],
            run_config=run_config
        )
        
        # Check that Agent was instantiated with reasoning_effort="high"
        for call in mock_agent_cls.call_args_list:
            kwargs = call.kwargs
            assert kwargs["model_settings"]["reasoning_effort"] == "high"
