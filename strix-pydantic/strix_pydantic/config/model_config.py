"""Model resolution with Go-parity fallback logic (v1.102.0 compatible)."""

import os


def resolve_model_config() -> tuple[str, str]:
    """
    Resolve LLM model configuration matching strix-go/pkg/llm/llm.go.

    Pydantic AI v1.102.0 uses string format: "provider:model"

    Returns:
        Tuple of (model_spec: str, display_name: str)
        model_spec is ready to pass to Agent(model_spec)

    Priority (matches Go implementation):
    1. STRIX_LLM env var (if set, exact model string)
    2. ANTHROPIC_API_KEY set → "claude-haiku-4-5"
    3. Fallback → "claude-haiku-4-5" (Anthropic default, sensible fallback)
    """
    model_override = os.getenv("STRIX_LLM", "").strip()

    if model_override:
        # Normalize to include provider prefix if needed
        normalized = normalize_model_spec(model_override)
        return normalized, model_override

    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic:claude-haiku-4-5", "claude-haiku-4-5"

    if os.getenv("OPENAI_API_KEY"):
        return "openai:gpt-4o-mini", "gpt-4o-mini"

    if os.getenv("GEMINI_API_KEY") or os.getenv("GOOGLE_API_KEY"):
        return "google-gla:gemini-3.5-flash", "gemini-3.5-flash"

    raise ValueError(
        "No LLM API key found. Set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY. "
        "Or specify a model explicitly with --model or STRIX_LLM."
    )


def normalize_model_spec(model_spec: str) -> str:
    """
    Normalize model specification string per strix-go detection logic.

    Handles model name patterns:
    - "claude-*" → "anthropic:claude-..."
    - "gemini-*" → "gemini:gemini-..."
    - "gpt-*", "o1-*", "o3-*" → "openai:..."
    - "bedrock/*" → "bedrock/..."
    - "azure/*" → "azure/..."
    - Explicit prefixes (anthropic:, openai:, gemini:) → passthrough

    Args:
        model_spec: Input model specification

    Returns:
        Normalized format suitable for pydantic-ai Agent()
    """
    model_spec = model_spec.strip()

    # Check for explicit provider prefix first
    if ":" in model_spec:
        provider, model = model_spec.split(":", 1)
        provider = provider.strip().lower()

        # Validate known providers
        if provider not in ["anthropic", "openai", "gemini", "googleai", "bedrock", "azure"]:
            raise ValueError(
                f"Unknown provider prefix '{provider}'. "
                f"Supported: anthropic, openai, gemini, googleai, bedrock, azure"
            )
        return model_spec

    # Infer provider from model name (matching Go logic)
    model_lower = model_spec.lower()

    # Anthropic detection: starts with "claude-" or contains "claude"
    if model_lower.startswith("claude-") or "claude" in model_lower:
        return f"anthropic:{model_spec}"

    # Gemini/Google detection: contains "gemini", "flash", or starts with those
    if "gemini" in model_lower or model_lower.startswith("gemini-"):
        return f"gemini:{model_spec}"
    if model_lower.startswith("flash"):
        return f"gemini:{model_spec}"

    # OpenAI detection: gpt-*, o1-*, o3-*, gpt4, gpt5, etc patterns
    if any(pat in model_lower for pat in ["gpt-", "gpt4", "gpt5", "o1-", "o3-"]):
        return f"openai:{model_spec}"

    # Fallback error
    raise ValueError(
        f"Cannot infer provider from model '{model_spec}'. "
        f"Use explicit prefix or include identifying keyword: "
        f"claude-*, gemini-*, gpt-*, o1-*, o3-*"
    )
