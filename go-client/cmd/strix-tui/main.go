package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"github.com/strix/go-client/internal/logger"
	"github.com/strix/go-client/internal/tui"
)

var (
	target      string
	scanMode    string
	backendURL  string
	model       string
	mockTools   bool
	instruction string
	skills      string
	timeout     float64
	verbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "strix-tui",
	Short: "Strix security scanner TUI client",
	Long:  "A terminal user interface client for Strix security scanner",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Setup logging
		zapLogger, err := logger.SetupLogging(".")
		if err != nil {
			log.Fatalf("Failed to setup logging: %v", err)
		}
		defer zapLogger.Sync()

		// Log startup config
		zapLogger.Info("Starting Strix TUI",
			zap.String("target", target),
			zap.String("scan_mode", scanMode),
			zap.String("backend", backendURL),
		)

		// Create model
		m := tui.NewModel(
			target,
			scanMode,
			backendURL,
			model,
			instruction,
			skills,
			mockTools,
			verbose,
			timeout,
			zapLogger,
		)

		// Create and run app
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("error running program: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.Flags().StringVarP(&target, "target", "t", "localhost", "Scan target")
	rootCmd.Flags().StringVar(&scanMode, "mode", "quick", "Scan mode (quick, standard, deep)")
	rootCmd.Flags().StringVar(&backendURL, "backend", "http://localhost:8000", "Backend API URL")
	rootCmd.Flags().StringVar(&model, "model", "", "LLM model to use")
	rootCmd.Flags().BoolVar(&mockTools, "mock-tools", false, "Use mock tools")
	rootCmd.Flags().StringVar(&instruction, "instruction", "Penetration testing engagement", "Custom instruction")
	rootCmd.Flags().StringVar(&skills, "skills", "", "Comma-separated list of skills")
	rootCmd.Flags().Float64Var(&timeout, "timeout", 120.0, "Scan timeout in seconds")
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "Verbose logging")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
