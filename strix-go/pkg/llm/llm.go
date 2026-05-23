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
		model = "gemini-3.5-flash" // Default to Gemini first
	}

	apiKey := os.Getenv("LLM_API_KEY")

	// Determine if it is a Bedrock model
	isBedrock := strings.HasPrefix(model, "bedrock/")

	if isBedrock {
		// Read environment variables
		region := os.Getenv("AWS_REGION")
		bearerToken := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")

		// Validate required environment variables
		if region == "" {
			return nil, fmt.Errorf("AWS_REGION environment variable is required for Bedrock models")
		}
		if bearerToken == "" {
			return nil, fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK environment variable is required for Bedrock models")
		}

		// Extract model ID by removing bedrock/ prefix
		modelID := strings.TrimPrefix(model, "bedrock/")
		if modelID == "" {
			return nil, fmt.Errorf("invalid Bedrock model format: model ID cannot be empty after 'bedrock/' prefix")
		}

		slog.Info("Initializing Bedrock client",
			slog.String("model", modelID),
			slog.String("region", region),
		)

		cli, err := NewBedrockLLM(ctx, region, modelID, bearerToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create Bedrock client: %w", err)
		}

		return &LLMClient{
			llm:       cli,
			modelName: modelID,
		}, nil
	}

	// Determine if it is an Azure model
	isAzure := strings.HasPrefix(model, "azure/")

	if isAzure {
		apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("AZURE_API_KEY")
		}

		apiBase := os.Getenv("AZURE_OPENAI_ENDPOINT")
		if apiBase == "" {
			apiBase = os.Getenv("AZURE_API_BASE")
		}

		apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
		if apiVersion == "" {
			apiVersion = os.Getenv("AZURE_API_VERSION")
		}
		if apiVersion == "" {
			apiVersion = "2024-02-01" // Default stable version
		}

		// Extract deployment name by removing azure/ prefix
		deploymentName := strings.TrimPrefix(model, "azure/")
		if deploymentName == "" {
			return nil, fmt.Errorf("invalid Azure model format: deployment name cannot be empty after 'azure/' prefix")
		}

		if apiKey == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_API_KEY or AZURE_API_KEY is required for Azure models")
		}
		if apiBase == "" {
			return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT or AZURE_API_BASE is required for Azure models")
		}

		slog.Info("Initializing Azure OpenAI client",
			slog.String("deployment", deploymentName),
			slog.String("api_base", apiBase),
			slog.String("api_version", apiVersion),
		)

		opts := []openai.Option{
			openai.WithAPIType(openai.APITypeAzure),
			openai.WithToken(apiKey),
			openai.WithBaseURL(apiBase),
			openai.WithModel(deploymentName),
			openai.WithAPIVersion(apiVersion),
		}

		cli, err := openai.New(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure OpenAI client: %w", err)
		}

		return &LLMClient{
			llm:       cli,
			modelName: deploymentName,
		}, nil
	}

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
