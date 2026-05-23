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

// focusSection constants: 0=tree, 1=tasks, 2=findings, 3=logs
const (
	focusTree     = 0
	focusTasks    = 1
	focusFindings = 2
	focusLogs     = 3
)

type model struct {
	target               string
	scanMode             string
	modelName            string
	logHandler           *StrixLogHandler
	startTime            time.Time
	elapsed              time.Duration
	focusSection         int // focusTree, focusTasks, focusFindings, focusLogs
	width                int
	height               int
	scanRunning          bool
	runFinished          bool
	leftTreeScrollOffset int
	leftTaskScrollOffset int
	findingsScrollOffset int
	logsScrollOffset     int
	tailLogs             bool
	confirmingQuit       bool

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
			if m.confirmingQuit {
				m.logHandler.Close()
				return m, tea.Quit
			} else {
				m.confirmingQuit = true
			}
		case "y":
			if m.confirmingQuit {
				m.logHandler.Close()
				return m, tea.Quit
			}
		case "n", "esc":
			if m.confirmingQuit {
				m.confirmingQuit = false
			}
		case "tab":
			if !m.confirmingQuit {
				if m.focusSection == focusLogs {
					m.focusSection = focusFindings
				} else {
					m.focusSection = focusLogs
				}
			}
		case "up", "k":
			if !m.confirmingQuit {
				switch m.focusSection {
				case focusFindings:
					if m.findingsScrollOffset > 0 {
						m.findingsScrollOffset--
					}
				case focusLogs:
					m.tailLogs = false
					if m.logsScrollOffset > 0 {
						m.logsScrollOffset--
					}
				}
			}
		case "down", "j":
			if !m.confirmingQuit {
				switch m.focusSection {
				case focusFindings:
					m.findingsScrollOffset++
				case focusLogs:
					m.logsScrollOffset++
				}
			}
		case "t":
			if !m.confirmingQuit && m.focusSection == focusLogs {
				m.tailLogs = true
			}
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

	activeColor := lipgloss.Color("#ffffff")
	inactiveColor := lipgloss.Color("#737373")
	dimColor := lipgloss.Color("#404040")

	availableHeight := m.height - 3 // Only footer now
	if availableHeight < 5 {
		availableHeight = 5
	}

	leftTotalWidth := m.width / 3
	if leftTotalWidth*3 < m.width {
		leftTotalWidth++
	}
	if leftTotalWidth < 30 && m.width > 30 {
		leftTotalWidth = 30
	}
	rightTotalWidth := m.width - leftTotalWidth

	leftWidth := leftTotalWidth - 2
	rightWidth := rightTotalWidth - 2

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

	// Split left panel height: 50% tree, 50% tasks (minimum 3 lines each)
	innerHeight := availableHeight - 2 // subtract border
	treeHeight := innerHeight / 2
	if treeHeight < 3 {
		treeHeight = 3
	}
	taskHeight := innerHeight - treeHeight - 1 // -1 for divider line
	if taskHeight < 2 {
		taskHeight = 2
	}

	// Title styles — active section gets bright green background, inactive gets subtle dark background
	activeTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(activeColor)

	dimTitleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#a3a3a3")).
		Background(lipgloss.Color("#262626"))

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
	// Pad to ensure fixed height of tree section
	for len(visibleTreeLines) < treeContentLines {
		visibleTreeLines = append(visibleTreeLines, "")
	}
	// Add padding space to non-empty tree lines
	for i, line := range visibleTreeLines {
		if line != "" {
			visibleTreeLines[i] = " " + line
		}
	}

	// Todo tasks — use cached slice, no lock needed
	var taskLines []string
	if len(m.cachedTodos) == 0 {
		taskLines = []string{" No tasks yet."}
	} else {
		for _, t := range m.cachedTodos {
			statusBox := "[ ]"
			switch t.Status {
			case "done":
				statusBox = "[✓]"
			case "in_progress":
				statusBox = "[/]"
			}
			line := fmt.Sprintf(" %s %s (%s)", statusBox, t.Title, t.Priority)
			rendered := lipgloss.NewStyle().Width(leftWidth - 3).Render(line)
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
	// Pad to ensure fixed height of task section
	for len(visibleTaskLines) < taskContentLines {
		visibleTaskLines = append(visibleTaskLines, "")
	}

	leftDivider := lipgloss.NewStyle().Foreground(dimColor).Render(strings.Repeat("─", leftWidth-2))

	treeTitle := treeTitleStyle.Width(leftWidth - 2).Render(" AGENTS GRAPH")
	taskTitle := taskTitleStyle.Width(leftWidth - 2).Render(" TODO TASKS")

	leftContent := strings.Join([]string{
		treeTitle,
		strings.Join(visibleTreeLines, "\n"),
		leftDivider,
		taskTitle,
		strings.Join(visibleTaskLines, "\n"),
	}, "\n")

	leftBox := leftBoxStyle.Render(leftContent)

	// ── Right panel: findings (top) + log stream (bottom) ──
	rightBorderColor := inactiveColor
	if m.focusSection == focusFindings || m.focusSection == focusLogs {
		rightBorderColor = activeColor
	}

	rightBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorderColor).
		Width(rightWidth).
		Height(availableHeight).
		MaxHeight(availableHeight)

	// Split right panel height: 2/3 findings, 1/3 logs
	findingsHeight := (innerHeight * 2) / 3
	if findingsHeight < 3 {
		findingsHeight = 3
	}
	logsHeight := innerHeight - findingsHeight - 1
	if logsHeight < 2 {
		logsHeight = 2
	}

	findingsTitleStyle := dimTitleStyle
	logsTitleStyle := dimTitleStyle
	switch m.focusSection {
	case focusFindings:
		findingsTitleStyle = activeTitleStyle
	case focusLogs:
		logsTitleStyle = activeTitleStyle
	}

	// Findings content
	var findingsView string
	if len(m.cachedNotes) == 0 {
		findingsView = " No findings/notes recorded yet."
	} else {
		if m.renderer == nil || m.lastRendererWidth != rightWidth {
			renderer, _ := glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(rightWidth-6),
			)
			m.renderer = renderer
			m.lastRendererWidth = rightWidth
		}

		var sb strings.Builder
		for _, n := range m.cachedNotes {
			header := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#d4d4d4")).
				Render(fmt.Sprintf(" [ %s ] (%s)", n.Title, n.Category))
			sb.WriteString(header + "\n")

			if m.renderer != nil {
				content, err := m.renderer.Render(n.Content)
				if err == nil {
					// Add indent to markdown content
					lines := strings.Split(strings.TrimSpace(content), "\n")
					for _, l := range lines {
						sb.WriteString("  " + l + "\n")
					}
				} else {
					sb.WriteString("  " + n.Content + "\n")
				}
			} else {
				sb.WriteString("  " + n.Content + "\n")
			}
			sb.WriteString("\n")
		}
		findingsView = sb.String()
	}

	findingsLines := strings.Split(findingsView, "\n")
	findingsContentLines := findingsHeight - 1
	if findingsContentLines < 1 {
		findingsContentLines = 1
	}

	maxFindingsOffset := len(findingsLines) - findingsContentLines
	if maxFindingsOffset < 0 {
		maxFindingsOffset = 0
	}
	if m.findingsScrollOffset > maxFindingsOffset {
		m.findingsScrollOffset = maxFindingsOffset
	}
	if m.findingsScrollOffset < 0 {
		m.findingsScrollOffset = 0
	}

	visibleFindingsLines := findingsLines[m.findingsScrollOffset:]
	if len(visibleFindingsLines) > findingsContentLines {
		visibleFindingsLines = visibleFindingsLines[:findingsContentLines]
	}
	// Pad to ensure fixed height of findings section
	for len(visibleFindingsLines) < findingsContentLines {
		visibleFindingsLines = append(visibleFindingsLines, "")
	}

	// Logs content
	logs := m.logHandler.GetTuiLogs()
	var wrappedLogs []string
	for _, log := range logs {
		// Detect prefix length (e.g., "[12:34:56] INF: ") to apply hanging indent
		prefixLen := 16
		if idx := strings.Index(log, ": "); idx != -1 {
			prefixLen = idx + 2
		}

		if len(log) <= prefixLen {
			wrappedLogs = append(wrappedLogs, " "+log)
			continue
		}

		prefix := log[:prefixLen]
		message := log[prefixLen:]

		msgWidth := rightWidth - 4 - prefixLen
		if msgWidth < 10 {
			// Fallback for very narrow terminals
			wrapped := lipgloss.NewStyle().Width(rightWidth - 4).Render(log)
			for _, l := range strings.Split(wrapped, "\n") {
				wrappedLogs = append(wrappedLogs, " "+l)
			}
			continue
		}

		wrappedMsg := lipgloss.NewStyle().Width(msgWidth).Render(message)
		msgLines := strings.Split(strings.TrimSpace(wrappedMsg), "\n")

		for i, l := range msgLines {
			if i == 0 {
				wrappedLogs = append(wrappedLogs, " "+prefix+strings.TrimSpace(l))
			} else {
				wrappedLogs = append(wrappedLogs, " "+strings.Repeat(" ", prefixLen)+strings.TrimSpace(l))
			}
		}
	}

	logsContentLines := logsHeight - 1
	if logsContentLines < 1 {
		logsContentLines = 1
	}

	if m.tailLogs {
		if len(wrappedLogs) > logsContentLines {
			m.logsScrollOffset = len(wrappedLogs) - logsContentLines
		} else {
			m.logsScrollOffset = 0
		}
	}

	maxLogsOffset := len(wrappedLogs) - logsContentLines
	if maxLogsOffset < 0 {
		maxLogsOffset = 0
	}
	if m.logsScrollOffset > maxLogsOffset {
		m.logsScrollOffset = maxLogsOffset
	}
	if m.logsScrollOffset < 0 {
		m.logsScrollOffset = 0
	}

	visibleLogsLines := wrappedLogs[m.logsScrollOffset:]
	if len(visibleLogsLines) > logsContentLines {
		visibleLogsLines = visibleLogsLines[:logsContentLines]
	}
	// Pad to ensure fixed height of logs section
	for len(visibleLogsLines) < logsContentLines {
		visibleLogsLines = append(visibleLogsLines, "")
	}

	rightDivider := lipgloss.NewStyle().Foreground(dimColor).Render(strings.Repeat("─", rightWidth-2))

	logsTitleText := " LOG STREAM"
	if m.tailLogs {
		logsTitleText += " (TAILING)"
	}

	findingsTitle := findingsTitleStyle.Width(rightWidth - 2).Render(" FINDINGS")
	logsTitle := logsTitleStyle.Width(rightWidth - 2).Render(logsTitleText)

	rightContent := strings.Join([]string{
		findingsTitle,
		strings.Join(visibleFindingsLines, "\n"),
		rightDivider,
		logsTitle,
		strings.Join(visibleLogsLines, "\n"),
	}, "\n")


	rightBox := rightBoxStyle.Render(rightContent)

	mainLayout := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	// Footer: Consolidated Status Bar
	statusStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#ffffff")).
		Padding(0, 1)

	metadataStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#404040")).
		Padding(0, 1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a3a3a3")).
		Background(lipgloss.Color("#262626")).
		Padding(0, 1)

	statusPart := statusStyle.Render("STRIX")
	metaText := fmt.Sprintf("%s | %s | %s | %s", m.target, m.scanMode, m.modelName, formatDuration(m.elapsed))
	metaPart := metadataStyle.Render(metaText)

	var helpText string
	if m.confirmingQuit {
		helpText = "QUIT? (Y: YES | N: NO)"
	} else {
		helpText = "TAB: FOCUS | ARROWS: SCROLL | T: TAIL | Q: QUIT"
	}
	helpPart := helpStyle.Render(helpText)

	// Calculate space for the gap
	fixedWidth := lipgloss.Width(statusPart) + lipgloss.Width(metaPart) + lipgloss.Width(helpPart)
	gapWidth := m.width - fixedWidth
	if gapWidth < 0 {
		gapWidth = 0
	}
	gapPart := helpStyle.Width(gapWidth).Render("")

	footer := lipgloss.JoinHorizontal(lipgloss.Top, statusPart, metaPart, gapPart, helpPart)

	return lipgloss.JoinVertical(lipgloss.Left, mainLayout, footer)
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

	statusColor := "#737373"
	switch node.Status {
	case "running":
		statusColor = "#ffffff"
	case "waiting":
		statusColor = "#a3a3a3"
	case "finished":
		statusColor = "#d4d4d4"
	case "failed":
		statusColor = "#ffffff" // Bold white for failure too, maybe with bold
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
func RunTUI(ctx context.Context, target, scanMode, modelName, runDir string, handler *StrixLogHandler, scanFunc func() error) error {
	m := model{
		target:       target,
		scanMode:     scanMode,
		modelName:    modelName,
		logHandler:   handler,
		startTime:    time.Now(),
		scanRunning:  true,
		focusSection: focusLogs,
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
