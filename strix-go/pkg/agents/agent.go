package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/llm"
	"github.com/usestrix/strix-go/pkg/runtime"
	"github.com/usestrix/strix-go/pkg/tools"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
)

type Agent struct {
	ID            string
	Name          string
	Task          string
	Skills        []string
	History       []llm.Message
	Sandbox       *runtime.SandboxInfo
	SandboxClient *runtime.SandboxClient
	LLM           *llm.LLMClient
	MaxIterations int
	Iteration     int
	ScanMode      string
	Interactive   bool
	SystemContext map[string]interface{}
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

func InitOrchestrator() {
	// Register the Spawn callback in the agents graph package to avoid circular imports
	agents_graph.SpawnAgentFunc = SpawnAgent
}

func SpawnAgent(ctx context.Context, parentID, childID, childName, task string, skills []string) error {
	slog.Info("Spawning agent",
		slog.String("parent_id", parentID),
		slog.String("child_id", childID),
		slog.String("child_name", childName),
		slog.Any("skills", skills),
	)

	llmClient, err := llm.NewLLMClient(ctx)
	if err != nil {
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
		ID:            childID,
		Name:          childName,
		Task:          task,
		Skills:        skills,
		LLM:           llmClient,
		MaxIterations: 300,
		History:       history,
		ScanMode:      cfg.ScanMode,
		Interactive:   cfg.Interactive,
		SystemContext: cfg.SystemPromptContext,
	}

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
	}

	// Register sandbox state in the agents graph registry for sharing
	agents_graph.GraphLock.Lock()
	if node, exists := agents_graph.AgentNodes[childID]; exists {
		node.Sandbox = agent.Sandbox
	}
	agents_graph.GraphLock.Unlock()

	// Initialize sandbox HTTP API execution client
	agent.SandboxClient = runtime.NewSandboxClient(agent.Sandbox.APIURL, agent.Sandbox.AuthToken)
	agent.syncConversationHistory()

	return agent.Run(ctx)
}

func (a *Agent) Run(ctx context.Context) error {
	slog.Info("Starting agent execution loop", slog.String("agent_id", a.ID), slog.String("agent_name", a.Name))

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
		systemPrompt, err := llm.CompileSystemPrompt("StrixAgent", a.Skills, a.ScanMode, a.Interactive, a.SystemContext)
		if err != nil {
			return fmt.Errorf("failed to compile system prompt: %w", err)
		}

		// 2. Chat completion
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
