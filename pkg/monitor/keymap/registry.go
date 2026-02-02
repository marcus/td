package keymap

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const sequenceTimeout = 500 * time.Millisecond

// Context represents a UI context for keybindings
type Context string

const (
	ContextGlobal            Context = "global"
	ContextMain              Context = "main"
	ContextModal             Context = "modal"
	ContextStats             Context = "stats"
	ContextSearch            Context = "search"
	ContextConfirm           Context = "confirm"
	ContextEpicTasks         Context = "epic-tasks"          // When task list in epic modal is focused
	ContextParentEpicFocused Context = "parent-epic-focused" // When parent epic row is focused
	ContextBlockedByFocused  Context = "blocked-by-focused"  // When blocked-by section is focused
	ContextBlocksFocused     Context = "blocks-focused"      // When blocks section is focused
	ContextHandoffs          Context = "handoffs"            // When handoffs modal is open
	ContextForm              Context = "form"                // When form modal is open
	ContextHelp              Context = "help"                // When help modal is open
	ContextBoardPicker       Context = "board-picker"        // When board picker is open
	ContextBoard             Context = "board"               // When board mode is active
	ContextGettingStarted    Context = "getting-started"    // When getting started modal is open
	ContextTDQHelp           Context = "tdq-help"           // When TDQ help modal is open
	ContextBoardEditor       Context = "board-editor"       // When board edit/create modal is open
	ContextCloseConfirm      Context = "close-confirm"      // When close confirmation modal is open (has text input)
	ContextSyncPrompt        Context = "td-sync-prompt"    // When sync prompt modal is open
)

// Command represents a named command that can be triggered by key bindings
type Command string

// All available commands
const (
	// Global commands
	CmdQuit       Command = "quit"
	CmdToggleHelp Command = "toggle-help"
	CmdRefresh    Command = "refresh"

	// Navigation commands
	CmdNextPanel    Command = "next-panel"
	CmdPrevPanel    Command = "prev-panel"
	CmdCursorDown   Command = "cursor-down"
	CmdCursorUp      Command = "cursor-up"
	CmdCursorTop     Command = "cursor-top"
	CmdCursorBottom  Command = "cursor-bottom"
	CmdHalfPageDown  Command = "half-page-down"
	CmdHalfPageUp    Command = "half-page-up"
	CmdFullPageDown  Command = "full-page-down"
	CmdFullPageUp    Command = "full-page-up"
	CmdScrollDown    Command = "scroll-down"
	CmdScrollUp      Command = "scroll-up"
	CmdSelect        Command = "select"
	CmdBack          Command = "back"
	CmdClose         Command = "close"
	CmdNavigatePrev  Command = "navigate-prev"
	CmdNavigateNext  Command = "navigate-next"

	// Action commands
	CmdOpenDetails    Command = "open-details"
	CmdOpenStats      Command = "open-stats"
	CmdSearch         Command = "search"
	CmdToggleClosed   Command = "toggle-closed"
	CmdMarkForReview  Command = "mark-for-review"
	CmdApprove        Command = "approve"
	CmdDelete         Command = "delete"
	CmdConfirm        Command = "confirm"
	CmdCancel         Command = "cancel"
	CmdCycleSortMode  Command = "cycle-sort-mode"

	// Search-specific commands
	CmdSearchConfirm   Command = "search-confirm"
	CmdSearchCancel    Command = "search-cancel"
	CmdSearchClear     Command = "search-clear"
	CmdSearchBackspace Command = "search-backspace"
	CmdSearchInput     Command = "search-input"

	// Epic task navigation commands
	CmdFocusTaskSection Command = "focus-task-section"
	CmdOpenEpicTask     Command = "open-epic-task"

	// Parent epic navigation
	CmdOpenParentEpic Command = "open-parent-epic"

	// Blocked-by/blocks navigation
	CmdOpenBlockedByIssue Command = "open-blocked-by-issue"
	CmdOpenBlocksIssue    Command = "open-blocks-issue"

	// Handoffs modal
	CmdOpenHandoffs Command = "open-handoffs"

	// Clipboard
	CmdCopyToClipboard   Command = "copy-to-clipboard"
	CmdCopyIDToClipboard Command = "copy-id-to-clipboard"

	// Form commands
	CmdNewIssue         Command = "new-issue"
	CmdEditIssue        Command = "edit-issue"
	CmdFormSubmit       Command = "form-submit"
	CmdFormCancel       Command = "form-cancel"
	CmdFormToggleExtend Command = "form-toggle-extend"
	CmdFormOpenEditor   Command = "form-open-editor"

	// Issue actions
	CmdCloseIssue  Command = "close-issue"
	CmdReopenIssue Command = "reopen-issue"

	// Filters
	CmdCycleTypeFilter Command = "cycle-type-filter"

	// Button navigation (for confirmation dialogs and forms)
	CmdNextButton Command = "next-button"
	CmdPrevButton Command = "prev-button"

	// Board commands
	CmdOpenBoardPicker        Command = "boards"
	CmdSelectBoard            Command = "select-board"
	CmdCloseBoardPicker       Command = "close-picker"
	CmdMoveIssueUp            Command = "move-up"
	CmdMoveIssueDown          Command = "move-down"
	CmdMoveIssueToTop         Command = "move-to-top"
	CmdMoveIssueToBottom      Command = "move-to-bottom"
	CmdExitBoardMode          Command = "exit"
	CmdToggleBoardClosed      Command = "closed"
	CmdCycleBoardStatusFilter Command = "status-filter"
	CmdToggleBoardView        Command = "view"

	// External integration commands
	CmdSendToWorktree Command = "send-to-worktree"

	// Board editor commands
	CmdEditBoard          Command = "edit-board"
	CmdNewBoard           Command = "new-board"
	CmdBoardEditorSave    Command = "board-editor-save"
	CmdBoardEditorCancel  Command = "board-editor-cancel"
	CmdBoardEditorDelete  Command = "board-editor-delete"

	// Getting started commands
	CmdOpenGettingStarted  Command = "open-getting-started"
	CmdInstallInstructions Command = "install-instructions"
)

