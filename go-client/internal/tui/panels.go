package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

func renderAgentsPanel(agents map[string]AgentStatus, currentAgent string, width, height int) string {
	content := titleStyle.Render("Agents") + "\n\n"

	roles := []string{"reconnaissance", "exploitation", "post_exploitation"}
	for _, role := range roles {
		status, exists := agents[role]
		if !exists {
			status = AgentStatus{Status: "initialized", Iterations: 0}
		}

		var icon string
		var style lipgloss.Style

		switch status.Status {
		case "completed":
			icon = "🟢"
			style = statusCompleted
		case "running":
			icon = "⚪"
			style = statusRunning
		case "failed":
			icon = "🔴"
			style = statusFailed
		default:
			icon = "○"
			style = dimStyle
		}

		line := fmt.Sprintf("%s %s (iter %d)\n", icon, role, status.Iterations)

		if role == currentAgent {
			style = style.Bold(true)
		}

		content += style.Render(line)
	}

	return agentsPanelStyle.
		Width(width).
		Height(height).
		Render(content)
}

func renderActivityPanel(target, scanMode, scanStatus string, elapsed int, width, height int) string {
	content := titleStyle.Render("Activity") + "\n\n"
	content += fmt.Sprintf("Target: %s\n", target)
	content += fmt.Sprintf("Mode: %s\n", scanMode)
	content += fmt.Sprintf("Status: %s\n", scanStatus)
	content += fmt.Sprintf("Elapsed: %ds\n", elapsed)

	return activityPanelStyle.
		Width(width).
		Height(height).
		Render(content)
}

func renderVulnerabilitiesPanel(vulns []Vulnerability, viewport string, width, height int, focused bool) string {
	title := "Vulnerabilities"
	if focused {
		title += " [FOCUSED]"
	}
	titleRendered := titleStyle.Render(title) + "\n"

	content := titleRendered + "\n" + viewport

	style := vulnsPanelStyle
	if focused {
		style = style.BorderForeground(lipgloss.Color("51")) // Cyan when focused
	}

	return style.
		Width(width).
		Height(height).
		Render(content)
}

func renderEventsPanel(viewport string, width, height int, focused bool) string {
	title := "Events"
	if focused {
		title += " [FOCUSED]"
	}
	titleRendered := titleStyle.Render(title) + "\n"

	content := titleRendered + "\n" + viewport

	style := eventsPanelStyle
	if focused {
		style = style.BorderForeground(lipgloss.Color("51")) // Cyan when focused
	}

	return style.
		Width(width).
		Height(height).
		Render(content)
}

