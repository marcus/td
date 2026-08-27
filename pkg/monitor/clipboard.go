package monitor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/marcus/td/internal/models"
)

// copyToClipboard copies text to the system clipboard.
// Uses pbcopy on macOS, xclip on Linux, clip.exe on Windows.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, fall back to xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool found (install xclip or xsel)")
		}
	case "windows":
		cmd = exec.Command("clip.exe")
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	if _, err := stdin.Write([]byte(text)); err != nil {
		return err
	}

	if err := stdin.Close(); err != nil {
		return err
	}

	return cmd.Wait()
}

// formatIssueAsMarkdown formats an issue as markdown for clipboard.
func formatIssueAsMarkdown(issue *models.Issue) string {
	var sb strings.Builder

	// Title with ID
	fmt.Fprintf(&sb, "# %s\n", issue.Title)
	fmt.Fprintf(&sb, "**ID:** `%s`\n", issue.ID)

	// Metadata
	fmt.Fprintf(&sb, "**Type:** %s | **Priority:** %s | **Status:** %s\n",
		issue.Type, issue.Priority, issue.Status)

	// Parent epic if set
	if issue.ParentID != "" {
		fmt.Fprintf(&sb, "**Parent:** `%s`\n", issue.ParentID)
	}

	// Description
	if issue.Description != "" {
		sb.WriteString("\n## Description\n\n")
		sb.WriteString(issue.Description)
		sb.WriteString("\n")
	}

	// Acceptance criteria
	if issue.Acceptance != "" {
		sb.WriteString("\n## Acceptance Criteria\n\n")
		sb.WriteString(issue.Acceptance)
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatEpicAsMarkdown formats an epic with all its child stories as markdown.
func formatEpicAsMarkdown(epic *models.Issue, children []models.Issue) string {
	var sb strings.Builder

	// Epic header
	fmt.Fprintf(&sb, "# Epic: %s\n", epic.Title)
	fmt.Fprintf(&sb, "**ID:** `%s`\n", epic.ID)
	fmt.Fprintf(&sb, "**Priority:** %s | **Status:** %s\n", epic.Priority, epic.Status)

	// Epic description
	if epic.Description != "" {
		sb.WriteString("\n## Description\n\n")
		sb.WriteString(epic.Description)
		sb.WriteString("\n")
	}

	// Acceptance criteria
	if epic.Acceptance != "" {
		sb.WriteString("\n## Acceptance Criteria\n\n")
		sb.WriteString(epic.Acceptance)
		sb.WriteString("\n")
	}

	// Child stories/tasks with full details
	if len(children) > 0 {
		sb.WriteString("\n## Tasks\n\n")
		for i, child := range children {
			if i > 0 {
				sb.WriteString("\n---\n\n")
			}
			statusIcon := statusIcon(child.Status)
			fmt.Fprintf(&sb, "### %s %s\n", statusIcon, child.Title)
			fmt.Fprintf(&sb, "**ID:** `%s`\n", child.ID)
			fmt.Fprintf(&sb, "**Type:** %s | **Priority:** %s | **Status:** %s\n",
				child.Type, child.Priority, child.Status)

			if child.Description != "" {
				sb.WriteString("\n#### Description\n\n")
				sb.WriteString(child.Description)
				sb.WriteString("\n")
			}

			if child.Acceptance != "" {
				sb.WriteString("\n#### Acceptance Criteria\n\n")
				sb.WriteString(child.Acceptance)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// statusIcon returns a status indicator for markdown.
func statusIcon(status models.Status) string {
	switch status {
	case models.StatusClosed:
		return "[x]"
	case models.StatusInProgress:
		return "[-]"
	case models.StatusInReview:
		return "[~]"
	case models.StatusBlocked:
		return "[!]"
	default:
		return "[ ]"
	}
}