// Binding maps a key or key sequence to a command in a specific context
type Binding struct {
	Key         string  // e.g., "tab", "ctrl+d", "g g"
	Command     Command // Command ID
	Context     Context // "global", "main", "modal", etc.
	Description string  // Human-readable description for help text
}

// Registry manages key bindings and command dispatch
type Registry struct {
	bindings      map[Context][]Binding // context -> bindings
	userOverrides map[string]Command    // "context:key" -> command
	pendingKey    string
	pendingTime   time.Time
	mu            sync.RWMutex
}

// NewRegistry creates a new keymap registry
func NewRegistry() *Registry {
	return &Registry{
		bindings:      make(map[Context][]Binding),
		userOverrides: make(map[string]Command),
	}
}

// RegisterBinding adds a key binding
func (r *Registry) RegisterBinding(b Binding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings[b.Context] = append(r.bindings[b.Context], b)
}

// RegisterBindings adds multiple key bindings
func (r *Registry) RegisterBindings(bindings []Binding) {
	for _, b := range bindings {
		r.RegisterBinding(b)
	}
}

// SetUserOverride sets a user-configured key override for a specific context
func (r *Registry) SetUserOverride(context Context, key string, cmd Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userOverrides[string(context)+":"+key] = cmd
}

// Lookup finds the command for a given key in the specified context
// Returns the command and whether a binding was found
// Checks: user overrides -> context bindings -> global bindings
func (r *Registry) Lookup(key tea.KeyMsg, activeContext Context) (Command, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	keyStr := KeyToString(key)

	// Check for pending key sequence
	if r.pendingKey != "" {
		if time.Since(r.pendingTime) < sequenceTimeout {
			seq := r.pendingKey + " " + keyStr
			r.pendingKey = ""
			if cmd, found := r.findCommand(seq, activeContext); found {
				return cmd, true
			}
			// Sequence didn't match, try just the new key
		} else {
			r.pendingKey = ""
		}
	}

	// Check if this key starts a sequence
	if r.isSequenceStart(keyStr, activeContext) {
		r.pendingKey = keyStr
		r.pendingTime = time.Now()
		return "", false
	}

	return r.findCommand(keyStr, activeContext)
}

// findCommand looks up a command for the given key in order of precedence
func (r *Registry) findCommand(key string, activeContext Context) (Command, bool) {
	// 1. Check user overrides for active context
	if activeContext != "" && activeContext != ContextGlobal {
		if cmd, ok := r.userOverrides[string(activeContext)+":"+key]; ok {
			return cmd, true
		}
	}
	// Check global user overrides
	if cmd, ok := r.userOverrides[string(ContextGlobal)+":"+key]; ok {
		return cmd, true
	}

	// 2. Check active context bindings
	if activeContext != "" && activeContext != ContextGlobal {
		if cmd, found := r.findInContext(key, activeContext); found {
			return cmd, true
		}
	}

	// 3. Fall back to global bindings
	return r.findInContext(key, ContextGlobal)
}

// findInContext finds a command for a key in a specific context
func (r *Registry) findInContext(key string, context Context) (Command, bool) {
	for _, b := range r.bindings[context] {
		if b.Key == key {
			return b.Command, true
		}
	}
	return "", false
}

