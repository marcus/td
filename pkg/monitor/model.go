package monitor

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/version"
	"github.com/marcus/td/pkg/monitor/keymap"
	"github.com/marcus/td/pkg/monitor/modal"
	"github.com/marcus/td/pkg/monitor/mouse"
)

// Model is the main Bubble Tea model for the monitor TUI
type Model struct {
	// Database and session
	DB        *db.DB
	SessionID string

	// Window dimensions
	Width  int
	Height int

	// Panel data
	FocusedIssue   *models.Issue
	InProgress     []models.Issue
	Activity       []ActivityItem
	TaskList       TaskListData
	RecentHandoffs []RecentHandoff // Handoffs since monitor started
	ActiveSessions []string        // Sessions with recent activity

	// UI state
	ActivePanel         Panel
	ScrollOffset        map[Panel]int
	Cursor              map[Panel]int    // Per-panel cursor position (selected row)
	SelectedID          map[Panel]string // Per-panel selected issue ID (preserved across refresh)
	ScrollIndependent   map[Panel]bool   // True when user scrolled viewport away from cursor
	HelpOpen            bool             // Whether help modal is open
	HelpScroll          int              // Current scroll position in help
	HelpTotalLines      int              // Cached total line count in help
	HelpFilter          string           // Filter text for help search
	HelpFilterMode      bool             // Whether typing in help filter
	ShowTDQHelp         bool             // Show TDQ query syntax help (when in search mode)
	TDQHelpModal        *modal.Modal     // Declarative modal instance for TDQ help
	TDQHelpMouseHandler *mouse.Handler   // Mouse handler for TDQ help modal
	LastRefresh         time.Time
	StartedAt           time.Time // When monitor started, to track new handoffs
	Err                 error     // Last error, if any
	Embedded            bool      // When true, skip footer (embedded in sidecar)

	// Flattened rows for selection
	TaskListRows    []TaskListRow // Flattened task list for selection
	CurrentWorkRows []string      // Issue IDs for current work panel (focused + in-progress)

	// Modal stack for stacking modals (empty = no modal open)
	ModalStack []ModalEntry

	// Search state
	SearchMode     bool            // Whether search mode is active
	SearchQuery    string          // Current search query
	SearchInput    textinput.Model // Text input for search (cursor support)
	IncludeClosed  bool            // Whether to include closed tasks
	SortMode       SortMode        // Task list sort order
	TypeFilterMode TypeFilterMode  // Type filter (epic, task, bug, etc.)

	// Confirmation dialog state (delete confirmation)
	ConfirmOpen        bool
	ConfirmAction      string // "delete"
	ConfirmIssueID     string
	ConfirmTitle       string
	ConfirmButtonFocus int // 0=Yes, 1=No (for delete confirmation) - legacy, kept for compatibility
	ConfirmButtonHover int // 0=none, 1=Yes, 2=No - legacy, kept for compatibility

	// Declarative delete confirmation modal
	DeleteConfirmModal        *modal.Modal   // Declarative modal instance
	DeleteConfirmMouseHandler *mouse.Handler // Mouse handler for delete confirmation modal

	// Close confirmation dialog state
	CloseConfirmOpen        bool
	CloseConfirmIssueID     string
	CloseConfirmTitle       string
	CloseConfirmInput       textinput.Model
	CloseConfirmButtonFocus int // 0=input, 1=Confirm, 2=Cancel - legacy, kept for compatibility
	CloseConfirmButtonHover int // 0=none, 1=Confirm, 2=Cancel - legacy, kept for compatibility

	// Declarative close confirmation modal
	CloseConfirmModal        *modal.Modal   // Declarative modal instance
	CloseConfirmMouseHandler *mouse.Handler // Mouse handler for close confirmation modal

	// Stats modal state
	StatsOpen         bool
	StatsLoading      bool
	StatsData         *StatsData
	StatsScroll       int
	StatsError        error
	StatsModal        *modal.Modal   // Declarative modal instance
	StatsMouseHandler *mouse.Handler // Mouse handler for stats modal

	// Handoffs modal state
	HandoffsOpen         bool
	HandoffsLoading      bool
	HandoffsData         []models.Handoff
	HandoffsCursor       int
	HandoffsScroll       int
	HandoffsError        error
	HandoffsModal        *modal.Modal   // Declarative modal instance
	HandoffsMouseHandler *mouse.Handler // Mouse handler for handoffs modal

	// Form modal state
	FormOpen  bool
	FormState *FormState

	// Getting Started modal state
	GettingStartedOpen         bool           // Whether getting started modal is open
	GettingStartedModal        *modal.Modal   // Declarative modal instance
	GettingStartedMouseHandler *mouse.Handler // Mouse handler for getting started modal
	AgentFilePath              string         // Detected agent file path (may be empty)
	AgentFileHasTD             bool           // Whether agent file already has td instructions
	IsFirstRunInit             bool           // Whether we're in real first-run flow (not H-key reopen)

	// Sync prompt modal state
	SyncPromptOpen      bool
	SyncPromptPhase     int
	SyncPromptProjects  []syncclient.ProjectResponse
	SyncPromptModal     *modal.Modal
	SyncPromptMouse     *mouse.Handler
	SyncPromptNameInput *textinput.Model
	SyncPromptCursor    int

	// Board picker state
	BoardPickerOpen         bool
	BoardPickerCursor       int
	BoardPickerHover        int // -1=none, 0+=hovered board index (legacy, used by modal)
	AllBoards               []models.Board
	BoardPickerModal        *modal.Modal   // Declarative modal instance
	BoardPickerMouseHandler *mouse.Handler // Mouse handler for board picker modal

	// Board editor modal state (edit/create/info overlay on board picker)
	BoardEditorOpen          bool
	BoardEditorMode          string        // "edit", "create", "info" (builtin read-only)
	BoardEditorBoard         *models.Board // Board being edited (nil for create)
	BoardEditorNameInput     *textinput.Model
	BoardEditorQueryInput    *textarea.Model
	BoardEditorModal         *modal.Modal            // Declarative modal instance
	BoardEditorMouseHandler  *mouse.Handler          // Mouse handler
	BoardEditorPreview       *boardEditorPreviewData // Shared pointer: survives stale closure captures
	BoardEditorDeleteConfirm bool                    // Whether delete confirmation is active

	// Kanban view state
	KanbanOpen       bool  // Whether kanban modal overlay is open
	KanbanCol        int   // Currently selected column (0-based)
	KanbanRow        int   // Currently selected row within the column (0-based)
	KanbanFullscreen bool  // Whether kanban view fills the entire viewport
	KanbanColScrolls []int // Per-column scroll offsets (one per kanbanColumnOrder entry)

	// Board mode state
	TaskListMode      TaskListMode       // Whether Task List shows categorized or board view
	BoardMode         BoardMode          // Active board mode state
	BoardStatusPreset StatusFilterPreset // Current status filter preset for cycling

	// Auto-sync callback (set by caller for periodic background sync)
	AutoSyncFunc     func() // Called periodically to push/pull in background
	AutoSyncInterval time.Duration
	LastAutoSync     time.Time

	// Configuration
	RefreshInterval time.Duration

	// Keymap registry for keyboard shortcuts
	Keymap *keymap.Registry

	// Status message (temporary feedback, e.g., "Copied to clipboard")
	StatusMessage string
	StatusIsError bool // true for error messages, false for success

	// Version checking
	Version     string // Current version
	UpdateAvail *version.UpdateAvailableMsg

	// Mouse support - panel bounds for hit-testing
	PanelBounds    map[Panel]Rect
	HoverPanel     Panel     // Panel currently under mouse cursor (-1 for none)
	LastClickTime  time.Time // For double-click detection
	LastClickPanel Panel     // Panel of last click
	LastClickRow   int       // Row of last click

	// Pane resizing (drag-to-resize)
	PaneHeights      [3]float64 // Height ratios (sum=1.0)
	DividerBounds    [2]Rect    // Hit regions for the 2 dividers between 3 panes
	DraggingDivider  int        // -1 = not dragging, 0 = first divider, 1 = second
	DividerHover     int        // -1 = none, 0 or 1 = which divider is hovered
	DragStartY       int        // Y position when drag started
	DragStartHeights [3]float64 // Pane heights when drag started
	BaseDir          string     // Base directory for config persistence

	// Clipboard function (nil = real system clipboard)
	ClipboardFn func(string) error

	// Custom renderers (for embedding with custom theming)
	PanelRenderer PanelRenderer // Custom panel border renderer (nil = default lipgloss)
	ModalRenderer ModalRenderer // Custom modal border renderer (nil = default lipgloss)

	// Markdown theme (for embedding with shared theme)
	MarkdownTheme *MarkdownThemeConfig // Custom markdown/syntax theme (nil = default td colors)
}

