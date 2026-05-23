package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/llm"
	"github.com/usestrix/strix-go/pkg/runtime"
	"github.com/usestrix/strix-go/pkg/tools"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
)

type Agent struct {
	ID               string
	Name             string
	Task             string
	Skills           []string
	History          []llm.Message
	Sandbox          *runtime.SandboxInfo
	SandboxClient    *runtime.SandboxClient
	LLM              *llm.LLMClient
	MaxIterations    int
	Iteration        int
	ScanMode         string
	Interactive      bool
	SystemContext    map[string]interface{}
	ExecutionContext tools.ExecutionContext
	AvailableTools   []string
	OwnerSandbox     bool // true if this agent created the sandbox and should clean it up
	Cancel           context.CancelFunc
}

type RunConfig struct {
	ScanMode            string
	Interactive         bool
	SystemPromptContext map[string]interface{}
}

var (
	runConfigLock sync.RWMutex
	runConfig     = RunConfig{ScanMode: "deep"}
)

func SetRunConfig(cfg RunConfig) {
	runConfigLock.Lock()
	defer runConfigLock.Unlock()

	runConfig = RunConfig{
		ScanMode:            cfg.ScanMode,
		Interactive:         cfg.Interactive,
		SystemPromptContext: cloneMap(cfg.SystemPromptContext),
	}
	if runConfig.ScanMode == "" {
		runConfig.ScanMode = "deep"
	}
}

func getRunConfig() RunConfig {
	runConfigLock.RLock()
	defer runConfigLock.RUnlock()

	return RunConfig{
		ScanMode:            runConfig.ScanMode,
		Interactive:         runConfig.Interactive,
		SystemPromptContext: cloneMap(runConfig.SystemPromptContext),
	}
}

// LLM API backoff configuration. Exponential schedule: 2s, 4s, 8s, 16s, 32s,
// capped at 60s. After maxLLMConsecutiveErrors in a row, the agent aborts
// rather than spinning forever on a permanent error (bad key, model not found).
const (
	maxLLMConsecutiveErrors = 6
	llmBackoffBase          = 2 * time.Second
	llmBackoffMax           = 60 * time.Second
)

func llmBackoffDelay(consecutiveErrors int) time.Duration {
	if consecutiveErrors < 1 {
		consecutiveErrors = 1
	}
	// 2 * 2^(n-1): 2s, 4s, 8s, 16s, 32s, 64s...
	shift := consecutiveErrors - 1
	if shift > 30 {
		return llmBackoffMax
	}
	delay := llmBackoffBase << shift
	if delay > llmBackoffMax || delay <= 0 {
		return llmBackoffMax
	}
	return delay
}

// Tool failure loop detection. After this many identical consecutive tool
// failures we inject a stronger observation telling the LLM to stop repeating
// itself. We don't abort outright — the LLM might still recover with the
// stronger nudge — but we do break out of the dispatch loop to force a fresh
// completion.
const toolFailureRepeatThreshold = 3

// History pruning configuration. Keeps last N messages and redacts old base64
// screenshots to prevent unbounded context growth.
const (
	historyMaxMessages = 50 // Keep last N messages in sliding window
	base64Threshold    = 100 // Minimum length to consider as potential base64/screenshot
)

