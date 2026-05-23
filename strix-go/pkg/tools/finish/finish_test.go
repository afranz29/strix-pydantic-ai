package finish

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
)

func resetFinishGraph() {
	agents_graph.GraphLock.Lock()
	defer agents_graph.GraphLock.Unlock()

	agents_graph.AgentNodes = make(map[string]*agents_graph.AgentNode)
	agents_graph.GraphEdges = nil
	agents_graph.RootAgentID = "agent_root"
}

func TestFinishScanMarksRootCompleteAndPersistsReport(t *testing.T) {
	resetFinishGraph()
	RunDir = t.TempDir()

	agents_graph.GraphLock.Lock()
	agents_graph.AgentNodes["agent_root"] = &agents_graph.AgentNode{
		ID:     "agent_root",
		Name:   "Root Agent",
		Status: "running",
	}
	agents_graph.GraphLock.Unlock()

	result, err := FinishScan(map[string]interface{}{
		"agent_id":           "agent_root",
		"executive_summary":  "summary",
		"methodology":        "method",
		"technical_analysis": "analysis",
		"recommendations":    "recommend",
	})
	if err != nil {
		t.Fatalf("FinishScan returned error: %v", err)
	}

	response := result.(map[string]interface{})
	if completed, _ := response["scan_completed"].(bool); !completed {
		t.Fatalf("expected scan_completed=true, got %#v", response)
	}

	reportPath := filepath.Join(RunDir, "final_report.json")
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected persisted report at %s: %v", reportPath, err)
	}
}

func TestFinishScanBlocksWhenAgentsStillActive(t *testing.T) {
	resetFinishGraph()
	RunDir = t.TempDir()

	agents_graph.GraphLock.Lock()
	agents_graph.AgentNodes["agent_root"] = &agents_graph.AgentNode{
		ID:     "agent_root",
		Name:   "Root Agent",
		Status: "running",
	}
	agents_graph.AgentNodes["agent_child"] = &agents_graph.AgentNode{
		ID:       "agent_child",
		Name:     "Child",
		Status:   "waiting",
		ParentID: "agent_root",
	}
	agents_graph.GraphLock.Unlock()

	result, err := FinishScan(map[string]interface{}{
		"agent_id":           "agent_root",
		"executive_summary":  "summary",
		"methodology":        "method",
		"technical_analysis": "analysis",
		"recommendations":    "recommend",
	})
	if err != nil {
		t.Fatalf("FinishScan returned error: %v", err)
	}

	response := result.(map[string]interface{})
	if success, _ := response["success"].(bool); success {
		t.Fatalf("expected finish_scan to fail while child is active: %#v", response)
	}
	if response["error"] != "agents_still_active" {
		t.Fatalf("unexpected error payload: %#v", response)
	}
}
