package agents_graph

import (
	"context"
	"testing"

	"github.com/usestrix/strix-go/pkg/llm"
)

func resetGraphState() {
	GraphLock.Lock()
	defer GraphLock.Unlock()

	AgentNodes = make(map[string]*AgentNode)
	GraphEdges = nil
	RootAgentID = ""
}

func TestCreateAgentInheritsParentHistory(t *testing.T) {
	resetGraphState()
	defer func() { SpawnAgentFunc = nil }()

	SpawnAgentFunc = func(ctx context.Context, parentID, childID, childName, task string, skills []string) error {
		return nil
	}

	GraphLock.Lock()
	AgentNodes["parent"] = &AgentNode{
		ID:    "parent",
		Name:  "Parent",
		Inbox: make(chan AgentMessage, 1),
		ConversationHistory: []llm.Message{
			{Role: "user", Content: "root"},
			{Role: "assistant", Content: "child work"},
		},
	}
	GraphLock.Unlock()

	result, err := CreateAgent(map[string]interface{}{
		"agent_id": "parent",
		"task":     "validate child",
		"name":     "Child",
	})
	if err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	response := result.(map[string]interface{})
	childID, _ := response["agent_id"].(string)
	if childID == "" {
		t.Fatalf("CreateAgent did not return an agent_id: %#v", response)
	}

	GraphLock.RLock()
	childNode := AgentNodes[childID]
	GraphLock.RUnlock()
	if childNode == nil {
		t.Fatalf("child node %q not found", childID)
	}
	if len(childNode.InitialHistory) != 2 {
		t.Fatalf("expected inherited history, got %#v", childNode.InitialHistory)
	}
	if childNode.InitialHistory[0].Content != "root" {
		t.Fatalf("unexpected inherited history: %#v", childNode.InitialHistory)
	}
}

func TestAgentFinishNotifiesParent(t *testing.T) {
	resetGraphState()

	parentInbox := make(chan AgentMessage, 1)
	GraphLock.Lock()
	AgentNodes["parent"] = &AgentNode{ID: "parent", Name: "Parent", Inbox: parentInbox}
	AgentNodes["child"] = &AgentNode{
		ID:       "child",
		Name:     "Child",
		Task:     "subtask",
		ParentID: "parent",
		Inbox:    make(chan AgentMessage, 1),
		Status:   "running",
	}
	GraphLock.Unlock()

	result, err := AgentFinish(map[string]interface{}{
		"agent_id":              "child",
		"result_summary":        "done",
		"findings":              []interface{}{"finding"},
		"final_recommendations": []interface{}{"recommendation"},
		"success":               false,
		"report_to_parent":      true,
	})
	if err != nil {
		t.Fatalf("AgentFinish returned error: %v", err)
	}

	response := result.(map[string]interface{})
	if notified, _ := response["parent_notified"].(bool); !notified {
		t.Fatalf("expected parent notification: %#v", response)
	}

	select {
	case msg := <-parentInbox:
		if msg.From != "child" {
			t.Fatalf("unexpected message sender: %#v", msg)
		}
	default:
		t.Fatal("expected completion message in parent inbox")
	}

	GraphLock.RLock()
	status := AgentNodes["child"].Status
	GraphLock.RUnlock()
	if status != "failed" {
		t.Fatalf("expected child status failed after success=false, got %q", status)
	}
}