// validateToolAvailability checks that all requested skills/tools exist and are
// available in the given execution context. Skips special skills like root_agent
// that are bootstrap skills, not actual tools.
func validateToolAvailability(skills []string, ctx tools.ExecutionContext) error {
	// Special skills that don't need to be validated as tools
	specialSkills := map[string]bool{
		"root_agent": true, // Bootstrap skill for root agent
	}

	var missing []string
	var unavailable []string

	for _, skill := range skills {
		// Skip special bootstrap skills
		if specialSkills[skill] {
			continue
		}

		if err := tools.ValidateToolCallInContext(skill, ctx); err != nil {
			// Check if tool doesn't exist or isn't available in context
			if strings.Contains(err.Error(), "does not exist") {
				missing = append(missing, skill)
			} else {
				unavailable = append(unavailable, skill)
			}
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("required tools not found: %v", missing)
	}
	if len(unavailable) > 0 {
		return fmt.Errorf("required tools not available in %s context: %v", ctx, unavailable)
	}

	return nil
}

// pruneHistory enforces a sliding window on agent history and redacts old base64
// screenshots. Keeps the last historyMaxMessages messages and redacts large
// base64-like strings (screenshots) in messages older than the window.
func pruneHistory(history []llm.Message) []llm.Message {
	if len(history) <= historyMaxMessages {
		return history
	}

	// Apply sliding window: keep last N messages
	pruned := history[len(history)-historyMaxMessages:]

	// Redact base64-like content (screenshots) in all but most recent messages
	recentThreshold := len(pruned) - 10 // Keep most recent 10 messages unmodified
	for i := 0; i < recentThreshold && i < len(pruned); i++ {
		pruned[i].Content = redactBase64Content(pruned[i].Content)
	}

	return pruned
}

// redactBase64Content finds and redacts large base64-like strings (typically
// screenshots) while preserving the message structure and other content.
func redactBase64Content(content string) string {
	// Match large base64-like strings (min 80 chars, consists of alphanumeric, +, /, =)
	// This pattern targets base64 encoded images/screenshots in tool output
	re := regexp.MustCompile(`[A-Za-z0-9+/=]{80,}`)
	matches := re.FindAllString(content, -1)

	result := content
	for _, match := range matches {
		// Replace with a placeholder indicating redaction
		placeholder := fmt.Sprintf("[base64-redacted-%d]", len(match))
		result = strings.ReplaceAll(result, match, placeholder)
	}
	return result
}

// toolCallSignature builds a stable string identifying a tool invocation so we
// can detect identical retries. Go's encoding/json sorts map keys, giving a
// deterministic output for map[string]interface{}.
func toolCallSignature(name string, kwargs map[string]interface{}) string {
	clean := make(map[string]interface{}, len(kwargs))
	for k, v := range kwargs {
		if k == "agent_id" {
			// Injected by us, not by the LLM — exclude so the signature reflects
			// only what the LLM actually emitted.
			continue
		}
		clean[k] = v
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return name + "|<unmarshalable>"
	}
	return name + "|" + string(data)
}

var (
	activeAgentsLock sync.RWMutex
	activeAgents     = make(map[string]*Agent)
)

func RegisterActiveAgent(a *Agent) {
	activeAgentsLock.Lock()
	defer activeAgentsLock.Unlock()
	activeAgents[a.ID] = a
}

func DeregisterActiveAgent(id string) {
	activeAgentsLock.Lock()
	defer activeAgentsLock.Unlock()
	delete(activeAgents, id)
}

func InitOrchestrator() {
	// Register the Spawn callback in the agents graph package to avoid circular imports
	agents_graph.SpawnAgentFunc = SpawnAgent
	tools.Register("load_skill", false, LoadSkill)
}

func LoadSkill(args map[string]interface{}) (interface{}, error) {
	skillsStr, _ := args["skills"].(string)

	var requestedSkills []string
	for _, s := range strings.Split(skillsStr, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			requestedSkills = append(requestedSkills, s)
		}
	}

	if len(requestedSkills) == 0 {
		return map[string]interface{}{
			"success":          false,
			"error":            "No skills provided. Pass one or more comma-separated skill names.",
			"requested_skills": []string{},
		}, nil
	}

	// Validate that all skills exist
	var invalidSkills []string
	for _, skill := range requestedSkills {
		_, err := llm.FindSkillFile(skill)
		if err != nil {
			invalidSkills = append(invalidSkills, skill)
		}
	}
	if len(invalidSkills) > 0 {
		return map[string]interface{}{
			"success":          false,
			"error":            fmt.Sprintf("Skill(s) not found: %s", strings.Join(invalidSkills, ", ")),
			"requested_skills": requestedSkills,
			"loaded_skills":    []string{},
		}, nil
	}

	agentID, _ := args["agent_id"].(string)
	activeAgentsLock.RLock()
	a, ok := activeAgents[agentID]
	activeAgentsLock.RUnlock()

	if !ok {
		return map[string]interface{}{
			"success":          false,
			"error":            fmt.Sprintf("Could not find running agent instance for runtime skill loading (agent_id=%s).", agentID),
			"requested_skills": requestedSkills,
			"loaded_skills":    []string{},
		}, nil
	}

	var newlyLoaded []string
	var alreadyLoaded []string
	for _, skill := range requestedSkills {
		exists := false
		for _, s := range a.Skills {
			if s == skill {
				exists = true
				break
			}
		}
		if exists {
			alreadyLoaded = append(alreadyLoaded, skill)
		} else {
			a.Skills = append(a.Skills, skill)
			newlyLoaded = append(newlyLoaded, skill)
		}
	}

	mergedSkills := append([]string{}, a.Skills...)
	if a.SystemContext == nil {
		a.SystemContext = make(map[string]interface{})
	}
	a.SystemContext["loaded_skills"] = mergedSkills

	return map[string]interface{}{
		"success":               true,
		"requested_skills":      requestedSkills,
		"loaded_skills":         requestedSkills,
		"newly_loaded_skills":   newlyLoaded,
		"already_loaded_skills": alreadyLoaded,
		"message":               "Skills loaded into this agent prompt context.",
	}, nil
}

