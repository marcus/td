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
		{Key: "esc", Command: CmdSearchClear, Context: ContextMain, Description: "Clear search filter"},

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
		{Key: "home", Command: CmdCursorTop, Context: ContextMain, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextMain, Description: "Go to bottom"},

		// Actions
		{Key: "enter", Command: CmdOpenDetails, Context: ContextMain, Description: "Open details"},
		{Key: "s", Command: CmdOpenStats, Context: ContextMain, Description: "Open statistics"},
		{Key: "h", Command: CmdOpenHandoffs, Context: ContextMain, Description: "Open handoffs"},
		{Key: "/", Command: CmdSearch, Context: ContextMain, Description: "Search"},
		{Key: "c", Command: CmdToggleClosed, Context: ContextMain, Description: "Toggle closed tasks"},
		{Key: "S", Command: CmdCycleSortMode, Context: ContextMain, Description: "Cycle sort mode"},
		{Key: "T", Command: CmdCycleTypeFilter, Context: ContextMain, Description: "Cycle type filter"},
		{Key: "r", Command: CmdMarkForReview, Context: ContextMain, Description: "Review/Refresh"},
		{Key: "R", Command: CmdMarkForReview, Context: ContextMain, Description: "Submit for review"},
		{Key: "a", Command: CmdApprove, Context: ContextMain, Description: "Approve issue"},
		{Key: "x", Command: CmdDelete, Context: ContextMain, Description: "Delete issue"},
		{Key: "C", Command: CmdCloseIssue, Context: ContextMain, Description: "Close issue"},
		{Key: "O", Command: CmdReopenIssue, Context: ContextMain, Description: "Reopen issue"},
		{Key: "n", Command: CmdNewIssue, Context: ContextMain, Description: "New issue"},
		{Key: "e", Command: CmdEditIssue, Context: ContextMain, Description: "Edit issue"},
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextMain, Description: "Copy issue as markdown"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextMain, Description: "Copy issue ID"},

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
		{Key: "home", Command: CmdCursorTop, Context: ContextModal, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextModal, Description: "Go to bottom"},

		// Issue navigation
		{Key: "h", Command: CmdNavigatePrev, Context: ContextModal, Description: "Previous issue"},
		{Key: "left", Command: CmdNavigatePrev, Context: ContextModal, Description: "Previous issue"},
		{Key: "l", Command: CmdNavigateNext, Context: ContextModal, Description: "Next issue"},
		{Key: "right", Command: CmdNavigateNext, Context: ContextModal, Description: "Next issue"},

		// Refresh
		{Key: "r", Command: CmdRefresh, Context: ContextModal, Description: "Refresh"},

		// Submit for review
		{Key: "R", Command: CmdMarkForReview, Context: ContextModal, Description: "Submit for review"},

		// Copy to clipboard
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextModal, Description: "Copy to clipboard"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextModal, Description: "Copy issue ID"},

		// Issue CRUD from modal
		{Key: "n", Command: CmdNewIssue, Context: ContextModal, Description: "New issue"},
		{Key: "e", Command: CmdEditIssue, Context: ContextModal, Description: "Edit issue"},
		{Key: "x", Command: CmdDelete, Context: ContextModal, Description: "Delete issue"},
		{Key: "C", Command: CmdCloseIssue, Context: ContextModal, Description: "Close issue"},
		{Key: "O", Command: CmdReopenIssue, Context: ContextModal, Description: "Reopen issue"},

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
		{Key: "home", Command: CmdCursorTop, Context: ContextStats, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextStats, Description: "Go to bottom"},

		// Refresh
		{Key: "r", Command: CmdRefresh, Context: ContextStats, Description: "Refresh"},

		// ============================================================
		// SEARCH MODE BINDINGS
		// Active when search input is focused
		// Note: Most keys are forwarded to textinput for cursor/editing support
		// ============================================================
		{Key: "esc", Command: CmdSearchCancel, Context: ContextSearch, Description: "Cancel search"},
		{Key: "enter", Command: CmdSearchConfirm, Context: ContextSearch, Description: "Apply search"},
		{Key: "ctrl+u", Command: CmdSearchClear, Context: ContextSearch, Description: "Clear search"},
		{Key: "ctrl+w", Command: CmdSearchClear, Context: ContextSearch, Description: "Clear search"},

		// ============================================================
		// CONFIRMATION DIALOG BINDINGS
		// Active when a confirmation dialog is shown
		// ============================================================
		{Key: "y", Command: CmdConfirm, Context: ContextConfirm, Description: "Confirm"},
		{Key: "Y", Command: CmdConfirm, Context: ContextConfirm, Description: "Confirm"},
		{Key: "n", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},
		{Key: "N", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},
		{Key: "esc", Command: CmdCancel, Context: ContextConfirm, Description: "Cancel"},
		{Key: "tab", Command: CmdNextButton, Context: ContextConfirm, Description: "Next button"},
		{Key: "shift+tab", Command: CmdPrevButton, Context: ContextConfirm, Description: "Previous button"},
		{Key: "enter", Command: CmdSelect, Context: ContextConfirm, Description: "Execute focused button"},

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
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextEpicTasks, Description: "Copy to clipboard"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextEpicTasks, Description: "Copy issue ID"},
		{Key: "h", Command: CmdNavigatePrev, Context: ContextEpicTasks, Description: "Previous task"},
		{Key: "left", Command: CmdNavigatePrev, Context: ContextEpicTasks, Description: "Previous task"},
		{Key: "l", Command: CmdNavigateNext, Context: ContextEpicTasks, Description: "Next task"},
		{Key: "right", Command: CmdNavigateNext, Context: ContextEpicTasks, Description: "Next task"},
		{Key: "O", Command: CmdReopenIssue, Context: ContextEpicTasks, Description: "Reopen task"},
		{Key: "R", Command: CmdMarkForReview, Context: ContextEpicTasks, Description: "Submit task for review"},
		{Key: "C", Command: CmdCloseIssue, Context: ContextEpicTasks, Description: "Close task"},

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
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextParentEpicFocused, Description: "Copy to clipboard"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextParentEpicFocused, Description: "Copy issue ID"},
		{Key: "tab", Command: CmdFocusTaskSection, Context: ContextParentEpicFocused, Description: "Next section"},

		// ============================================================
		// BLOCKED-BY FOCUSED BINDINGS
		// Active when blocked-by section is focused in modal
		// ============================================================
		{Key: "j", Command: CmdCursorDown, Context: ContextBlockedByFocused, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextBlockedByFocused, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextBlockedByFocused, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextBlockedByFocused, Description: "Move up"},
		{Key: "enter", Command: CmdOpenBlockedByIssue, Context: ContextBlockedByFocused, Description: "Open issue"},
		{Key: "tab", Command: CmdFocusTaskSection, Context: ContextBlockedByFocused, Description: "Next section"},
		{Key: "esc", Command: CmdClose, Context: ContextBlockedByFocused, Description: "Close modal"},
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextBlockedByFocused, Description: "Copy to clipboard"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextBlockedByFocused, Description: "Copy issue ID"},

		// ============================================================
		// BLOCKS FOCUSED BINDINGS
		// Active when blocks section is focused in modal
		// ============================================================
		{Key: "j", Command: CmdCursorDown, Context: ContextBlocksFocused, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextBlocksFocused, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextBlocksFocused, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextBlocksFocused, Description: "Move up"},
		{Key: "enter", Command: CmdOpenBlocksIssue, Context: ContextBlocksFocused, Description: "Open issue"},
		{Key: "tab", Command: CmdFocusTaskSection, Context: ContextBlocksFocused, Description: "Next section"},
		{Key: "esc", Command: CmdClose, Context: ContextBlocksFocused, Description: "Close modal"},
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextBlocksFocused, Description: "Copy to clipboard"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextBlocksFocused, Description: "Copy issue ID"},

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
		{Key: "home", Command: CmdCursorTop, Context: ContextHandoffs, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextHandoffs, Description: "Go to bottom"},
		{Key: "r", Command: CmdRefresh, Context: ContextHandoffs, Description: "Refresh"},

		// ============================================================
		// FORM MODAL BINDINGS
		// Active when form modal is open
		// Note: Most key handling is delegated to huh.Form
		// ============================================================
		{Key: "ctrl+s", Command: CmdFormSubmit, Context: ContextForm, Description: "Submit form"},
		{Key: "esc", Command: CmdFormCancel, Context: ContextForm, Description: "Cancel form"},
		{Key: "ctrl+x", Command: CmdFormToggleExtend, Context: ContextForm, Description: "Toggle extended fields"},
		{Key: "ctrl+o", Command: CmdFormOpenEditor, Context: ContextForm, Description: "Open in external editor"},

		// ============================================================
		// HELP MODAL BINDINGS
		// Active when the help modal is open
		// ============================================================
		{Key: "?", Command: CmdToggleHelp, Context: ContextHelp, Description: "Close help"},
		{Key: "esc", Command: CmdToggleHelp, Context: ContextHelp, Description: "Close help"},
		{Key: "q", Command: CmdToggleHelp, Context: ContextHelp, Description: "Close help"},

		// Scrolling
		{Key: "j", Command: CmdScrollDown, Context: ContextHelp, Description: "Scroll down"},
		{Key: "down", Command: CmdScrollDown, Context: ContextHelp, Description: "Scroll down"},
		{Key: "k", Command: CmdScrollUp, Context: ContextHelp, Description: "Scroll up"},
		{Key: "up", Command: CmdScrollUp, Context: ContextHelp, Description: "Scroll up"},

		// Vim-style page navigation
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextHelp, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextHelp, Description: "Half page up"},
		{Key: "ctrl+f", Command: CmdFullPageDown, Context: ContextHelp, Description: "Full page down"},
		{Key: "ctrl+b", Command: CmdFullPageUp, Context: ContextHelp, Description: "Full page up"},
		{Key: "pgdown", Command: CmdFullPageDown, Context: ContextHelp, Description: "Page down"},
		{Key: "pgup", Command: CmdFullPageUp, Context: ContextHelp, Description: "Page up"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextHelp, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextHelp, Description: "Go to top"},
		{Key: "home", Command: CmdCursorTop, Context: ContextHelp, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextHelp, Description: "Go to bottom"},

		// ============================================================
		// BOARD PICKER BINDINGS
		// Active when board picker is open
		// ============================================================
		{Key: "b", Command: CmdOpenBoardPicker, Context: ContextMain, Description: "Open board picker"},
		{Key: "j", Command: CmdCursorDown, Context: ContextBoardPicker, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextBoardPicker, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextBoardPicker, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextBoardPicker, Description: "Move up"},
		{Key: "enter", Command: CmdSelectBoard, Context: ContextBoardPicker, Description: "Select board"},
		{Key: "esc", Command: CmdCloseBoardPicker, Context: ContextBoardPicker, Description: "Close picker"},
		{Key: "q", Command: CmdCloseBoardPicker, Context: ContextBoardPicker, Description: "Close picker"},

		// ============================================================
		// BOARD MODE BINDINGS
		// Active when viewing a board (board mode is active)
		// ============================================================
		{Key: "j", Command: CmdCursorDown, Context: ContextBoard, Description: "Move down"},
		{Key: "down", Command: CmdCursorDown, Context: ContextBoard, Description: "Move down"},
		{Key: "k", Command: CmdCursorUp, Context: ContextBoard, Description: "Move up"},
		{Key: "up", Command: CmdCursorUp, Context: ContextBoard, Description: "Move up"},
		{Key: "J", Command: CmdMoveIssueDown, Context: ContextBoard, Description: "Move issue down"},
		{Key: "K", Command: CmdMoveIssueUp, Context: ContextBoard, Description: "Move issue up"},
		{Key: "enter", Command: CmdOpenDetails, Context: ContextBoard, Description: "Open issue"},
		{Key: "c", Command: CmdToggleBoardClosed, Context: ContextBoard, Description: "Toggle closed"},
		{Key: "F", Command: CmdCycleBoardStatusFilter, Context: ContextBoard, Description: "Cycle status filter"},
		{Key: "esc", Command: CmdExitBoardMode, Context: ContextBoard, Description: "Exit to All Issues"},
		{Key: "b", Command: CmdOpenBoardPicker, Context: ContextBoard, Description: "Open board picker"},
		{Key: "G", Command: CmdCursorBottom, Context: ContextBoard, Description: "Go to bottom"},
		{Key: "g g", Command: CmdCursorTop, Context: ContextBoard, Description: "Go to top"},
		{Key: "ctrl+d", Command: CmdHalfPageDown, Context: ContextBoard, Description: "Half page down"},
		{Key: "ctrl+u", Command: CmdHalfPageUp, Context: ContextBoard, Description: "Half page up"},
		{Key: "y", Command: CmdCopyToClipboard, Context: ContextBoard, Description: "Copy issue as markdown"},
		{Key: "Y", Command: CmdCopyIDToClipboard, Context: ContextBoard, Description: "Copy issue ID"},
		{Key: "r", Command: CmdRefresh, Context: ContextBoard, Description: "Refresh"},
		{Key: "v", Command: CmdToggleBoardView, Context: ContextBoard, Description: "Toggle swimlanes/backlog view"},

		// Panel navigation (same as ContextMain)
		{Key: "tab", Command: CmdNextPanel, Context: ContextBoard, Description: "Next panel"},
		{Key: "shift+tab", Command: CmdPrevPanel, Context: ContextBoard, Description: "Previous panel"},

		// Search (same as ContextMain)
		{Key: "/", Command: CmdSearch, Context: ContextBoard, Description: "Search"},

		// Issue actions (same as ContextMain)
		{Key: "C", Command: CmdCloseIssue, Context: ContextBoard, Description: "Close issue"},
		{Key: "O", Command: CmdReopenIssue, Context: ContextBoard, Description: "Reopen issue"},
		{Key: "n", Command: CmdNewIssue, Context: ContextBoard, Description: "New issue"},
		{Key: "e", Command: CmdEditIssue, Context: ContextBoard, Description: "Edit issue"},
		{Key: "x", Command: CmdDelete, Context: ContextBoard, Description: "Delete issue"},
		{Key: "a", Command: CmdApprove, Context: ContextBoard, Description: "Approve issue"},
		{Key: "R", Command: CmdMarkForReview, Context: ContextBoard, Description: "Submit for review"},

		// Other actions (same as ContextMain)
		{Key: "s", Command: CmdOpenStats, Context: ContextBoard, Description: "Open statistics"},
		{Key: "h", Command: CmdOpenHandoffs, Context: ContextBoard, Description: "Open handoffs"},
		{Key: "S", Command: CmdCycleSortMode, Context: ContextBoard, Description: "Cycle sort mode"},
		{Key: "T", Command: CmdCycleTypeFilter, Context: ContextBoard, Description: "Cycle type filter"},

		// Additional navigation (same as ContextMain)
		{Key: "ctrl+f", Command: CmdFullPageDown, Context: ContextBoard, Description: "Full page down"},
		{Key: "ctrl+b", Command: CmdFullPageUp, Context: ContextBoard, Description: "Full page up"},
		{Key: "home", Command: CmdCursorTop, Context: ContextBoard, Description: "Go to top"},
		{Key: "end", Command: CmdCursorBottom, Context: ContextBoard, Description: "Go to bottom"},
	}
}

// RegisterDefaults registers all default bindings with the registry.
func RegisterDefaults(r *Registry) {
	r.RegisterBindings(DefaultBindings())
}
