package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// HelpSection represents a group of bindings in help text
type HelpSection struct {
	Title    string
	Bindings []HelpBinding
}

// HelpBinding represents a single binding for display
type HelpBinding struct {
	Keys        string // Combined keys like "j / k" or "↑ / ↓"
	Description string
}

// GenerateHelp generates help text from the registry bindings
func (r *Registry) GenerateHelp() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("\nMONITOR TUI - Key Bindings\n")

	// Build navigation section manually for better grouping
	sb.WriteString("\nNAVIGATION:\n")
	navBindings := []HelpBinding{
		{Keys: "Tab / Shift+Tab", Description: "Switch between panels"},
		{Keys: "1 / 2 / 3", Description: "Jump to panel"},
		{Keys: "↑ / ↓ / j / k", Description: "Move cursor"},
		{Keys: "Ctrl+d / Ctrl+u", Description: "Half page down/up"},
		{Keys: "Ctrl+f / Ctrl+b", Description: "Full page down/up"},
		{Keys: "G / g g", Description: "Jump to bottom/top"},
		{Keys: "Enter", Description: "Open issue details"},
	}
	for _, b := range navBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nMODALS:\n")
	modalBindings := []HelpBinding{
		{Keys: "↑ / ↓ / j / k", Description: "Scroll (k at top focuses parent epic)"},
		{Keys: "Ctrl+d / Ctrl+u", Description: "Half page down/up"},
		{Keys: "← / → / h / l", Description: "Navigate prev/next issue"},
		{Keys: "Enter", Description: "Open focused epic / close modal"},
		{Keys: "Esc", Description: "Close modal (return to previous)"},
		{Keys: "r", Description: "Refresh modal content"},
		{Keys: "Tab", Description: "Focus epic task list (if epic)"},
	}
	for _, b := range modalBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nEPIC TASKS (when focused):\n")
	epicBindings := []HelpBinding{
		{Keys: "↑ / ↓ / j / k", Description: "Select task in list"},
		{Keys: "Enter", Description: "Open selected task"},
		{Keys: "Tab", Description: "Exit task list"},
		{Keys: "Esc", Description: "Close modal"},
	}
	for _, b := range epicBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nACTIONS:\n")
	actionBindings := []HelpBinding{
		{Keys: "r", Description: "Mark for review (Current Work) / Refresh"},
		{Keys: "a", Description: "Approve issue (Task List reviewable)"},
		{Keys: "x", Description: "Delete issue (confirmation required)"},
		{Keys: "s", Description: "Show statistics dashboard"},
		{Keys: "h", Description: "Show handoffs modal"},
		{Keys: "S", Description: "Cycle sort (priority/created/updated)"},
		{Keys: "/", Description: "Search tasks"},
		{Keys: "c", Description: "Toggle closed tasks"},
		{Keys: "q / Ctrl+C", Description: "Quit"},
	}
	for _, b := range actionBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nHANDOFFS MODAL:\n")
	handoffBindings := []HelpBinding{
		{Keys: "↑ / ↓ / j / k", Description: "Select handoff"},
		{Keys: "Enter", Description: "Open issue for selected handoff"},
		{Keys: "Esc", Description: "Close handoffs modal"},
		{Keys: "r", Description: "Refresh handoffs"},
	}
	for _, b := range handoffBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nSEARCH (TDQ Query Language):\n")
	searchBindings := []HelpBinding{
		{Keys: "Enter", Description: "Confirm search"},
		{Keys: "Esc", Description: "Cancel search"},
		{Keys: "Backspace", Description: "Delete character"},
		{Keys: "?", Description: "Show TDQ syntax help"},
	}
	for _, b := range searchBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nPress ? to close help\n")

	return sb.String()
}

