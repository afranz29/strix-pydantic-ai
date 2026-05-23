package _interface

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	"github.com/usestrix/strix-go/pkg/tools/notes"
	"github.com/usestrix/strix-go/pkg/tools/todo"
)

// TUIHandler is a thread-safe custom slog handler that writes to a run log file
// and maintains a memory buffer of logs for real-time visualization.
type TUIHandler struct {
	mu      sync.Mutex
	logs    []string
	logFile *os.File
	parent  slog.Handler
}

func NewTUIHandler(logPath string) (*TUIHandler, error) {
	var f *os.File
	var parent slog.Handler
	if logPath != "" {
		var err error
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		parent = slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	return &TUIHandler{
		logs:    make([]string, 0, 500),
		logFile: f,
		parent:  parent,
	}, nil
}

func (h *TUIHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *TUIHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.parent != nil {
		_ = h.parent.Handle(ctx, r)
	}

	timeStr := r.Time.Format("15:04:05")
	var levelStr string
	switch r.Level {
	case slog.LevelDebug:
		levelStr = "DBG"
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
	r.Attrs(func(a slog.Attr) bool {
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value.Any())
		return true
	})

	h.logs = append(h.logs, msg)
	if len(h.logs) > 500 {
		h.logs = h.logs[1:]
	}
	return nil
}

func (h *TUIHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *TUIHandler) WithGroup(name string) slog.Handler       { return h }

func (h *TUIHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.logFile != nil {
		_ = h.logFile.Close()
	}
}

func (h *TUIHandler) GetLogs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	copied := make([]string, len(h.logs))
	copy(copied, h.logs)
	return copied
}

type tickMsg time.Time

type model struct {
	target      string
	scanMode    string
	logHandler  *TUIHandler
	startTime   time.Time
	elapsed     time.Duration
	activeTab   int // 0: Logs, 1: Findings, 2: Todo Checklist
	focusLeft   bool
	width       int
	height      int
	scanRunning bool
	runFinished bool
}

func (m model) Init() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.logHandler.Close()
			return m, tea.Quit
		case "tab":
			m.focusLeft = !m.focusLeft
		case "right":
			if m.focusLeft {
				m.focusLeft = false
			} else {
				m.activeTab = (m.activeTab + 1) % 3
			}
		case "left":
			if !m.focusLeft {
				m.focusLeft = true
			} else {
				m.activeTab = (m.activeTab + 2) % 3
			}
		case "1":
			m.activeTab = 0
		case "2":
			m.activeTab = 1
		case "3":
			m.activeTab = 2
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if m.scanRunning && !m.runFinished {
			m.elapsed = time.Since(m.startTime)
		}

		agents_graph.GraphLock.RLock()
		rootNode, exists := agents_graph.AgentNodes[agents_graph.RootAgentID]
		agents_graph.GraphLock.RUnlock()

		if exists && (rootNode.Status == "finished" || rootNode.Status == "failed") {
			m.runFinished = true
		}

		return m, tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