// isSequenceStart checks if this key could start a multi-key sequence
func (r *Registry) isSequenceStart(key string, activeContext Context) bool {
	prefix := key + " "

	// Check all contexts that could be active
	contexts := []Context{ContextGlobal}
	if activeContext != "" && activeContext != ContextGlobal {
		contexts = append(contexts, activeContext)
	}

	for _, ctx := range contexts {
		for _, b := range r.bindings[ctx] {
			if strings.HasPrefix(b.Key, prefix) {
				return true
			}
		}
	}

	// Also check user overrides
	for k := range r.userOverrides {
		// k is "context:key", extract the key part
		parts := strings.SplitN(k, ":", 2)
		if len(parts) == 2 && strings.HasPrefix(parts[1], prefix) {
			return true
		}
	}

	return false
}

// ResetPending clears any pending key sequence
func (r *Registry) ResetPending() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingKey = ""
}

// HasPending returns true if there's a pending key sequence
func (r *Registry) HasPending() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pendingKey != "" && time.Since(r.pendingTime) < sequenceTimeout
}

// PendingKey returns the current pending key (for UI display)
func (r *Registry) PendingKey() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.pendingKey != "" && time.Since(r.pendingTime) < sequenceTimeout {
		return r.pendingKey
	}
	return ""
}

// BindingsForContext returns all bindings for a given context (including global)
func (r *Registry) BindingsForContext(context Context) []Binding {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Binding
	// Add context-specific bindings first
	result = append(result, r.bindings[context]...)
	// Add global bindings
	if context != ContextGlobal {
		result = append(result, r.bindings[ContextGlobal]...)
	}
	return result
}

// AllContexts returns all contexts that have bindings
func (r *Registry) AllContexts() []Context {
	r.mu.RLock()
	defer r.mu.RUnlock()
	contexts := make([]Context, 0, len(r.bindings))
	for ctx := range r.bindings {
		contexts = append(contexts, ctx)
	}
	return contexts
}

// KeyToString converts a tea.KeyMsg to a string representation
func KeyToString(key tea.KeyMsg) string {
	switch key.Type {
	case tea.KeyCtrlC:
		return "ctrl+c"
	case tea.KeyCtrlA:
		return "ctrl+a"
	case tea.KeyCtrlB:
		return "ctrl+b"
	case tea.KeyCtrlD:
		return "ctrl+d"
	case tea.KeyCtrlE:
		return "ctrl+e"
	case tea.KeyCtrlF:
		return "ctrl+f"
	case tea.KeyCtrlG:
		return "ctrl+g"
	case tea.KeyCtrlH:
		return "ctrl+h"
	case tea.KeyTab:
		return "tab"
	case tea.KeyCtrlJ:
		return "ctrl+j"
	case tea.KeyCtrlK:
		return "ctrl+k"
	case tea.KeyCtrlL:
		return "ctrl+l"
	case tea.KeyEnter:
		return "enter"
	case tea.KeyCtrlN:
		return "ctrl+n"
	case tea.KeyCtrlO:
		return "ctrl+o"
	case tea.KeyCtrlP:
		return "ctrl+p"
	case tea.KeyCtrlQ:
		return "ctrl+q"
	case tea.KeyCtrlR:
		return "ctrl+r"
	case tea.KeyCtrlS:
		return "ctrl+s"
	case tea.KeyCtrlT:
		return "ctrl+t"
	case tea.KeyCtrlU:
		return "ctrl+u"
	case tea.KeyCtrlV:
		return "ctrl+v"
	case tea.KeyCtrlW:
		return "ctrl+w"
	case tea.KeyCtrlX:
		return "ctrl+x"
	case tea.KeyCtrlY:
		return "ctrl+y"
	case tea.KeyCtrlZ:
		return "ctrl+z"
	case tea.KeyEsc:
		return "esc"
	case tea.KeySpace:
		return "space"
	case tea.KeyBackspace:
		return "backspace"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	case tea.KeyLeft:
		return "left"
	case tea.KeyRight:
		return "right"
	case tea.KeyHome:
		return "home"
	case tea.KeyEnd:
		return "end"
	case tea.KeyPgUp:
		return "pgup"
	case tea.KeyPgDown:
		return "pgdown"
	case tea.KeyDelete:
		return "delete"
	case tea.KeyShiftTab:
		return "shift+tab"
	case tea.KeyRunes:
		return string(key.Runes)
	default:
		return key.String()
	}
}

// IsPrintable returns true if the key represents a printable character
func IsPrintable(key tea.KeyMsg) bool {
	if key.Type != tea.KeyRunes {
		return false
	}
	if len(key.Runes) != 1 {
		return false
	}
	r := key.Runes[0]
	return r >= ' ' && r <= '~'
}
