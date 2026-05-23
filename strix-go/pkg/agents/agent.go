package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

	agent := &Agent{
		ID:            childID,
		Name:          childName,
		Task:          task,
		Skills:        skills,
		LLM:           llmClient,
		MaxIterations: 300,
		History: []llm.Message{
			{Role: "user", Content: fmt.Sprintf("Your core task is: %s", task)},
		},
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

	return agent.Run(ctx)
}

func (a *Agent) Run(ctx context.Context) error {
	slog.Info("Starting agent execution loop", slog.String("agent_id", a.ID), slog.String("agent_name", a.Name))

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
		systemPrompt, err := llm.CompileSystemPrompt("StrixAgent", a.Skills, nil)
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
			slog.Error("LLM completion request failed",
				slog.String("agent_id", a.ID),
				slog.String("agent_name", a.Name),
				slog.Any("error", err),
			)
			time.Sleep(5 * time.Second)
			continue
		}

		slog.Debug("LLM completion received",
			slog.String("agent_id", a.ID),
			slog.String("agent_name", a.Name),
			slog.String("completion", completion),
		)

		// Record assistant thought/tool call to history
		a.History = append(a.History, llm.Message{Role: "assistant", Content: completion})

		// 3. Parse XML tool invocations
		toolCalls := llm.ParseToolInvocations(completion)
		if len(toolCalls) == 0 {
			// If LLM did not execute tools, add fallback warning user message
			a.History = append(a.History, llm.Message{
				Role:    "user",
				Content: "You did not make any tool calls. If you are finished, invoke agent_finish (for sub-agents) or finish_scan (for root agent). Otherwise, execute a valid tool call to proceed.",
			})
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

			// Inject agent ID metadata for local handlers context tracking
			if call.Kwargs == nil {
				call.Kwargs = make(map[string]interface{})
			}
			call.Kwargs["agent_id"] = a.ID

			if def.SandboxExecution {
				// Route to container tool server via HTTP bridge
				result, toolErr = a.SandboxClient.ExecuteTool(ctx, a.ID, call.ToolName, call.Kwargs)
			} else {
				// Execute local Go handler function
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
				observation = fmt.Sprintf("<observation>\nError executing tool: %v\n</observation>", toolErr)
			} else {
				slog.Info("Tool executed successfully",
					slog.String("agent_id", a.ID),
					slog.String("tool_name", call.ToolName),
				)
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
		}
	}

	slog.Info("Execution loop finished", slog.String("agent_id", a.ID), slog.String("agent_name", a.Name))
	return nil
}

func jsonMarshalIndent(v interface{}) ([]byte, error) {
	if str, ok := v.(string); ok {
		return []byte(str), nil
	}
	return json.MarshalIndent(v, "", "  ")
}
