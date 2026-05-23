package finish

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/usestrix/strix-go/pkg/tools"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	"github.com/usestrix/strix-go/pkg/tools/notes"
)

var RunDir string

type ScanReport struct {
	ExecutiveSummary  string    `json:"executive_summary"`
	Methodology       string    `json:"methodology"`
	TechnicalAnalysis string    `json:"technical_analysis"`
	Recommendations   string    `json:"recommendations"`
	CompletedAt       time.Time `json:"completed_at"`
}

func RegisterFinishTools() {
	tools.Register("finish_scan", false, FinishScan)
}

func FinishScan(args map[string]interface{}) (interface{}, error) {
	agentID, _ := args["agent_id"].(string)
	executiveSummary, _ := args["executive_summary"].(string)
	methodology, _ := args["methodology"].(string)
	technicalAnalysis, _ := args["technical_analysis"].(string)
	recommendations, _ := args["recommendations"].(string)

	validationErrors := []string{}
	if strings.TrimSpace(executiveSummary) == "" {
		validationErrors = append(validationErrors, "Executive summary cannot be empty")
	}
	if strings.TrimSpace(methodology) == "" {
		validationErrors = append(validationErrors, "Methodology cannot be empty")
	}
	if strings.TrimSpace(technicalAnalysis) == "" {
		validationErrors = append(validationErrors, "Technical analysis cannot be empty")
	}
	if strings.TrimSpace(recommendations) == "" {
		validationErrors = append(validationErrors, "Recommendations cannot be empty")
	}
	if len(validationErrors) > 0 {
		return map[string]interface{}{
			"success": false,
			"message": "Validation failed",
			"errors":  validationErrors,
		}, nil
	}

	// Create a summary note so it's visible in the TUI Findings tab
	summaryContent := fmt.Sprintf("# Executive Summary\n%s\n\n# Technical Analysis\n%s\n\n# Recommendations\n%s\n\n# Methodology\n%s",
		executiveSummary, technicalAnalysis, recommendations, methodology)
	
	_, _ = notes.CreateNote(map[string]interface{}{
		"title":    "Final Scan Summary",
		"content":  summaryContent,
		"category": "findings",
	})

	agents_graph.GraphLock.Lock()
	defer agents_graph.GraphLock.Unlock()

	node, exists := agents_graph.AgentNodes[agentID]
	if !exists {
		return map[string]interface{}{
			"success":        false,
			"scan_completed": false,
			"error":          "Current agent not found in graph",
		}, nil
	}
	if node.ParentID != "" {
		return map[string]interface{}{
			"success":        false,
			"scan_completed": false,
			"error":          "This tool can only be used by the root/main agent",
			"suggestion":     "If you are a subagent, use agent_finish from agents_graph tool instead",
		}, nil
	}

	activeAgents := make([]map[string]interface{}, 0)
	for otherID, otherNode := range agents_graph.AgentNodes {
		if otherID == agentID {
			continue
		}
		if otherNode.Status == "running" || otherNode.Status == "waiting" {
			activeAgents = append(activeAgents, map[string]interface{}{
				"id":     otherID,
				"name":   otherNode.Name,
				"task":   otherNode.Task,
				"status": otherNode.Status,
			})
		}
	}
	if len(activeAgents) > 0 {
		return map[string]interface{}{
			"success":        false,
			"scan_completed": false,
			"error":          "agents_still_active",
			"message":        "Cannot finish scan: agents are still active",
			"active_agents":  activeAgents,
			"suggestions": []string{
				"Use wait_for_message to wait for all agents to complete",
				"Use send_message_to_agent if you need agents to complete immediately",
				"Check view_agent_graph to inspect agent states",
			},
			"total_active": len(activeAgents),
		}, nil
	}

	report := ScanReport{
		ExecutiveSummary:  strings.TrimSpace(executiveSummary),
		Methodology:       strings.TrimSpace(methodology),
		TechnicalAnalysis: strings.TrimSpace(technicalAnalysis),
		Recommendations:   strings.TrimSpace(recommendations),
		CompletedAt:       time.Now().UTC(),
	}

	node.Status = "finished"
	node.FinishedAt = report.CompletedAt
	node.Result = map[string]interface{}{
		"executive_summary":  report.ExecutiveSummary,
		"methodology":        report.Methodology,
		"technical_analysis": report.TechnicalAnalysis,
		"recommendations":    report.Recommendations,
		"success":            true,
	}

	if err := persistReport(report); err != nil {
		return map[string]interface{}{
			"success":        true,
			"scan_completed": true,
			"message":        "Scan completed, but the final report could not be persisted",
			"warning":        err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"success":        true,
		"scan_completed": true,
		"message":        "Scan completed successfully",
	}, nil
}

func persistReport(report ScanReport) error {
	if RunDir == "" {
		return nil
	}

	reportPath := filepath.Join(RunDir, "final_report.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode final report: %w", err)
	}
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write final report: %w", err)
	}

	return nil
}