func StopCurrentAgent() bool {
	agents_graph.GraphLock.Lock()
	defer agents_graph.GraphLock.Unlock()

	// 1. Find all active agent nodes with status "running" or "waiting"
	var candidates []*agents_graph.AgentNode
	for _, node := range agents_graph.AgentNodes {
		if node.ID == agents_graph.RootAgentID {
			// Do not stop the root agent via this path, to allow the root to handle errors/shutdown
			continue
		}
		if node.Status == "running" || node.Status == "waiting" {
			candidates = append(candidates, node)
		}
	}

	if len(candidates) == 0 {
		return false
	}

	// 2. Find the most recently created candidate (latest CreatedAt)
	var target *agents_graph.AgentNode
	for _, c := range candidates {
		if target == nil || c.CreatedAt.After(target.CreatedAt) {
			target = c
		}
	}

	if target == nil {
		return false
	}

	// 3. Find the running Agent instance
	activeAgentsLock.RLock()
	a, ok := activeAgents[target.ID]
	activeAgentsLock.RUnlock()

	if !ok || a.Cancel == nil {
		return false
	}

	slog.Info("Stopping active sub-agent via operator interrupt",
		slog.String("agent_id", target.ID),
		slog.String("name", target.Name),
	)

	// 4. Cancel the agent's context
	a.Cancel()

	// 5. Update graph status to failed
	target.Status = "failed"
	target.FinishedAt = time.Now().UTC()

	// 6. Notify the parent agent if one exists
	if target.ParentID != "" {
		if parentNode, exists := agents_graph.AgentNodes[target.ParentID]; exists {
			messageID := fmt.Sprintf("interrupt_%d", time.Now().UnixNano()/1e6%1000000)
			interruptMessage := fmt.Sprintf(
				"<inter_agent_message>\n"+
				"Agent Identity:\n"+
				"- ID: %s\n"+
				"- Name: %s\n\n"+
				"Notification: This agent was interrupted and stopped by the operator. Any task delegated to it has failed.\n"+
				"</inter_agent_message>",
				target.ID, target.Name,
			)
			message := agents_graph.AgentMessage{
				ID:        messageID,
				From:      target.ID,
				To:        target.ParentID,
				Content:   interruptMessage,
				MsgType:   "information",
				Priority:  "high",
				Timestamp: time.Now().UTC(),
			}

			// Enqueue message to parent inbox channel
			select {
			case parentNode.Inbox <- message:
				parentNode.ConversationHistory = append(parentNode.ConversationHistory, llm.Message{
					Role:    "user",
					Content: message.Content,
				})
				slog.Info("Notified parent agent of sub-agent interrupt",
					slog.String("parent_id", target.ParentID),
					slog.String("sub_agent_id", target.ID),
				)
			default:
				slog.Warn("Failed to enqueue interrupt message to parent inbox (channel full)",
					slog.String("parent_id", target.ParentID),
				)
			}
		}
	}

	return true
}

