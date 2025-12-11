package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		sess, err := session.GetOrCreate(baseDir)
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

// agentInstructionText is the text to add to agent files
const agentInstructionText = `## MANDATORY: Use td for Task Management

Run td session --new "name" then td usage for workflow. This tells you what to work on next. Use td usage -q after first read.
`

// knownAgentFiles lists agent instruction files in priority order
var knownAgentFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
	"COPILOT.md",
	"CURSOR.md",
	".github/copilot-instructions.md",
}

func suggestAgentFileAddition(baseDir string) {
	fmt.Println()

	// Check for existing agent files
	var foundFile string
	for _, name := range knownAgentFiles {
		path := filepath.Join(baseDir, name)
		if _, err := os.Stat(path); err == nil {
			foundFile = path
			break
		}
	}

	if foundFile != "" {
		// Check if already contains td instruction
		content, err := os.ReadFile(foundFile)
		if err == nil && strings.Contains(string(content), "td session") {
			return // Already has td instructions
		}

		fmt.Printf("Found %s. Add td instructions?\n", filepath.Base(foundFile))
		fmt.Println()
		fmt.Println("Text to add:")
		fmt.Println("---")
		fmt.Print(agentInstructionText)
		fmt.Println("---")
		fmt.Println()
		fmt.Print("Add to file? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "y" || response == "yes" {
			if err := prependToFile(foundFile, agentInstructionText); err != nil {
				output.Error("failed to update %s: %v", filepath.Base(foundFile), err)
			} else {
				output.Success("Added td instructions to %s", filepath.Base(foundFile))
			}
		}
	} else {
		// No agent file found, just show suggestion
		fmt.Println("Tip: Add this to your CLAUDE.md, AGENTS.md, or similar agent file:")
		fmt.Println()
		fmt.Print(agentInstructionText)
	}
}

func prependToFile(path string, text string) error {
	// Read existing content
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Find safe insertion point (after any frontmatter or initial heading)
	contentStr := string(content)
	insertPos := 0

	// Skip YAML frontmatter if present
	if strings.HasPrefix(contentStr, "---") {
		if endIdx := strings.Index(contentStr[3:], "---"); endIdx != -1 {
			insertPos = endIdx + 6 // Skip past closing ---
			// Skip any newlines after frontmatter
			for insertPos < len(contentStr) && contentStr[insertPos] == '\n' {
				insertPos++
			}
		}
	}

	// Skip initial # heading if present at insertion point
	if insertPos < len(contentStr) && contentStr[insertPos] == '#' {
		if nlIdx := strings.Index(contentStr[insertPos:], "\n"); nlIdx != -1 {
			insertPos += nlIdx + 1
			// Skip blank lines after heading
			for insertPos < len(contentStr) && contentStr[insertPos] == '\n' {
				insertPos++
			}
		}
	}

	// Build new content
	var newContent strings.Builder
	newContent.WriteString(contentStr[:insertPos])
	newContent.WriteString(text)
	newContent.WriteString("\n")
	newContent.WriteString(contentStr[insertPos:])

	return os.WriteFile(path, []byte(newContent.String()), 0644)
}

func init() {
	rootCmd.AddCommand(initCmd)
}
