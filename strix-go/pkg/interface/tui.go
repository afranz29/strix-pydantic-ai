package _interface

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	"github.com/usestrix/strix-go/pkg/tools/notes"
	"github.com/usestrix/strix-go/pkg/tools/todo"
)

// tickMsg is used for periodic TUI clock updates
type tickMsg time.Time

// snapshotMsg carries data fetched from background goroutines
type snapshotMsg struct {
	treeStr     string
	todos       []*todo.Todo
	notes       []*notes.Note
	runFinished bool
}

// focusSection constants: 0=agent tree, 1=todo tasks, 2=right panel
const (
	focusTree  = 0
	focusTasks = 1
	focusRight = 2
)

type model struct {
	target               string
	scanMode             string
	logHandler           *StrixLogHandler
	startTime            time.Time
	elapsed              time.Duration
	activeTab            int // 0: Logs, 1: Findings
	focusSection         int // focusTree, focusTasks, focusRight
	width                int
	height               int
	scanRunning          bool
	runFinished          bool
	leftTreeScrollOffset int
	leftTaskScrollOffset int
	rightScrollOffsets   [2]int
	tailLogs             bool

	// Cached data updated by background snapshots — View() reads only these
	cachedTreeStr string
	cachedTodos   []*todo.Todo
	cachedNotes   []*notes.Note

	// Cached markdown renderer to avoid expensive recreation on every frame
	renderer           *glamour.TermRenderer
	lastRendererWidth  int
}

