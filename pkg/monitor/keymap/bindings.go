package keymap

// DefaultBindings returns the default key bindings for the monitor TUI.
// Bindings are organized by context and follow vim conventions where applicable.
func DefaultBindings() []Binding {
	return []Binding{
		// ============================================================
		// GLOBAL BINDINGS
		// These work in all contexts unless overridden
		// ============================================================
		{Key: "q", Command: CmdQuit, Context: ContextGlobal, Description: "Quit"},
		{Key: "ctrl+c", Command: CmdQuit, Context: ContextGlobal, Description: "Quit"},
		{Key: "?", Command: CmdToggleHelp, Context: ContextGlobal, Description: "Toggle help"},

		// ============================================================
		// MAIN PANEL BINDINGS
		// Active when no modal is open and not in search mode
		// ============================================================

		// Panel navigation
		{Key: "tab", Command: CmdNextPanel, Context: ContextMain, Description: "Next panel"},
		{Key: "shift+tab", Command: CmdPrevPanel, Context: ContextMain, Description: "Previous panel"},
		{Key: "1", Command: CmdFocusPanel1, Context: ContextMain, Description: "Focus Current Work"},
		{Key: "2", Command: CmdFocusPanel2, Context: ContextMain, Description: "Focus Task List"},
		{Key: "3", Command: CmdFocusPanel3, Context: ContextMain, Description: "Focus Activity"},

		// Cursor movement
		{Key: "j", Command: CmdCursorDown, Context: ContextMain, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextMain, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextMain, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextMain, Description: "Move up"},

		// Vim-style page navigation (new!)
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextMain, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextMain, Description: "Half page up"},
		{Key: "ctrl+f", Command: CmdFullPageDown, Context: ContextMain, Description: "Full page down"},
		{Key: "ctrl+b", Command: CmdFullPageUp, Context: ContextMain, Description: "Full page up"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextMain, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextMain, Description: "Go to top"},

		// Actions
		{Key: "enter", Command: CmdOpenDetails, Context: ContextMain, Description: "Open details"},
		{Key: "s", Command: CmdOpenStats, Context: ContextMain, Description: "Open statistics"},
		{Key: "h", Command: CmdOpenHandoffs, Context: ContextMain, Description: "Open handoffs"},
		{Key: "/", Command: CmdSearch, Context: ContextMain, Description: "Search"},
		{Key: "c", Command: CmdToggleClosed, Context: ContextMain, Description: "Toggle closed tasks"},
		{Key: "S", Command: CmdCycleSortMode, Context: ContextMain, Description: "Cycle sort mode"},
		{Key: "r", Command: CmdMarkForReview, Context: ContextMain, Description: "Review/Refresh"},
		{Key: "a", Command: CmdApprove, Context: ContextMain, Description: "Approve issue"},
		{Key: "x", Command: CmdDelete, Context: ContextMain, Description: "Delete issue"},

		// ============================================================
		// MODAL BINDINGS (Issue Details)
		// Active when the issue details modal is open
		// ============================================================
		{Key: "esc", Command: CmdClose, Context: ContextModal, Description: "Close modal"},
		{Key: "enter", Command: CmdClose, Context: ContextModal, Description: "Close modal"},

		// Scrolling
		{Key: "j", Command: CmdScrollDown, Context: ContextModal, Description: "Scroll down"},
		{Key: "down", Command: CmdScrollDown, Context: ContextModal, Description: "Scroll down"},
		{Key: "k", Command: CmdScrollUp, Context: ContextModal, Description: "Scroll up"},
		{Key: "up", Command: CmdScrollUp, Context: ContextModal, Description: "Scroll up"},

		// Vim-style page navigation
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextModal, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextModal, Description: "Half page up"},
		{Key: "ctrl+f", Command: CmdFullPageDown, Context: ContextModal, Description: "Full page down"},
		{Key: "ctrl+b", Command: CmdFullPageUp, Context: ContextModal, Description: "Full page up"},
		{Key: "pgdown", Command: CmdFullPageDown, Context: ContextModal, Description: "Page down"},
		{Key: "pgup", Command: CmdFullPageUp, Context: ContextModal, Description: "Page up"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextModal, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextModal, Description: "Go to top"},

		// Issue navigation
		{Key: "h", Command: CmdNavigatePrev, Context: ContextModal, Description: "Previous issue"},
		{Key: "left", Command: CmdNavigatePrev, Context: ContextModal, Description: "Previous issue"},
		{Key: "l", Command: CmdNavigateNext, Context: ContextModal, Description: "Next issue"},
		{Key: "right", Command: CmdNavigateNext, Context: ContextModal, Description: "Next issue"},

		// Refresh
		{Key: "r", Command: CmdRefresh, Context: ContextModal, Description: "Refresh"},

		// ============================================================
		// STATS MODAL BINDINGS
		// Active when the statistics modal is open
		// ============================================================
		{Key: "esc", Command: CmdClose, Context: ContextStats, Description: "Close modal"},
		{Key: "enter", Command: CmdClose, Context: ContextStats, Description: "Close modal"},

		// Scrolling
		{Key: "j", Command: CmdScrollDown, Context: ContextStats, Description: "Scroll down"},
		{Key: "down", Command: CmdScrollDown, Context: ContextStats, Description: "Scroll down"},
		{Key: "k", Command: CmdScrollUp, Context: ContextStats, Description: "Scroll up"},
		{Key: "up", Command: CmdScrollUp, Context: ContextStats, Description: "Scroll up"},

		// Vim-style page navigation
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextStats, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextStats, Description: "Half page up"},
		{Key: "ctrl+f", Command: CmdFullPageDown, Context: ContextStats, Description: "Full page down"},
		{Key: "ctrl+b", Command: CmdFullPageUp, Context: ContextStats, Description: "Full page up"},
		{Key: "pgdown", Command: CmdFullPageDown, Context: ContextStats, Description: "Page down"},
		{Key: "pgup", Command: CmdFullPageUp, Context: ContextStats, Description: "Page up"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextStats, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextStats, Description: "Go to top"},

		// Refresh
		{Key: "r", Command: CmdRefresh, Context: ContextStats, Description: "Refresh"},

		// ============================================================
		// SEARCH MODE BINDINGS
		// Active when search input is focused
		// ============================================================
		{Key: "esc", Command: CmdSearchCancel, Context: ContextSearch, Description: "Cancel search"},
		{Key: "enter", Command: CmdSearchConfirm, Context: ContextSearch, Description: "Apply search"},
		{Key: "ctrl+u", Command: CmdSearchClear, Context: ContextSearch, Description: "Clear search"},
		{Key: "ctrl+w", Command: CmdSearchClear, Context: ContextSearch, Description: "Clear search"},
		{Key: "backspace", Command: CmdSearchBackspace, Context: ContextSearch, Description: "Delete character"},
		// Note: printable characters are handled specially, not via bindings

		// ============================================================
		// CONFIRMATION DIALOG BINDINGS
		// Active when a confirmation dialog is shown
		// ============================================================
		{Key: "y", Command: CmdConfirm, Context: ContextConfirm, Description: "Confirm"},
		{Key: "Y", Command: CmdConfirm, Context: ContextConfirm, Description: "Confirm"},
		{Key: "n", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},
		{Key: "N", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},
		{Key: "esc", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},

		// ============================================================
		// EPIC TASKS BINDINGS (when task section in epic modal is focused)
		// Active when viewing tasks within an epic
		// ============================================================
		{Key: "j", Command: CmdCursorDown, Context: ContextEpicTasks, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextEpicTasks, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextEpicTasks, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextEpicTasks, Description: "Move up"},
		{Key: "enter", Command: CmdOpenEpicTask, Context: ContextEpicTasks, Description: "Open task"},
		{Key: "tab", Command: CmdFocusTaskSection, Context: ContextEpicTasks, Description: "Exit task list"},
		{Key: "esc", Command: CmdClose, Context: ContextEpicTasks, Description: "Close modal"},

		// Modal context: add tab to toggle task section focus
		{Key: "tab", Command: CmdFocusTaskSection, Context: ContextModal, Description: "Focus task list"},

		// ============================================================
		// PARENT EPIC FOCUSED BINDINGS
		// Active when parent epic row is focused in modal
		// ============================================================
		{Key: "enter", Command: CmdOpenParentEpic, Context: ContextParentEpicFocused, Description: "Open parent epic"},
		{Key: "esc", Command: CmdClose, Context: ContextParentEpicFocused, Description: "Close modal"},
		{Key: "j", Command: CmdCursorDown, Context: ContextParentEpicFocused, Description: "Unfocus epic"},
		{Key: "down", Command: CmdCursorDown, Context: ContextParentEpicFocused, Description: "Unfocus epic"},
		{Key: "k", Command: CmdCursorUp, Context: ContextParentEpicFocused, Description: "Stay on epic"},
		{Key: "up", Command: CmdCursorUp, Context: ContextParentEpicFocused, Description: "Stay on epic"},

		// ============================================================
		// HANDOFFS MODAL BINDINGS
		// Active when the handoffs modal is open
		// ============================================================
		{Key: "esc", Command: CmdClose, Context: ContextHandoffs, Description: "Close modal"},
		{Key: "enter", Command: CmdOpenDetails, Context: ContextHandoffs, Description: "Open issue"},
		{Key: "j", Command: CmdCursorDown, Context: ContextHandoffs, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextHandoffs, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextHandoffs, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextHandoffs, Description: "Move up"},
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextHandoffs, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextHandoffs, Description: "Half page up"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextHandoffs, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextHandoffs, Description: "Go to top"},
		{Key: "r", Command: CmdRefresh, Context: ContextHandoffs, Description: "Refresh"},
	}
}

// RegisterDefaults registers all default bindings with the registry.
func RegisterDefaults(r *Registry) {
	r.RegisterBindings(DefaultBindings())
}