// NewModel creates a new monitor model
func NewModel(database *db.DB, sessionID string, interval time.Duration, ver string, baseDir string) Model {
	// Initialize keymap with default bindings
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	// Load pane heights from config (or use defaults)
	paneHeights, _ := config.GetPaneHeights(baseDir)

	// Initialize search input
	searchInput := textinput.New()
	searchInput.Placeholder = "search"
	searchInput.Prompt = "" // No prompt, we show triangle icon separately
	searchInput.Width = 50  // Reasonable width for search queries
	searchInput.CharLimit = 200

	return Model{
		DB:                database,
		SessionID:         sessionID,
		RefreshInterval:   interval,
		ScrollOffset:      make(map[Panel]int),
		Cursor:            make(map[Panel]int),
		SelectedID:        make(map[Panel]string),
		ScrollIndependent: make(map[Panel]bool),
		ActivePanel:       PanelCurrentWork,
		StartedAt:         time.Now(),
		SearchMode:        false,
		SearchQuery:       "",
		SearchInput:       searchInput,
		IncludeClosed:     false,
		Keymap:            km,
		Version:           ver,
		PanelBounds:       make(map[Panel]Rect),
		HoverPanel:        -1,
		LastClickPanel:    -1,
		LastClickRow:      -1,
		PaneHeights:       paneHeights,
		DraggingDivider:   -1,
		DividerHover:      -1,
		BaseDir:           baseDir,
	}
}

// NewEmbedded creates a monitor model for embedding in external applications.
// It uses a shared database connection pool to prevent connection leaks when
// Model values are copied in Update().
// The caller must call Close() when done to release resources.
func NewEmbedded(baseDir string, interval time.Duration, ver string) (*Model, error) {
	resolvedBaseDir := db.ResolveBaseDir(baseDir)

	// Use shared DB to prevent connection leaks on Model value copies
	database, err := getSharedDB(resolvedBaseDir)
	if err != nil {
		return nil, err
	}

	sess, err := session.GetOrCreate(database)
	if err != nil {
		releaseSharedDB(resolvedBaseDir)
		return nil, err
	}

	m := NewModel(database, sess.ID, interval, ver, resolvedBaseDir)
	m.Embedded = true
	return &m, nil
}