func SpawnAgent(ctx context.Context, parentID, childID, childName, task string, skills []string) error {
	slog.Info("Spawning agent",
		slog.String("parent_id", parentID),
		slog.String("child_id", childID),
		slog.String("child_name", childName),
		slog.Any("skills", skills),
	)

	agentCtx, agentCancel := context.WithCancel(ctx)

	llmClient, err := llm.NewLLMClient(agentCtx)
	if err != nil {
		agentCancel()
		return fmt.Errorf("failed to initialize LLM client: %w", err)
	}

	cfg := getRunConfig()
	history := []llm.Message{}
	agents_graph.GraphLock.RLock()
	if node, exists := agents_graph.AgentNodes[childID]; exists && len(node.InitialHistory) > 0 {
		history = cloneHistory(node.InitialHistory)
	}
	agents_graph.GraphLock.RUnlock()
	if len(history) == 0 {
		history = []llm.Message{}
	}
	history = append(history, llm.Message{Role: "user", Content: fmt.Sprintf("Your core task is: %s", task)})

	agent := &Agent{
		ID:               childID,
		Name:             childName,
		Task:             task,
		Skills:           skills,
		LLM:              llmClient,
		MaxIterations:    300,
		History:          history,
		ScanMode:         cfg.ScanMode,
		Interactive:      cfg.Interactive,
		SystemContext:    cfg.SystemPromptContext,
		ExecutionContext: tools.ExecutionContextParent, // Will be updated if sandbox is available
		AvailableTools:   tools.GetAvailableTools(tools.ExecutionContextParent),
		Cancel:           agentCancel,
	}

	RegisterActiveAgent(agent)
	defer DeregisterActiveAgent(childID)

	// 1. Resolve Sandbox association: reuse parent's sandbox or create a new one
	if parentID != "" {
		agents_graph.GraphLock.RLock()
		parentVal, exists := agents_graph.AgentNodes[parentID]
		agents_graph.GraphLock.RUnlock()

		if exists && parentVal.Sandbox != nil {
			if sandbox, ok := parentVal.Sandbox.(*runtime.SandboxInfo); ok {
				agent.Sandbox = sandbox
				slog.Info("Reusing parent sandbox container",
					slog.String("agent_id", childID),
					slog.String("parent_id", parentID),
					slog.String("container_id", sandbox.WorkspaceID),
				)
			}
		}
	}

	// If no parent sandbox found, spawn new container
	if agent.Sandbox == nil {
		slog.Info("No parent sandbox container found, creating a new container", slog.String("agent_id", childID))
		docker, err := runtime.NewDockerRuntime()
		if err != nil {
			return fmt.Errorf("failed to create Docker client: %w", err)
		}

		sandbox, err := docker.CreateSandbox(ctx, childID, nil)
		if err != nil {
			return fmt.Errorf("failed to initialize sandbox container: %w", err)
		}

		agent.Sandbox = sandbox
		agent.OwnerSandbox = true // Agent created this sandbox, so it should clean it up
	}

	// Agents are orchestrators that run in parent context.
	// The sandbox is a container for tools to execute in, not where the agent runs.
	// All agents need parent context to spawn sub-agents, create tasks, etc.
	// (This was already set correctly in SpawnAgent initialization)

	// Register sandbox state in the agents graph registry for sharing
	agents_graph.GraphLock.Lock()
	if node, exists := agents_graph.AgentNodes[childID]; exists {
		node.Sandbox = agent.Sandbox
	}
	agents_graph.GraphLock.Unlock()

	// Initialize sandbox HTTP API execution client
	agent.SandboxClient = runtime.NewSandboxClient(agent.Sandbox.APIURL, agent.Sandbox.AuthToken)
	agent.syncConversationHistory()

	// Validate that all requested skills/tools exist before starting execution
	if err := validateToolAvailability(agent.Skills, agent.ExecutionContext); err != nil {
		return fmt.Errorf("tool validation failed for agent %s: %w", childID, err)
	}

	return agent.Run(agentCtx)
}

