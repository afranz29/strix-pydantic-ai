package _interface

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type tuiState struct {
	mu         sync.Mutex
	tuiLogs    []string
	maxTuiLogs int
	systemFile *os.File
	agentFile  *os.File
	writeStdout bool
	spinnerIdx int
	lastStatus string
}

// StrixLogHandler is a custom slog handler that routes logs to different files
// based on whether they are agent-specific or system-wide.
type StrixLogHandler struct {
	systemLog      slog.Handler
	agentLog       slog.Handler
	isAgentHandler bool
	shared         *tuiState
}

func NewStrixLogHandler(logDir string, writeStdout bool) (*StrixLogHandler, error) {
	systemPath := filepath.Join(logDir, "strix.log")
	agentPath := filepath.Join(logDir, "agent.log")

	sf, err := os.OpenFile(systemPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open system log: %w", err)
	}

	af, err := os.OpenFile(agentPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		sf.Close()
		return nil, fmt.Errorf("failed to open agent log: %w", err)
	}

	logOpts := &slog.HandlerOptions{Level: slog.LevelDebug}

	return &StrixLogHandler{
		systemLog: slog.NewTextHandler(sf, logOpts),
		agentLog:  slog.NewTextHandler(af, logOpts),
		shared: &tuiState{
			systemFile: sf,
			agentFile:  af,
			tuiLogs:    make([]string, 0, 500),
			maxTuiLogs: 500,
			writeStdout: writeStdout,
		},
	}, nil
}

func (h *StrixLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *StrixLogHandler) Handle(ctx context.Context, r slog.Record) error {
	isAgentActivity := h.isAgentHandler
	if !isAgentActivity {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "agent_id" || a.Key == "child_id" || a.Key == "parent_id" {
				isAgentActivity = true
				return false // stop iteration
			}
			return true
		})
	}

	// Also allow high-level scan lifecycle notifications to be treated as agent activity for TUI
	msgLower := strings.ToLower(r.Message)
	isLifecycle := strings.Contains(msgLower, "penetration test initiating") || strings.Contains(msgLower, "scan completed")

	var err error
	if isAgentActivity {
		err = h.agentLog.Handle(ctx, r)
	} else {
		err = h.systemLog.Handle(ctx, r)
	}

	// Update TUI buffer if it's agent activity or lifecycle and level is Info or higher
	if (isAgentActivity || isLifecycle) && r.Level >= slog.LevelInfo {
		// Filter out only the most verbose system setup / Docker / framework noise from the TUI
		if !strings.Contains(msgLower, "reusing parent sandbox") &&
			!strings.Contains(msgLower, "notes database") &&
			!strings.Contains(msgLower, "tool schemas") &&
			!strings.Contains(msgLower, "gemini client") {

			h.appendToTuiBuffer(r)
		}
	}

	return err
}

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (h *StrixLogHandler) appendToTuiBuffer(r slog.Record) {
	// Format with emoji indicators and high-level findings
	symbol := "→"
	msgLower := strings.ToLower(r.Message)

	// Show activity spinner for LLM and processing activities
	if strings.Contains(msgLower, "sending request to llm") ||
	   strings.Contains(msgLower, "requesting llm completion") ||
	   strings.Contains(msgLower, "proxying tool execution") {
		h.shared.mu.Lock()
		h.shared.spinnerIdx = (h.shared.spinnerIdx + 1) % len(spinnerChars)
		spinner := spinnerChars[h.shared.spinnerIdx]
		h.shared.lastStatus = fmt.Sprintf("%s %s", spinner, r.Message)
		if h.shared.writeStdout {
			fmt.Print("\r" + h.shared.lastStatus + "                    ")
		}
		h.shared.mu.Unlock()
		return
	}

	// Map messages to appropriate symbols
	switch {
	case strings.Contains(msgLower, "penetration test initiating"):
		symbol = "→"
	case strings.Contains(msgLower, "spawning agent"):
		symbol = "🚀"
	case strings.Contains(msgLower, "starting agent execution"):
		symbol = "⚙"
	case strings.Contains(msgLower, "invoking tool"):
		symbol = "🔧"
	case strings.Contains(msgLower, "tool executed successfully"):
		symbol = "✓"
	case strings.Contains(msgLower, "graph of agents"):
		symbol = "📊"
	case strings.Contains(msgLower, "registered new agent"):
		symbol = "📝"
	case strings.Contains(msgLower, "llm completion received"):
		symbol = "💭"
	case strings.Contains(msgLower, "agent entering wait"):
		symbol = "⏸"
	case strings.Contains(msgLower, "wait state timed out"):
		symbol = "⏱"
	case strings.Contains(msgLower, "tool execution failed"):
		symbol = "❌"
	case strings.Contains(msgLower, "error") || r.Level == slog.LevelError:
		symbol = "❌"
	case strings.Contains(msgLower, "warn") || r.Level == slog.LevelWarn:
		symbol = "⚠"
	case strings.Contains(msgLower, "finish"):
		symbol = "🏁"
	case strings.Contains(msgLower, "discovered") || strings.Contains(msgLower, "found"):
		symbol = "🔍"
	case strings.Contains(msgLower, "vulnerability") || strings.Contains(msgLower, "vulnerable"):
		symbol = "🚨"
	case strings.Contains(msgLower, "creating") || strings.Contains(msgLower, "created"):
		symbol = "✨"
	}

	msg := fmt.Sprintf("%s %s", symbol, r.Message)

	// Add key attributes
	var attrs []string
	var omittedCount int
	r.Attrs(func(a slog.Attr) bool {
		// Omit only the most verbose attributes
		if slices.Contains([]string{"completion", "kwargs"}, a.Key) {
			omittedCount++
			return true
		}

		// Skip IDs for cleaner output
		if slices.Contains([]string{"agent_id", "child_id"}, a.Key) {
			return true
		}

		// Include everything else: results, findings, tool details, etc.
		value := sanitizeTuiAttrValue(a.Value.Any())
		if value != "" {
			attrs = append(attrs, fmt.Sprintf("%s=%s", a.Key, value))
		}
		return true
	})

	if len(attrs) > 0 {
		msg += " " + strings.Join(attrs, " ")
	}
	if omittedCount > 0 {
		msg += fmt.Sprintf(" [+%d details]", omittedCount)
	}

	h.shared.mu.Lock()
	defer h.shared.mu.Unlock()
	h.shared.tuiLogs = append(h.shared.tuiLogs, msg)
	if len(h.shared.tuiLogs) > h.shared.maxTuiLogs {
		h.shared.tuiLogs = h.shared.tuiLogs[1:]
	}
	h.shared.lastStatus = msg

	if h.shared.writeStdout {
		// Clear spinner line and print new message
		fmt.Print("\r")
		fmt.Println(msg)
	}
}