// fetchSnapshotCmd runs data fetching in a background goroutine so the event
// loop (and therefore key handling) is never blocked by lock contention.
func fetchSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		agents_graph.GraphLock.RLock()
		rootID := agents_graph.RootAgentID
		treeStr := ""
		if rootID != "" {
			treeStr = buildTreeStringLocked(rootID, "", true, 0)
		} else {
			treeStr = "Initializing Agent Orchestration Graph..."
		}
		rootNode, exists := agents_graph.AgentNodes[agents_graph.RootAgentID]
		finished := exists && (rootNode.Status == "finished" || rootNode.Status == "failed")
		agents_graph.GraphLock.RUnlock()

		return snapshotMsg{
			treeStr:     treeStr,
			todos:       todo.GetTodoList(),
			notes:       notes.GetNotesList(),
			runFinished: finished,
		}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }),
		fetchSnapshotCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.logHandler.Close()
			return m, tea.Quit
		case "tab":
			m.focusSection = (m.focusSection + 1) % 3
		case "right":
			if m.focusSection == focusRight {
				m.activeTab = (m.activeTab + 1) % 2
			} else {
				m.focusSection = focusRight
			}
		case "left":
			if m.focusSection == focusRight {
				m.activeTab = (m.activeTab + 1) % 2
			} else {
				m.focusSection = focusTree
			}
		case "up", "k":
			switch m.focusSection {
			case focusTree:
				if m.leftTreeScrollOffset > 0 {
					m.leftTreeScrollOffset--
				}
			case focusTasks:
				if m.leftTaskScrollOffset > 0 {
					m.leftTaskScrollOffset--
				}
			case focusRight:
				if m.activeTab == 0 {
					m.tailLogs = false
				}
				if m.rightScrollOffsets[m.activeTab] > 0 {
					m.rightScrollOffsets[m.activeTab]--
				}
			}
		case "down", "j":
			switch m.focusSection {
			case focusTree:
				m.leftTreeScrollOffset++
			case focusTasks:
				m.leftTaskScrollOffset++
			case focusRight:
				m.rightScrollOffsets[m.activeTab]++
			}
		case "t":
			if m.focusSection == focusRight && m.activeTab == 0 {
				m.tailLogs = true
			}
		case "1":
			m.activeTab = 0
			m.focusSection = focusRight
		case "2":
			m.activeTab = 1
			m.focusSection = focusRight
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if m.scanRunning && !m.runFinished {
			m.elapsed = time.Since(m.startTime)
		}
		return m, tea.Batch(
			tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }),
			fetchSnapshotCmd(),
		)

	case snapshotMsg:
		m.cachedTreeStr = msg.treeStr
		m.cachedTodos = msg.todos
		m.cachedNotes = msg.notes
		if msg.runFinished {
			m.runFinished = true
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width < 20 || m.height < 10 {
		return "Terminal too small"
	}

	activeColor := lipgloss.Color("#22c55e")
	inactiveColor := lipgloss.Color("#15803d")
	dimColor := lipgloss.Color("#404040")

	availableHeight := m.height - 5
	if availableHeight < 5 {
		availableHeight = 5
	}

	leftTotalWidth := m.width / 3
	rightTotalWidth := m.width - leftTotalWidth

	leftWidth := leftTotalWidth - 2
	rightWidth := rightTotalWidth - 2

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

	// Tabs (right panel)
	tabStyle := lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("#262626")).Foreground(lipgloss.Color("#a3a3a3"))
	activeTabStyle := lipgloss.NewStyle().Padding(0, 2).Bold(true).Background(lipgloss.Color("#22c55e")).Foreground(lipgloss.Color("#000000"))

	tabs := []string{"[1] Log Stream", "[2] Findings"}
	var tabViews []string
	for i, t := range tabs {
		if i == m.activeTab {
			tabViews = append(tabViews, activeTabStyle.Render(t))
		} else {
			tabViews = append(tabViews, tabStyle.Render(t))
		}
	}
	tabRow := lipgloss.NewStyle().Width(m.width).MaxHeight(1).Render(lipgloss.JoinHorizontal(lipgloss.Top, tabViews...))

	// ── Left panel: agent tree (top) + todo tasks (bottom) ──
	leftBorderColor := inactiveColor
	if m.focusSection == focusTree || m.focusSection == focusTasks {
		leftBorderColor = activeColor
	}

	leftBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorderColor).
		Width(leftWidth).
		Height(availableHeight).
		MaxHeight(availableHeight)

	// Split left panel height: 60% tree, 40% tasks (minimum 3 lines each)
	innerHeight := availableHeight - 2 // subtract border
	treeHeight := (innerHeight * 6) / 10
	if treeHeight < 3 {
		treeHeight = 3
	}
	taskHeight := innerHeight - treeHeight - 1 // -1 for divider line
	if taskHeight < 2 {
		taskHeight = 2
	}

	// Title styles — active section gets bright green, inactive gets dim
	activeTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(activeColor)
	dimTitleStyle := lipgloss.NewStyle().Bold(false).Foreground(lipgloss.Color(dimColor))

	treeTitleStyle := dimTitleStyle
	taskTitleStyle := dimTitleStyle
	switch m.focusSection {
	case focusTree:
		treeTitleStyle = activeTitleStyle
	case focusTasks:
		taskTitleStyle = activeTitleStyle
	}

	// Agent tree — use cached string, no lock needed
	treeLines := strings.Split(strings.TrimSpace(m.cachedTreeStr), "\n")
	treeContentLines := treeHeight - 1
	if treeContentLines < 1 {
		treeContentLines = 1
	}

	treeOffset := m.leftTreeScrollOffset
	if treeOffset > len(treeLines)-1 {
		treeOffset = len(treeLines) - 1
	}
	if treeOffset < 0 {
		treeOffset = 0
	}
	visibleTreeLines := treeLines[treeOffset:]
	if len(visibleTreeLines) > treeContentLines {
		visibleTreeLines = visibleTreeLines[:treeContentLines]
	}

	// Todo tasks — use cached slice, no lock needed
	var taskLines []string
	if len(m.cachedTodos) == 0 {
		taskLines = []string{"No tasks yet."}
	} else {
		for _, t := range m.cachedTodos {
			statusBox := "[ ]"
			switch t.Status {
			case "done":
				statusBox = "[✓]"
			case "in_progress":
				statusBox = "[/]"
			}
			line := fmt.Sprintf("%s %s (%s)", statusBox, t.Title, t.Priority)
			rendered := lipgloss.NewStyle().Width(leftWidth - 2).Render(line)
			taskLines = append(taskLines, strings.Split(rendered, "\n")...)
		}
	}

	taskContentLines := taskHeight - 1
	if taskContentLines < 1 {
		taskContentLines = 1
	}

	taskOffset := m.leftTaskScrollOffset
	if taskOffset > len(taskLines)-1 {
		taskOffset = len(taskLines) - 1
	}
	if taskOffset < 0 {
		taskOffset = 0
	}
	visibleTaskLines := taskLines[taskOffset:]
	if len(visibleTaskLines) > taskContentLines {
		visibleTaskLines = visibleTaskLines[:taskContentLines]
	}

	divider := lipgloss.NewStyle().Foreground(inactiveColor).Render(strings.Repeat("─", leftWidth))

	leftContent := strings.Join([]string{
		treeTitleStyle.Render("── Agents Graph ──"),
		strings.Join(visibleTreeLines, "\n"),
		divider,
		taskTitleStyle.Render("── Todo Tasks ──"),
		strings.Join(visibleTaskLines, "\n"),
	}, "\n")

	leftBox := leftBoxStyle.Render(leftContent)

	// ── Right panel ──
	rightBorderColor := inactiveColor
	if m.focusSection == focusRight {
		rightBorderColor = activeColor
	}

	rightBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorderColor).
		Width(rightWidth).
		Height(availableHeight).
		MaxHeight(availableHeight)

	rightContentHeight := availableHeight - 4
	if rightContentHeight < 1 {
		rightContentHeight = 1
	}

	var rightView string

	switch m.activeTab {
	case 0: // Log Stream
		logs := m.logHandler.GetTuiLogs()

		var wrappedLines []string
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

		maxOffset := len(wrappedLines) - rightContentHeight
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.rightScrollOffsets[0] > maxOffset {
			m.rightScrollOffsets[0] = maxOffset
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

	case 1: // Findings — use cached notes, no lock needed
		if len(m.cachedNotes) == 0 {
			rightView = "No findings/notes recorded yet."
		} else {
			// Create or update renderer only when width changes to avoid expensive recreation
			if m.renderer == nil || m.lastRendererWidth != rightWidth {
				renderer, _ := glamour.NewTermRenderer(
					glamour.WithAutoStyle(),
					glamour.WithWordWrap(rightWidth-4),
				)
				m.renderer = renderer
				m.lastRendererWidth = rightWidth
			}

			var sb strings.Builder
			for _, n := range m.cachedNotes {
				header := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#22c55e")).
					Render(fmt.Sprintf("[ %s ] (%s) - %s", n.Title, n.Category, n.UpdatedAt))
				sb.WriteString(header + "\n")

				if m.renderer != nil {
					content, err := m.renderer.Render(n.Content)
					if err == nil {
						sb.WriteString(content)
					} else {
						sb.WriteString(n.Content + "\n")
					}
				} else {
					sb.WriteString(n.Content + "\n")
				}
				sb.WriteString("\n")
			}

			wrappedLines := strings.Split(sb.String(), "\n")

			maxOffset := len(wrappedLines) - rightContentHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			if m.rightScrollOffsets[1] > maxOffset {
				m.rightScrollOffsets[1] = maxOffset
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
	}

	rightTitleText := fmt.Sprintf("── %s ──", tabs[m.activeTab])
	if m.activeTab == 0 && m.tailLogs {
		rightTitleText += " (Tailing)"
	}
	rightContent := fmt.Sprintf("%s\n%s", activeTitleStyle.Render(rightTitleText), rightView)
	rightBox := rightBoxStyle.Render(rightContent)

	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	// Footer
	footerSeparator := lipgloss.NewStyle().
		Foreground(inactiveColor).
		Render(strings.Repeat("─", m.width))

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a3a3a3")).
		Width(m.width).
		MaxHeight(1)
	footerText := " Tab: Cycle focus | Arrows/JK: Scroll | 1,2: Tabs | T: Tail Logs | Q: Quit"
	footer := footerStyle.Render(truncateString(footerText, m.width))

	return lipgloss.JoinVertical(lipgloss.Left, header, tabRow, mainLayout, footerSeparator, footer)
}

func formatDuration(d time.Duration) string {
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// buildTreeString acquires the read lock and builds the tree string.
// Must only be called from background goroutines (not View/Update).
func buildTreeString(id string, indent string, isLast bool) string {
	agents_graph.GraphLock.RLock()
	defer agents_graph.GraphLock.RUnlock()
	return buildTreeStringLocked(id, indent, isLast, 0)
}

// buildTreeStringLocked builds the tree string; caller must hold GraphLock.RLock.
func buildTreeStringLocked(id string, indent string, isLast bool, depth int) string {
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
		nodeLine += buildTreeStringLocked(childID, nextIndent, lastChild, depth+1)
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
		target:       target,
		scanMode:     scanMode,
		logHandler:   handler,
		startTime:    time.Now(),
		scanRunning:  true,
		focusSection: focusRight,
		tailLogs:     true,
	}

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