// EmbeddedOptions configures an embedded monitor model.
type EmbeddedOptions struct {
	BaseDir       string        // Base directory for database and config
	Interval      time.Duration // Refresh interval
	Version       string        // Version string for display
	PanelRenderer PanelRenderer // Custom panel border renderer (nil = default lipgloss)
	ModalRenderer ModalRenderer // Custom modal border renderer (nil = default lipgloss)

	// MarkdownTheme configures markdown rendering to share themes with embedder.
	// Pass colors from your theme to get consistent syntax highlighting.
	// If nil, uses td's default ANSI 256 color palette.
	MarkdownTheme *MarkdownThemeConfig
}

// NewEmbeddedWithOptions creates a monitor model with custom options.
// It uses a shared database connection pool to prevent connection leaks when
// Model values are copied in Update().
// The caller must call Close() when done to release resources.
func NewEmbeddedWithOptions(opts EmbeddedOptions) (*Model, error) {
	resolvedBaseDir := db.ResolveBaseDir(opts.BaseDir)

	// Use shared DB to prevent connection leaks on Model value copies
	database, err := getSharedDB(resolvedBaseDir)
	if err != nil {
		return nil, err
	}

	sess, err := session.GetOrCreate(database)
	if err != nil {
		releaseSharedDB(resolvedBaseDir)
		return nil, err
	}

	m := NewModel(database, sess.ID, opts.Interval, opts.Version, resolvedBaseDir)
	m.Embedded = true
	m.PanelRenderer = opts.PanelRenderer
	m.ModalRenderer = opts.ModalRenderer
	m.MarkdownTheme = opts.MarkdownTheme
	return &m, nil
}

// Close releases resources held by an embedded monitor.
// Only call this if the model was created with NewEmbedded or NewEmbeddedWithOptions.
// For embedded monitors, this releases the reference to the shared database pool.
// The actual connection is only closed when all references are released.
func (m *Model) Close() error {
	if m.DB != nil && m.Embedded && m.BaseDir != "" {
		return releaseSharedDB(m.BaseDir)
	} else if m.DB != nil {
		// Non-embedded model: close directly
		return m.DB.Close()
	}
	return nil
}

// helpVisibleHeight returns the number of visible lines for the help modal.
// Calculates modal height as 80% of terminal height, clamped to 15-40, minus 4 for border and footer.
func (m Model) helpVisibleHeight() int {
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}
	return modalHeight - 4 // Subtract border and footer
}

