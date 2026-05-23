package agents_graph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/llm"
	"github.com/usestrix/strix-go/pkg/tools"
)

type AgentMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Content   string    `json:"content"`
	MsgType   string    `json:"message_type"`
	Priority  string    `json:"priority"`
	Timestamp time.Time `json:"timestamp"`
}

type AgentNode struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	Task                string                 `json:"task"`
	Status              string                 `json:"status"` // "running", "waiting", "finished", "failed"
	ParentID            string                 `json:"parent_id"`
	CreatedAt           time.Time              `json:"created_at"`
	FinishedAt          time.Time              `json:"finished_at,omitempty"`
	Result              map[string]interface{} `json:"result,omitempty"`
	WaitingReason       string                 `json:"waiting_reason,omitempty"`
	Inbox               chan AgentMessage      `json:"-"`
	Sandbox             interface{}            `json:"-"`
	InitialHistory      []llm.Message          `json:"-"`
	ConversationHistory []llm.Message          `json:"-"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // "delegation", "message"
}

var (
	GraphLock   sync.RWMutex
	AgentNodes  = make(map[string]*AgentNode)
	GraphEdges  []GraphEdge
	RootAgentID string

	// SpawnAgentFunc is a decoupling callback set by package agents to avoid circular dependencies
	SpawnAgentFunc func(ctx context.Context, parentID, childID, childName, task string, skills []string) error
)

func RegisterAgentsGraphTools() {
	tools.Register("create_agent", false, CreateAgent)
	tools.Register("send_message_to_agent", false, SendMessageToAgent)
	tools.Register("wait_for_message", false, WaitForMessage)
	tools.Register("agent_finish", false, AgentFinish)
	tools.Register("view_agent_graph", false, ViewAgentGraph)
}

func generateAgentID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("agent_%s", hex.EncodeToString(b))
}

func CreateAgent(args map[string]interface{}) (interface{}, error) {
	GraphLock.Lock()
	defer GraphLock.Unlock()

	parentID, _ := args["agent_id"].(string)
	task, _ := args["task"].(string)
	name, _ := args["name"].(string)
	rawSkills, _ := args["skills"].(string)
	inheritContext, ok := args["inherit_context"].(bool)
	if !ok {
		inheritContext = true
	}

	if strings.TrimSpace(task) == "" {
		return map[string]interface{}{"success": false, "error": "Task description cannot be empty"}, nil
	}
	if strings.TrimSpace(name) == "" {
		return map[string]interface{}{"success": false, "error": "Agent name cannot be empty"}, nil
	}

	var skills []string
	if rawSkills != "" {
		for _, s := range strings.Split(rawSkills, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				skills = append(skills, s)
			}
		}
	}

	childID := generateAgentID()
	var initialHistory []llm.Message
	if inheritContext && parentID != "" {
		if parent, exists := AgentNodes[parentID]; exists && len(parent.ConversationHistory) > 0 {
			initialHistory = cloneHistory(parent.ConversationHistory)
		}
	}

	node := &AgentNode{
		ID:             childID,
		Name:           name,
		Task:           task,
		Status:         "running",
		ParentID:       parentID,
		CreatedAt:      time.Now().UTC(),
		Inbox:          make(chan AgentMessage, 100),
		InitialHistory: initialHistory,
	}

	AgentNodes[childID] = node

	slog.Info("Graph of Agents: Registered new agent node",
		slog.String("agent_id", childID),
		slog.String("name", name),
		slog.String("parent_id", parentID),
	)

	if parentID != "" {
		GraphEdges = append(GraphEdges, GraphEdge{
			From: parentID,
			To:   childID,
			Type: "delegation",
		})
	} else if RootAgentID == "" {
		RootAgentID = childID
	}

	if SpawnAgentFunc != nil {
		go func() {
			_ = SpawnAgentFunc(context.Background(), parentID, childID, name, task, skills)
		}()
	} else {
		return map[string]interface{}{"success": false, "error": "Spawn agent hook is not configured"}, nil
	}

	return map[string]interface{}{
		"success":  true,
		"agent_id": childID,
		"message":  fmt.Sprintf("Agent '%s' created and started asynchronously", name),
		"agent_info": map[string]interface{}{
			"id":        childID,
			"name":      name,
			"status":    "running",
			"parent_id": parentID,
		},
	}, nil
}

func SendMessageToAgent(args map[string]interface{}) (interface{}, error) {
	GraphLock.Lock()
	defer GraphLock.Unlock()

	senderID, _ := args["agent_id"].(string)
	targetID, _ := args["target_agent_id"].(string)
	msgContent, _ := args["message"].(string)
	msgType, _ := args["message_type"].(string)
	if msgType == "" {
		msgType = "information"
	}
	priority, _ := args["priority"].(string)
	if priority == "" {
		priority = "normal"
	}

	targetNode, exists := AgentNodes[targetID]
	if !exists {
		slog.Warn("Graph of Agents: Attempted to send message to non-existent agent",
			slog.String("sender_id", senderID),
			slog.String("target_id", targetID),
		)
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Target agent '%s' not found", targetID)}, nil
	}

	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano()/1e6%1000000)
	message := AgentMessage{
		ID:        msgID,
		From:      senderID,
		To:        targetID,
		Content:   msgContent,
		MsgType:   msgType,
		Priority:  priority,
		Timestamp: time.Now().UTC(),
	}

	// Send message to Inbox queue non-blockingly
	select {
	case targetNode.Inbox <- message:
		slog.Debug("Graph of Agents: Message enqueued to agent inbox",
			slog.String("message_id", msgID),
			slog.String("from", senderID),
			slog.String("to", targetID),
		)
		// If agent was waiting, wake it by setting status back to running
		if targetNode.Status == "waiting" {
			targetNode.Status = "running"
		}
	default:
		slog.Error("Graph of Agents: Agent inbox queue full, message dropped",
			slog.String("message_id", msgID),
			slog.String("from", senderID),
			slog.String("to", targetID),
		)
		return map[string]interface{}{"success": false, "error": "Target agent inbox queue full"}, nil
	}

	GraphEdges = append(GraphEdges, GraphEdge{
		From: senderID,
		To:   targetID,
		Type: "message",
	})

	return map[string]interface{}{
		"success":    true,
		"message_id": msgID,
		"message":    fmt.Sprintf("Message sent from '%s' to '%s'", senderID, targetNode.Name),
	}, nil
}

func WaitForMessage(args map[string]interface{}) (interface{}, error) {
	GraphLock.Lock()

	agentID, _ := args["agent_id"].(string)
	reason, _ := args["reason"].(string)
	if reason == "" {
		reason = "Waiting for messages from other agents"
	}

	node, exists := AgentNodes[agentID]
	if !exists {
		GraphLock.Unlock()
		return map[string]interface{}{"success": false, "error": "Agent not found"}, nil
	}

	node.Status = "waiting"
	node.WaitingReason = reason
	slog.Info("Agent entering wait state for incoming message",
		slog.String("agent_id", agentID),
		slog.String("reason", reason),
	)
	GraphLock.Unlock()

	// Wait on Inbox channel or timeout
	select {
	case msg := <-node.Inbox:
		GraphLock.Lock()
		node.Status = "running"
		node.WaitingReason = ""
		GraphLock.Unlock()

		slog.Info("Agent received message and resumed execution",
			slog.String("agent_id", agentID),
			slog.String("message_id", msg.ID),
			slog.String("from", msg.From),
		)

		return map[string]interface{}{
			"success": true,
			"status":  "resumed",
			"message": "Received message",
			"sender":  msg.From,
			"content": msg.Content,
		}, nil

	case <-time.After(10 * time.Minute):
		GraphLock.Lock()
		node.Status = "running"
		node.WaitingReason = ""
		GraphLock.Unlock()

		slog.Warn("Agent wait state timed out", slog.String("agent_id", agentID))

		return map[string]interface{}{
			"success": false,
			"error":   "Waiting timed out",
		}, nil
	}
}

func AgentFinish(args map[string]interface{}) (interface{}, error) {
	GraphLock.Lock()
	defer GraphLock.Unlock()

	agentID, _ := args["agent_id"].(string)
	summary, _ := args["result_summary"].(string)
	findings := interfaceSliceToStrings(args["findings"])
	finalRecommendations := interfaceSliceToStrings(args["final_recommendations"])
	successVal, ok := args["success"].(bool)
	if !ok {
		successVal = true
	}
	reportToParent, ok := args["report_to_parent"].(bool)
	if !ok {
		reportToParent = true
	}

	node, exists := AgentNodes[agentID]
	if !exists {
		return map[string]interface{}{"agent_completed": false, "error": "Agent not found"}, nil
	}
	if node.ParentID == "" {
		return map[string]interface{}{
			"agent_completed": false,
			"error":           "This tool can only be used by subagents. Root/main agents must use finish_scan instead.",
			"parent_notified": false,
		}, nil
	}

	node.Status = "finished"
	if !successVal {
		node.Status = "failed"
	}
	node.FinishedAt = time.Now().UTC()
	node.Result = map[string]interface{}{
		"summary":         strings.TrimSpace(summary),
		"findings":        findings,
		"success":         successVal,
		"recommendations": finalRecommendations,
	}

	parentNotified := false
	if reportToParent && node.ParentID != "" {
		if parentNode, exists := AgentNodes[node.ParentID]; exists {
			messageID := fmt.Sprintf("report_%d", time.Now().UnixNano()/1e6%1000000)
			reportMessage := formatCompletionReport(node, summary, findings, finalRecommendations, successVal)
			message := AgentMessage{
				ID:        messageID,
				From:      agentID,
				To:        node.ParentID,
				Content:   reportMessage,
				MsgType:   "information",
				Priority:  "high",
				Timestamp: time.Now().UTC(),
			}

			if enqueueMessageLocked(parentNode, message) {
				GraphEdges = append(GraphEdges, GraphEdge{
					From: agentID,
					To:   node.ParentID,
					Type: "message",
				})
				parentNotified = true
			}
		}
	}

	slog.Info("Agent execution finished",
		slog.String("agent_id", agentID),
		slog.String("status", node.Status),
		slog.Bool("success", successVal),
	)

	return map[string]interface{}{
		"agent_completed": true,
		"parent_notified": parentNotified,
		"completion_summary": map[string]interface{}{
			"agent_id":            agentID,
			"agent_name":          node.Name,
			"task":                node.Task,
			"success":             successVal,
			"findings_count":      len(findings),
			"has_recommendations": len(finalRecommendations) > 0,
			"finished_at":         node.FinishedAt,
		},
	}, nil
}

func ViewAgentGraph(args map[string]interface{}) (interface{}, error) {
	GraphLock.RLock()
	defer GraphLock.RUnlock()

	var sb strings.Builder
	sb.WriteString("=== AGENT GRAPH STRUCTURE ===\n")

	var buildTree func(id string, depth int)
	buildTree = func(id string, depth int) {
		node, exists := AgentNodes[id]
		if !exists {
			return
		}

		indent := strings.Repeat("  ", depth)
		sb.WriteString(fmt.Sprintf("%s* %s (%s)\n", indent, node.Name, id))
		sb.WriteString(fmt.Sprintf("%s  Task: %s\n", indent, node.Task))
		sb.WriteString(fmt.Sprintf("%s  Status: %s\n", indent, node.Status))

		children := []string{}
		for _, edge := range GraphEdges {
			if edge.From == id && edge.Type == "delegation" {
				children = append(children, edge.To)
			}
		}

		if len(children) > 0 {
			sb.WriteString(fmt.Sprintf("%s   Children:\n", indent))
			for _, child := range children {
				buildTree(child, depth+2)
			}
		}
	}

	rootID := RootAgentID
	if rootID == "" && len(AgentNodes) > 0 {
		// Fallback to first parentless node
		for id, node := range AgentNodes {
			if node.ParentID == "" {
				rootID = id
				break
			}
		}
	}

	if rootID != "" {
		buildTree(rootID, 0)
	} else {
		sb.WriteString("No agents in the graph yet\n")
	}

	return map[string]interface{}{
		"success":         true,
		"graph_structure": sb.String(),
		"summary": map[string]interface{}{
			"total_agents": len(AgentNodes),
		},
	}, nil
}

func cloneHistory(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}

	cloned := make([]llm.Message, len(history))
	copy(cloned, history)
	return cloned
}

func enqueueMessageLocked(targetNode *AgentNode, message AgentMessage) bool {
	select {
	case targetNode.Inbox <- message:
		if targetNode.Status == "waiting" {
			targetNode.Status = "running"
		}
		return true
	default:
		slog.Error("Graph of Agents: Agent inbox queue full, message dropped",
			slog.String("message_id", message.ID),
			slog.String("from", message.From),
			slog.String("to", message.To),
		)
		return false
	}
}

func interfaceSliceToStrings(raw interface{}) []string {
	switch value := raw.(type) {
	case []string:
		cloned := make([]string, len(value))
		copy(cloned, value)
		return cloned
	case []interface{}:
		var result []string
		for _, item := range value {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				result = append(result, str)
			}
		}
		return result
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []string{strings.TrimSpace(value)}
	default:
		return nil
	}
}

func formatCompletionReport(
	node *AgentNode,
	summary string,
	findings []string,
	recommendations []string,
	success bool,
) string {
	var findingsXML strings.Builder
	for _, finding := range findings {
		findingsXML.WriteString(fmt.Sprintf("        <finding>%s</finding>\n", finding))
	}

	var recommendationsXML strings.Builder
	for _, recommendation := range recommendations {
		recommendationsXML.WriteString(fmt.Sprintf("        <recommendation>%s</recommendation>\n", recommendation))
	}

	return fmt.Sprintf(`<agent_completion_report>
    <agent_info>
        <agent_name>%s</agent_name>
        <agent_id>%s</agent_id>
        <task>%s</task>
        <status>%s</status>
        <completion_time>%s</completion_time>
    </agent_info>
    <results>
        <summary>%s</summary>
        <findings>
%s        </findings>
        <recommendations>
%s        </recommendations>
    </results>
</agent_completion_report>`,
		node.Name,
		node.ID,
		node.Task,
		map[bool]string{true: "SUCCESS", false: "FAILED"}[success],
		node.FinishedAt.Format(time.RFC3339),
		strings.TrimSpace(summary),
		findingsXML.String(),
		recommendationsXML.String(),
	)
}
