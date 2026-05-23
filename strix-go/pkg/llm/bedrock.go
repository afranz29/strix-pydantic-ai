package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// BedrockLLM implements the llms.Model interface for AWS Bedrock
type BedrockLLM struct {
	region      string
	modelID     string
	bearerToken string
	httpClient  *http.Client
}

// NewBedrockLLM creates a new Bedrock LLM client
func NewBedrockLLM(ctx context.Context, region, modelID, bearerToken string) (*BedrockLLM, error) {
	if region == "" {
		return nil, fmt.Errorf("region cannot be empty")
	}
	if modelID == "" {
		return nil, fmt.Errorf("modelID cannot be empty")
	}
	if bearerToken == "" {
		return nil, fmt.Errorf("bearerToken cannot be empty")
	}

	return &BedrockLLM{
		region:      region,
		modelID:     modelID,
		bearerToken: bearerToken,
		httpClient: &http.Client{
			Timeout: 150 * time.Second,
		},
	}, nil
}

// GenerateContent implements the llms.Model interface
func (b *BedrockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Convert langchaingo messages to Bedrock Converse API format
	converseMessages := make([]map[string]interface{}, 0, len(messages))
	var systemPrompt string

	for _, msg := range messages {
		// Extract system messages separately
		if msg.Role == llms.ChatMessageTypeSystem {
			for _, part := range msg.Parts {
				if textPart, ok := part.(llms.TextContent); ok {
					systemPrompt = textPart.Text
				}
			}
			continue
		}

		// Convert role
		role := "user"
		if msg.Role == llms.ChatMessageTypeAI {
			role = "assistant"
		}

		// Extract text content
		var text string
		for _, part := range msg.Parts {
			if textPart, ok := part.(llms.TextContent); ok {
				text = textPart.Text
			}
		}

		if text != "" {
			converseMessages = append(converseMessages, map[string]interface{}{
				"role": role,
				"content": []map[string]interface{}{
					{
						"text": text,
					},
				},
			})
		}
	}

	// Build request body for Bedrock Converse API
	requestBody := map[string]interface{}{
		"messages": converseMessages,
	}

	// Add system prompt if present
	if systemPrompt != "" {
		requestBody["system"] = []map[string]interface{}{
			{
				"text": systemPrompt,
			},
		}
	}

	// Add inference configuration (can be extended later)
	requestBody["inferenceConfig"] = map[string]interface{}{
		"maxTokens": 4096,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Construct endpoint URL
	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", b.region, b.modelID)

	slog.Debug("Sending Bedrock API request",
		slog.String("endpoint", endpoint),
		slog.String("model", b.modelID),
		slog.Int("message_count", len(converseMessages)),
	)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", b.bearerToken))

	// Execute request
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		slog.Error("Bedrock API returned error",
			slog.Int("status_code", resp.StatusCode),
			slog.String("response_body", string(respBody)),
		)
		return nil, fmt.Errorf("bedrock API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var converseResp struct {
		Output struct {
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &converseResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract response text
	var responseText string
	if len(converseResp.Output.Message.Content) > 0 {
		responseText = converseResp.Output.Message.Content[0].Text
	}

	if responseText == "" {
		slog.Error("Bedrock API returned empty response",
			slog.String("response_body", string(respBody)),
		)
		return nil, fmt.Errorf("bedrock API returned empty response")
	}

	slog.Debug("Bedrock API response received",
		slog.String("model", b.modelID),
		slog.Int("response_length", len(responseText)),
		slog.Int("input_tokens", converseResp.Usage.InputTokens),
		slog.Int("output_tokens", converseResp.Usage.OutputTokens),
	)

	// Convert to langchaingo response format
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: responseText,
			},
		},
	}, nil
}

// Call implements the llms.Model interface (legacy method)
func (b *BedrockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, prompt),
	}

	resp, err := b.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	return resp.Choices[0].Content, nil
}
