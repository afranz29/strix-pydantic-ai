package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/usestrix/strix-go/pkg/agents"
	"github.com/usestrix/strix-go/pkg/llm"
	_interface "github.com/usestrix/strix-go/pkg/interface"
	"github.com/usestrix/strix-go/pkg/runtime"
	"github.com/usestrix/strix-go/pkg/tools"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	finishpkg "github.com/usestrix/strix-go/pkg/tools/finish"
	"github.com/usestrix/strix-go/pkg/tools/notes"
	"github.com/usestrix/strix-go/pkg/tools/reporting"
	"github.com/usestrix/strix-go/pkg/tools/thinking"
	"github.com/usestrix/strix-go/pkg/tools/todo"
)

var (
	targets         []string
	instruction     string
	instructionFile string
	nonInteractive  bool
	scanMode        string
	scopeMode       string
	diffBase        string
	configPath      string
	review          bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "strix",
		Short: "Strix Multi-Agent Cybersecurity Penetration Testing Tool",
		Long: `Strix are autonomous AI agents that find and validate vulnerabilities.
This Go command line orchestrates Docker sandbox environments and controls the multi-agent graph.`,
		RunE: runScan,
	}

	rootCmd.Flags().StringSliceVarP(&targets, "target", "t", nil, "Target to test (URL, repository, local directory path, domain name, or IP address)")
	rootCmd.Flags().StringVarP(&instruction, "instruction", "i", "", "Custom instructions for the penetration test")
	rootCmd.Flags().StringVar(&instructionFile, "instruction-file", "", "Path to a file containing detailed custom instructions")
	rootCmd.Flags().BoolVarP(&nonInteractive, "non-interactive", "n", false, "Run in non-interactive mode (no TUI, exits on completion)")
	rootCmd.Flags().StringVarP(&scanMode, "scan-mode", "m", "deep", "Scan mode: 'quick' for CI/CD, 'standard' for routine, or 'deep'")
	rootCmd.Flags().StringVar(&scopeMode, "scope-mode", "auto", "Scope mode: 'auto', 'diff', or 'full'")
	rootCmd.Flags().StringVar(&diffBase, "diff-base", "", "Target branch or commit to compare against")
	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to custom config file (JSON)")
	rootCmd.Flags().BoolVarP(&review, "review", "r", false, "Enable step-by-step interactive review of agent spawning and completion")

	_ = rootCmd.MarkFlagRequired("target")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveStrixDir() string {
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(cwd, "strix-python"),
		filepath.Join(cwd, "strix"),
		filepath.Join(cwd, "..", "strix-python"),
		filepath.Join(cwd, "..", "strix"),
		filepath.Join(cwd, "..", "..", "strix-python"),
		filepath.Join(cwd, "..", "..", "strix"),
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			toolsPath := filepath.Join(path, "tools")
			if tInfo, err := os.Stat(toolsPath); err == nil && tInfo.IsDir() {
				return path
			}
		}
	}
	return "strix-python"
}

func applyConfigFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file '%s': %w", configPath, err)
	}

	var parsed struct {
		Env map[string]interface{} `json:"env"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse config file '%s': %w", configPath, err)
	}

	for key, value := range parsed.Env {
		strValue, ok := value.(string)
		if !ok {
			continue
		}
		if err := os.Setenv(key, strValue); err != nil {
			return fmt.Errorf("failed to set env var %s from config: %w", key, err)
		}
	}

	return nil
}

func classifyTarget(raw string) map[string]string {
	if parsedURL, err := url.Parse(raw); err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		return map[string]string{"type": "web_application", "value": raw}
	}
	if ip := net.ParseIP(raw); ip != nil {
		return map[string]string{"type": "ip_address", "value": raw}
	}
	if info, err := os.Stat(raw); err == nil {
		targetType := "local_code"
		if !info.IsDir() {
			targetType = "local_file"
		}
		return map[string]string{"type": targetType, "value": raw}
	}
	if strings.HasSuffix(raw, ".git") || strings.Contains(raw, "github.com/") || strings.Contains(raw, "gitlab.com/") {
		return map[string]string{"type": "repository", "value": raw}
	}
	return map[string]string{"type": "domain", "value": raw}
}

func buildSystemPromptContext() map[string]interface{} {
	authorizedTargets := make([]map[string]string, 0, len(targets))
	for _, target := range targets {
		authorizedTargets = append(authorizedTargets, classifyTarget(target))
	}

	scopeSource := "cli_targets"
	if scopeMode == "diff" {
		scopeSource = "cli_targets_diff_scope"
	}

	return map[string]interface{}{
		"scope_source":                          scopeSource,
		"authorization_source":                  "strix_go_cli_targets",
		"authorized_targets":                    authorizedTargets,
		"user_instructions_do_not_expand_scope": true,
	}
}

func runScan(cmd *cobra.Command, args []string) error {
	if review {
		nonInteractive = true
	}

	if instruction != "" && instructionFile != "" {
		return fmt.Errorf("cannot specify both --instruction and --instruction-file")
	}

	if configPath != "" {
		if err := applyConfigFile(configPath); err != nil {
			return err
		}
	}

	if instructionFile != "" {
		data, err := os.ReadFile(instructionFile)
		if err != nil {
			return fmt.Errorf("failed to read instruction file '%s': %w", instructionFile, err)
		}
		instruction = string(data)
	}

	// 1. Resolve run directory first so we can redirect logging to it
	baseDir := "strix_go_runs"
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	sanitizedTarget := strings.ReplaceAll(targets[0], ".", "-")
	sanitizedTarget = strings.ReplaceAll(sanitizedTarget, "/", "-")
	sanitizedTarget = strings.ReplaceAll(sanitizedTarget, ":", "-")
	runName := fmt.Sprintf("%d_run_%s", time.Now().Unix(), sanitizedTarget)
	runDir := filepath.Join(baseDir, runName)
	err = os.MkdirAll(runDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create run directory: %w", err)
	}
	notes.RunDir = runDir
	todo.RunDir = runDir
	reporting.RunDir = runDir
	finishpkg.RunDir = runDir

	// 2. Initialize slog to write to shared log files in baseDir
	handler, err := _interface.NewStrixLogHandler(baseDir, nonInteractive)
	if err != nil {
		return err
	}
	defer handler.Close()

	slog.SetDefault(slog.New(handler))

	modelName := llm.GetModelName()

	// Print single notification to console
	fmt.Printf("Logs are being saved to: %s\n", filepath.Join(baseDir, "strix.log"))
	fmt.Printf("Agent activity is being saved to: %s\n", filepath.Join(baseDir, "agent.log"))
	fmt.Printf("Model being used: %s\n", modelName)

	slog.Info("STRIX Penetration Test Initiating...",
		slog.Any("targets", targets),
		slog.String("scan_mode", scanMode),
		slog.String("model", modelName),
		slog.Bool("non_interactive", nonInteractive),
	)

	// 3. Initialize Orchestrator and Registries
	agents.InitOrchestrator()
	notes.RegisterNotesTools()
	reporting.RegisterReportingTools()
	todo.RegisterTodoTools()
	thinking.RegisterThinkingTools()
	agents_graph.RegisterAgentsGraphTools()
	agents_graph.ReviewAgents = review
	finishpkg.RegisterFinishTools()

	// 4. Resolve paths and load tool XML schemas dynamically
	strixDir := resolveStrixDir()
	toolsDir := filepath.Join(strixDir, "tools")

	slog.Debug("Loading tool schemas...", slog.String("directory", toolsDir))
	err = filepath.Walk(toolsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), "_schema.xml") {
			errLoad := tools.LoadSchemaFile(path)
			if errLoad != nil {
				slog.Warn("Failed to load schema file", slog.String("path", path), slog.Any("error", errLoad))
			} else {
				slog.Debug("Schema file loaded successfully", slog.String("path", path))
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to load tool schemas: %w", err)
	}

	slog.Info("Run output directory configured", slog.String("run_dir", runDir))

	systemPromptContext := buildSystemPromptContext()
	agents.SetRunConfig(agents.RunConfig{
		ScanMode:            scanMode,
		Interactive:         !nonInteractive,
		SystemPromptContext: systemPromptContext,
	})

	// 5. Build task description
	var taskParts []string
	taskParts = append(taskParts, "Targets:")
	for _, target := range targets {
		taskParts = append(taskParts, fmt.Sprintf("- %s", target))
	}
	switch scopeMode {
	case "diff":
		diffTarget := diffBase
		if diffTarget == "" {
			diffTarget = "HEAD"
		}
		taskParts = append(taskParts, "", "Scope Constraints:")
		taskParts = append(taskParts, fmt.Sprintf("- Diff-scope mode is active. Prioritize changed files and adjacent execution paths relative to %s.", diffTarget))
	case "full":
		taskParts = append(taskParts, "", "Scope Constraints:")
		taskParts = append(taskParts, "- Full-scope mode is active. Assess the entire target rather than narrowing to changed files.")
	default:
		taskParts = append(taskParts, "", "Scope Constraints:")
		taskParts = append(taskParts, "- Auto scope mode is active. Use target context to decide whether to focus on changed files or assess the broader surface.")
	}
	if instruction != "" {
		taskParts = append(taskParts, fmt.Sprintf("\nSpecial instructions: %s", instruction))
	}
	taskDescription := strings.Join(taskParts, "\n")

	// 6. Initialize the root agent node structure in the graph
	rootID := "agent_root"
	agents_graph.GraphLock.Lock()
	agents_graph.RootAgentID = rootID
	agents_graph.AgentNodes[rootID] = &agents_graph.AgentNode{
		ID:                  rootID,
		Name:                "Root Agent",
		Task:                taskDescription,
		Status:              "running",
		CreatedAt:           time.Now().UTC(),
		Inbox:               make(chan agents_graph.AgentMessage, 100),
		ConversationHistory: nil,
	}
	agents_graph.GraphLock.Unlock()

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Cleanup function to destroy all sandboxes
	cleanup := func() {
		printFindingsSummary()
		slog.Info("Shutting down - cleaning up sandboxes...")
		docker, err := runtime.NewDockerRuntime()
		if err != nil {
			slog.Error("Failed to create Docker client for cleanup", slog.Any("error", err))
			return
		}

		agents_graph.GraphLock.Lock()
		defer agents_graph.GraphLock.Unlock()

		for _, node := range agents_graph.AgentNodes {
			if node.Sandbox != nil {
				if sandbox, ok := node.Sandbox.(*runtime.SandboxInfo); ok {
					slog.Info("Destroying sandbox container",
						slog.String("agent_id", node.ID),
						slog.String("container_id", sandbox.WorkspaceID))
					_ = docker.DestroySandbox(context.Background(), sandbox.WorkspaceID)
				}
			}
		}
	}
	defer cleanup()

	// Listen for interrupt signal in goroutine
	exitChan := make(chan struct{})
	go func() {
		for {
			<-sigChan
			if nonInteractive {
				slog.Info("Received interrupt signal (CTRL-C). Attempting to stop the current active sub-agent...")
				if agents.StopCurrentAgent() {
					slog.Info("Sub-agent stopped. Resuming parent flow. Press CTRL-C again to stop the main scan.")
					continue
				}
			}
			slog.Info("Received interrupt signal, initiating graceful shutdown...")
			cancel()
			close(exitChan)
			return
		}
	}()

	scanFunc := func() error {
		return agents.SpawnAgent(ctx, "", rootID, "Root Agent", taskDescription, []string{"root_agent"})
	}

	// Run scan and handle interrupts
	scanErr := make(chan error, 1)
	go func() {
		if nonInteractive {
			err := scanFunc()
			if err != nil {
				scanErr <- fmt.Errorf("root agent execution failed: %w", err)
				return
			}
			slog.Info("STRIX Penetration Test Completed Successfully.")
			scanErr <- nil
		} else {
			// Interactive TUI mode
			slog.Info("Starting TUI...")
			err := _interface.RunTUI(ctx, targets[0], scanMode, modelName, runDir, handler, scanFunc)
			if err != nil {
				scanErr <- fmt.Errorf("TUI execution failed: %w", err)
				return
			}
			scanErr <- nil
		}
	}()

	// Wait for either scan completion or interrupt signal
	select {
	case err := <-scanErr:
		return err
	case <-exitChan:
		slog.Info("Scan interrupted by user")
		return nil
	}
}

func printFindingsSummary() {
	fmt.Println()
	fmt.Println("================================================================================")
	fmt.Println("                       STRIX PENETRATION TEST SUMMARY                           ")
	fmt.Println("================================================================================")

	// 1. Print Agent Statuses
	fmt.Println("\n🤖 [AGENT STATUSES]")
	agents_graph.GraphLock.RLock()
	var agentIDs []string
	for id := range agents_graph.AgentNodes {
		agentIDs = append(agentIDs, id)
	}
	// Sort by CreatedAt or ID
	for i := 0; i < len(agentIDs)-1; i++ {
		for j := i + 1; j < len(agentIDs); j++ {
			nodeI := agents_graph.AgentNodes[agentIDs[i]]
			nodeJ := agents_graph.AgentNodes[agentIDs[j]]
			if nodeJ.CreatedAt.Before(nodeI.CreatedAt) {
				agentIDs[i], agentIDs[j] = agentIDs[j], agentIDs[i]
			}
		}
	}
	for _, id := range agentIDs {
		node := agents_graph.AgentNodes[id]
		fmt.Printf("- %s (%s): %s\n", node.Name, node.ID, node.Status)
		if node.WaitingReason != "" {
			fmt.Printf("  (Waiting: %s)\n", node.WaitingReason)
		}
	}
	agents_graph.GraphLock.RUnlock()

	// 2. Print Vulnerabilities
	vulnerabilities := reporting.GetVulnerabilityReports()
	fmt.Printf("\n🚨 [VULNERABILITIES IDENTIFIED (%d)]\n", len(vulnerabilities))
	if len(vulnerabilities) == 0 {
		fmt.Println("- No vulnerability reports generated so far.")
	} else {
		// Sort by CVSS Score desc
		for i := 0; i < len(vulnerabilities)-1; i++ {
			for j := i + 1; j < len(vulnerabilities); j++ {
				if vulnerabilities[j].CVSSScore > vulnerabilities[i].CVSSScore {
					vulnerabilities[i], vulnerabilities[j] = vulnerabilities[j], vulnerabilities[i]
				}
			}
		}
		for _, v := range vulnerabilities {
			endpointInfo := ""
			if v.Endpoint != "" {
				method := "GET"
				if v.Method != "" {
					method = v.Method
				}
				endpointInfo = fmt.Sprintf(" [%s %s]", method, v.Endpoint)
			}
			fmt.Printf("- [%s - %.1f] %s on %s%s\n", 
				strings.ToUpper(v.CVSSSeverity), 
				v.CVSSScore, 
				v.Title, 
				v.Target,
				endpointInfo,
			)
		}
	}

	// 3. Print Notes / Findings Summaries
	allNotes := notes.GetNotesList()
	fmt.Printf("\n📝 [NOTES & FINDINGS (%d)]\n", len(allNotes))
	if len(allNotes) == 0 {
		fmt.Println("- No notes recorded.")
	} else {
		// Filter and print notes
		for _, n := range allNotes {
			fmt.Printf("- [%s] %s\n", n.Category, n.Title)
			// Truncate content preview to first few lines or characters
			lines := strings.Split(n.Content, "\n")
			previewLines := 0
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					fmt.Printf("  %s\n", trimmed)
					previewLines++
					if previewLines >= 3 {
						break
					}
				}
			}
			if len(lines) > previewLines {
				fmt.Println("  ...")
			}
		}
	}
	fmt.Println("================================================================================")
	fmt.Println()
}
