package llm

import (
	"context"
	"os"
	"testing"
)

func TestNewLLMClient_Anthropic(t *testing.T) {
	// Save original env vars
	origLLM := os.Getenv("STRIX_LLM")
	origKey := os.Getenv("LLM_API_KEY")
	origAnthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	origGeminiKey := os.Getenv("GEMINI_API_KEY")
	origGoogleKey := os.Getenv("GOOGLE_API_KEY")

	defer func() {
		os.Setenv("STRIX_LLM", origLLM)
		os.Setenv("LLM_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", origAnthropicKey)
		os.Setenv("GEMINI_API_KEY", origGeminiKey)
		os.Setenv("GOOGLE_API_KEY", origGoogleKey)
	}()

	t.Run("ValidAnthropicConfigWithPrefix", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "anthropic/claude-3-5-sonnet-20241022")
		os.Setenv("ANTHROPIC_API_KEY", "fake-anthropic-key")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if client.modelName != "claude-3-5-sonnet-20241022" {
			t.Errorf("Expected model name claude-3-5-sonnet-20241022, got %s", client.modelName)
		}
	})

	t.Run("ValidAnthropicConfigWithoutPrefix", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "claude-haiku-4-5")
		os.Setenv("ANTHROPIC_API_KEY", "fake-anthropic-key")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if client.modelName != "claude-haiku-4-5" {
			t.Errorf("Expected model name claude-haiku-4-5, got %s", client.modelName)
		}
	})

	t.Run("DefaultModelPrecedence_AnthropicSet", func(t *testing.T) {
		os.Unsetenv("STRIX_LLM")
		os.Setenv("ANTHROPIC_API_KEY", "fake-anthropic-key")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if client.modelName != "claude-haiku-4-5" {
			t.Errorf("Expected default model name claude-haiku-4-5 when ANTHROPIC_API_KEY is set, got %s", client.modelName)
		}
	})

	t.Run("DefaultModelPrecedence_AnthropicUnset", func(t *testing.T) {
		os.Unsetenv("STRIX_LLM")
		os.Unsetenv("ANTHROPIC_API_KEY")
		// Set Gemini key to ensure googleai client can initialize if required
		os.Setenv("GEMINI_API_KEY", "fake-gemini-key")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if client.modelName != "gemini-3.5-flash" {
			t.Errorf("Expected default model name gemini-3.5-flash when ANTHROPIC_API_KEY is unset, got %s", client.modelName)
		}
	})
}
