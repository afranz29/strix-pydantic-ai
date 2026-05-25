"""Tests for model configuration and resolution."""

import pytest

from strix_pydantic.config.model_config import normalize_model_spec, resolve_model_config


def _clear_llm_env(monkeypatch):
    for env_var in [
        "STRIX_LLM",
        "ANTHROPIC_API_KEY",
        "OPENAI_API_KEY",
        "GEMINI_API_KEY",
        "GOOGLE_API_KEY",
    ]:
        monkeypatch.delenv(env_var, raising=False)


class TestResolveModelConfig:
    """Tests for model resolution logic."""

    def test_with_anthropic_key(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("ANTHROPIC_API_KEY", "test-key")

        model_spec, display = resolve_model_config()
        assert model_spec == "anthropic:claude-haiku-4-5"
        assert display == "claude-haiku-4-5"

    def test_with_openai_key(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("OPENAI_API_KEY", "test-key")

        model_spec, display = resolve_model_config()
        assert model_spec == "openai:gpt-4o-mini"
        assert display == "gpt-4o-mini"

    def test_with_gemini_key(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("GEMINI_API_KEY", "test-key")

        model_spec, display = resolve_model_config()
        assert model_spec == "google:gemini-3.5-flash"
        assert display == "gemini-3.5-flash"

    def test_without_api_keys_raises(self, monkeypatch):
        _clear_llm_env(monkeypatch)

        with pytest.raises(ValueError, match="No LLM API key found"):
            resolve_model_config()

    def test_strix_llm_override(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("STRIX_LLM", "openai:gpt-5-mini")

        model_spec, _ = resolve_model_config()
        assert model_spec == "openai:gpt-5-mini"

    def test_strix_llm_without_prefix(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("STRIX_LLM", "claude-opus")

        model_spec, _ = resolve_model_config()
        assert model_spec == "anthropic:claude-opus"

    def test_strix_llm_azure_colon_override(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("STRIX_LLM", "azure:gpt-4o")

        model_spec, _ = resolve_model_config()
        assert model_spec == "azure:gpt-4o"

    def test_strix_llm_azure_slash_override(self, monkeypatch):
        _clear_llm_env(monkeypatch)
        monkeypatch.setenv("STRIX_LLM", "azure/gpt-4o")

        model_spec, _ = resolve_model_config()
        assert model_spec == "azure:gpt-4o"


class TestNormalizeModelSpec:
    """Tests for model specification normalization."""

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("claude-haiku-4-5", "anthropic:claude-haiku-4-5"),
            ("claude-3-5-sonnet-20241022", "anthropic:claude-3-5-sonnet-20241022"),
            ("claude-opus", "anthropic:claude-opus"),
        ],
    )
    def test_claude_models(self, input_spec, expected):
        assert normalize_model_spec(input_spec) == expected

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("gemini-3.5-flash", "google:gemini-3.5-flash"),
            ("gemini-1.5-pro", "google:gemini-1.5-pro"),
            ("gemini-2.0-flash", "google:gemini-2.0-flash"),
        ],
    )
    def test_gemini_models(self, input_spec, expected):
        assert normalize_model_spec(input_spec) == expected

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("gemini:gemini-3.1-pro-preview", "google:gemini-3.1-pro-preview"),
            ("google:gemini-3.1-pro-preview", "google:gemini-3.1-pro-preview"),
            ("googleai:gemini-3.1-pro-preview", "google:gemini-3.1-pro-preview"),
            ("google-gla:gemini-3.1-pro-preview", "google:gemini-3.1-pro-preview"),
        ],
    )
    def test_gemini_provider_aliases(self, input_spec, expected):
        assert normalize_model_spec(input_spec) == expected

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("gpt-4o", "openai:gpt-4o"),
            ("gpt-4-turbo", "openai:gpt-4-turbo"),
            ("gpt-5-mini", "openai:gpt-5-mini"),
            ("o1-preview", "openai:o1-preview"),
            ("o3-mini", "openai:o3-mini"),
        ],
    )
    def test_openai_models(self, input_spec, expected):
        assert normalize_model_spec(input_spec) == expected

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("azure:gpt-4o", "azure:gpt-4o"),
            ("AZURE:gpt-4o", "azure:gpt-4o"),
            ("azure/gpt-4o", "azure:gpt-4o"),
        ],
    )
    def test_azure_models(self, input_spec, expected):
        assert normalize_model_spec(input_spec) == expected

    def test_azure_slash_priority_over_openai_inference(self):
        assert normalize_model_spec("azure/gpt-4o") == "azure:gpt-4o"

    def test_explicit_prefix_passthrough(self):
        spec = "anthropic:claude-3-5-sonnet-20241022"
        assert normalize_model_spec(spec) == spec

    def test_unknown_provider_prefix(self):
        with pytest.raises(ValueError, match="Unknown provider prefix"):
            normalize_model_spec("notreal:model")

    def test_invalid_model_name(self):
        with pytest.raises(ValueError, match="Cannot infer provider"):
            normalize_model_spec("unknown-model-xyz")

    def test_empty_model_name_with_provider(self):
        with pytest.raises(ValueError, match="Model name cannot be empty"):
            normalize_model_spec("azure:")


class TestAgentModelWiring:
    """Tests that model specs stay provider-correct when constructing agents."""

    def test_build_agents_with_azure_model_spec(self, monkeypatch):
        monkeypatch.setenv("AZURE_OPENAI_ENDPOINT", "https://example.openai.azure.com/")
        monkeypatch.setenv("AZURE_OPENAI_API_KEY", "test-key")
        monkeypatch.setenv("OPENAI_API_VERSION", "2024-02-01")

        from strix_pydantic.agents.types import RunConfig
        from strix_pydantic.interface.cli import _build_agents

        run_config = RunConfig(
            model_name="azure:gpt-4o",
            scan_mode="standard",
            non_interactive=True,
        )

        agents = _build_agents("azure:gpt-4o", [], run_config)

        assert set(agents.keys()) == {
            "reconnaissance",
            "exploitation",
            "post_exploitation",
        }

        for agent in agents.values():
            assert agent.model.model_name == "gpt-4o"
            assert agent.model.provider.name == "azure"
