package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary   = lipgloss.Color("39")   // Blue
	colorSuccess   = lipgloss.Color("42")   // Green
	colorWarning   = lipgloss.Color("226")  // Yellow
	colorError     = lipgloss.Color("196")  // Red
	colorAccent    = lipgloss.Color("141")  // Purple
	colorDim       = lipgloss.Color("241")  // Gray
	colorCyan      = lipgloss.Color("51")   // Cyan
	colorBrightRed = lipgloss.Color("202")  // Bright red

	agentsPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1)

	activityPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorWarning).
		Padding(1)

	vulnsPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSuccess).
		Padding(1)

	eventsPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1)

	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255"))

	dimStyle = lipgloss.NewStyle().
		Foreground(colorDim)

	statusRunning = lipgloss.NewStyle().
		Foreground(colorCyan)

	statusCompleted = lipgloss.NewStyle().
		Foreground(colorSuccess)

	statusFailed = lipgloss.NewStyle().
		Foreground(colorError)

	severityCritical = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorError)

	severityHigh = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorBrightRed)

	severityMedium = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWarning)

	severityLow = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorPrimary)

	severityInfo = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorCyan)
)

func severityStyle(severity string) lipgloss.Style {
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

func splitWidth(total int) (int, int, int) {
	panelWidth := (total - 6) / 3
	return panelWidth, panelWidth, panelWidth
}
