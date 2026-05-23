package _interface

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	"github.com/usestrix/strix-go/pkg/tools/notes"
	"github.com/usestrix/strix-go/pkg/tools/todo"
)

// tickMsg is used for periodic TUI updates
type tickMsg time.Time

type model struct {
	target             string
	scanMode           string
	logHandler         *StrixLogHandler
	startTime          time.Time
	elapsed            time.Duration
	activeTab          int // 0: Logs, 1: Findings, 2: Todo Checklist
	focusLeft          bool
	width              int
	height             int
	scanRunning        bool
	runFinished        bool
	leftScrollOffset   int
	rightScrollOffsets [3]int
	tailLogs           bool
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
		case "up", "k":
			if m.focusLeft {
				if m.leftScrollOffset > 0 {
					m.leftScrollOffset--
				}
			} else {
				if m.activeTab == 0 {
					m.tailLogs = false
				}
				if m.rightScrollOffsets[m.activeTab] > 0 {
					m.rightScrollOffsets[m.activeTab]--
				}
			}
		case "down", "j":
			if m.focusLeft {
				m.leftScrollOffset++
			} else {
				m.rightScrollOffsets[m.activeTab]++
			}
		case "t":
			if m.activeTab == 0 {
				m.tailLogs = true
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

	availableHeight := m.height - 7
	if availableHeight < 1 {
		availableHeight = 1
	}

	leftTotalWidth := m.width / 3
	rightTotalWidth := m.width - leftTotalWidth

	leftWidth := leftTotalWidth - 2
	rightWidth := rightTotalWidth - 2

	leftBorderColor := inactiveBorderColor
	if m.focusLeft {
		leftBorderColor = activeBorderColor
	}

	leftBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorderColor).
		Width(leftWidth).
		Height(availableHeight).
		MaxHeight(availableHeight)

	rightBorderColor := inactiveBorderColor
	if !m.focusLeft {
		rightBorderColor = activeBorderColor
	}

	rightBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorderColor).
		Width(rightWidth).
		Height(availableHeight).
		MaxHeight(availableHeight)

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#15803d")).
		Padding(0, 2).
		Width(m.width).
		MaxHeight(1)

	headerText := fmt.Sprintf("STRIX ORCHESTRATOR | Target: %s | Mode: %s | Time: %s",
		m.target, m.scanMode, formatDuration(m.elapsed))
	header := headerStyle.Render(truncateString(headerText, m.width-4))

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
	tabRow := lipgloss.NewStyle().Width(m.width).MaxHeight(1).Render(lipgloss.JoinHorizontal(lipgloss.Top, tabViews...))

	// Left panel: Agents Hierarchy tree
	var treeView string
	agents_graph.GraphLock.RLock()
	rootID := agents_graph.RootAgentID
	agents_graph.GraphLock.RUnlock()

	if rootID != "" {
		treeView = buildTreeString(rootID, "", true)
	} else {
		treeView = "Initializing Agent Orchestration Graph..."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#22c55e"))

	// Truncate/slice tree view to fit left box with scrolling
	treeLines := strings.Split(treeView, "\n")
	treeLimit := availableHeight - 4
	if treeLimit < 1 {
		treeLimit = 1
	}

	if m.leftScrollOffset > len(treeLines)-1 {
		m.leftScrollOffset = len(treeLines) - 1
	}
	if m.leftScrollOffset < 0 {
		m.leftScrollOffset = 0
	}

	visibleTreeLines := treeLines
	if len(treeLines) > m.leftScrollOffset {
		visibleTreeLines = treeLines[m.leftScrollOffset:]
	}
	if len(visibleTreeLines) > treeLimit {
		visibleTreeLines = visibleTreeLines[:treeLimit]
	}
	treeView = strings.Join(visibleTreeLines, "\n")

	leftContent := fmt.Sprintf("%s\n%s", titleStyle.Render("── Agents Graph ──"), treeView)
	leftBox := leftBoxStyle.Render(leftContent)

	// Right panel based on activeTab
	var rightView string
	rightContentHeight := availableHeight - 4
	if rightContentHeight < 1 {
		rightContentHeight = 1
	}

	switch m.activeTab {
	case 0:
		logs := m.logHandler.GetTuiLogs()

		var wrappedLines []string
		// Optimization: only wrap logs that could possibly be visible
		// This is still a bit naive but better than wrapping 500 lines every 150ms
		// If tailing, we only need the last rightContentHeight lines.
		// If scrolling, we need lines around the offset.

		for _, log := range logs {
			wrapped := lipgloss.NewStyle().Width(rightWidth).Render(log)
			wrappedLines = append(wrappedLines, strings.Split(wrapped, "\n")...)
		}

		if m.tailLogs {
			if len(wrappedLines) > rightContentHeight {
				m.rightScrollOffsets[0] = len(wrappedLines) - rightContentHeight
			} else {
				m.rightScrollOffsets[0] = 0
			}
		}

		if m.rightScrollOffsets[0] > len(wrappedLines)-1 && len(wrappedLines) > 0 {
			m.rightScrollOffsets[0] = len(wrappedLines) - 1
		}
		if m.rightScrollOffsets[0] < 0 {
			m.rightScrollOffsets[0] = 0
		}

		visibleLogs := wrappedLines
		if len(wrappedLines) > m.rightScrollOffsets[0] {
			visibleLogs = wrappedLines[m.rightScrollOffsets[0]:]
		}
		if len(visibleLogs) > rightContentHeight {
			visibleLogs = visibleLogs[:rightContentHeight]
		}
		rightView = strings.Join(visibleLogs, "\n")

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

			wrapped := lipgloss.NewStyle().Width(rightWidth).Render(sb.String())
			wrappedLines := strings.Split(wrapped, "\n")

			if m.rightScrollOffsets[1] > len(wrappedLines)-1 && len(wrappedLines) > 0 {
				m.rightScrollOffsets[1] = len(wrappedLines) - 1
			}
			if m.rightScrollOffsets[1] < 0 {
				m.rightScrollOffsets[1] = 0
			}

			visibleLines := wrappedLines
			if len(wrappedLines) > m.rightScrollOffsets[1] {
				visibleLines = wrappedLines[m.rightScrollOffsets[1]:]
			}
			if len(visibleLines) > rightContentHeight {
				visibleLines = visibleLines[:rightContentHeight]
			}
			rightView = strings.Join(visibleLines, "\n")
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

			wrapped := lipgloss.NewStyle().Width(rightWidth).Render(sb.String())
			wrappedLines := strings.Split(wrapped, "\n")

			if m.rightScrollOffsets[2] > len(wrappedLines)-1 && len(wrappedLines) > 0 {
				m.rightScrollOffsets[2] = len(wrappedLines) - 1
			}
			if m.rightScrollOffsets[2] < 0 {
				m.rightScrollOffsets[2] = 0
			}

			visibleLines := wrappedLines
			if len(wrappedLines) > m.rightScrollOffsets[2] {
				visibleLines = wrappedLines[m.rightScrollOffsets[2]:]
			}
			if len(visibleLines) > rightContentHeight {
				visibleLines = visibleLines[:rightContentHeight]
			}
			rightView = strings.Join(visibleLines, "\n")
		}
	}

	rightTitleText := fmt.Sprintf("── %s ──", tabs[m.activeTab])
	if m.activeTab == 0 && m.tailLogs {
		rightTitleText += " (Tailing)"
	}
	rightContent := fmt.Sprintf("%s\n%s", titleStyle.Render(rightTitleText), rightView)
	rightBox := rightBoxStyle.Render(rightContent)

	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373")).
		Width(m.width).
		MaxHeight(1)
	footerText := " Tab: Toggle focus | Arrows/JK: Scroll/Tabs | 1,2,3: Tabs | T: Tail Logs | Q: Quit"
	footer := footerStyle.Render(truncateString(footerText, m.width))

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
	// Root call takes the lock once and passes it down via a helper or we just rely on recursion
	// but the current implementation takes/releases lock for every node.
	// Let's wrap it.
	agents_graph.GraphLock.RLock()
	defer agents_graph.GraphLock.RUnlock()
	return buildTreeStringRecursive(id, indent, isLast, 0)
}

func buildTreeStringRecursive(id string, indent string, isLast bool, depth int) string {
	node, exists := agents_graph.AgentNodes[id]
	if !exists {
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
	if depth > 0 {
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

	var nextIndent string
	if depth == 0 {
		nextIndent = ""
	} else {
		if isLast {
			nextIndent = indent + "    "
		} else {
			nextIndent = indent + "│   "
		}
	}

	for i, childID := range children {
		lastChild := i == len(children)-1
		nodeLine += buildTreeStringRecursive(childID, nextIndent, lastChild, depth+1)
	}
	return nodeLine
}

func truncateString(s string, maxLen int) string {
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return lipgloss.NewStyle().MaxWidth(maxLen).Render(s)
	}
	return lipgloss.NewStyle().MaxWidth(maxLen).Render(s)
}

// RunTUI runs the Strix scan within a background goroutine and monitors it using Bubble Tea.
func RunTUI(ctx context.Context, target, scanMode, runDir string, handler *StrixLogHandler, scanFunc func() error) error {
	m := model{
		target:      target,
		scanMode:    scanMode,
		logHandler:  handler,
		startTime:   time.Now(),
		scanRunning: true,
		focusLeft:   true,
		tailLogs:    true,
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
