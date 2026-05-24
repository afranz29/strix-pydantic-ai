"""Tests for model configuration and resolution."""

import os
import pytest

from strix_pydantic.config.model_config import (
    normalize_model_spec,
    resolve_model_config,
)


class TestResolveModelConfig:
    """Tests for model resolution logic (Go parity)."""

    def test_with_anthropic_key(self, monkeypatch):
        """Test model resolution when ANTHROPIC_API_KEY is set."""
        monkeypatch.setenv("ANTHROPIC_API_KEY", "test-key")
        monkeypatch.delenv("STRIX_LLM", raising=False)

        model_spec, display = resolve_model_config()
        assert model_spec == "anthropic:claude-haiku-4-5"
        assert "claude-haiku-4-5" in display
        assert "Anthropic" in display

    def test_without_anthropic_key(self, monkeypatch):
        """Test model resolution falls back to Gemini without Anthropic key."""
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
        monkeypatch.delenv("STRIX_LLM", raising=False)

        model_spec, display = resolve_model_config()
        assert model_spec == "gemini:gemini-3.5-flash"
        assert "gemini-3.5-flash" in display
        assert "Google" in display

    def test_strix_llm_override(self, monkeypatch):
        """Test STRIX_LLM env var overrides all defaults."""
        monkeypatch.setenv("STRIX_LLM", "openai:gpt-5-mini")
        monkeypatch.setenv("ANTHROPIC_API_KEY", "ignored")

        model_spec, display = resolve_model_config()
        assert model_spec == "openai:gpt-5-mini"

    def test_strix_llm_without_prefix(self, monkeypatch):
        """Test STRIX_LLM without explicit prefix gets normalized."""
        monkeypatch.setenv("STRIX_LLM", "claude-opus")
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)

        model_spec, display = resolve_model_config()
        assert model_spec == "anthropic:claude-opus"


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
        """Test Claude model name normalization."""
        assert normalize_model_spec(input_spec) == expected

    @pytest.mark.parametrize(
        "input_spec,expected",
        [
            ("gemini-3.5-flash", "gemini:gemini-3.5-flash"),
            ("gemini-1.5-pro", "gemini:gemini-1.5-pro"),
            ("gemini-2.0-flash", "gemini:gemini-2.0-flash"),
        ],
    )
    def test_gemini_models(self, input_spec, expected):
        """Test Gemini model name normalization."""
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
        """Test OpenAI model name normalization."""
        assert normalize_model_spec(input_spec) == expected

    def test_explicit_prefix_passthrough(self):
        """Test that explicit prefixes are preserved."""
        spec = "anthropic:claude-3-5-sonnet-20241022"
        assert normalize_model_spec(spec) == spec

    def test_invalid_model_name(self):
        """Test that invalid model names raise ValueError."""
        with pytest.raises(ValueError, match="Cannot infer provider"):
            normalize_model_spec("unknown-model-xyz")

    def test_prefix_normalization_priority(self):
        """Test that explicit prefixes take priority over inference."""
        # Explicit prefix should not be re-normalized
        spec = "openai:claude-fake"  # Invalid but has explicit prefix
        assert normalize_model_spec(spec) == spec