// GenerateTDQHelp generates help text for TDQ query language
func (r *Registry) GenerateTDQHelp() string {
	var sb strings.Builder

	sb.WriteString("\nTDQ QUERY LANGUAGE - Search Syntax\n")
	sb.WriteString("═══════════════════════════════════\n\n")

	sb.WriteString("BASIC OPERATORS:\n")
	ops := []HelpBinding{
		{Keys: "field = value", Description: "Exact match"},
		{Keys: "field != value", Description: "Not equal"},
		{Keys: `field ~ "text"`, Description: "Contains (case-insensitive)"},
		{Keys: "field < value", Description: "Less than"},
		{Keys: "field > value", Description: "Greater than"},
		{Keys: "field <= value", Description: "Less than or equal"},
		{Keys: "field >= value", Description: "Greater than or equal"},
	}
	for _, b := range ops {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nBOOLEAN LOGIC:\n")
	bools := []HelpBinding{
		{Keys: "expr AND expr", Description: "Both must match"},
		{Keys: "expr OR expr", Description: "Either matches"},
		{Keys: "NOT expr", Description: "Negation"},
		{Keys: "(expr)", Description: "Grouping"},
	}
	for _, b := range bools {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nFIELDS:\n")
	fields := []HelpBinding{
		{Keys: "status", Description: "open, in_progress, blocked, in_review, closed"},
		{Keys: "type", Description: "bug, feature, task, epic, chore"},
		{Keys: "priority", Description: "P0, P1, P2, P3, P4"},
		{Keys: "points", Description: "1, 2, 3, 5, 8, 13, 21"},
		{Keys: "labels", Description: "comma-separated tags"},
		{Keys: "title / description", Description: "text search"},
		{Keys: "created / updated", Description: "date fields"},
		{Keys: "implementer / reviewer", Description: "session IDs"},
	}
	for _, b := range fields {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nFUNCTIONS:\n")
	funcs := []HelpBinding{
		{Keys: "has(field)", Description: "Field is not empty"},
		{Keys: "is(status)", Description: "Shorthand status check"},
		{Keys: "any(field, v1, v2)", Description: "Field matches any value"},
		{Keys: "descendant_of(id)", Description: "Children of epic"},
	}
	for _, b := range funcs {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nCROSS-ENTITY:\n")
	cross := []HelpBinding{
		{Keys: `log.message ~ "x"`, Description: "Search log messages"},
		{Keys: "log.type = blocker", Description: "Filter by log type"},
		{Keys: `comment.text ~ "x"`, Description: "Search comments"},
		{Keys: "file.role = test", Description: "Linked file role"},
	}
	for _, b := range cross {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nSPECIAL VALUES:\n")
	special := []HelpBinding{
		{Keys: "@me", Description: "Current session"},
		{Keys: "today / -7d", Description: "Relative dates"},
		{Keys: "EMPTY", Description: "Empty/null field"},
	}
	for _, b := range special {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nSORTING:\n")
	sortOps := []HelpBinding{
		{Keys: "sort:priority", Description: "Sort by priority (default)"},
		{Keys: "sort:-created", Description: "Newest first"},
		{Keys: "sort:-updated", Description: "Recently updated first"},
		{Keys: "sort:created", Description: "Oldest first"},
	}
	for _, b := range sortOps {
		sb.WriteString(fmt.Sprintf("  %-22s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nEXAMPLES:\n")
	examples := []string{
		`  type = bug AND priority <= P1`,
		`  status = open AND created >= -7d`,
		`  sort:-created status = open`,
		`  implementer = @me AND is(in_progress)`,
		`  log.type = blocker`,
	}
	for _, ex := range examples {
		sb.WriteString(ex + "\n")
	}

	sb.WriteString("\nPress ? to close | Plain text = simple search\n")

	return sb.String()
}

// FooterHelp generates a compact help string for the footer
func (r *Registry) FooterHelp() string {
	return "q:quit s:stats h:handoffs S:sort /:search c:closed r:review a:approve x:del tab:panel ?:help"
}

// ModalFooterHelp generates help text for the modal footer
func (r *Registry) ModalFooterHelp() string {
	return "↑↓:scroll  Ctrl+d/u:½page  ←→:prev/next  esc:close  r:refresh"
}

// StatsFooterHelp generates help text for the stats modal footer
func (r *Registry) StatsFooterHelp() string {
	return "↑↓:scroll  Ctrl+d/u:½page  esc:close  r:refresh"
}

// CommandHelp returns help info for a specific command
func CommandHelp(cmd Command) string {
	switch cmd {
	case CmdQuit:
		return "Exit the monitor"
	case CmdToggleHelp:
		return "Show/hide keyboard shortcuts"
	case CmdRefresh:
		return "Refresh data from database"
	case CmdNextPanel:
		return "Move focus to next panel"
	case CmdPrevPanel:
		return "Move focus to previous panel"
	case CmdCursorDown:
		return "Move cursor down one row"
	case CmdCursorUp:
		return "Move cursor up one row"
	case CmdCursorTop:
		return "Jump to first row"
	case CmdCursorBottom:
		return "Jump to last row"
	case CmdHalfPageDown:
		return "Scroll down half a page"
	case CmdHalfPageUp:
		return "Scroll up half a page"
	case CmdFullPageDown:
		return "Scroll down a full page"
	case CmdFullPageUp:
		return "Scroll up a full page"
	case CmdOpenDetails:
		return "Open issue details modal"
	case CmdOpenStats:
		return "Open statistics dashboard"
	case CmdOpenHandoffs:
		return "Open handoffs modal"
	case CmdSearch:
		return "Enter search mode"
	case CmdToggleClosed:
		return "Show/hide closed tasks"
	case CmdCycleSortMode:
		return "Cycle sort: priority → created → updated"
	case CmdMarkForReview:
		return "Mark issue for review"
	case CmdApprove:
		return "Approve a reviewable issue"
	case CmdDelete:
		return "Delete an issue"
	case CmdFocusTaskSection:
		return "Toggle focus on epic task list"
	case CmdOpenEpicTask:
		return "Open selected task from epic"
	case CmdOpenParentEpic:
		return "Open parent epic from story/task"
	default:
		return string(cmd)
	}
}

// BindingsByCommand groups bindings by command for help generation
func (r *Registry) BindingsByCommand(context Context) map[Command][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[Command][]string)

	// Add bindings for this context
	for _, b := range r.bindings[context] {
		result[b.Command] = append(result[b.Command], formatKey(b.Key))
	}

	// Add global bindings
	if context != ContextGlobal {
		for _, b := range r.bindings[ContextGlobal] {
			// Don't add if already defined in context
			if _, exists := result[b.Command]; !exists {
				result[b.Command] = append(result[b.Command], formatKey(b.Key))
			}
		}
	}

	return result
}

// formatKey formats a key string for display
func formatKey(key string) string {
	replacements := map[string]string{
		"up":        "↑",
		"down":      "↓",
		"left":      "←",
		"right":     "→",
		"enter":     "Enter",
		"esc":       "Esc",
		"tab":       "Tab",
		"shift+tab": "Shift+Tab",
		"space":     "Space",
		"backspace": "Backspace",
		"ctrl+":     "Ctrl+",
		"pgup":      "PgUp",
		"pgdown":    "PgDn",
	}

	result := key
	for old, new := range replacements {
		result = strings.ReplaceAll(result, old, new)
	}
	return result
}

// AllCommands returns all defined commands sorted alphabetically
func AllCommands() []Command {
	cmds := []Command{
		CmdQuit, CmdToggleHelp, CmdRefresh,
		CmdNextPanel, CmdPrevPanel, CmdFocusPanel1, CmdFocusPanel2, CmdFocusPanel3,
		CmdCursorDown, CmdCursorUp, CmdCursorTop, CmdCursorBottom,
		CmdHalfPageDown, CmdHalfPageUp, CmdFullPageDown, CmdFullPageUp,
		CmdScrollDown, CmdScrollUp, CmdSelect, CmdBack, CmdClose,
		CmdNavigatePrev, CmdNavigateNext,
		CmdOpenDetails, CmdOpenStats, CmdOpenHandoffs, CmdSearch, CmdToggleClosed, CmdCycleSortMode,
		CmdMarkForReview, CmdApprove, CmdDelete, CmdConfirm, CmdCancel,
		CmdSearchConfirm, CmdSearchCancel, CmdSearchClear, CmdSearchBackspace, CmdSearchInput,
		CmdFocusTaskSection, CmdOpenEpicTask, CmdOpenParentEpic,
	}

	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i] < cmds[j]
	})

	return cmds
}
