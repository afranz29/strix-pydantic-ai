package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/strix/go-client/internal/api"
	"go.uber.org/zap"
)

type AgentStatus struct {
	Status     string
	Iterations int
}

type Vulnerability struct {
	Title       string
	Severity    string
	Description string
	CVEID       string
	Parameter   string
	POC         string
}

type LogEntry struct {
	Level   string
	Message string
}

type Model struct {
	// Config
	target      string
	scanMode    string
	backendURL  string
	model       string
	mockTools   bool
	instruction string
	skills      string
	timeout     float64
	verbose     bool

	// State
	scanID      string
	scanStatus  string
	elapsed     int
	currentAgent string

	// Data
	agents          map[string]AgentStatus
	vulnerabilities []Vulnerability
	activityLog     []LogEntry

	// UI
	width  int
	height int

	// Viewports
	eventsViewport viewport.Model
	vulnsViewport  viewport.Model
	ready          bool
	focusedPanel   string // "events" or "vulns"

	// Backend
	apiClient *api.Client

	// Logging
	logger *zap.Logger
}

func NewModel(
	target, scanMode, backendURL, model, instruction, skills string,
	mockTools, verbose bool,
	timeout float64,
	logger *zap.Logger,
) *Model {
	return &Model{
		target:          target,
		scanMode:        scanMode,
		backendURL:      backendURL,
		model:           model,
		mockTools:       mockTools,
		instruction:     instruction,
		skills:          skills,
		timeout:         timeout,
		verbose:         verbose,
		scanStatus:      "initializing",
		agents:          make(map[string]AgentStatus),
		vulnerabilities: []Vulnerability{},
		activityLog:     []LogEntry{},
		apiClient:       api.NewClient(backendURL),
		logger:          logger,
		focusedPanel:    "events",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.startScan(),
		tickEvery(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewports()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			// Switch focus between panels
			if m.focusedPanel == "events" {
				m.focusedPanel = "vulns"
			} else {
				m.focusedPanel = "events"
			}
			return m, nil
		case "up", "k":
			if m.focusedPanel == "events" {
				m.eventsViewport.LineUp(1)
			} else {
				m.vulnsViewport.LineUp(1)
			}
			return m, nil
		case "down", "j":
			if m.focusedPanel == "events" {
				m.eventsViewport.LineDown(1)
			} else {
				m.vulnsViewport.LineDown(1)
			}
			return m, nil
		case "pgup":
			if m.focusedPanel == "events" {
				m.eventsViewport.HalfViewUp()
			} else {
				m.vulnsViewport.HalfViewUp()
			}
			return m, nil
		case "pgdown":
			if m.focusedPanel == "events" {
				m.eventsViewport.HalfViewDown()
			} else {
				m.vulnsViewport.HalfViewDown()
			}
			return m, nil
		case "home":
			if m.focusedPanel == "events" {
				m.eventsViewport.GotoTop()
			} else {
				m.vulnsViewport.GotoTop()
			}
			return m, nil
		case "end":
			if m.focusedPanel == "events" {
				m.eventsViewport.GotoBottom()
			} else {
				m.vulnsViewport.GotoBottom()
			}
			return m, nil
		}

	case ScanStartedMsg:
		m.scanID = msg.ScanID
		m.scanStatus = "running"
		m.elapsed = 0
		m.logger.Info("Scan started", zap.String("scan_id", m.scanID))
		return m, m.streamEvents()

	case EventReceivedMsg:
		m = m.handleEvent(msg.Event)
		if m.ready {
			m.updateViewportContent()
		}
		return m, readNextEvent(msg.Stream)

	case TickMsg:
		m.elapsed++
		return m, tickEvery()

	case ErrorMsg:
		m.scanStatus = "error: " + msg.Err.Error()
		m.logger.Error("Error", zap.Error(msg.Err))
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	return renderLayout(m)
}

func (m *Model) updateViewports() {
	topHeight := (m.height - 4) / 2
	bottomHeight := m.height - topHeight - 4
	panelWidth := (m.width - 6) / 3

	if m.height > 4 {
		if !m.ready {
			m.eventsViewport = viewport.New(m.width-8, bottomHeight-4)
			m.vulnsViewport = viewport.New(panelWidth-4, topHeight-4)
		} else {
			m.eventsViewport.Width = m.width - 8
			m.eventsViewport.Height = bottomHeight - 4
			m.vulnsViewport.Width = panelWidth - 4
			m.vulnsViewport.Height = topHeight - 4
		}
	}
}

func (m *Model) updateViewportContent() {
	// Update vulnerabilities viewport
	var vulnContent strings.Builder
	if len(m.vulnerabilities) == 0 {
		vulnContent.WriteString(dimStyle.Render("No vulnerabilities found yet..."))
	} else {
		for _, vuln := range m.vulnerabilities {
			style := getSeverityStyle(vuln.Severity)
			vulnContent.WriteString(style.Render(fmt.Sprintf("(%s) ", strings.ToUpper(vuln.Severity))))
			vulnContent.WriteString(lipgloss.NewStyle().Bold(true).Render(vuln.Title) + "\n")
			vulnContent.WriteString(vuln.Description + "\n")
			vulnContent.WriteString(dimStyle.Render(strings.Repeat("-", 40)) + "\n\n")
		}
	}
	m.vulnsViewport.SetContent(vulnContent.String())

	// Update events viewport
	var eventsContent strings.Builder
	if len(m.activityLog) == 0 {
		eventsContent.WriteString(dimStyle.Render("Waiting for agent activity..."))
	} else {
		for _, entry := range m.activityLog {
			var style lipgloss.Style
			switch entry.Level {
			case "success":
				style = statusCompleted
			case "warning":
				style = lipgloss.NewStyle().Foreground(colorWarning)
			case "error":
				style = statusFailed
			default:
				style = lipgloss.NewStyle().Foreground(colorCyan)
			}

			wrapped := wordWrap(entry.Message, m.width-12)
			eventsContent.WriteString(style.Render(wrapped) + "\n")
		}
	}
	m.eventsViewport.SetContent(eventsContent.String())
	m.eventsViewport.GotoBottom()
}

func (m *Model) handleEvent(event interface{}) Model {
	switch e := event.(type) {

	case api.ScanStartedEvent:
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "info",
			Message: "📨 Scan started",
		})

	case api.ScanConfiguredEvent:
		msg := fmt.Sprintf("⚙️  Configuration: %d tools", e.ToolsCount)
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "info",
			Message: msg,
		})

	case api.AgentStartedEvent:
		m.currentAgent = e.Role
		m.agents[e.Role] = AgentStatus{
			Status:     "running",
			Iterations: e.Iteration,
		}
		m.logger.Info("Agent started",
			zap.String("role", e.Role),
			zap.Int("iteration", e.Iteration))
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "info",
			Message: "🤖 " + e.Role + " started",
		})

	case api.AgentThinkingEvent:
		if len(e.Thinking) > 0 {
			truncated := truncate(e.Thinking, 150)
			m.activityLog = append(m.activityLog, LogEntry{
				Level:   "info",
				Message: "💭 Thinking: " + truncated,
			})
		}

	case api.AgentTokenUsageEvent:
		msg := fmt.Sprintf("📊 Tokens → in: %d, out: %d", e.InputTokens, e.OutputTokens)
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "info",
			Message: msg,
		})

	case api.ToolExecutedEvent:
		msg := "🔧 Tool: " + e.ToolName
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "info",
			Message: msg,
		})

	case api.AgentCompletedEvent:
		m.agents[e.Role] = AgentStatus{
			Status:     "completed",
			Iterations: e.Iteration,
		}
		m.logger.Info("Agent completed",
			zap.String("role", e.Role),
			zap.Int("vulnerabilities", e.VulnerabilitiesFound))
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   "success",
			Message: "✅ " + e.Role + " completed",
		})

	case api.VulnerabilityFoundEvent:
		m.vulnerabilities = append(m.vulnerabilities, Vulnerability{
			Title:       e.Title,
			Severity:    e.Severity,
			Description: e.Description,
			CVEID:       e.CVEID,
			Parameter:   e.Parameter,
			POC:         e.POC,
		})
		m.logger.Info("Vulnerability found",
			zap.String("title", e.Title),
			zap.String("severity", e.Severity))

	case api.LogMessageEvent:
		m.activityLog = append(m.activityLog, LogEntry{
			Level:   e.Level,
			Message: e.Message,
		})

	case api.ScanCompletedEvent:
		m.scanStatus = fmt.Sprintf("completed (%d vulnerabilities)", e.VulnerabilitiesCount)
		m.logger.Info("Scan completed",
			zap.Int("vulnerabilities", e.VulnerabilitiesCount),
			zap.Float64("duration", e.DurationSeconds))

	case api.ScanFailedEvent:
		m.scanStatus = "failed: " + e.Error
		m.logger.Error("Scan failed", zap.String("error", e.Error))
	}

	return *m
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func wordWrap(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	var currentLine strings.Builder

	for _, word := range words {
		if currentLine.Len()+len(word)+1 > maxWidth && currentLine.Len() > 0 {
			result.WriteString(currentLine.String() + "\n")
			currentLine.Reset()
		}

		if currentLine.Len() > 0 {
			currentLine.WriteString(" ")
		}
		currentLine.WriteString(word)
	}

	if currentLine.Len() > 0 {
		result.WriteString(currentLine.String())
	}

	return result.String()
}

func getSeverityStyle(severity string) lipgloss.Style {
	switch severity {
	case "critical":
		return severityCritical
	case "high":
		return severityHigh
	case "medium":
		return severityMedium
	case "low":
		return severityLow
	default:
		return severityInfo
	}
}
