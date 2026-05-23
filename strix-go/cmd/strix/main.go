package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/usestrix/strix-go/pkg/agents"
	_interface "github.com/usestrix/strix-go/pkg/interface"
	"github.com/usestrix/strix-go/pkg/tools"
	"github.com/usestrix/strix-go/pkg/tools/agents_graph"
	finishpkg "github.com/usestrix/strix-go/pkg/tools/finish"
	"github.com/usestrix/strix-go/pkg/tools/notes"
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
	rootCmd.Flags().StringVar(&instruction, "instruction", "", "Custom instructions for the penetration test")
	rootCmd.Flags().StringVar(&instructionFile, "instruction-file", "", "Path to a file containing detailed custom instructions")
	rootCmd.Flags().BoolVarP(&nonInteractive, "non-interactive", "n", false, "Run in non-interactive mode (no TUI, exits on completion)")
	rootCmd.Flags().StringVarP(&scanMode, "scan-mode", "m", "deep", "Scan mode: 'quick' for CI/CD, 'standard' for routine, or 'deep'")
	rootCmd.Flags().StringVar(&scopeMode, "scope-mode", "auto", "Scope mode: 'auto', 'diff', or 'full'")
	rootCmd.Flags().StringVar(&diffBase, "diff-base", "", "Target branch or commit to compare against")
	rootCmd.Flags().StringVar(&configPath, "config", "", "Path to custom config file (JSON)")

	_ = rootCmd.MarkFlagRequired("target")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveStrixDir() string {
	cwd, _ := os.Getwd()
	path := filepath.Join(cwd, "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	path = filepath.Join(cwd, "..", "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	path = filepath.Join(cwd, "..", "..", "strix")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return "strix"
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
	sanitizedTarget := strings.ReplaceAll(targets[0], ".", "-")
	sanitizedTarget = strings.ReplaceAll(sanitizedTarget, "/", "-")
	sanitizedTarget = strings.ReplaceAll(sanitizedTarget, ":", "-")
	runName := fmt.Sprintf("run_%s_%d", sanitizedTarget, time.Now().Unix())
	runDir := filepath.Join("strix_runs", runName)
	err := os.MkdirAll(runDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create run directory: %w", err)
	}
	notes.RunDir = runDir
	todo.RunDir = runDir
	finishpkg.RunDir = runDir

	// 2. Initialize slog to write to separate log files inside runDir
	handler, err := _interface.NewStrixLogHandler(runDir)
	if err != nil {
		return err
	}
	defer handler.Close()

	slog.SetDefault(slog.New(handler))

	// Print single notification to console
	fmt.Printf("Logs are being saved to: %s\n", filepath.Join(runDir, "strix.log"))
	fmt.Printf("Agent activity is being saved to: %s\n", filepath.Join(runDir, "agent.log"))

	slog.Info("STRIX Penetration Test Initiating...",
		slog.Any("targets", targets),
		slog.String("scan_mode", scanMode),
		slog.Bool("non_interactive", nonInteractive),
	)

	// 3. Initialize Orchestrator and Registries
	agents.InitOrchestrator()
	notes.RegisterNotesTools()
	todo.RegisterTodoTools()
	thinking.RegisterThinkingTools()
	agents_graph.RegisterAgentsGraphTools()
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

	ctx := context.Background()
	scanFunc := func() error {
		return agents.SpawnAgent(ctx, "", rootID, "Root Agent", taskDescription, []string{"root_agent"})
	}

	if nonInteractive {
		err = scanFunc()
		if err != nil {
			return fmt.Errorf("root agent execution failed: %w", err)
		}
		slog.Info("STRIX Penetration Test Completed Successfully.")
		return nil
	}

	// Interactive TUI mode
	slog.Info("Starting TUI...")
	err = _interface.RunTUI(ctx, targets[0], scanMode, runDir, handler, scanFunc)
	if err != nil {
		return fmt.Errorf("TUI execution failed: %w", err)
	}

	return nil
}
