// Package output provides styled terminal output helpers (success, error,
// warning, issue formatting) using lipgloss.
package output

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/td/internal/models"
)

var (
	// Styles
	titleStyle    = lipgloss.NewStyle().Bold(true)
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	priorityStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	statusStyles  = map[models.Status]lipgloss.Style{
		models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		models.StatusInProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		models.StatusBlocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		models.StatusInReview:   lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		models.StatusClosed:     lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
	}
)

// OutputMode determines output format
type OutputMode int

const (
	ModeShort OutputMode = iota
	ModeLong
	ModeJSON
)

// Success prints a success message
func Success(format string, args ...interface{}) {
	fmt.Println(successStyle.Render(fmt.Sprintf(format, args...)))
}

// Error prints an error message
func Error(format string, args ...interface{}) {
	fmt.Println(errorStyle.Render("ERROR: " + fmt.Sprintf(format, args...)))
}

// Warning prints a warning message
func Warning(format string, args ...interface{}) {
	fmt.Println(warningStyle.Render("Warning: " + fmt.Sprintf(format, args...)))
}

// Info prints an info message
func Info(format string, args ...interface{}) {
	fmt.Println(fmt.Sprintf(format, args...))
}

// JSON outputs data as JSON
func JSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// Error codes for structured JSON output
const (
	ErrCodeNotFound          = "not_found"
	ErrCodeInvalidInput      = "invalid_input"
	ErrCodeConflict          = "conflict"
	ErrCodeCannotSelfApprove = "cannot_self_approve"
	ErrCodeHandoffRequired   = "handoff_required"
	ErrCodeDatabaseError     = "database_error"
	ErrCodeGitError          = "git_error"
	ErrCodeNoActiveSession   = "no_active_session"
)

// JSONError outputs an error as JSON
func JSONError(code, message string) {
	fmt.Printf(`{"error":{"code":"%s","message":"%s"}}`, code, message)
	fmt.Println()
}

// JSONErrorWithDetails outputs an error as JSON with additional context
func JSONErrorWithDetails(code, message string, details map[string]interface{}) {
	errObj := map[string]interface{}{
		"code":    code,
		"message": message,
	}
	if len(details) > 0 {
		errObj["details"] = details
	}
	result := map[string]interface{}{
		"error": errObj,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

// FormatStatus formats a status with color
func FormatStatus(s models.Status) string {
	style, ok := statusStyles[s]
	if !ok {
		return string(s)
	}
	return style.Render(fmt.Sprintf("[%s]", s))
}

// FormatPriority formats a priority
func FormatPriority(p models.Priority) string {
	return priorityStyle.Render(fmt.Sprintf("[%s]", p))
}

// FormatPoints returns empty string if points is 0, otherwise "Npts"
func FormatPoints(points int) string {
	if points == 0 {
		return ""
	}
	return fmt.Sprintf("%dpts", points)
}

// FormatPointsSuffix returns "  Npts" if points > 0, empty string otherwise
// Useful for appending to format strings
func FormatPointsSuffix(points int) string {
	if points == 0 {
		return ""
	}
	return fmt.Sprintf("  %dpts", points)
}

// FormatIssueShort formats an issue in short format
func FormatIssueShort(issue *models.Issue) string {
	var parts []string
	parts = append(parts, titleStyle.Render(issue.ID))
	parts = append(parts, FormatPriority(issue.Priority))
	parts = append(parts, issue.Title)

	if issue.Points > 0 {
		parts = append(parts, subtleStyle.Render(fmt.Sprintf("%dpts", issue.Points)))
	}

	parts = append(parts, subtleStyle.Render(string(issue.Type)))
	parts = append(parts, FormatStatus(issue.Status))

	return strings.Join(parts, "  ")
}

// FormatIssueDeleted formats a deleted issue showing [deleted] marker instead of status
func FormatIssueDeleted(issue *models.Issue) string {
	var parts []string
	parts = append(parts, titleStyle.Render(issue.ID))
	parts = append(parts, FormatPriority(issue.Priority))
	parts = append(parts, issue.Title)

	if issue.Points > 0 {
		parts = append(parts, subtleStyle.Render(fmt.Sprintf("%dpts", issue.Points)))
	}

	parts = append(parts, subtleStyle.Render(string(issue.Type)))
	parts = append(parts, errorStyle.Render("[deleted]"))

	return strings.Join(parts, "  ")
}

// FormatIssueLong formats an issue in long format
func FormatIssueLong(issue *models.Issue, logs []models.Log, handoff *models.Handoff) string {
	var sb strings.Builder

	// Header
	sb.WriteString(titleStyle.Render(fmt.Sprintf("%s: %s", issue.ID, issue.Title)))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Status: %s\n", FormatStatus(issue.Status)))
	sb.WriteString(fmt.Sprintf("Type: %s | Priority: %s", issue.Type, issue.Priority))
	if issue.Points > 0 {
		sb.WriteString(fmt.Sprintf(" | Points: %d", issue.Points))
	}
	if issue.Minor {
		sb.WriteString(" | Minor")
	}
	sb.WriteString("\n")

	if len(issue.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(issue.Labels, ", ")))
	}

	// Description
	if issue.Description != "" {
		sb.WriteString("\n")
		sb.WriteString(subtleStyle.Render("Description:"))
		sb.WriteString("\n")
		sb.WriteString(issue.Description)
		sb.WriteString("\n")
	}

	// Acceptance criteria
	if issue.Acceptance != "" {
		sb.WriteString("\n")
		sb.WriteString(subtleStyle.Render("Acceptance Criteria:"))
		sb.WriteString("\n")
		sb.WriteString(issue.Acceptance)
		sb.WriteString("\n")
	}

	// Handoff
	if handoff != nil {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("CURRENT HANDOFF (%s, %s):\n", handoff.SessionID, FormatTimeAgo(handoff.Timestamp)))

		if len(handoff.Done) > 0 {
			sb.WriteString("  Done:\n")
			for _, item := range handoff.Done {
				sb.WriteString(fmt.Sprintf("    - %s\n", item))
			}
		}
		if len(handoff.Remaining) > 0 {
			sb.WriteString("  Remaining:\n")
			for _, item := range handoff.Remaining {
				sb.WriteString(fmt.Sprintf("    - %s\n", item))
			}
		}
		if len(handoff.Decisions) > 0 {
			sb.WriteString("  Decisions:\n")
			for _, item := range handoff.Decisions {
				sb.WriteString(fmt.Sprintf("    - %s\n", item))
			}
		}
		if len(handoff.Uncertain) > 0 {
			sb.WriteString("  Uncertain:\n")
			for _, item := range handoff.Uncertain {
				sb.WriteString(fmt.Sprintf("    - %s\n", item))
			}
		}
	}

	// Session log
	if len(logs) > 0 {
		sb.WriteString("\nSESSION LOG:\n")
		for _, log := range logs {
			typeIndicator := ""
			if log.Type != models.LogTypeProgress {
				typeIndicator = fmt.Sprintf(" [%s]", log.Type)
			}
			sb.WriteString(fmt.Sprintf("  [%s]%s %s\n",
				log.Timestamp.Format("15:04"),
				typeIndicator,
				log.Message))
		}
	}

	// Review status
	if issue.Status == models.StatusInReview {
		sb.WriteString("\nAWAITING REVIEW - requires different session to approve/reject\n")
	}

	return sb.String()
}

