package agents

import (
	"context"
	"testing"
	"time"

	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
)

func TestStopCurrentAgent(t *testing.T) {
	// Save current root agent ID and nodes to restore later
	agents_graph.GraphLock.Lock()
	oldRootID := agents_graph.RootAgentID
	oldNodes := agents_graph.AgentNodes
	
	rootID := "test_root"
	agents_graph.RootAgentID = rootID
	agents_graph.AgentNodes = make(map[string]*agents_graph.AgentNode)
	
	// 1. Initialize root agent in graph
	rootInbox := make(chan agents_graph.AgentMessage, 10)
	agents_graph.AgentNodes[rootID] = &agents_graph.AgentNode{
		ID:        rootID,
		Name:      "Root Agent",
		Status:    "waiting",
		CreatedAt: time.Now().Add(-10 * time.Minute),
		Inbox:     rootInbox,
	}

	// 2. Initialize a sub-agent in graph and in activeAgents map
	subID := "test_subagent_456"
	subNode := &agents_graph.AgentNode{
		ID:        subID,
		Name:      "Sub Agent",
		ParentID:  rootID,
		Status:    "running",
		CreatedAt: time.Now(),
		Inbox:     make(chan agents_graph.AgentMessage, 10),
	}
	agents_graph.AgentNodes[subID] = subNode
	agents_graph.GraphLock.Unlock()

	defer func() {
		agents_graph.GraphLock.Lock()
		agents_graph.RootAgentID = oldRootID
		agents_graph.AgentNodes = oldNodes
		agents_graph.GraphLock.Unlock()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentCtx, agentCancel := context.WithCancel(ctx)
	agent := &Agent{
		ID:     subID,
		Name:   "Sub Agent",
		Cancel: agentCancel,
	}

	RegisterActiveAgent(agent)
	defer DeregisterActiveAgent(subID)

	// Run agent context monitor
	doneChan := make(chan struct{})
	go func() {
		<-agentCtx.Done()
		close(doneChan)
	}()

	// 3. Stop the current sub-agent via StopCurrentAgent()
	ok := StopCurrentAgent()
	if !ok {
		t.Fatalf("expected StopCurrentAgent to return true, got false")
	}

	// 4. Verify context was cancelled
	select {
	case <-doneChan:
		// success
	case <-time.After(1 * time.Second):
		t.Errorf("agentCtx was not cancelled within timeout")
	}

	// 5. Verify graph node status was set to failed
	agents_graph.GraphLock.RLock()
	status := subNode.Status
	agents_graph.GraphLock.RUnlock()
	if status != "failed" {
		t.Errorf("expected subagent status to be 'failed', got %q", status)
	}

	// 6. Verify parent inbox was notified
	select {
	case msg := <-rootInbox:
		if msg.From != subID {
			t.Errorf("expected notification from %q, got %q", subID, msg.From)
		}
	default:
		t.Errorf("expected parent agent's inbox to receive an interrupt notification")
	}
}