func (m model) View() string {
	if m.width < 20 || m.height < 10 {
		return "Terminal too small"
	}

	activeBorderColor := lipgloss.Color("#22c55e")
	inactiveBorderColor := lipgloss.Color("#15803d")

	leftBorderColor := inactiveBorderColor
	if m.focusLeft {
		leftBorderColor = activeBorderColor
	}

	leftBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorderColor).
		Width(m.width/3 - 2).
		Height(m.height - 7)

	rightBorderColor := inactiveBorderColor
	if !m.focusLeft {
		rightBorderColor = activeBorderColor
	}

	rightBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorderColor).
		Width(2*m.width/3 - 2).
		Height(m.height - 7)

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#15803d")).
		Padding(0, 2).
		Width(m.width)

	headerText := fmt.Sprintf("STRIX ORCHESTRATOR | Target: %s | Mode: %s | Time: %s",
		m.target, m.scanMode, formatDuration(m.elapsed))
	header := headerStyle.Render(headerText)

	// Tabs
	tabStyle := lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("#262626")).Foreground(lipgloss.Color("#a3a3a3"))
	activeTabStyle := lipgloss.NewStyle().Padding(0, 2).Bold(true).Background(lipgloss.Color("#22c55e")).Foreground(lipgloss.Color("#000000"))

	tabs := []string{"[1] Log Stream", "[2] Notes / Findings", "[3] Todo Tasks"}
	var tabViews []string
	for i, t := range tabs {
		if i == m.activeTab {
			tabViews = append(tabViews, activeTabStyle.Render(t))
		} else {
			tabViews = append(tabViews, tabStyle.Render(t))
		}
	}
	tabRow := lipgloss.JoinHorizontal(lipgloss.Top, tabViews...)

	// Left panel: Agents Hierarchy tree
	agents_graph.GraphLock.RLock()
	rootID := agents_graph.RootAgentID
	agents_graph.GraphLock.RUnlock()

	var treeView string
	if rootID != "" {
		treeView = buildTreeString(rootID, "", true)
	} else {
		treeView = "Initializing Agent Orchestration Graph..."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#22c55e"))
	leftContent := fmt.Sprintf("%s\n%s", titleStyle.Render("── Agents Graph ──"), treeView)
	leftBox := leftBoxStyle.Render(leftContent)

	// Right panel based on activeTab
	var rightView string
	switch m.activeTab {
	case 0:
		logs := m.logHandler.GetLogs()
		maxLines := m.height - 10
		if maxLines <= 0 {
			maxLines = 1
		}
		if len(logs) > maxLines {
			logs = logs[len(logs)-maxLines:]
		}
		rightView = strings.Join(logs, "\n")
	case 1:
		noteList := notes.GetNotesList()
		if len(noteList) == 0 {
			rightView = "No findings/notes recorded yet."
		} else {
			var sb strings.Builder
			for _, n := range noteList {
				sb.WriteString(fmt.Sprintf("[ %s ] (%s) - %s\n", n.Title, n.Category, n.UpdatedAt))
				contentLines := strings.Split(n.Content, "\n")
				for i, line := range contentLines {
					if i > 3 {
						sb.WriteString("  ...\n")
						break
					}
					sb.WriteString(fmt.Sprintf("  %s\n", line))
				}
				sb.WriteString("\n")
			}
			rightView = sb.String()
		}
	case 2:
		todoList := todo.GetTodoList()
		if len(todoList) == 0 {
			rightView = "No tasks listed in todo queue."
		} else {
			var sb strings.Builder
			for _, t := range todoList {
				statusBox := "[ ]"
				switch t.Status {
				case "done":
					statusBox = "[x]"
				case "in_progress":
					statusBox = "[/]"
				}
				sb.WriteString(fmt.Sprintf("%s %s (%s)\n", statusBox, t.Title, t.Priority))
				if t.Description != "" {
					sb.WriteString(fmt.Sprintf("    %s\n", t.Description))
				}
			}
			rightView = sb.String()
		}
	}

	rightTitleText := fmt.Sprintf("── %s ──", tabs[m.activeTab])
	rightContent := fmt.Sprintf("%s\n%s", titleStyle.Render(rightTitleText), rightView)
	rightBox := rightBoxStyle.Render(rightContent)

	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373")).
		Width(m.width)
	footerText := " Tab: Toggle focus | Arrow Keys: Switch panel/tabs | 1, 2, 3: Tabs | Q: Quit scan"
	footer := footerStyle.Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left, header, tabRow, mainLayout, footer)
}

func formatDuration(d time.Duration) string {
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func buildTreeString(id string, indent string, isLast bool) string {
	agents_graph.GraphLock.RLock()
	node, exists := agents_graph.AgentNodes[id]
	if !exists {
		agents_graph.GraphLock.RUnlock()
		return ""
	}

	statusColor := "#a3a3a3"
	switch node.Status {
	case "running":
		statusColor = "#22c55e"
	case "waiting":
		statusColor = "#eab308"
	case "finished":
		statusColor = "#3b82f6"
	case "failed":
		statusColor = "#ef4444"
	}

	var prefix string
	if indent != "" {
		if isLast {
			prefix = "└── "
		} else {
			prefix = "├── "
		}
	}

	nodeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
	nodeLine := fmt.Sprintf("%s%s%s (%s)\n", indent, prefix, node.Name, nodeStyle.Render(node.Status))

	var children []string
	for _, edge := range agents_graph.GraphEdges {
		if edge.From == id && edge.Type == "delegation" {
			children = append(children, edge.To)
		}
	}
	agents_graph.GraphLock.RUnlock()

	var nextIndent string
	if indent != "" {
		if isLast {
			nextIndent = indent + "    "
		} else {
			nextIndent = indent + "│   "
		}
	}

	for i, childID := range children {
		lastChild := i == len(children)-1
		nodeLine += buildTreeString(childID, nextIndent, lastChild)
	}
	return nodeLine
}

// RunTUI runs the Strix scan within a background goroutine and monitors it using Bubble Tea.
func RunTUI(ctx context.Context, target, scanMode, runDir string, scanFunc func() error) error {
	logPath := filepath.Join(runDir, "strix.log")
	handler, err := NewTUIHandler(logPath)
	if err != nil {
		return err
	}
	defer handler.Close()

	// Direct global slog through our custom handler
	slog.SetDefault(slog.New(handler))

	m := model{
		target:      target,
		scanMode:    scanMode,
		logHandler:  handler,
		startTime:   time.Now(),
		scanRunning: true,
		focusLeft:   true,
	}

	// Trigger scanning function asynchronously
	go func() {
		err := scanFunc()
		if err != nil {
			slog.Error("Scan execution failed", slog.Any("error", err))
		} else {
			slog.Info("Scan completed successfully")
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