func (a *Agent) Run(ctx context.Context) error {
	slog.Info("Starting agent execution loop", slog.String("agent_id", a.ID), slog.String("agent_name", a.Name))

	// Clean up sandbox on exit (only if this agent created it, not if inherited from parent)
	defer func() {
		if a.OwnerSandbox && a.Sandbox != nil && a.ID != "" {
			slog.Info("Cleaning up agent sandbox", slog.String("agent_id", a.ID), slog.String("container_id", a.Sandbox.WorkspaceID))
			docker, err := runtime.NewDockerRuntime()
			if err == nil {
				_ = docker.DestroySandbox(ctx, a.Sandbox.WorkspaceID)
			}
		}
	}()

	// Backoff state for LLM API errors. Reset on successful completion.
	var llmConsecutiveErrors int

	// Loop detector for tool failures. If the LLM keeps repeating the same
	// failed tool call, we escalate the observation so it has stronger signal
	// to change approach.
	var lastFailedToolSig string
	var lastFailedToolCount int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if we exceeded iteration budget
		if a.Iteration >= a.MaxIterations {
			slog.Error("Maximum iterations budget reached",
				slog.String("agent_id", a.ID),
				slog.String("agent_name", a.Name),
				slog.Int("max_iterations", a.MaxIterations),
			)
			break
		}

		a.Iteration++

		// Check status in graph
		agents_graph.GraphLock.RLock()
		node, exists := agents_graph.AgentNodes[a.ID]
		agents_graph.GraphLock.RUnlock()
		if exists && (node.Status == "finished" || node.Status == "failed") {
			break
		}

		// 1. Render system prompt containing XML schemas and skills Markdown
		systemPrompt, err := llm.CompileSystemPromptWithContext(
			"StrixAgent",
			a.Skills,
			a.ScanMode,
			a.Interactive,
			a.SystemContext,
			a.ExecutionContext,
		)
		if err != nil {
			return fmt.Errorf("failed to compile system prompt: %w", err)
		}

		// 2. Prune history to prevent unbounded context growth
		a.History = pruneHistory(a.History)

		// 3. Chat completion
		slog.Debug("Requesting LLM completion",
			slog.String("agent_id", a.ID),
			slog.String("agent_name", a.Name),
			slog.Int("iteration", a.Iteration),
		)
		completion, err := a.LLM.GenerateChatCompletion(ctx, systemPrompt, a.History)
		if err != nil {
			llmConsecutiveErrors++
			if llmConsecutiveErrors >= maxLLMConsecutiveErrors {
				slog.Error("LLM completion failed too many times in a row, aborting agent",
					slog.String("agent_id", a.ID),
					slog.String("agent_name", a.Name),
					slog.Int("consecutive_errors", llmConsecutiveErrors),
					slog.Any("last_error", err),
				)
				return fmt.Errorf("agent %s aborted after %d consecutive LLM errors: %w", a.ID, llmConsecutiveErrors, err)
			}
			delay := llmBackoffDelay(llmConsecutiveErrors)
			slog.Warn("LLM completion request failed, backing off",
				slog.String("agent_id", a.ID),
				slog.String("agent_name", a.Name),
				slog.Int("consecutive_errors", llmConsecutiveErrors),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		llmConsecutiveErrors = 0

		slog.Debug("LLM completion received",
			slog.String("agent_id", a.ID),
			slog.String("agent_name", a.Name),
			slog.String("completion", completion),
		)

		// Record assistant thought/tool call to history
		a.History = append(a.History, llm.Message{Role: "assistant", Content: completion})
		a.syncConversationHistory()

		// 3. Parse XML tool invocations
		toolCalls := llm.ParseToolInvocations(completion)
		if len(toolCalls) == 0 {
			// If LLM did not execute tools, add fallback warning user message
			a.History = append(a.History, llm.Message{
				Role:    "user",
				Content: "You did not make any tool calls. If you are finished, invoke agent_finish (for sub-agents) or finish_scan (for root agent). Otherwise, execute a valid tool call to proceed.",
			})
			a.syncConversationHistory()
			continue
		}

		// 4. Dispatch tool calls
		for _, call := range toolCalls {
			slog.Info("Invoking tool",
				slog.String("agent_id", a.ID),
				slog.String("agent_name", a.Name),
				slog.Int("iteration", a.Iteration),
				slog.String("tool_name", call.ToolName),
				slog.Any("kwargs", call.Kwargs),
			)

			def, ok := tools.GetTool(call.ToolName)
			if !ok {
				slog.Warn("Tool not registered in the system",
					slog.String("agent_id", a.ID),
					slog.String("tool_name", call.ToolName),
				)
				obs := fmt.Sprintf("<observation>\nError: Tool '%s' is not registered in the system.\n</observation>", call.ToolName)
				a.History = append(a.History, llm.Message{Role: "user", Content: obs})
				continue
			}

			var result interface{}
			var toolErr error

			call.Kwargs = tools.NormalizeArguments(def, call.Kwargs)

			if call.Kwargs == nil {
				call.Kwargs = make(map[string]interface{})
			}

			if def.SandboxExecution {
				// Sandbox tools get agent_id via the top-level request field
				// (ToolExecutionRequest.AgentID). Don't put it in kwargs — the
				// Python tool server forwards kwargs straight to the tool
				// function, which would reject the unexpected argument.
				result, toolErr = a.SandboxClient.ExecuteTool(ctx, a.ID, call.ToolName, call.Kwargs)
			} else {
				// Local Go handlers (finish, agents_graph, todo) look up
				// agent_id from kwargs for context tracking.
				call.Kwargs["agent_id"] = a.ID
				if handler, ok := def.Handler.(func(map[string]interface{}) (interface{}, error)); ok {
					result, toolErr = handler(call.Kwargs)
				} else {
					toolErr = fmt.Errorf("local tool handler has invalid signature")
				}
			}

			var observation string
			if toolErr != nil {
				slog.Error("Tool execution failed",
					slog.String("agent_id", a.ID),
					slog.String("tool_name", call.ToolName),
					slog.Any("error", toolErr),
				)

				sig := toolCallSignature(call.ToolName, call.Kwargs)
				if sig == lastFailedToolSig {
					lastFailedToolCount++
				} else {
					lastFailedToolSig = sig
					lastFailedToolCount = 1
				}

				if lastFailedToolCount >= toolFailureRepeatThreshold {
					slog.Warn("LLM is repeating an identical failing tool call",
						slog.String("agent_id", a.ID),
						slog.String("tool_name", call.ToolName),
						slog.Int("repeat_count", lastFailedToolCount),
					)
					observation = fmt.Sprintf(
						"<observation>\nError executing tool: %v\n\n"+
							"NOTE: You have now called %s with these exact arguments %d times in a row "+
							"and it has failed every time with the same error. This approach is not working. "+
							"Do NOT call this tool with the same arguments again. Either change the parameters, "+
							"try a different tool, or invoke agent_finish/finish_scan to terminate.\n</observation>",
						toolErr, call.ToolName, lastFailedToolCount,
					)
				} else {
					observation = fmt.Sprintf("<observation>\nError executing tool: %v\n</observation>", toolErr)
				}
			} else {
				slog.Info("Tool executed successfully",
					slog.String("agent_id", a.ID),
					slog.String("tool_name", call.ToolName),
					slog.Any("result", result),
				)
				lastFailedToolSig = ""
				lastFailedToolCount = 0
				// Format output as JSON or string
				data, err := jsonMarshalIndent(result)
				if err != nil {
					observation = fmt.Sprintf("<observation>\n%v\n</observation>", result)
				} else {
					observation = fmt.Sprintf("<observation>\n%s\n</observation>", string(data))
				}
			}

			// Add observation back to history
			a.History = append(a.History, llm.Message{Role: "user", Content: observation})
			a.syncConversationHistory()
		}
	}

	slog.Info("Execution loop finished", slog.String("agent_id", a.ID), slog.String("agent_name", a.Name))
	return nil
}

func (a *Agent) syncConversationHistory() {
	agents_graph.GraphLock.Lock()
	defer agents_graph.GraphLock.Unlock()

	if node, exists := agents_graph.AgentNodes[a.ID]; exists {
		node.ConversationHistory = cloneHistory(a.History)
	}
}

func cloneHistory(history []llm.Message) []llm.Message {
	if len(history) == 0 {
		return nil
	}

	cloned := make([]llm.Message, len(history))
	copy(cloned, history)
	return cloned
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return nil
	}

	cloned := make(map[string]interface{}, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func jsonMarshalIndent(v interface{}) ([]byte, error) {
	if str, ok := v.(string); ok {
		return []byte(str), nil
	}
	return json.MarshalIndent(v, "", "  ")
}
