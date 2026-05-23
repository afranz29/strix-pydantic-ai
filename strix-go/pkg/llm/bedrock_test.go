package llm

import (
	"context"
	"testing"
)

func TestNewBedrockLLM(t *testing.T) {
	tests := []struct {
		name        string
		region      string
		modelID     string
		bearerToken string
		wantErr     bool
		errMsg      string
	}{
		{
			name:        "valid parameters",
			region:      "us-east-1",
			modelID:     "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
			bearerToken: "test-token",
			wantErr:     false,
		},
		{
			name:        "empty region",
			region:      "",
			modelID:     "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
			bearerToken: "test-token",
			wantErr:     true,
			errMsg:      "region cannot be empty",
		},
		{
			name:        "empty modelID",
			region:      "us-east-1",
			modelID:     "",
			bearerToken: "test-token",
			wantErr:     true,
			errMsg:      "modelID cannot be empty",
		},
		{
			name:        "empty bearerToken",
			region:      "us-east-1",
			modelID:     "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
			bearerToken: "",
			wantErr:     true,
			errMsg:      "bearerToken cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client, err := NewBedrockLLM(ctx, tt.region, tt.modelID, tt.bearerToken)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewBedrockLLM() expected error but got nil")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("NewBedrockLLM() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewBedrockLLM() unexpected error = %v", err)
				}
				if client == nil {
					t.Errorf("NewBedrockLLM() returned nil client")
				}
				if client != nil {
					if client.region != tt.region {
						t.Errorf("BedrockLLM.region = %v, want %v", client.region, tt.region)
					}
					if client.modelID != tt.modelID {
						t.Errorf("BedrockLLM.modelID = %v, want %v", client.modelID, tt.modelID)
					}
				}
			}
		})
	}
}
