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

	// Generate sections based on context
	sections := []struct {
		context Context
		title   string
	}{
		{ContextMain, "NAVIGATION"},
		{ContextModal, "MODALS"},
		{ContextMain, "ACTIONS"},
		{ContextSearch, "SEARCH"},
		{ContextConfirm, "CONFIRMATION"},
	}

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
		{Keys: "↑ / ↓ / j / k", Description: "Scroll modal content"},
		{Keys: "Ctrl+d / Ctrl+u", Description: "Half page down/up"},
		{Keys: "← / → / h / l", Description: "Navigate prev/next issue"},
		{Keys: "Esc / Enter", Description: "Close modal"},
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
		{Keys: "/", Description: "Search tasks"},
		{Keys: "c", Description: "Toggle closed tasks"},
		{Keys: "q / Ctrl+C", Description: "Quit"},
	}
	for _, b := range actionBindings {
		sb.WriteString(fmt.Sprintf("  %-20s %s\n", b.Keys, b.Description))
	}

	sb.WriteString("\nPress ? to close help\n")

	// Add sections from actual bindings (unused for now but available)
	_ = sections

	return sb.String()
}

// FooterHelp generates a compact help string for the footer
func (r *Registry) FooterHelp() string {
	return "q:quit s:stats /:search r:review a:approve x:del tab:panel ↑↓:sel enter:details ?:help"
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
	case CmdSearch:
		return "Enter search mode"
	case CmdToggleClosed:
		return "Show/hide closed tasks"
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
		CmdOpenDetails, CmdOpenStats, CmdSearch, CmdToggleClosed,
		CmdMarkForReview, CmdApprove, CmdDelete, CmdConfirm, CmdCancel,
		CmdSearchConfirm, CmdSearchCancel, CmdSearchClear, CmdSearchBackspace, CmdSearchInput,
		CmdFocusTaskSection, CmdOpenEpicTask,
	}

	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i] < cmds[j]
	})

	return cmds
}
