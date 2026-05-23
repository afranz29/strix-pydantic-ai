package llm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/openai"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMClient struct {
	llm       llms.Model
	modelName string
}

func NewLLMClient(ctx context.Context) (*LLMClient, error) {
	model := os.Getenv("STRIX_LLM")
	if model == "" {
		model = "gemini-3.1-flash-lite" // Default to Gemini first
	}

	apiKey := os.Getenv("LLM_API_KEY")

	// Determine if it is a Gemini/Google model
	isGemini := strings.Contains(strings.ToLower(model), "gemini") ||
		strings.HasPrefix(model, "googleai/") ||
		strings.HasPrefix(model, "gemini/")

	if isGemini {
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}

		canonicalModel := model
		if strings.HasPrefix(model, "gemini/") {
			canonicalModel = strings.TrimPrefix(model, "gemini/")
		} else if strings.HasPrefix(model, "googleai/") {
			canonicalModel = strings.TrimPrefix(model, "googleai/")
		} else if strings.HasPrefix(model, "strix/") {
			base := strings.TrimPrefix(model, "strix/")
			if base == "gemini-3-pro-preview" || base == "gemini-1.5-pro" {
				canonicalModel = "gemini-1.5-pro"
			} else {
				canonicalModel = base
			}
		}

		slog.Info("Initializing Gemini client", slog.String("model", canonicalModel))

		var opts []googleai.Option
		if apiKey != "" {
			opts = append(opts, googleai.WithAPIKey(apiKey))
		}
		opts = append(opts, googleai.WithDefaultModel(canonicalModel))

		cli, err := googleai.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create langchaingo googleai client: %w", err)
		}

		return &LLMClient{
			llm:       cli,
			modelName: canonicalModel,
		}, nil
	}

	// Fallback to OpenAI provider for other models
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	apiBase := os.Getenv("LLM_API_BASE")

	canonicalModel := model
	if strings.HasPrefix(model, "openai/") {
		canonicalModel = strings.TrimPrefix(model, "openai/")
	}

	slog.Info("Initializing OpenAI fallback client",
		slog.String("model", canonicalModel),
		slog.String("api_base", apiBase),
	)

	var opts []openai.Option
	if apiKey != "" {
		opts = append(opts, openai.WithToken(apiKey))
	}
	if apiBase != "" {
		opts = append(opts, openai.WithBaseURL(apiBase))
	}
	opts = append(opts, openai.WithModel(canonicalModel))

	cli, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create langchaingo openai client: %w", err)
	}

	return &LLMClient{
		llm:       cli,
		modelName: canonicalModel,
	}, nil
}

func (c *LLMClient) GenerateChatCompletion(ctx context.Context, systemPrompt string, history []Message) (string, error) {
	var messages []llms.MessageContent

	if systemPrompt != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt))
	}

	for _, msg := range history {
		role := llms.ChatMessageTypeHuman
		if msg.Role == "assistant" {
			role = llms.ChatMessageTypeAI
		} else if msg.Role == "system" {
			role = llms.ChatMessageTypeSystem
		}

		messages = append(messages, llms.TextParts(role, msg.Content))
	}

	slog.Debug("Sending request to LLM",
		slog.String("model", c.modelName),
		slog.Int("history_length", len(history)),
	)

	resp, err := c.llm.GenerateContent(ctx, messages, llms.WithModel(c.modelName))
	if err != nil {
		slog.Error("LLM generation request failed",
			slog.String("model", c.modelName),
			slog.Any("error", err),
		)
		return "", fmt.Errorf("langchaingo completion request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		slog.Error("LLM generation returned empty choices", slog.String("model", c.modelName))
		return "", fmt.Errorf("langchaingo returned empty choices")
	}

	slog.Debug("LLM response generated successfully",
		slog.String("model", c.modelName),
		slog.Int("response_length", len(resp.Choices[0].Content)),
	)

	return resp.Choices[0].Content, nil
}
