// Package output provides styled terminal output helpers (success, error,
// warning, issue formatting) using lipgloss.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
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

// The stream split here is the contract that keeps `--json` usable:
//
//	stdout = the command's RESULT (JSON envelopes, listings, confirmations)
//	stderr = DIAGNOSTICS about the command (errors, warnings, notices)
//
// Error and Warning are diagnostics, so they go to stderr unconditionally —
// not "only when --json is set". A great many commands print a human error
// line and then emit a JSON envelope; routing by mode would mean auditing
// every one of those ~68 sites to pass a mode down, and would still leave
// stdout carrying two kinds of thing. Sending diagnostics to stderr always is
// both the smaller change and what every other CLI does, so piping stdout to
// a parser works without the caller having to know which code path ran.
//
// Success and Info stay on stdout: their content IS the result (see below).

// Success prints a success message. This is the human-mode RESULT of a
// mutation — the counterpart of the JSON envelope emitted on the --json path,
// and normally in the sibling branch of the same if/else. It stays on stdout:
// a user running `td add ... > out.txt` expects the confirmation in the file,
// and moving it would hide the command's own answer from anyone reading
// stdout. It is not a diagnostic.
func Success(format string, args ...interface{}) {
	fmt.Println(successStyle.Render(fmt.Sprintf(format, args...)))
}

// Error prints an error message to stderr. It is a diagnostic about the
// command, never the command's result, and it is frequently printed on the
// same path that then emits a JSON error envelope on stdout — printing both
// to stdout leaves a --json caller with unparseable output. A terminal user
// sees no difference: stderr is unbuffered and interleaved on a tty.
func Error(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("ERROR: "+fmt.Sprintf(format, args...)))
}

// Warning prints a warning message to stderr, for the same reason as Error: a
// warning annotates the command, it is not what the command was asked to
// produce. (This function used to print to stdout, which is why a separate
// WarningErr existed; now that Warning itself is safe, WarningErr is gone and
// its call sites use Warning.)
func Warning(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, warningStyle.Render("Warning: "+fmt.Sprintf(format, args...)))
}

// Info prints an info message to stdout. Unlike Error and Warning, Info's
// content is primary output — "No boards found" is the literal answer to
// `td board list`, and moving it to stderr would empty stdout for a command
// that succeeded. Info is the one of these four where a stderr move would be
// a new bug rather than a fix, so it stays put; anything genuinely
// out-of-band should call Warning instead.
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

// EmitIssue emits a standard JSON envelope for a mutation that produced or
// affected a single issue. The envelope is {"id","status","action","issue"}
// where "issue" is the full issue object (relying on models.Issue json tags).
// Keys from extra are merged last and override the defaults, allowing callers
// to attach additional context (e.g. {"session": "..."}). This lives in the
// output package (alongside JSON) because models has no internal imports, so
// importing models here creates no cycle.
func EmitIssue(action string, issue *models.Issue, extra map[string]any) error {
	payload := map[string]any{
		"action": action,
		"issue":  issue,
	}
	if issue != nil {
		payload["id"] = issue.ID
		payload["status"] = issue.Status
	}
	for k, v := range extra {
		payload[k] = v
	}
	return JSON(payload)
}

