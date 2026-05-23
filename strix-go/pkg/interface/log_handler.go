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

	h.shared.mu.Lock()
	defer h.shared.mu.Unlock()
	h.shared.tuiLogs = append(h.shared.tuiLogs, msg)
	if len(h.shared.tuiLogs) > h.shared.maxTuiLogs {
		h.shared.tuiLogs = h.shared.tuiLogs[1:]
	}

	if h.shared.writeStdout {
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
	const maxLen = 160
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