// helpMaxScroll returns the maximum scroll offset for the help modal.
func (m Model) helpMaxScroll() int {
	maxScroll := m.HelpTotalLines - m.helpVisibleHeight()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

// clampHelpScroll ensures HelpScroll is within valid bounds [0, helpMaxScroll()].
func (m *Model) clampHelpScroll() {
	if m.HelpScroll < 0 {
		m.HelpScroll = 0
	}
	maxScroll := m.helpMaxScroll()
	if m.HelpScroll > maxScroll {
		m.HelpScroll = maxScroll
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.fetchData(),
		m.scheduleTick(),
		m.restoreLastViewedBoard(),
		m.restoreFilterState(),
		m.checkFirstRun(),
	}

	// Start async version check (non-blocking)
	if m.Version != "" && !version.IsDevelopmentVersion(m.Version) {
		cmds = append(cmds, version.CheckAsync(m.Version))
	}

	return tea.Batch(cmds...)
}

// restoreLastViewedBoard returns a command that restores the last viewed board on launch
func (m Model) restoreLastViewedBoard() tea.Cmd {
	return func() tea.Msg {
		board, err := m.DB.GetLastViewedBoard()
		if err != nil || board == nil {
			return nil // No last viewed board, stay in panel mode
		}
		return RestoreLastBoardMsg{Board: board}
	}
}

// restoreFilterState returns a command that restores saved filter state on launch
func (m Model) restoreFilterState() tea.Cmd {
	return func() tea.Msg {
		state, err := config.GetFilterState(m.BaseDir)
		if err != nil || state == nil {
			return nil
		}
		// Only restore if there's actual filter state
		if state.SearchQuery == "" && state.SortMode == "" && state.TypeFilter == "" && !state.IncludeClosed {
			return nil
		}
		return RestoreFilterMsg{
			SearchQuery:    state.SearchQuery,
			SortMode:       SortModeFromString(state.SortMode),
			TypeFilterMode: TypeFilterModeFromString(state.TypeFilter),
			IncludeClosed:  state.IncludeClosed,
		}
	}
}

// RestoreLastBoardMsg is sent when restoring the last viewed board on launch
type RestoreLastBoardMsg struct {
	Board *models.Board
}

// RestoreFilterMsg is sent when restoring saved filter state on launch
type RestoreFilterMsg struct {
	SearchQuery    string
	SortMode       SortMode
	TypeFilterMode TypeFilterMode
	IncludeClosed  bool
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle TickMsg before any UI-mode interceptions to keep the poll chain
	// alive. Without this, opening a form (or other overlay that intercepts all
	// messages) would swallow the TickMsg, preventing scheduleTick() from being
	// called, permanently breaking the periodic refresh cycle.
	if _, ok := msg.(TickMsg); ok {
		cmds := []tea.Cmd{m.fetchData(), m.scheduleTick()}
		if m.TaskListMode == TaskListModeBoard && m.BoardMode.Board != nil {
			cmds = append(cmds, m.fetchBoardIssues(m.BoardMode.Board.ID))
		}
		if modalCmd := m.fetchModalDataIfOpen(); modalCmd != nil {
			cmds = append(cmds, modalCmd)
		}
		// Periodic auto-sync (backup path — primary sync runs in independent goroutine
		// in cmd/monitor.go, since BubbleTea Cmd dispatch can stall under some PTYs)
		if m.AutoSyncFunc != nil && m.AutoSyncInterval > 0 && time.Since(m.LastAutoSync) >= m.AutoSyncInterval {
			m.LastAutoSync = time.Now()
			syncFn := m.AutoSyncFunc
			cmds = append(cmds, func() tea.Msg {
				syncFn()
				return nil
			})
		}
		return m, tea.Batch(cmds...)
	}

	// Form mode: forward all messages to huh form first
	if m.FormOpen && m.FormState != nil && m.FormState.Form != nil {
		return m.handleFormUpdate(msg)
	}

	// Board editor mode: forward non-key messages to inputs (cursor blink, etc.)
	if m.BoardEditorOpen && m.BoardEditorMode != "info" {
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			var cmds []tea.Cmd
			if m.BoardEditorNameInput != nil {
				var nameCmd tea.Cmd
				*m.BoardEditorNameInput, nameCmd = m.BoardEditorNameInput.Update(msg)
				if nameCmd != nil {
					cmds = append(cmds, nameCmd)
				}
			}
			if m.BoardEditorQueryInput != nil {
				var queryCmd tea.Cmd
				*m.BoardEditorQueryInput, queryCmd = m.BoardEditorQueryInput.Update(msg)
				if queryCmd != nil {
					cmds = append(cmds, queryCmd)
				}
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
	}

	// Close confirmation mode: forward non-key messages to textinput (cursor blink, etc.)
	// Key messages are handled in handleKey() via the declarative modal
	if m.CloseConfirmOpen {
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			var inputCmd tea.Cmd
			m.CloseConfirmInput, inputCmd = m.CloseConfirmInput.Update(msg)
			if inputCmd != nil {
				return m, inputCmd
			}
		}
	}

	// Search mode: forward non-key messages to textinput (cursor blink, etc.)
	// Key messages are handled in handleKey() to avoid double-processing
	if m.SearchMode {
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			var inputCmd tea.Cmd
			m.SearchInput, inputCmd = m.SearchInput.Update(msg)
			if inputCmd != nil {
				return m, inputCmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.updatePanelBounds()
		// Re-render markdown if modal is open (width may have changed)
		if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
			if modal.Issue.Description != "" || modal.Issue.Acceptance != "" {
				width := m.modalContentWidth()
				return m, m.renderMarkdownAsync(modal.IssueID, modal.Issue.Description, modal.Issue.Acceptance, width)
			}
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	// NOTE: TickMsg is handled above the form/overlay interception block
	// to prevent the poll chain from breaking. Do not add a TickMsg case here.

	case RefreshDataMsg:
		m.FocusedIssue = msg.FocusedIssue
		m.InProgress = msg.InProgress
		m.Activity = msg.Activity
		m.TaskList = msg.TaskList
		m.RecentHandoffs = msg.RecentHandoffs
		m.ActiveSessions = msg.ActiveSessions
		m.LastRefresh = msg.Timestamp

		// Build flattened rows for selection
		m.buildCurrentWorkRows()
		m.buildTaskListRows()

		// Restore cursor positions from saved issue IDs
		m.restoreCursors()
		return m, nil

	case IssueDetailsMsg:
		// Only update if this is for the currently open modal
		if modal := m.CurrentModal(); modal != nil && msg.IssueID == modal.IssueID {
			// Detect initial load vs reactive refresh
			isInitialLoad := modal.Issue == nil

			modal.Loading = false
			modal.Error = msg.Error
			modal.Issue = msg.Issue
			modal.Handoff = msg.Handoff
			modal.Logs = msg.Logs
			modal.Comments = msg.Comments
			modal.BlockedBy = msg.BlockedBy
			modal.Blocks = msg.Blocks
			modal.EpicTasks = msg.EpicTasks
			modal.ParentEpic = msg.ParentEpic
			if isInitialLoad {
				modal.ParentEpicFocused = false // Only reset focus on initial load
			}

			// Calculate content lines for scroll clamping
			modal.ContentLines = m.estimateModalContentLines(modal)

			// Auto-focus task section for epics with tasks (enables j/k navigation)
			// Only on initial load - preserve cursor state during reactive refreshes
			if isInitialLoad && msg.Issue != nil && msg.Issue.Type == models.TypeEpic && len(msg.EpicTasks) > 0 {
				modal.TaskSectionFocused = true
				modal.EpicTasksCursor = 0
			}

			// On refresh, clamp cursors to valid range if items were removed
			if !isInitialLoad {
				if len(modal.EpicTasks) > 0 && modal.EpicTasksCursor >= len(modal.EpicTasks) {
					modal.EpicTasksCursor = len(modal.EpicTasks) - 1
				}
				if len(modal.BlockedBy) > 0 && modal.BlockedByCursor >= len(modal.BlockedBy) {
					modal.BlockedByCursor = len(modal.BlockedBy) - 1
				}
				if len(modal.Blocks) > 0 && modal.BlocksCursor >= len(modal.Blocks) {
					modal.BlocksCursor = len(modal.Blocks) - 1
				}
			}

			// Trigger async markdown rendering (expensive)
			if msg.Issue != nil && (msg.Issue.Description != "" || msg.Issue.Acceptance != "") {
				width := m.modalContentWidth()
				return m, m.renderMarkdownAsync(msg.IssueID, msg.Issue.Description, msg.Issue.Acceptance, width)
			}
		}
		return m, nil

	case MarkdownRenderedMsg:
		// Only update if this is for the currently open modal
		if modal := m.CurrentModal(); modal != nil && msg.IssueID == modal.IssueID {
			modal.DescRender = msg.DescRender
			modal.AcceptRender = msg.AcceptRender
			// Recalculate content lines after markdown rendering
			modal.ContentLines = m.estimateModalContentLines(modal)
		}
		return m, nil

	case StatsDataMsg:
		// Only update if stats modal is open
		if m.StatsOpen {
			m.StatsLoading = false
			m.StatsError = msg.Error
			m.StatsData = msg.Data
			// Create declarative modal now that data is available
			if msg.Error == nil && msg.Data != nil && msg.Data.ExtendedStats != nil {
				m.StatsModal = m.createStatsModal()
				m.StatsModal.Reset()
			}
		}
		return m, nil

	case HandoffsDataMsg:
		// Only update if handoffs modal is open
		if m.HandoffsOpen {
			m.HandoffsLoading = false
			m.HandoffsError = msg.Error
			m.HandoffsData = msg.Data
			// Create declarative modal now that data is available
			if msg.Error == nil && len(msg.Data) > 0 {
				m.HandoffsModal = m.createHandoffsModal()
				m.HandoffsModal.Reset()
			}
		}
		return m, nil

	case ClearStatusMsg:
		m.StatusMessage = ""
		m.StatusIsError = false
		return m, nil

	case FirstRunCheckMsg:
		m.AgentFilePath = msg.AgentFilePath
		m.AgentFileHasTD = msg.HasInstructions
		if msg.IsFirstRun {
			m.IsFirstRunInit = true
			m.GettingStartedOpen = true
			m.GettingStartedModal = m.createGettingStartedModal()
			m.GettingStartedModal.Reset()
			m.GettingStartedMouseHandler = mouse.NewHandler()
		}
		return m, nil

	case InstallInstructionsResultMsg:
		if msg.Success {
			m.StatusMessage = msg.Message
			m.StatusIsError = false
			m.AgentFileHasTD = true
			// Recreate modal to show updated state (checkmark)
			if m.GettingStartedOpen {
				m.GettingStartedModal = m.createGettingStartedModal()
			}
		} else {
			m.StatusMessage = msg.Message
			m.StatusIsError = true
		}
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })

	case version.UpdateAvailableMsg:
		m.UpdateAvail = &msg
		return m, nil

	case PaneHeightsSavedMsg:
		// Pane heights saved (or failed) - just ignore errors silently
		return m, nil

	case boardEditorDebounceMsg:
		// Only execute if board editor is still open and query matches current input
		if m.BoardEditorOpen && m.BoardEditorQueryInput != nil && msg.Query == m.BoardEditorQueryInput.Value() {
			return m, m.boardEditorQueryPreview(msg.Query)
		}
		return m, nil

	case BoardEditorSaveResultMsg:
		if msg.Error != nil {
			m.StatusMessage = "Error: " + msg.Error.Error()
			m.StatusIsError = true
			return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
		}
		action := "Updated"
		if msg.IsNew {
			action = "Created"
		}
		m.StatusMessage = action + " board: " + msg.Board.Name
		m.StatusIsError = false
		m.closeBoardEditorModal()
		// Refresh boards list to pick up changes
		return m, tea.Batch(
			m.fetchBoards(),
			tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		)

	case BoardEditorDeleteResultMsg:
		if msg.Error != nil {
			m.StatusMessage = "Error: " + msg.Error.Error()
			m.StatusIsError = true
			return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })
		}
		m.StatusMessage = "Board deleted"
		m.StatusIsError = false
		m.closeBoardEditorModal()
		// If the deleted board was the active board, exit board mode
		if m.BoardMode.Board != nil && m.BoardMode.Board.ID == msg.BoardID {
			m.TaskListMode = TaskListModeCategorized
			m.BoardMode.Board = nil
		}
		return m, tea.Batch(
			m.fetchBoards(),
			tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} }),
		)

	case BoardEditorQueryPreviewMsg:
		// Only update if the board editor is still open and query matches
		if m.BoardEditorOpen && m.BoardEditorPreview != nil && m.BoardEditorQueryInput != nil && msg.Query == m.BoardEditorQueryInput.Value() {
			// Write to the shared pointer so the modal's Custom closures see updates
			m.BoardEditorPreview.Count = msg.Count
			m.BoardEditorPreview.Titles = msg.Titles
			m.BoardEditorPreview.Error = msg.Error
			m.BoardEditorPreview.Query = msg.Query
		}
		return m, nil

	case BoardsDataMsg:
		m.AllBoards = msg.Boards
		if msg.Error != nil {
			m.StatusMessage = "Error loading boards: " + msg.Error.Error()
			m.StatusIsError = true
			// Close the modal on error
			m.closeBoardPickerModal()
			return m, nil
		}
		// Create declarative modal now that data is available
		if m.BoardPickerOpen && len(msg.Boards) > 0 {
			m.BoardPickerModal = m.createBoardPickerModal()
			m.BoardPickerModal.Reset()
		}
		return m, nil

	case BoardIssuesMsg:
		if m.BoardMode.Board != nil && m.BoardMode.Board.ID == msg.BoardID {
			if msg.Error != nil {
				m.StatusMessage = "Error loading board issues: " + msg.Error.Error()
				m.StatusIsError = true
			}
			// Apply search filter to board issues (for both backlog and swimlanes)
			filteredIssues := filterBoardIssuesByQuery(msg.Issues, m.SearchQuery)
			m.BoardMode.Issues = filteredIssues
			// Build swimlane data using filtered issues
			m.BoardMode.SwimlaneData = CategorizeBoardIssues(m.DB, filteredIssues, m.SessionID, m.SortMode, msg.RejectedIDs)
			m.BoardMode.SwimlaneRows = BuildSwimlaneRows(m.BoardMode.SwimlaneData)

			// Clamp kanban cursor if the kanban view is open (data may have changed)
			if m.KanbanOpen {
				m.clampKanbanRow()
				m.ensureKanbanCursorVisible()
			}

			// Restore selection if we have a pending selection ID (from move operations)
			if m.BoardMode.PendingSelectionID != "" {
				// Find the issue in the backlog view
				for i, biv := range m.BoardMode.Issues {
					if biv.Issue.ID == m.BoardMode.PendingSelectionID {
						m.BoardMode.Cursor = i
						m.ensureBoardCursorVisible()
						break
					}
				}
				// Find the issue in swimlanes view
				for i, row := range m.BoardMode.SwimlaneRows {
					if row.Issue.ID == m.BoardMode.PendingSelectionID {
						m.BoardMode.SwimlaneCursor = i
						m.ensureSwimlaneCursorVisible()
						break
					}
				}
				m.BoardMode.PendingSelectionID = "" // Clear after use
			}
		}
		return m, nil

	case RestoreLastBoardMsg:
		if msg.Board != nil {
			m.TaskListMode = TaskListModeBoard
			m.ActivePanel = PanelTaskList // Focus the Task List panel
			m.BoardMode.Board = msg.Board
			m.BoardMode.Cursor = 0
			m.BoardMode.ScrollOffset = 0
			m.BoardMode.SwimlaneCursor = 0
			m.BoardMode.SwimlaneScroll = 0
			m.BoardMode.StatusFilter = DefaultBoardStatusFilter()
			m.BoardMode.ViewMode = BoardViewModeFromString(msg.Board.ViewMode)
			return m, m.fetchBoardIssues(msg.Board.ID)
		}
		return m, nil

	case RestoreFilterMsg:
		m.SearchQuery = msg.SearchQuery
		m.SortMode = msg.SortMode
		m.TypeFilterMode = msg.TypeFilterMode
		m.IncludeClosed = msg.IncludeClosed
		// Update the search input to show restored query
		m.SearchInput.SetValue(msg.SearchQuery)
		// Refresh data with restored filters
		return m, m.fetchData()

	case SyncPromptDataMsg:
		if msg.Error != nil || msg.Projects == nil {
			return m, nil
		}
		m.SyncPromptOpen = true
		m.SyncPromptPhase = syncPromptPhaseList
		m.SyncPromptProjects = msg.Projects
		m.SyncPromptCursor = 0
		m.SyncPromptModal = m.buildSyncPromptListModal(msg.Projects)
		m.SyncPromptMouse = mouse.NewHandler()
		return m, nil

	case SyncPromptLinkResultMsg:
		if msg.Success {
			m.StatusMessage = fmt.Sprintf("Linked to %s", msg.ProjectName)
			m.StatusIsError = false
		} else {
			m.StatusMessage = fmt.Sprintf("Link failed: %v", msg.Error)
			m.StatusIsError = true
		}
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })

	case SyncPromptCreateResultMsg:
		if msg.Success {
			m.StatusMessage = fmt.Sprintf("Created and linked %s", msg.ProjectName)
			m.StatusIsError = false
		} else {
			m.StatusMessage = fmt.Sprintf("Create failed: %v", msg.Error)
			m.StatusIsError = true
		}
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return ClearStatusMsg{} })

	case OpenIssueByIDMsg:
		if msg.IssueID != "" {
			return m.pushModal(msg.IssueID, m.ActivePanel)
		}
		return m, nil
	}

	return m, nil
}

