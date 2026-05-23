package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type ToolExecutionRequest struct {
	AgentID  string                 `json:"agent_id"`
	ToolName string                 `json:"tool_name"`
	Kwargs   map[string]interface{} `json:"kwargs"`
}

type ToolExecutionResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type SandboxClient struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
}

func NewSandboxClient(baseURL, authToken string) *SandboxClient {
	return &SandboxClient{
		BaseURL:   baseURL,
		AuthToken: authToken,
		HTTPClient: &http.Client{
			Timeout: 150 * time.Second, // Tool execution timeout inside container
		},
	}
}

func (c *SandboxClient) ExecuteTool(ctx context.Context, agentID, toolName string, kwargs map[string]interface{}) (interface{}, error) {
	reqURL := fmt.Sprintf("%s/execute", c.BaseURL)

	reqPayload := ToolExecutionRequest{
		AgentID:  agentID,
		ToolName: toolName,
		Kwargs:   kwargs,
	}

	slog.Debug("Proxying tool execution request to sandbox container",
		slog.String("agent_id", agentID),
		slog.String("tool_name", toolName),
		slog.String("url", reqURL),
	)

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		slog.Error("Failed to marshal sandbox tool payload", slog.String("tool_name", toolName), slog.Any("error", err))
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.AuthToken))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		slog.Error("Sandbox tool proxy HTTP request failed",
			slog.String("tool_name", toolName),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Sandbox tool proxy returned non-OK status",
			slog.String("tool_name", toolName),
			slog.Int("status_code", resp.StatusCode),
			slog.String("response_body", string(body)),
		)
		return nil, fmt.Errorf("http error status %d: %s", resp.StatusCode, string(body))
	}

	var toolResp ToolExecutionResponse
	if err := json.NewDecoder(resp.Body).Decode(&toolResp); err != nil {
		slog.Error("Failed to decode sandbox tool response payload", slog.String("tool_name", toolName), slog.Any("error", err))
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if toolResp.Error != "" {
		slog.Error("Sandbox tool server reported execution error",
			slog.String("tool_name", toolName),
			slog.String("execution_error", toolResp.Error),
		)
		return nil, fmt.Errorf("sandbox execution error: %s", toolResp.Error)
	}

	slog.Debug("Sandbox tool proxy completed successfully", slog.String("tool_name", toolName))
	return toolResp.Result, nil
}
