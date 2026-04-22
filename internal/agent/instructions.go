// Package agent provides utilities for detecting and configuring AI agent instruction files.
package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// InstructionText is the mandatory td usage instructions to add to agent files.
const InstructionText = `## MANDATORY: Use td for Task Management

Run td usage --new-session at conversation start (or after /clear). This tells you what to work on next.

Sessions are automatic (based on terminal/agent context). Optional:
- td session "name" to label the current session
- td session --new to force a new session in the same context

Do NOT start a new session mid-work to satisfy td review rules. Use a real
reviewer sub-agent or separate agent context. An independent review is required;
the close may be delegated to any involved session.

You cannot review your own implementation, but you can close after an
independent review has been recorded. Under review_policy_mode=delegated:
  td approve <id> --record-only --reason "..."   # reviewer records approval
  td approve <id>                                 # any involved session closes

Use td usage -q after first read.
`

// KnownAgentFiles lists agent instruction files in priority order.
// AGENTS.md is preferred since td supports multiple agent types.
var KnownAgentFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"CLAUDE.local.md",
	"GEMINI.md",
	"GEMINI.local.md",
	"CODEX.md",
	"COPILOT.md",
	"CURSOR.md",
	".github/copilot-instructions.md",
}

// DetectAgentFile finds the first existing agent file in baseDir.
// Returns the full path if found, empty string if none exist.
func DetectAgentFile(baseDir string) string {
	for _, name := range KnownAgentFiles {
		path := filepath.Join(baseDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// PreferredAgentFile returns the best agent file to use for installation.
// Priority:
// 1. If AGENTS.md exists, use it (td supports many agents)
// 2. Use the first existing known agent file
// 3. If none exist, prefer AGENTS.md for new installations
func PreferredAgentFile(baseDir string) string {
	agentsPath := filepath.Join(baseDir, "AGENTS.md")

	// If AGENTS.md exists, always prefer it
	if fileExists(agentsPath) {
		return agentsPath
	}

	// Use the first existing known agent file
	for _, name := range KnownAgentFiles[1:] { // skip AGENTS.md, already checked
		path := filepath.Join(baseDir, name)
		if fileExists(path) {
			return path
		}
	}

	// None exist - prefer AGENTS.md for new installations
	return agentsPath
}

// HasTDInstructions checks if the file already contains td instructions.
func HasTDInstructions(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(content), "td usage")
}

// AnyFileHasTDInstructions checks all known agent files in baseDir for td instructions.
// Returns true if any file already contains the instructions (dedup check).
func AnyFileHasTDInstructions(baseDir string) bool {
	for _, name := range KnownAgentFiles {
		path := filepath.Join(baseDir, name)
		if HasTDInstructions(path) {
			return true
		}
	}
	return false
}

// InstallInstructions adds td instructions to an agent file.
// Creates the file if it doesn't exist.
func InstallInstructions(path string) error {
	// If file doesn't exist, create it with just the instructions
	if !fileExists(path) {
		// Ensure parent directory exists
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(InstructionText), 0644)
	}

	// File exists - prepend instructions
	return prependToFile(path, InstructionText)
}

// prependToFile adds text at a smart location in the file.
// Inserts after any YAML frontmatter and initial # heading.
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

// fileExists returns true if the path exists and is a file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