// EmitResult emits a standard JSON envelope for a mutation that is not tied to
// a single issue. The envelope is {"action"} merged with any keys from extra
// (extra overrides).
func EmitResult(action string, extra map[string]any) error {
	payload := map[string]any{
		"action": action,
	}
	for k, v := range extra {
		payload[k] = v
	}
	return JSON(payload)
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

// jsonErrorBody is the inner error object for the JSON error envelope.
// json.Marshal of these structs guarantees proper escaping of quotes,
// backslashes, and newlines in code/message/details.
type jsonErrorBody struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type jsonErrorEnvelope struct {
	Error jsonErrorBody `json:"error"`
}

// JSONError outputs an error as JSON. The envelope shape is
// {"error":{"code":"...","message":"..."}} emitted on a single line. It encodes
// via json so that a message containing quotes, backslashes, or newlines still
// produces valid, parseable JSON. HTML escaping is disabled so that characters
// like <, >, and & are emitted verbatim, matching the prior printf-based output
// byte-for-byte for messages that do not require JSON escaping.
func JSONError(code, message string) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(jsonErrorEnvelope{
		Error: jsonErrorBody{Code: code, Message: message},
	}); err != nil {
		// Encode of plain strings cannot realistically fail; fall back to a
		// minimal valid envelope so callers always get parseable output.
		fmt.Println(`{"error":{"code":"internal_error","message":"failed to encode error"}}`)
		return
	}
	// Encoder already appends a trailing newline; print as-is.
	fmt.Print(buf.String())
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
	// Short output has many call sites (list, query, search, and show), so keep
	// its only free-text field safe at the renderer boundary instead of relying
	// on every caller to remember SanitizedForDisplay.
	parts = append(parts, sanitizeIssueTitle(issue.Title))

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
	fmt.Fprintf(&sb, "Status: %s\n", FormatStatus(issue.Status))
	fmt.Fprintf(&sb, "Type: %s | Priority: %s", issue.Type, issue.Priority)
	if issue.Points > 0 {
		fmt.Fprintf(&sb, " | Points: %d", issue.Points)
	}
	if issue.Minor {
		sb.WriteString(" | Minor")
	}
	sb.WriteString("\n")

	if len(issue.Labels) > 0 {
		fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	if issue.DeferUntil != nil {
		fmt.Fprintf(&sb, "Deferred until: %s", *issue.DeferUntil)
		if issue.DeferCount > 0 {
			fmt.Fprintf(&sb, " (deferred %dx)", issue.DeferCount)
		}
		sb.WriteString("\n")
	}
	if issue.DueDate != nil {
		fmt.Fprintf(&sb, "Due: %s\n", *issue.DueDate)
	}

	// Description
	if issue.Description != "" {
		sb.WriteString("\n")
		sb.WriteString(subtleStyle.Render("Description:"))
		sb.WriteString("\n")
		// NOT sanitized here: by this point the description may already be
		// glamour-rendered, and stripping ESC from that output leaves literal
		// "[38;5;252m" garbage all over `td show -m`. Stored text is sanitized
		// before rendering — see SanitizeIssueText and its callers.
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
		fmt.Fprintf(&sb, "CURRENT HANDOFF (%s, %s):\n", handoff.SessionID, FormatTimeAgo(handoff.Timestamp))

		if len(handoff.Done) > 0 {
			sb.WriteString("  Done:\n")
			for _, item := range handoff.Done {
				fmt.Fprintf(&sb, "    - %s\n", IndentContinuation(item))
			}
		}
		if len(handoff.Remaining) > 0 {
			sb.WriteString("  Remaining:\n")
			for _, item := range handoff.Remaining {
				fmt.Fprintf(&sb, "    - %s\n", IndentContinuation(item))
			}
		}
		if len(handoff.Decisions) > 0 {
			sb.WriteString("  Decisions:\n")
			for _, item := range handoff.Decisions {
				fmt.Fprintf(&sb, "    - %s\n", IndentContinuation(item))
			}
		}
		if len(handoff.Uncertain) > 0 {
			sb.WriteString("  Uncertain:\n")
			for _, item := range handoff.Uncertain {
				fmt.Fprintf(&sb, "    - %s\n", IndentContinuation(item))
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
			fmt.Fprintf(&sb, "  [%s]%s %s\n",
				log.Timestamp.Format("15:04"),
				typeIndicator,
				IndentContinuation(log.Message))
		}
	}

	// Review status
	if issue.Status == models.StatusInReview {
		sb.WriteString("\nAWAITING REVIEW - requires an implementation-independent session to approve/reject\n")
	}

	return sb.String()
}

// SanitizedForDisplay returns a copy of the issue with its terminal-facing
// free text sanitized. Call it at every FormatIssueLong call site, on the
// STORED issue, before any markdown rendering. FormatIssueShort sanitizes its
// title internally because its many callers do not render the other prose.
//
// It exists because hand-copying SanitizeIssueText at each call site already
// failed once: two `td show` paths got it and `td list --long` — which renders
// the identical block from the identical function — did not. One helper makes
// the omission impossible to repeat by accident.
func SanitizedForDisplay(issue *models.Issue) *models.Issue {
	if issue == nil {
		return nil
	}
	clean := *issue
	clean.Title = sanitizeIssueTitle(clean.Title)
	clean.Description = SanitizeIssueText(clean.Description)
	clean.Acceptance = SanitizeIssueText(clean.Acceptance)
	return &clean
}

// sanitizeIssueTitle prepares the issue title for its single-line terminal
// slots. Unlike description and acceptance, a title has no meaningful
// multi-line form: retaining a newline lets stored text start a forged section
// immediately below the real issue header. SanitizeIssueText first strips
// cursor controls and normalizes CR/CRLF, then newlines are collapsed.
func sanitizeIssueTitle(s string) string {
	return strings.ReplaceAll(SanitizeIssueText(s), "\n", " ")
}

// SanitizeIssueText prepares stored free text for display: it strips control
// characters (see SanitizeRendered) and normalizes carriage returns to
// newlines.
//
// Call this on STORED text before any rendering step. Sanitizing rendered
// output instead destroys the escape sequences the renderer legitimately
// emitted — glamour output run through SanitizeRendered comes out as walls of
// literal "[38;5;252m".
//
// CR normalization matters on its own: SanitizeRendered preserves \r so that
// callers doing their own line handling see it, but a bare CR left in a prose
// block returns the cursor to column zero and lets stored text forge a
// flush-left section header with no residue at all.
func SanitizeIssueText(s string) string {
	s = SanitizeRendered(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// SanitizeRendered strips control characters that let stored text forge
// structure in terminal output. Escape sequences are the sharper half of the
// problem: ESC[E moves the cursor to the next line with no newline byte in the
// data, so a message can start what looks like a fresh log entry at column
// zero while passing any check that only looks for "\n". Cursor movement,
// colour, and erase sequences can equally overwrite lines already drawn.
//
// td applies its own styling around this text, so the DATA never has a
// legitimate reason to carry ESC or other C0 controls. Tab and newline are
// preserved; newline handling is layered on by the callers that need it.
func SanitizeRendered(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool {
		return r == 0x1b || (unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r')
	}) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			b.WriteRune(r)
		case r == 0x1b || unicode.IsControl(r):
			// Drop silently rather than substituting a visible marker: the
			// goal is that the text cannot move the cursor, and a marker would
			// itself become a way to fake output.
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// indentLogContinuation makes multi-line log messages read as one entry.
//
// Log text is free-form and reaches this renderer from `--reason`, `--message`,
// `td log`, the API, and sync — none of which forbid newlines. Rendered flush
// left, a crafted message forges convincing extra entries:
//
//	td approve <id> --self-review --reason "ok
//	[09:15] Approved by: security-team"
//
// That mattered less when the audit trail was a secondary record. It matters
// now that an unverifiable attestation is what stands in for mechanical review
// independence: a reader who cannot trust the log cannot audit the attestation.
//
// Continuation lines are prefixed so they cannot be mistaken for their own
// entry, rather than rejecting newlines at input — the text may have arrived
// from a peer or an older client, so the renderer is the only place that sees
// every path. Carriage returns are normalized for the same reason.
func IndentContinuation(msg string) string {
	msg = SanitizeRendered(msg)
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	if !strings.Contains(msg, "\n") {
		return msg
	}
	lines := strings.Split(msg, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "         | " + lines[i]
	}
	return strings.Join(lines, "\n")
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
