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

// StrixLogHandler is a custom slog handler that routes logs to different files
// based on whether they are agent-specific or system-wide.
type StrixLogHandler struct {
	mu             sync.Mutex
	systemLog      slog.Handler
	agentLog       slog.Handler
	systemFile     *os.File
	agentFile      *os.File
	isAgentHandler bool

	// TUI buffer
	tuiLogs    []string
	maxTuiLogs int
}

func NewStrixLogHandler(runDir string) (*StrixLogHandler, error) {
	systemPath := filepath.Join(runDir, "strix.log")
	agentPath := filepath.Join(runDir, "agent.log")

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
		systemLog:  slog.NewTextHandler(sf, logOpts),
		agentLog:   slog.NewTextHandler(af, logOpts),
		systemFile: sf,
		agentFile:  af,
		tuiLogs:    make([]string, 0, 500),
		maxTuiLogs: 500,
	}, nil
}

func (h *StrixLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *StrixLogHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

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
		// Filter out verbose system setup / Docker / framework noise from the TUI
		if !strings.Contains(msgLower, "sandbox container") &&
			!strings.Contains(msgLower, "docker image") &&
			!strings.Contains(msgLower, "notes database") &&
			!strings.Contains(msgLower, "tool schemas") &&
			!strings.Contains(msgLower, "gemini client") {

			h.appendToTuiBuffer(r)
		}
	}

	return err
}

func (h *StrixLogHandler) appendToTuiBuffer(r slog.Record) {
	timeStr := r.Time.Format("15:04:05")
	var levelStr string
	switch r.Level {
	case slog.LevelInfo:
		levelStr = "INF"
	case slog.LevelWarn:
		levelStr = "WRN"
	case slog.LevelError:
		levelStr = "ERR"
	default:
		levelStr = r.Level.String()
	}

	msg := fmt.Sprintf("[%s] %s: %s", timeStr, levelStr, r.Message)
	omittedAttrs := 0
	r.Attrs(func(a slog.Attr) bool {
		// Clean up the TUI log message by omitting agent/parent ID noise in the printed line.
		if a.Key == "agent_id" || a.Key == "child_id" || a.Key == "parent_id" {
			return true
		}
		if slices.Contains([]string{"completion", "kwargs", "result"}, a.Key) {
			omittedAttrs++
			return true
		}

		value := sanitizeTuiAttrValue(a.Value.Any())
		if value != "" {
			msg += fmt.Sprintf(" %s=%s", a.Key, value)
		}
		return true
	})
	if omittedAttrs > 0 {
		msg += fmt.Sprintf(" details_omitted=%d", omittedAttrs)
	}

	h.tuiLogs = append(h.tuiLogs, msg)
	if len(h.tuiLogs) > h.maxTuiLogs {
		h.tuiLogs = h.tuiLogs[1:]
	}
}

func sanitizeTuiAttrValue(v interface{}) string {
	s := fmt.Sprint(v)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	const maxLen = 160
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func (h *StrixLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()

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
		systemFile:     h.systemFile,
		agentFile:      h.agentFile,
		isAgentHandler: isAgent,
		tuiLogs:        h.tuiLogs,
		maxTuiLogs:     h.maxTuiLogs,
	}
}

func (h *StrixLogHandler) WithGroup(name string) slog.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()

	return &StrixLogHandler{
		systemLog:      h.systemLog.WithGroup(name),
		agentLog:       h.agentLog.WithGroup(name),
		systemFile:     h.systemFile,
		agentFile:      h.agentFile,
		isAgentHandler: h.isAgentHandler,
		tuiLogs:        h.tuiLogs,
		maxTuiLogs:     h.maxTuiLogs,
	}
}

func (h *StrixLogHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.systemFile != nil {
		h.systemFile.Close()
	}
	if h.agentFile != nil {
		h.agentFile.Close()
	}
}

func (h *StrixLogHandler) GetTuiLogs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	copied := make([]string, len(h.tuiLogs))
	copy(copied, h.tuiLogs)
	return copied
}
