package agents

import (
	"testing"
	"time"
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
