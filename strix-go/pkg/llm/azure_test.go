package llm

import (
	"context"
	"os"
	"testing"
)

func TestNewLLMClient_Azure(t *testing.T) {
	// Save original env vars
	origLLM := os.Getenv("STRIX_LLM")
	origKey := os.Getenv("AZURE_OPENAI_API_KEY")
	origBase := os.Getenv("AZURE_OPENAI_ENDPOINT")
	origVersion := os.Getenv("AZURE_OPENAI_API_VERSION")

	defer func() {
		os.Setenv("STRIX_LLM", origLLM)
		os.Setenv("AZURE_OPENAI_API_KEY", origKey)
		os.Setenv("AZURE_OPENAI_ENDPOINT", origBase)
		os.Setenv("AZURE_OPENAI_API_VERSION", origVersion)
	}()

	t.Run("ValidAzureConfig", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "azure/gpt-4o")
		os.Setenv("AZURE_OPENAI_API_KEY", "fake-key")
		os.Setenv("AZURE_OPENAI_ENDPOINT", "https://example.com")
		os.Setenv("AZURE_OPENAI_API_VERSION", "2024-02-01")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if client.modelName != "gpt-4o" {
			t.Errorf("Expected model name gpt-4o, got %s", client.modelName)
		}
	})

	t.Run("MissingAPIKey", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "azure/gpt-4o")
		os.Unsetenv("AZURE_OPENAI_API_KEY")
		os.Unsetenv("AZURE_API_KEY")
		os.Setenv("AZURE_OPENAI_ENDPOINT", "https://example.com")

		_, err := NewLLMClient(context.Background())
		if err == nil {
			t.Error("Expected error due to missing API key, got nil")
		}
	})

	t.Run("MissingEndpoint", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "azure/gpt-4o")
		os.Setenv("AZURE_OPENAI_API_KEY", "fake-key")
		os.Unsetenv("AZURE_OPENAI_ENDPOINT")
		os.Unsetenv("AZURE_API_BASE")

		_, err := NewLLMClient(context.Background())
		if err == nil {
			t.Error("Expected error due to missing endpoint, got nil")
		}
	})

	t.Run("FallbackEnvVars", func(t *testing.T) {
		os.Setenv("STRIX_LLM", "azure/gpt-4o")
		os.Unsetenv("AZURE_OPENAI_API_KEY")
		os.Setenv("AZURE_API_KEY", "fallback-key")
		os.Unsetenv("AZURE_OPENAI_ENDPOINT")
		os.Setenv("AZURE_API_BASE", "https://fallback.com")

		client, err := NewLLMClient(context.Background())
		if err != nil {
			t.Fatalf("Expected no error with fallback env vars, got %v", err)
		}

		if client.modelName != "gpt-4o" {
			t.Errorf("Expected model name gpt-4o, got %s", client.modelName)
		}
	})
}