// CurrentContextString returns the current keymap context as a sidecar-formatted string.
// This is used by sidecar's TD plugin to determine which shortcuts to display.
func (m Model) CurrentContextString() string {
	return keymap.ContextToSidecar(m.currentContext())
}

// View implements tea.Model
func (m Model) View() string {
	return m.renderView()
}

// scheduleTick returns a command that sends a TickMsg after the refresh interval
func (m Model) scheduleTick() tea.Cmd {
	return tea.Tick(m.RefreshInterval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// fetchData returns a command that fetches all data and sends a RefreshDataMsg
func (m Model) fetchData() tea.Cmd {
	return func() tea.Msg {
		data := FetchData(m.DB, m.SessionID, m.StartedAt, m.SearchQuery, m.IncludeClosed, m.SortMode)
		return data
	}
}

// fetchModalDataIfOpen returns a command to refresh the current modal's data
// if a modal is open, otherwise returns nil
func (m Model) fetchModalDataIfOpen() tea.Cmd {
	modal := m.CurrentModal()
	if modal == nil || modal.Loading {
		return nil
	}
	return m.fetchIssueDetails(modal.IssueID)
}

// fetchIssueDetails returns a command that fetches issue details for the modal
func (m Model) fetchIssueDetails(issueID string) tea.Cmd {
	return func() tea.Msg {
		msg := IssueDetailsMsg{IssueID: issueID}

		// Fetch issue
		issue, err := m.DB.GetIssue(issueID)
		if err != nil {
			msg.Error = err
			return msg
		}
		msg.Issue = issue

		// Fetch latest handoff (may not exist)
		handoff, _ := m.DB.GetLatestHandoff(issueID)
		msg.Handoff = handoff

		// Fetch recent logs (cap at 20)
		logs, _ := m.DB.GetLogs(issueID, 20)
		msg.Logs = logs

		// Fetch comments
		comments, _ := m.DB.GetComments(issueID)
		msg.Comments = comments

		// Fetch parent epic if this issue has a parent
		if issue.ParentID != "" {
			if parent, err := m.DB.GetIssue(issue.ParentID); err == nil && parent.Type == models.TypeEpic {
				msg.ParentEpic = parent
			}
			// Silently ignore errors - parent may have been deleted
		}

		// Fetch dependencies (blocked by) and dependents (blocks) with batch query
		depIDs, _ := m.DB.GetDependencies(issueID)
		blockedIDs, _ := m.DB.GetBlockedBy(issueID)

		// Combine IDs for single batch fetch
		allRelatedIDs := append(depIDs, blockedIDs...)
		if len(allRelatedIDs) > 0 {
			relatedIssues, _ := m.DB.GetIssuesByIDs(allRelatedIDs)
			// Build lookup map
			issueMap := make(map[string]models.Issue)
			for _, i := range relatedIssues {
				issueMap[i.ID] = i
			}
			// Split into BlockedBy and Blocks
			for _, depID := range depIDs {
				if i, ok := issueMap[depID]; ok {
					msg.BlockedBy = append(msg.BlockedBy, i)
				}
			}
			for _, blockedID := range blockedIDs {
				if i, ok := issueMap[blockedID]; ok {
					msg.Blocks = append(msg.Blocks, i)
				}
			}
		}

		// Fetch child tasks if this is an epic
		if issue.Type == models.TypeEpic {
			epicTasks, _ := m.DB.ListIssues(db.ListIssuesOptions{ParentID: issueID})
			msg.EpicTasks = epicTasks
		}

		return msg
	}
}

// fetchStats returns a command that fetches stats data for the stats modal
func (m Model) fetchStats() tea.Cmd {
	return func() tea.Msg {
		return FetchStats(m.DB)
	}
}

// fetchHandoffs returns a command that fetches all handoffs
func (m Model) fetchHandoffs() tea.Cmd {
	return func() tea.Msg {
		handoffs, err := m.DB.GetRecentHandoffs(50, time.Time{})
		return HandoffsDataMsg{Data: handoffs, Error: err}
	}
}

// ensureBoardCursorVisible adjusts the board scroll offset to keep the cursor visible.
// Uses content height matching the rendering (panelHeight - 3) and dynamically
// accounts for scroll indicator lines based on current scroll position.
func (m *Model) ensureBoardCursorVisible() {
	if m.BoardMode.ViewMode == BoardViewSwimlanes {
		m.ensureSwimlaneCursorVisible()
		return
	}

	totalItems := len(m.BoardMode.Issues)
	contentHeight := m.panelHeight(PanelTaskList) - 3 // matches rendering's maxLines
	if contentHeight < 1 {
		contentHeight = 10
	}

	cursor := m.BoardMode.Cursor
	offset := m.BoardMode.ScrollOffset
	needsScroll := totalItems > contentHeight

	// Calculate effective visible items matching rendering indicator logic
	effectiveHeight := contentHeight
	if needsScroll && offset > 0 {
		effectiveHeight-- // up indicator
	}
	if needsScroll && offset+effectiveHeight < totalItems {
		effectiveHeight-- // down indicator
	}
	if effectiveHeight < 1 {
		effectiveHeight = 1
	}

	// Scroll down if cursor below viewport
	if cursor >= offset+effectiveHeight {
		// After scrolling down, offset > 0 so up indicator will appear.
		// Use worst-case (both indicators) for the new offset calculation
		// to ensure cursor is always visible regardless of indicator state.
		worstCase := contentHeight - 2
		if worstCase < 1 {
			worstCase = 1
		}
		m.BoardMode.ScrollOffset = cursor - worstCase + 1
	}

	// Scroll up if cursor above viewport
	if cursor < m.BoardMode.ScrollOffset {
		m.BoardMode.ScrollOffset = cursor
	}

	// Clamp scroll offset to valid range
	maxScroll := m.maxScrollOffset(PanelTaskList)
	if m.BoardMode.ScrollOffset > maxScroll {
		m.BoardMode.ScrollOffset = maxScroll
	}
	if m.BoardMode.ScrollOffset < 0 {
		m.BoardMode.ScrollOffset = 0
	}
}

// ensureSwimlaneCursorVisible adjusts the swimlane scroll offset to keep the cursor visible.
// Accounts for category headers and separator lines that consume display space.
func (m *Model) ensureSwimlaneCursorVisible() {
	totalItems := len(m.BoardMode.SwimlaneRows)
	contentHeight := m.panelHeight(PanelTaskList) - 3 // matches rendering's maxLines
	if contentHeight < 1 {
		contentHeight = 10
	}

	cursor := m.BoardMode.SwimlaneCursor
	offset := m.BoardMode.SwimlaneScroll
	// Use total display lines (items + headers + separators) not raw item count
	totalDisplayLines := m.swimlaneLinesFromOffset(0, totalItems)
	needsScroll := totalDisplayLines > contentHeight

	// Calculate effective visible items accounting for indicators and headers
	effectiveHeight := contentHeight
	if needsScroll && offset > 0 {
		effectiveHeight-- // up indicator
	}
	if needsScroll && m.swimlaneLinesFromOffset(offset, totalItems) > effectiveHeight {
		effectiveHeight-- // down indicator
	}
	// Subtract category header lines between offset and cursor
	headerLines := m.swimlaneHeaderLinesBetween(offset, cursor)
	effectiveHeight -= headerLines
	if effectiveHeight < 1 {
		effectiveHeight = 1
	}

	// Scroll down if cursor below viewport
	if cursor >= offset+effectiveHeight {
		// Find the smallest offset where cursor is visible by starting from
		// cursor (trivially fits as 1 item) and walking back to show more context.
		newOffset := cursor
		for newOffset > 0 {
			lines := m.swimlaneLinesFromOffset(newOffset-1, cursor+1)
			available := contentHeight
			if newOffset-1 > 0 {
				available-- // up indicator when scrolled
			}
			if cursor+1 < totalItems {
				available-- // down indicator when more items below
			}
			if lines > available {
				break
			}
			newOffset--
		}
		m.BoardMode.SwimlaneScroll = newOffset
	}

	// Scroll up if cursor above viewport
	if cursor < m.BoardMode.SwimlaneScroll {
		m.BoardMode.SwimlaneScroll = cursor
	}

	// Clamp scroll offset to valid range
	maxScroll := m.maxScrollOffset(PanelTaskList)
	if m.BoardMode.SwimlaneScroll > maxScroll {
		m.BoardMode.SwimlaneScroll = maxScroll
	}
	if m.BoardMode.SwimlaneScroll < 0 {
		m.BoardMode.SwimlaneScroll = 0
	}
}

// swimlaneHeaderLinesBetween counts category header and separator lines between
// two swimlane row indices. Matches renderBoardSwimlanesView's header logic.
func (m Model) swimlaneHeaderLinesBetween(startIdx, endIdx int) int {
	rows := m.BoardMode.SwimlaneRows
	if len(rows) == 0 || startIdx >= endIdx {
		return 0
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	// Track category from before the start (matches rendering's skip loop)
	var currentCategory TaskListCategory
	for i := 0; i < startIdx && i < len(rows); i++ {
		currentCategory = rows[i].Category
	}

	lines := 0
	for i := startIdx; i < endIdx; i++ {
		if rows[i].Category != currentCategory {
			if i > startIdx {
				lines++ // blank separator (only if not first visible item)
			}
			lines++ // category header
			currentCategory = rows[i].Category
		}
	}
	return lines
}

// swimlaneLinesFromOffset counts total display lines (items + headers + separators)
// for swimlane rows from startIdx to endIdx (exclusive). Matches renderBoardSwimlanesView.
func (m Model) swimlaneLinesFromOffset(startIdx, endIdx int) int {
	rows := m.BoardMode.SwimlaneRows
	if len(rows) == 0 || startIdx >= len(rows) {
		return 0
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	// Track category from before the start (matches rendering's skip loop)
	var currentCategory TaskListCategory
	for i := 0; i < startIdx && i < len(rows); i++ {
		currentCategory = rows[i].Category
	}

	lines := 0
	for i := startIdx; i < endIdx; i++ {
		if rows[i].Category != currentCategory {
			if lines > 0 {
				lines++ // blank separator
			}
			lines++ // category header
			currentCategory = rows[i].Category
		}
		lines++ // the row itself
	}
	return lines
}

// swimlaneMaxScroll returns the maximum valid scroll offset for swimlane view.
// Walks backwards from the end to find the smallest offset where all remaining
// content (items + headers) fits in the available space with an up indicator.
func (m Model) swimlaneMaxScroll(contentHeight int) int {
	totalItems := len(m.BoardMode.SwimlaneRows)
	if totalItems == 0 {
		return 0
	}

	// At max scroll: up indicator present (1 line), no down indicator
	availableForContent := contentHeight - 1
	if availableForContent < 1 {
		return 0
	}

	// Walk backwards to find the smallest offset where content fits
	for offset := totalItems - 1; offset >= 0; offset-- {
		lines := m.swimlaneLinesFromOffset(offset, totalItems)
		if lines > availableForContent {
			if offset+1 < totalItems {
				return offset + 1
			}
			return offset
		}
	}
	return 0
}