// FormatTimeAgo formats a time as a human-readable "ago" string
func FormatTimeAgo(t time.Time) string {
	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("2006-01-02")
	}
}

// ShortSHA safely shortens a git SHA to 7 characters or returns as-is if shorter
func ShortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// FormatGitState formats git state for display
func FormatGitState(sha, branch string, dirty int) string {
	state := fmt.Sprintf("%s (%s)", ShortSHA(sha), branch)
	if dirty > 0 {
		state += fmt.Sprintf(" %d dirty", dirty)
	} else {
		state += " clean"
	}
	return state
}

// IssueOneLiner returns a concise single-line issue representation
// Format: "td-abc1: Title [status]" or "td-abc1 \"Title\" [status]" with quotes
func IssueOneLiner(issue *models.Issue) string {
	return fmt.Sprintf("%s \"%s\" %s", issue.ID, issue.Title, FormatStatus(issue.Status))
}

// IssueOneLinerPlain returns issue one-liner without status styling (for text contexts)
func IssueOneLinerPlain(issue *models.Issue) string {
	return fmt.Sprintf("%s \"%s\" [%s]", issue.ID, issue.Title, issue.Status)
}

// StatusBadge returns a status indicator with symbol
// e.g., "○ open", "▶ in_progress", "✓ closed", "✗ blocked", "◎ in_review"
func StatusBadge(status models.Status) string {
	symbols := map[models.Status]string{
		models.StatusOpen:       "○",
		models.StatusInProgress: "▶",
		models.StatusBlocked:    "✗",
		models.StatusInReview:   "◎",
		models.StatusClosed:     "✓",
	}
	symbol, ok := symbols[status]
	if !ok {
		symbol = "?"
	}
	style, hasStyle := statusStyles[status]
	if hasStyle {
		return style.Render(fmt.Sprintf("%s %s", symbol, status))
	}
	return fmt.Sprintf("%s %s", symbol, status)
}

// SectionHeader returns a formatted section header for CLI output
// e.g., "\nDEPENDENCIES:\n"
func SectionHeader(title string) string {
	return fmt.Sprintf("\n%s:\n", strings.ToUpper(title))
}

// IndentLines indents each line by the specified number of spaces
func IndentLines(lines []string, spaces int) []string {
	indent := strings.Repeat(" ", spaces)
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = indent + line
	}
	return result
}

// IndentString indents each line in a string by the specified number of spaces
func IndentString(s string, spaces int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	indented := IndentLines(lines, spaces)
	return strings.Join(indented, "\n")
}

// BulletList formats items as a bulleted list with optional indentation
func BulletList(items []string, indent int) []string {
	prefix := strings.Repeat(" ", indent)
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = prefix + "- " + item
	}
	return result
}

// DependencyLine formats a dependency with optional status mark
// e.g., "  td-abc1: Title [status] ✓"
func DependencyLine(issue *models.Issue, showResolved bool) string {
	statusMark := ""
	if showResolved && issue.Status == models.StatusClosed {
		statusMark = " ✓"
	}
	return fmt.Sprintf("    %s: %s %s%s", issue.ID, issue.Title, FormatStatus(issue.Status), statusMark)
}
