package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcus/td/internal/agent"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Initialize a new td project",
	Long:    `Creates the local .todos directory and SQLite database.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// Check if already initialized
		if _, err := os.Stat(filepath.Join(baseDir, ".todos")); err == nil {
			output.Warning(".todos/ already exists")
			return nil
		}

		// Initialize database
		database, err := db.Initialize(baseDir)
		if err != nil {
			output.Error("failed to initialize database: %v", err)
			return err
		}
		defer database.Close()

		fmt.Println("INITIALIZED .todos/")

		// Add to .gitignore if in a git repo
		if git.IsRepo() {
			gitignorePath := filepath.Join(baseDir, ".gitignore")
			addToGitignore(gitignorePath)
		}

		// Create session
		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("failed to create session: %v", err)
			return err
		}

		fmt.Printf("Session: %s\n", sess.ID)

		// Suggest adding td usage to agent file
		suggestAgentFileAddition(baseDir)

		return nil
	},
}

func addToGitignore(path string) {
	// Read existing content
	content, _ := os.ReadFile(path)
	contentStr := string(content)

	// Check if already present
	if strings.Contains(contentStr, ".todos/") {
		return
	}

	// Append to file
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Add newline if file doesn't end with one
	if len(contentStr) > 0 && !strings.HasSuffix(contentStr, "\n") {
		f.WriteString("\n")
	}

	f.WriteString(".todos/\n")
	fmt.Println("Added .todos/ to .gitignore")
}

func suggestAgentFileAddition(baseDir string) {
	fmt.Println()

	// Check for existing agent files
	foundFile := agent.DetectAgentFile(baseDir)

	if foundFile != "" {
		// Check if already contains td instruction
		if agent.HasTDInstructions(foundFile) {
			return // Already has td instructions
		}

		fmt.Printf("Found %s. Add td instructions?\n", filepath.Base(foundFile))
		fmt.Println()
		fmt.Println("Text to add:")
		fmt.Println("---")
		fmt.Print(agent.InstructionText)
		fmt.Println("---")
		fmt.Println()
		fmt.Print("Add to file? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "y" || response == "yes" {
			if err := agent.InstallInstructions(foundFile); err != nil {
				output.Error("failed to update %s: %v", filepath.Base(foundFile), err)
			} else {
				output.Success("Added td instructions to %s", filepath.Base(foundFile))
			}
		}
	} else {
		// No agent file found, just show suggestion
		fmt.Println("Tip: Add this to your CLAUDE.md, AGENTS.md, or similar agent file:")
		fmt.Println()
		fmt.Print(agent.InstructionText)
	}
}

func init() {
	rootCmd.AddCommand(initCmd)
}
