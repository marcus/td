package keymap

// ExportedBinding is a binding format that sidecar can consume.
// Context is mapped to sidecar's td-prefixed context names.
type ExportedBinding struct {
	Key     string // e.g., "tab", "ctrl+d", "g g"
	Command string // Command ID string
	Context string // Sidecar context: "td-monitor", "td-modal", etc.
}

// ExportedCommand provides command metadata for sidecar's command palette.
type ExportedCommand struct {
	ID          string // Command ID
	Name        string // Short display name for footer (1-2 words)
	Description string // Full description for palette
	Context     string // Sidecar context
	Priority    int    // 1-3 = footer visible, 4+ = palette only
}

// contextToSidecar maps TD contexts to sidecar context names.
var contextToSidecar = map[Context]string{
	ContextGlobal:            "td-global",
	ContextMain:              "td-monitor",
	ContextModal:             "td-modal",
	ContextStats:             "td-stats",
	ContextSearch:            "td-search",
	ContextConfirm:           "td-confirm",
	ContextEpicTasks:         "td-epic-tasks",
	ContextParentEpicFocused: "td-parent-epic",
	ContextHandoffs:          "td-handoffs",
	ContextHelp:              "td-help",
	ContextBoard:             "td-board",
	ContextBoardPicker:       "td-board-picker",
}

// commandMetadata defines display info and priority for each command.
// Priority: 1-3 = footer visible, 4+ = palette only
var commandMetadata = map[Command]struct {
	Name        string
	Description string
	Priority    int
}{
	// High priority - always in footer (P1)
	CmdOpenDetails:   {"Details", "Open issue details", 1},
	CmdApprove:       {"Approve", "Approve issue", 1},
	CmdMarkForReview: {"Review", "Mark for review", 1},
	CmdSearch:        {"Search", "Search issues", 1},
	CmdClose:         {"Close", "Close modal", 1},

	// Medium priority - footer when space allows (P2)
	CmdOpenStats:     {"Stats", "Open statistics", 2},
	CmdOpenHandoffs:  {"Handoffs", "Open handoffs", 2},
	CmdToggleClosed:  {"Closed", "Toggle closed tasks", 2},
	CmdDelete:        {"Delete", "Delete issue", 2},
	CmdCloseIssue:    {"Close", "Close issue", 2},
	CmdReopenIssue:   {"Reopen", "Reopen closed issue", 2},
	CmdRefresh:       {"Refresh", "Refresh data", 2},
	CmdCycleSortMode:   {"Sort", "Cycle sort mode", 2},
	CmdCycleTypeFilter: {"Type", "Cycle type filter", 2},

	// Board mode controls (P2)
	CmdToggleBoardView:   {"View", "Toggle swimlanes/backlog view", 2},
	CmdToggleBoardClosed: {"Closed", "Toggle closed in board", 2},

	// Lower priority - palette only (P3+)
	CmdToggleHelp:      {"Help", "Toggle help overlay", 3},
	CmdQuit:            {"Quit", "Quit application", 3},
	CmdCopyToClipboard:   {"Copy", "Copy to clipboard", 3},
	CmdCopyIDToClipboard: {"CopyID", "Copy issue ID", 3},

	// Navigation - usually palette only (P4)
	CmdNextPanel:  {"Next", "Next panel", 4},
	CmdPrevPanel:  {"Prev", "Previous panel", 4},
	CmdCursorDown: {"Down", "Move cursor down", 5},
	CmdCursorUp:        {"Up", "Move cursor up", 5},
	CmdCursorTop:       {"Top", "Go to top", 5},
	CmdCursorBottom:    {"Bottom", "Go to bottom", 5},
	CmdHalfPageDown:    {"Page↓", "Half page down", 5},
	CmdHalfPageUp:      {"Page↑", "Half page up", 5},
	CmdFullPageDown:    {"PgDn", "Full page down", 5},
	CmdFullPageUp:      {"PgUp", "Full page up", 5},
	CmdScrollDown:      {"Scroll↓", "Scroll down", 5},
	CmdScrollUp:        {"Scroll↑", "Scroll up", 5},
	CmdSelect:          {"Select", "Select item", 5},
	CmdBack:            {"Back", "Go back", 5},
	CmdNavigatePrev:    {"Prev", "Previous issue", 5},
	CmdNavigateNext:    {"Next", "Next issue", 5},
	CmdFocusTaskSection:   {"Tasks", "Focus task section", 4},
	CmdOpenEpicTask:       {"Open", "Open epic task", 4},
	CmdOpenParentEpic:     {"Parent", "Open parent epic", 4},
	CmdOpenBlockedByIssue: {"Open", "Open blocker issue", 4},
	CmdOpenBlocksIssue:    {"Open", "Open blocked issue", 4},

	// Search mode - context specific (P4)
	CmdSearchConfirm:   {"Apply", "Apply search", 4},
	CmdSearchCancel:    {"Cancel", "Cancel search", 4},
	CmdSearchClear:     {"Clear", "Clear search", 4},
	CmdSearchBackspace: {"Delete", "Delete character", 5},
	CmdSearchInput:     {"Input", "Input character", 5},

	// Confirm dialog (P4)
	CmdConfirm: {"Yes", "Confirm action", 4},
	CmdCancel:  {"No", "Cancel action", 4},
}

// ExportBindings returns all bindings in a format sidecar can consume.
func (r *Registry) ExportBindings() []ExportedBinding {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []ExportedBinding
	for ctx, bindings := range r.bindings {
		sidecarCtx := contextToSidecar[ctx]
		if sidecarCtx == "" {
			sidecarCtx = "td-" + string(ctx)
		}
		for _, b := range bindings {
			result = append(result, ExportedBinding{
				Key:     b.Key,
				Command: string(b.Command),
				Context: sidecarCtx,
			})
		}
	}
	return result
}

// ExportCommands returns command metadata for sidecar's command palette.
// Returns unique commands with their metadata, mapped to sidecar contexts.
func (r *Registry) ExportCommands() []ExportedCommand {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Track which commands we've seen for each context
	seen := make(map[string]bool)
	var result []ExportedCommand

	for ctx, bindings := range r.bindings {
		sidecarCtx := contextToSidecar[ctx]
		if sidecarCtx == "" {
			sidecarCtx = "td-" + string(ctx)
		}

		for _, b := range bindings {
			key := sidecarCtx + ":" + string(b.Command)
			if seen[key] {
				continue
			}
			seen[key] = true

			meta, ok := commandMetadata[b.Command]
			if !ok {
				// Fallback for unknown commands
				meta = struct {
					Name        string
					Description string
					Priority    int
				}{
					Name:        string(b.Command),
					Description: b.Description,
					Priority:    5,
				}
			}

			result = append(result, ExportedCommand{
				ID:          string(b.Command),
				Name:        meta.Name,
				Description: meta.Description,
				Context:     sidecarCtx,
				Priority:    meta.Priority,
			})
		}
	}

	return result
}

// ContextToSidecar converts a TD context to its sidecar equivalent.
func ContextToSidecar(ctx Context) string {
	if s, ok := contextToSidecar[ctx]; ok {
		return s
	}
	return "td-" + string(ctx)
}
