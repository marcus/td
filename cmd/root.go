package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version string
	baseDir string
)

// SetVersion sets the version string
func SetVersion(v string) {
	version = v
}

var rootCmd = &cobra.Command{
	Use:   "td",
	Short: "Local task and session management CLI",
	Long: `td - A minimalist local task and session management CLI designed for AI-assisted development workflows.

Optimized for session continuity—capturing working state so new context windows can resume where previous ones stopped.`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initBaseDir)

	// Define command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "workflow", Title: "Workflow Commands:"},
		&cobra.Group{ID: "query", Title: "Query Commands:"},
		&cobra.Group{ID: "shortcuts", Title: "Shortcuts:"},
		&cobra.Group{ID: "session", Title: "Session Commands:"},
		&cobra.Group{ID: "files", Title: "File Commands:"},
		&cobra.Group{ID: "system", Title: "System Commands:"},
	)

	// Assign built-in commands to system group
	rootCmd.SetHelpCommandGroupID("system")
	rootCmd.SetCompletionCommandGroupID("system")
}

func initBaseDir() {
	var err error
	baseDir, err = os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
}

// getBaseDir returns the base directory for the project
func getBaseDir() string {
	return baseDir
}
