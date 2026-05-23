package agents

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/usestrix/strix-go/pkg/llm"
	"github.com/usestrix/strix-go/pkg/tools"
)

func TestLLMBackoffDelayExponentialAndCapped(t *testing.T) {
	cases := []struct {
		consecutive int
		want        time.Duration
	}{
		{0, llmBackoffBase},        // floor
		{1, 2 * time.Second},       // 2s
		{2, 4 * time.Second},       // 4s
		{3, 8 * time.Second},       // 8s
		{4, 16 * time.Second},      // 16s
		{5, 32 * time.Second},      // 32s
		{6, llmBackoffMax},         // 64s -> capped at 60s
		{10, llmBackoffMax},        // far past cap
		{100, llmBackoffMax},       // shift past sane range -> still capped
	}
	for _, tc := range cases {
		got := llmBackoffDelay(tc.consecutive)
		if got != tc.want {
			t.Errorf("llmBackoffDelay(%d) = %s, want %s", tc.consecutive, got, tc.want)
		}
	}
}

func TestToolCallSignatureIsStable(t *testing.T) {
	a := toolCallSignature("terminal_execute", map[string]interface{}{
		"command":  "nmap -sV target",
		"timeout":  30,
		"agent_id": "agent_root", // should be stripped
	})
	b := toolCallSignature("terminal_execute", map[string]interface{}{
		"timeout":  30,
		"command":  "nmap -sV target",
		"agent_id": "agent_other", // different agent_id, still same signature
	})
	if a != b {
		t.Errorf("signatures should be equal regardless of map iteration order and agent_id:\n  a=%s\n  b=%s", a, b)
	}
}

func TestToolCallSignatureDistinguishesArguments(t *testing.T) {
	a := toolCallSignature("terminal_execute", map[string]interface{}{"command": "ls"})
	b := toolCallSignature("terminal_execute", map[string]interface{}{"command": "pwd"})
	c := toolCallSignature("browser_action", map[string]interface{}{"command": "ls"})
	if a == b {
		t.Errorf("different command args should yield different signatures: %s == %s", a, b)
	}
	if a == c {
		t.Errorf("different tool names should yield different signatures: %s == %s", a, c)
	}
}

func TestPruneHistoryEnforcesSlidingWindow(t *testing.T) {
	// Create history larger than the window
	history := make([]llm.Message, 60)
	for i := range history {
		history[i] = llm.Message{
			Role:    "user",
			Content: "message " + string(rune(i)),
		}
	}

	pruned := pruneHistory(history)

	// Should keep only the last historyMaxMessages (50)
	if len(pruned) != historyMaxMessages {
		t.Errorf("pruneHistory should keep last %d messages, got %d", historyMaxMessages, len(pruned))
	}

	// First message in pruned should be the 10th from original (60 - 50)
	if pruned[0].Content != history[10].Content {
		t.Errorf("pruned history should start from position 10 of original")
	}
}

func TestPruneHistoryKeepsSmallHistory(t *testing.T) {
	history := []llm.Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
	}

	pruned := pruneHistory(history)

	if len(pruned) != len(history) {
		t.Errorf("small history should not be pruned, got %d want %d", len(pruned), len(history))
	}
}

func TestRedactBase64ContentRemovesScreenshots(t *testing.T) {
	// Large base64 string (would typically be a screenshot)
	base64Str := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	content := fmt.Sprintf("Tool output:\n%s\nEnd of output", base64Str)

	redacted := redactBase64Content(content)

	// Should have replaced the base64 string
	if strings.Contains(redacted, base64Str) {
		t.Errorf("redactBase64Content should remove base64 content")
	}
	if !strings.Contains(redacted, "[base64-redacted-") {
		t.Errorf("redactBase64Content should add redaction placeholder")
	}
	if !strings.Contains(redacted, "Tool output:") {
		t.Errorf("redactBase64Content should preserve non-base64 content")
	}
}

func TestRedactBase64ContentPreservesShortStrings(t *testing.T) {
	// Short string that looks like base64 but is below threshold (80)
	shortB64 := "SGVsbG8gV29ybGQ="
	content := fmt.Sprintf("Message: %s", shortB64)

	redacted := redactBase64Content(content)

	// Should NOT have redacted short strings
	if !strings.Contains(redacted, shortB64) {
		t.Errorf("redactBase64Content should not redact strings shorter than 80 chars")
	}
}

func TestValidateToolAvailabilityMissingTools(t *testing.T) {
	// Test with non-existent tool - this should fail
	err := validateToolAvailability([]string{"nonexistent_tool_xyz_12345"}, tools.ExecutionContextParent)
	if err == nil {
		t.Errorf("validateToolAvailability should fail for non-existent tools")
	}
	if !strings.Contains(err.Error(), "required tools not found") && !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error message should mention missing tools, got: %v", err)
	}
}

func TestPruneHistoryRedactsOldMessages(t *testing.T) {
	// Create history with many messages
	history := make([]llm.Message, 60)
	base64Data := strings.Repeat("A", 150) // Large base64-like string
	for i := range history {
		if i < 45 { // Older messages (will be redacted)
			history[i] = llm.Message{
				Role:    "user",
				Content: fmt.Sprintf("Message with data: %s", base64Data),
			}
		} else { // Recent messages (will be kept as-is)
			history[i] = llm.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Response %d", i),
			}
		}
	}

	pruned := pruneHistory(history)

	if len(pruned) != historyMaxMessages {
		t.Errorf("pruned history length should be %d, got %d", historyMaxMessages, len(pruned))
	}

	// Check that older messages were redacted
	for i := 0; i < len(pruned)-10 && i < len(pruned); i++ {
		if strings.Contains(pruned[i].Content, "A") && len(pruned[i].Content) > 100 {
			t.Errorf("message %d should have been redacted", i)
		}
	}
}

