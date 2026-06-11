package tui

import (
	"github.com/charmbracelet/lipgloss"
)

func renderLayout(m Model) string {
	if m.width < 40 || m.height < 15 {
		return "Terminal too small. Minimum: 40x15"
	}

	topHeight := (m.height - 4) / 2
	bottomHeight := m.height - topHeight - 4

	w1, w2, w3 := splitWidth(m.width)

	agentsView := renderAgentsPanel(m.agents, m.currentAgent, w1, topHeight)
	activityView := renderActivityPanel(m.target, m.scanMode, m.scanStatus, m.elapsed, w2, topHeight)
	vulnsView := renderVulnerabilitiesPanel(m.vulnerabilities, m.vulnsViewport.View(), w3, topHeight, m.focusedPanel == "vulns")

	topRow := lipgloss.JoinHorizontal(
		lipgloss.Top,
		agentsView,
		activityView,
		vulnsView,
	)

	eventsView := renderEventsPanel(m.eventsViewport.View(), m.width-4, bottomHeight, m.focusedPanel == "events")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		topRow,
		eventsView,
	)
}
