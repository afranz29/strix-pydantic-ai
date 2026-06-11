package api

import "time"

type ScanRequest struct {
	Target     string  `json:"target"`
	ScanMode   string  `json:"scan_mode"`
	Model      string  `json:"model,omitempty"`
	MockTools  bool    `json:"mock_tools"`
	Confirm    bool    `json:"confirm"`
	Instruction string `json:"instruction"`
	SandboxURL string `json:"sandbox_url,omitempty"`
	Skills     string  `json:"skills"`
	Timeout    float64 `json:"timeout"`
	Verbose    bool    `json:"verbose"`
}

type ScanResponse struct {
	ScanID   string `json:"scan_id"`
	Target   string `json:"target"`
	ScanMode string `json:"scan_mode"`
}

type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	ScanID    string    `json:"scan_id"`
}

type ScanStartedEvent struct {
	Event
	Target   string `json:"target"`
	ScanMode string `json:"scan_mode"`
	Model    string `json:"model"`
}

type ScanConfiguredEvent struct {
	Event
	ToolsCount int      `json:"tools_count"`
	Skills     []string `json:"skills"`
	SandboxURL string   `json:"sandbox_url"`
	MockTools  bool     `json:"mock_tools"`
}

type AgentStartedEvent struct {
	Event
	Role      string `json:"role"`
	Iteration int    `json:"iteration"`
}

type AgentMessageEvent struct {
	Event
	Role      string `json:"role"`
	Iteration int    `json:"iteration"`
	Message   string `json:"message"`
}

type AgentCompletedEvent struct {
	Event
	Role                 string `json:"role"`
	Iteration            int    `json:"iteration"`
	VulnerabilitiesFound int    `json:"vulnerabilities_found"`
}

type VulnerabilityFoundEvent struct {
	Event
	Role        string `json:"role"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	CVEID       string `json:"cve_id,omitempty"`
	Parameter   string `json:"parameter,omitempty"`
	POC         string `json:"poc,omitempty"`
}

type AgentThinkingEvent struct {
	Event
	Role      string `json:"role"`
	Iteration int    `json:"iteration"`
	Thinking  string `json:"thinking"`
}

type AgentTokenUsageEvent struct {
	Event
	Role               string `json:"role"`
	Iteration          int    `json:"iteration"`
	InputTokens        int    `json:"input_tokens"`
	OutputTokens       int    `json:"output_tokens"`
	CacheReadTokens    int    `json:"cache_read_tokens"`
	CacheWriteTokens   int    `json:"cache_write_tokens"`
}

type ToolExecutedEvent struct {
	Event
	ToolName string  `json:"tool_name"`
	Status   string  `json:"status"`
	DurationMs float64 `json:"duration_ms,omitempty"`
	ExitCode int     `json:"exit_code,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type LogMessageEvent struct {
	Event
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ScanCompletedEvent struct {
	Event
	DurationSeconds      float64 `json:"duration_seconds"`
	VulnerabilitiesCount int     `json:"vulnerabilities_count"`
	Iterations           int     `json:"iterations"`
}

type ScanFailedEvent struct {
	Event
	Error            string  `json:"error"`
	DurationSeconds  float64 `json:"duration_seconds"`
}