func sanitizeTuiAttrValue(v interface{}) string {
	s := fmt.Sprint(v)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	const maxLen = 300
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func (h *StrixLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	isAgent := h.isAgentHandler
	if !isAgent {
		for _, a := range attrs {
			if a.Key == "agent_id" || a.Key == "child_id" || a.Key == "parent_id" {
				isAgent = true
				break
			}
		}
	}

	return &StrixLogHandler{
		systemLog:      h.systemLog.WithAttrs(attrs),
		agentLog:       h.agentLog.WithAttrs(attrs),
		isAgentHandler: isAgent,
		shared:         h.shared,
	}
}

func (h *StrixLogHandler) WithGroup(name string) slog.Handler {
	return &StrixLogHandler{
		systemLog:      h.systemLog.WithGroup(name),
		agentLog:       h.agentLog.WithGroup(name),
		isAgentHandler: h.isAgentHandler,
		shared:         h.shared,
	}
}

func (h *StrixLogHandler) Close() {
	h.shared.mu.Lock()
	defer h.shared.mu.Unlock()
	if h.shared.systemFile != nil {
		h.shared.systemFile.Close()
	}
	if h.shared.agentFile != nil {
		h.shared.agentFile.Close()
	}
}

func (h *StrixLogHandler) GetTuiLogs() []string {
	h.shared.mu.Lock()
	defer h.shared.mu.Unlock()
	copied := make([]string, len(h.shared.tuiLogs))
	copy(copied, h.shared.tuiLogs)
	return copied
}
