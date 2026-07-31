// Package agent provides utilities for detecting and configuring AI agent instruction files.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// Versioned markers let future td releases update their own guidance without
	// disturbing the rest of a project's agent file.
	InstructionStartMarker   = "<!-- td-agent-instructions:start -->"
	InstructionVersion       = 3
	instructionVersionPrefix = "<!-- td-agent-instructions:version="
	instructionVersionSuffix = " -->"
	InstructionVersionMarker = instructionVersionPrefix + "3" + instructionVersionSuffix
	InstructionEndMarker     = "<!-- td-agent-instructions:end -->"

	// InstructionBody is intentionally compact because agent files consume
	// context on every turn. Detailed workflow guidance remains available from
	// `td usage` and command-specific help.
	InstructionBody = `## Working with td

td keeps task context durable across sessions. In a new context, run ` + "`td usage --new-session -q`" + ` to see current work.

Use your judgment about how much tracking a task needs. For substantive work: ` + "`td start <id>`" + `, record progress with ` + "`td log`" + `, hand off with ` + "`td handoff <id>`" + `, then ` + "`td review <id>`" + `.

Closing needs a review. Say who did it (default trusted mode; delegated/strict allow only the first):

- independent session: ` + "`td approve <id> --reason \"...\"`" + `
- a sub-agent: ` + "`td approve <id> --reviewed-by \"<who>\"`" + `
- you: ` + "`td approve <id> --self-review --reason \"...\"`" + `

Prefer a reviewer with its own ` + "`TD_CONTEXT_ID`" + `; never name one who did not review.

Run ` + "`td usage`" + ` or ` + "`td <command> --help`" + `.`

	// InstructionText is the complete versioned block installed in agent files.
	InstructionText = InstructionStartMarker + "\n" + InstructionVersionMarker + "\n\n" + InstructionBody + "\n\n" + InstructionEndMarker + "\n"
)

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

// HasTDInstructions checks if the file already contains td guidance.
func HasTDInstructions(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(content)
	return strings.Contains(text, InstructionStartMarker) ||
		strings.Contains(text, "td usage")
}

// AnyFileHasTDInstructions checks all known agent files in baseDir for td guidance.
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

// markedInstructionsVersion returns the version of a td-owned block. A marked
// block without a parseable version is treated as version 0 by its caller.
func markedInstructionsVersion(text string) (int, bool) {
	start := strings.Index(text, instructionVersionPrefix)
	if start == -1 {
		return 0, false
	}
	valueStart := start + len(instructionVersionPrefix)
	valueEndOffset := strings.Index(text[valueStart:], instructionVersionSuffix)
	if valueEndOffset == -1 {
		return 0, false
	}
	version, err := strconv.Atoi(text[valueStart : valueStart+valueEndOffset])
	if err != nil {
		return 0, false
	}
	return version, true
}

// OutdatedMarkedInstructionsFile returns the first recognized agent file with
// a td-owned block from an older version. Unmarked legacy guidance is excluded
// because td cannot safely determine which surrounding text it owns. Blocks
// from newer td versions are left untouched to prevent accidental downgrades.
func OutdatedMarkedInstructionsFile(baseDir string) string {
	for _, name := range KnownAgentFiles {
		path := filepath.Join(baseDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(content)
		if !strings.Contains(text, InstructionStartMarker) ||
			!strings.Contains(text, InstructionEndMarker) {
			continue
		}
		version, ok := markedInstructionsVersion(text)
		if !ok || version < InstructionVersion {
			return path
		}
	}
	return ""
}

// InstallInstructions adds or updates td guidance in an agent file.
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

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	if strings.Contains(text, InstructionStartMarker) &&
		strings.Contains(text, InstructionEndMarker) {
		if version, ok := markedInstructionsVersion(text); ok && version > InstructionVersion {
			return fmt.Errorf("td guidance version %d is newer than supported version %d; leaving it unchanged", version, InstructionVersion)
		}
	}
	if updated, ok := replaceMarkedInstructions(text); ok {
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// File exists - prepend instructions
	return prependToFile(path, InstructionText)
}

// replaceMarkedInstructions updates only the block owned by td. Stable boundary
// markers make upgrades safe; the version marker identifies the installed copy.
func replaceMarkedInstructions(content string) (string, bool) {
	start := strings.Index(content, InstructionStartMarker)
	if start == -1 {
		return "", false
	}

	endOffset := strings.Index(content[start:], InstructionEndMarker)
	if endOffset == -1 {
		return "", false
	}
	end := start + endOffset + len(InstructionEndMarker)

	replacement := strings.TrimSuffix(InstructionText, "\n")
	return content[:start] + replacement + content[end:], true
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
