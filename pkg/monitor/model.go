package monitor

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/pkg/monitor/keymap"
)

// Panel represents which panel is active
type Panel int

const (
	PanelCurrentWork Panel = iota
	PanelTaskList
	PanelActivity
)

// ActivityItem represents a unified activity item (log, action, or comment)
type ActivityItem struct {
	Timestamp time.Time
	SessionID string
	Type      string // "log", "action", "comment"
	IssueID   string
	Message   string
	LogType   models.LogType    // for logs
	Action    models.ActionType // for actions
}

// TaskListData holds categorized issues for the task list panel
type TaskListData struct {
	Ready      []models.Issue
	Reviewable []models.Issue
	Blocked    []models.Issue
	Closed     []models.Issue
}

// TaskListCategory represents the category of a task list row
type TaskListCategory string

const (
	CategoryReviewable TaskListCategory = "REVIEW"
	CategoryReady      TaskListCategory = "READY"
	CategoryBlocked    TaskListCategory = "BLOCKED"
	CategoryClosed     TaskListCategory = "CLOSED"
)

// SortMode represents task list sorting
type SortMode int

const (
	SortByPriority    SortMode = iota // Default: priority ASC
	SortByCreatedDesc                 // created_at DESC (newest first)
	SortByUpdatedDesc                 // updated_at DESC (recently changed first)
)

// String returns display name for sort mode
func (s SortMode) String() string {
	switch s {
	case SortByCreatedDesc:
		return "created"
	case SortByUpdatedDesc:
		return "updated"
	default:
		return "priority"
	}
}

// ToDBOptions returns SortBy and SortDesc for ListIssuesOptions
func (s SortMode) ToDBOptions() (sortBy string, sortDesc bool) {
	switch s {
	case SortByCreatedDesc:
		return "created_at", true
	case SortByUpdatedDesc:
		return "updated_at", true
	default:
		return "priority", false
	}
}

// TaskListRow represents a single selectable row in the task list panel
type TaskListRow struct {
	Issue    models.Issue
	Category TaskListCategory
}

// RecentHandoff represents a recent handoff for display
type RecentHandoff struct {
	IssueID   string
	SessionID string
	Timestamp time.Time
}

// ModalEntry represents a single modal in the stack
type ModalEntry struct {
	// Core
	IssueID     string
	SourcePanel Panel // Only meaningful for base entry (depth 1)

	// Display
	Scroll int

	// Async data
	Loading      bool
	Error        error
	Issue        *models.Issue
	Handoff      *models.Handoff
	Logs         []models.Log
	BlockedBy    []models.Issue
	Blocks       []models.Issue
	DescRender   string
	AcceptRender string

	// Epic-specific (when Issue.Type == "epic")
	EpicTasks          []models.Issue
	EpicTasksCursor    int
	TaskSectionFocused bool

	// Parent epic (when issue has ParentID pointing to an epic)
	ParentEpic        *models.Issue
	ParentEpicFocused bool
}

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
	ActivePanel  Panel
	ScrollOffset map[Panel]int
	Cursor       map[Panel]int    // Per-panel cursor position (selected row)
	SelectedID   map[Panel]string // Per-panel selected issue ID (preserved across refresh)
	ShowHelp     bool
	ShowTDQHelp  bool // Show TDQ query syntax help (when in search mode)
	LastRefresh  time.Time
	StartedAt    time.Time // When monitor started, to track new handoffs
	Err          error     // Last error, if any
	Embedded     bool      // When true, skip footer (embedded in sidecar)

	// Flattened rows for selection
	TaskListRows    []TaskListRow // Flattened task list for selection
	CurrentWorkRows []string      // Issue IDs for current work panel (focused + in-progress)

	// Modal stack for stacking modals (empty = no modal open)
	ModalStack []ModalEntry

	// Search state
	SearchMode    bool     // Whether search mode is active
	SearchQuery   string   // Current search query
	IncludeClosed bool     // Whether to include closed tasks
	SortMode      SortMode // Task list sort order

	// Confirmation dialog state
	ConfirmOpen    bool
	ConfirmAction  string // "delete"
	ConfirmIssueID string
	ConfirmTitle   string

	// Stats modal state
	StatsOpen    bool
	StatsLoading bool
	StatsData    *StatsData
	StatsScroll  int
	StatsError   error

	// Configuration
	RefreshInterval time.Duration

	// Keymap registry for keyboard shortcuts
	Keymap *keymap.Registry
}

// MinWidth is the minimum terminal width for proper display
const MinWidth = 40

// MinHeight is the minimum terminal height for proper display
const MinHeight = 15

// TickMsg triggers a data refresh
type TickMsg time.Time

// RefreshDataMsg carries refreshed data
type RefreshDataMsg struct {
	FocusedIssue   *models.Issue
	InProgress     []models.Issue
	Activity       []ActivityItem
	TaskList       TaskListData
	RecentHandoffs []RecentHandoff
	ActiveSessions []string
	Timestamp      time.Time
}

// IssueDetailsMsg carries fetched issue details for the modal
type IssueDetailsMsg struct {
	IssueID    string
	Issue      *models.Issue
	Handoff    *models.Handoff
	Logs       []models.Log
	BlockedBy  []models.Issue // Dependencies (issues blocking this one)
	Blocks     []models.Issue // Dependents (issues blocked by this one)
	EpicTasks  []models.Issue // Child tasks (when issue is an epic)
	ParentEpic *models.Issue  // Parent epic (when issue.ParentID is set)
	Error      error
}

// MarkdownRenderedMsg carries pre-rendered markdown for the modal
type MarkdownRenderedMsg struct {
	IssueID    string
	DescRender string
	AcceptRender string
}

// NewModel creates a new monitor model
func NewModel(database *db.DB, sessionID string, interval time.Duration) Model {
	// Initialize keymap with default bindings
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	return Model{
		DB:              database,
		SessionID:       sessionID,
		RefreshInterval: interval,
		ScrollOffset:    make(map[Panel]int),
		Cursor:          make(map[Panel]int),
		SelectedID:      make(map[Panel]string),
		ActivePanel:     PanelCurrentWork,
		StartedAt:       time.Now(),
		SearchMode:      false,
		SearchQuery:     "",
		IncludeClosed:   false,
		Keymap:          km,
	}
}

// NewEmbedded creates a monitor model for embedding in external applications.
// It opens the database and creates/gets a session automatically.
// The caller must call Close() when done to release resources.
func NewEmbedded(baseDir string, interval time.Duration) (*Model, error) {
	database, err := db.Open(baseDir)
	if err != nil {
		return nil, err
	}

	sess, err := session.GetOrCreate(baseDir)
	if err != nil {
		database.Close()
		return nil, err
	}

	m := NewModel(database, sess.ID, interval)
	m.Embedded = true
	return &m, nil
}

// Close releases resources held by an embedded monitor.
// Only call this if the model was created with NewEmbedded.
func (m *Model) Close() error {
	if m.DB != nil {
		return m.DB.Close()
	}
	return nil
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchData(),
		m.scheduleTick(),
	)
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case TickMsg:
		return m, tea.Batch(m.fetchData(), m.scheduleTick())

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
			modal.Loading = false
			modal.Error = msg.Error
			modal.Issue = msg.Issue
			modal.Handoff = msg.Handoff
			modal.Logs = msg.Logs
			modal.BlockedBy = msg.BlockedBy
			modal.Blocks = msg.Blocks
			modal.EpicTasks = msg.EpicTasks
			modal.ParentEpic = msg.ParentEpic
			modal.ParentEpicFocused = false // Reset focus on load

			// Auto-focus task section for epics with tasks (enables j/k navigation)
			if msg.Issue != nil && msg.Issue.Type == models.TypeEpic && len(msg.EpicTasks) > 0 {
				modal.TaskSectionFocused = true
				modal.EpicTasksCursor = 0
			}

			// Trigger async markdown rendering (expensive)
			if msg.Issue != nil && (msg.Issue.Description != "" || msg.Issue.Acceptance != "") {
				width := m.Width - 20
				if width < 40 {
					width = 40
				}
				return m, m.renderMarkdownAsync(msg.IssueID, msg.Issue.Description, msg.Issue.Acceptance, width)
			}
		}
		return m, nil

	case MarkdownRenderedMsg:
		// Only update if this is for the currently open modal
		if modal := m.CurrentModal(); modal != nil && msg.IssueID == modal.IssueID {
			modal.DescRender = msg.DescRender
			modal.AcceptRender = msg.AcceptRender
		}
		return m, nil

	case StatsDataMsg:
		// Only update if stats modal is open
		if m.StatsOpen {
			m.StatsLoading = false
			m.StatsError = msg.Error
			m.StatsData = msg.Data
		}
		return m, nil
	}

	return m, nil
}

// currentContext returns the keymap context based on current UI state
func (m Model) currentContext() keymap.Context {
	if m.ConfirmOpen {
		return keymap.ContextConfirm
	}
	if m.StatsOpen {
		return keymap.ContextStats
	}
	if m.ModalOpen() {
		if modal := m.CurrentModal(); modal != nil {
			// Check if parent epic row is focused
			if modal.ParentEpicFocused {
				return keymap.ContextParentEpicFocused
			}
			// Check if epic tasks section is focused
			if modal.TaskSectionFocused {
				return keymap.ContextEpicTasks
			}
		}
		return keymap.ContextModal
	}
	if m.SearchMode {
		return keymap.ContextSearch
	}
	return keymap.ContextMain
}

// handleKey processes key input using the centralized keymap registry
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ctx := m.currentContext()

	// Search mode handles printable characters specially
	if ctx == keymap.ContextSearch {
		// Special case: ? triggers help even in search mode
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' {
			return m.executeCommand(keymap.CmdToggleHelp)
		}
		if keymap.IsPrintable(msg) {
			m.SearchQuery += string(msg.Runes)
			return m, m.fetchData()
		}
		// Handle space specially in search mode
		if msg.Type == tea.KeySpace {
			m.SearchQuery += " "
			return m, m.fetchData()
		}
	}

	// Look up command from keymap
	cmd, found := m.Keymap.Lookup(msg, ctx)
	if !found {
		return m, nil
	}

	// Execute command
	return m.executeCommand(cmd)
}

// executeCommand executes a keymap command and returns the updated model and any tea.Cmd
func (m Model) executeCommand(cmd keymap.Command) (tea.Model, tea.Cmd) {
	switch cmd {
	// Global commands
	case keymap.CmdQuit:
		return m, tea.Quit

	case keymap.CmdToggleHelp:
		// Show TDQ help when in search mode, regular help otherwise
		if m.SearchMode {
			m.ShowTDQHelp = !m.ShowTDQHelp
			m.ShowHelp = false
		} else {
			m.ShowHelp = !m.ShowHelp
			m.ShowTDQHelp = false
		}
		return m, nil

	case keymap.CmdRefresh:
		if modal := m.CurrentModal(); modal != nil {
			return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(modal.IssueID))
		}
		if m.StatsOpen {
			return m, m.fetchStats()
		}
		return m, m.fetchData()

	// Panel navigation (main context)
	case keymap.CmdNextPanel:
		m.ActivePanel = (m.ActivePanel + 1) % 3
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	case keymap.CmdPrevPanel:
		m.ActivePanel = (m.ActivePanel + 2) % 3
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	case keymap.CmdFocusPanel1:
		m.ActivePanel = PanelCurrentWork
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	case keymap.CmdFocusPanel2:
		m.ActivePanel = PanelTaskList
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	case keymap.CmdFocusPanel3:
		m.ActivePanel = PanelActivity
		m.clampCursor(m.ActivePanel)
		m.ensureCursorVisible(m.ActivePanel)
		return m, nil

	// Cursor movement
	case keymap.CmdCursorDown, keymap.CmdScrollDown:
		if modal := m.CurrentModal(); modal != nil {
			if modal.ParentEpicFocused {
				// Unfocus parent epic, move past epic zone so next j scrolls
				modal.ParentEpicFocused = false
				modal.Scroll = 1
			} else if modal.TaskSectionFocused {
				// Move epic task cursor
				if modal.EpicTasksCursor < len(modal.EpicTasks)-1 {
					modal.EpicTasksCursor++
				}
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top with parent epic, focus it first before scrolling
				modal.ParentEpicFocused = true
			} else {
				modal.Scroll++
			}
		} else if m.StatsOpen {
			m.StatsScroll++
		} else {
			m.moveCursor(1)
		}
		return m, nil

	case keymap.CmdCursorUp, keymap.CmdScrollUp:
		if modal := m.CurrentModal(); modal != nil {
			if modal.ParentEpicFocused {
				// Already at top, stay focused on epic
			} else if modal.TaskSectionFocused {
				// Move epic task cursor
				if modal.EpicTasksCursor > 0 {
					modal.EpicTasksCursor--
				}
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top of scroll with parent epic, focus it
				modal.ParentEpicFocused = true
			} else if modal.Scroll > 0 {
				modal.Scroll--
			}
		} else if m.StatsOpen {
			if m.StatsScroll > 0 {
				m.StatsScroll--
			}
		} else {
			m.moveCursor(-1)
		}
		return m, nil

	case keymap.CmdCursorTop:
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll = 0
		} else if m.StatsOpen {
			m.StatsScroll = 0
		} else {
			m.Cursor[m.ActivePanel] = 0
			m.saveSelectedID(m.ActivePanel)
			m.ensureCursorVisible(m.ActivePanel)
		}
		return m, nil

	case keymap.CmdCursorBottom:
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll = 9999 // Will be clamped by view
		} else if m.StatsOpen {
			m.StatsScroll = 9999 // Will be clamped by view
		} else {
			count := m.rowCount(m.ActivePanel)
			if count > 0 {
				m.Cursor[m.ActivePanel] = count - 1
				m.saveSelectedID(m.ActivePanel)
				m.ensureCursorVisible(m.ActivePanel)
			}
		}
		return m, nil

	case keymap.CmdHalfPageDown:
		pageSize := m.visibleHeightForPanel(m.ActivePanel) / 2
		if pageSize < 1 {
			pageSize = 5
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll += pageSize
		} else if m.StatsOpen {
			m.StatsScroll += pageSize
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(1)
			}
		}
		return m, nil

	case keymap.CmdHalfPageUp:
		pageSize := m.visibleHeightForPanel(m.ActivePanel) / 2
		if pageSize < 1 {
			pageSize = 5
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll -= pageSize
			if modal.Scroll < 0 {
				modal.Scroll = 0
			}
		} else if m.StatsOpen {
			m.StatsScroll -= pageSize
			if m.StatsScroll < 0 {
				m.StatsScroll = 0
			}
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(-1)
			}
		}
		return m, nil

	case keymap.CmdFullPageDown:
		pageSize := m.visibleHeightForPanel(m.ActivePanel)
		if pageSize < 1 {
			pageSize = 10
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll += pageSize
		} else if m.StatsOpen {
			m.StatsScroll += pageSize
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(1)
			}
		}
		return m, nil

	case keymap.CmdFullPageUp:
		pageSize := m.visibleHeightForPanel(m.ActivePanel)
		if pageSize < 1 {
			pageSize = 10
		}
		if modal := m.CurrentModal(); modal != nil {
			modal.Scroll -= pageSize
			if modal.Scroll < 0 {
				modal.Scroll = 0
			}
		} else if m.StatsOpen {
			m.StatsScroll -= pageSize
			if m.StatsScroll < 0 {
				m.StatsScroll = 0
			}
		} else {
			for i := 0; i < pageSize; i++ {
				m.moveCursor(-1)
			}
		}
		return m, nil

	// Modal navigation
	case keymap.CmdNavigatePrev:
		return m.navigateModal(-1)

	case keymap.CmdNavigateNext:
		return m.navigateModal(1)

	case keymap.CmdClose:
		if m.ModalOpen() {
			m.closeModal()
		} else if m.StatsOpen {
			m.closeStatsModal()
		}
		return m, nil

	// Actions
	case keymap.CmdOpenDetails:
		return m.openModal()

	case keymap.CmdOpenStats:
		return m.openStatsModal()

	case keymap.CmdSearch:
		m.SearchMode = true
		m.SearchQuery = ""
		return m, nil

	case keymap.CmdToggleClosed:
		m.IncludeClosed = !m.IncludeClosed
		return m, m.fetchData()

	case keymap.CmdCycleSortMode:
		m.SortMode = (m.SortMode + 1) % 3
		return m, m.fetchData()

	case keymap.CmdMarkForReview:
		// Mark for review if in Current Work panel, otherwise refresh
		if m.ActivePanel == PanelCurrentWork {
			return m.markForReview()
		}
		return m, m.fetchData()

	case keymap.CmdApprove:
		if m.ActivePanel == PanelTaskList {
			return m.approveIssue()
		}
		return m, nil

	case keymap.CmdDelete:
		return m.confirmDelete()

	// Search commands
	case keymap.CmdSearchConfirm:
		m.SearchMode = false
		m.ShowTDQHelp = false
		return m, nil

	case keymap.CmdSearchCancel:
		// If TDQ help is open, close it but stay in search mode
		if m.ShowTDQHelp {
			m.ShowTDQHelp = false
			return m, nil
		}
		// Otherwise exit search mode entirely
		m.SearchMode = false
		m.SearchQuery = ""
		return m, m.fetchData()

	case keymap.CmdSearchClear:
		m.SearchQuery = ""
		return m, m.fetchData()

	case keymap.CmdSearchBackspace:
		if len(m.SearchQuery) > 0 {
			m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-1]
			return m, m.fetchData()
		}
		return m, nil

	// Confirmation commands
	case keymap.CmdConfirm:
		if m.ConfirmOpen && m.ConfirmAction == "delete" {
			return m.executeDelete()
		}
		return m, nil

	case keymap.CmdCancel:
		if m.ConfirmOpen {
			m.ConfirmOpen = false
		}
		return m, nil

	// Epic task navigation
	case keymap.CmdFocusTaskSection:
		if modal := m.CurrentModal(); modal != nil {
			// Only toggle if this is an epic with tasks
			if modal.Issue != nil && modal.Issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0 {
				modal.TaskSectionFocused = !modal.TaskSectionFocused
				if modal.TaskSectionFocused {
					// Reset cursor when entering task section
					modal.EpicTasksCursor = 0
				}
			}
		}
		return m, nil

	case keymap.CmdOpenEpicTask:
		if modal := m.CurrentModal(); modal != nil && modal.TaskSectionFocused {
			if modal.EpicTasksCursor < len(modal.EpicTasks) {
				taskID := modal.EpicTasks[modal.EpicTasksCursor].ID
				modal.TaskSectionFocused = false // Unfocus before pushing
				return m.pushModal(taskID, m.ModalSourcePanel())
			}
		}
		return m, nil

	case keymap.CmdOpenParentEpic:
		if modal := m.CurrentModal(); modal != nil && modal.ParentEpic != nil {
			modal.ParentEpicFocused = false // Unfocus before pushing
			return m.pushModal(modal.ParentEpic.ID, m.ModalSourcePanel())
		}
		return m, nil
	}

	return m, nil
}

// navigateModal moves to the prev/next issue in the source panel's list
// Only works at modal depth 1 (base modal)
func (m Model) navigateModal(delta int) (tea.Model, tea.Cmd) {
	// Only allow h/l navigation at depth 1
	if m.ModalDepth() != 1 {
		return m, nil
	}

	modal := m.CurrentModal()
	if modal == nil {
		return m, nil
	}

	// Get the list of issue IDs for the source panel (panel that opened the modal)
	var issueIDs []string
	switch m.ModalSourcePanel() {
	case PanelCurrentWork:
		issueIDs = m.CurrentWorkRows
	case PanelTaskList:
		for _, row := range m.TaskListRows {
			issueIDs = append(issueIDs, row.Issue.ID)
		}
	case PanelActivity:
		// For activity, collect unique issue IDs
		seen := make(map[string]bool)
		for _, item := range m.Activity {
			if item.IssueID != "" && !seen[item.IssueID] {
				seen[item.IssueID] = true
				issueIDs = append(issueIDs, item.IssueID)
			}
		}
	}

	if len(issueIDs) == 0 {
		return m, nil
	}

	// Find current position
	currentIdx := -1
	for i, id := range issueIDs {
		if id == modal.IssueID {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		return m, nil
	}

	// Calculate new position with bounds
	newIdx := currentIdx + delta
	if newIdx < 0 || newIdx >= len(issueIDs) {
		return m, nil // At boundary, don't wrap
	}

	// Navigate to new issue - replace the current modal entry
	newIssueID := issueIDs[newIdx]
	modal.IssueID = newIssueID
	modal.Scroll = 0
	modal.Loading = true
	modal.Error = nil
	modal.Issue = nil
	modal.Handoff = nil
	modal.Logs = nil
	modal.BlockedBy = nil
	modal.Blocks = nil
	modal.EpicTasks = nil
	modal.EpicTasksCursor = 0
	modal.TaskSectionFocused = false
	modal.ParentEpic = nil
	modal.ParentEpicFocused = false
	modal.DescRender = ""
	modal.AcceptRender = ""

	// Update cursor position to match in source panel
	m.Cursor[m.ModalSourcePanel()] = newIdx
	m.saveSelectedID(m.ModalSourcePanel())

	return m, m.fetchIssueDetails(newIssueID)
}

// openModal opens the details modal for the currently selected issue
func (m Model) openModal() (tea.Model, tea.Cmd) {
	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}

	return m.pushModal(issueID, m.ActivePanel)
}

// pushModal pushes a new modal onto the stack
func (m Model) pushModal(issueID string, sourcePanel Panel) (tea.Model, tea.Cmd) {
	entry := ModalEntry{
		IssueID:     issueID,
		SourcePanel: sourcePanel,
		Loading:     true,
	}
	m.ModalStack = append(m.ModalStack, entry)

	return m, m.fetchIssueDetails(issueID)
}

// closeModal pops the top modal from the stack
func (m *Model) closeModal() {
	if len(m.ModalStack) > 0 {
		m.ModalStack = m.ModalStack[:len(m.ModalStack)-1]
	}
}

// ModalOpen returns true if any modal is open
func (m Model) ModalOpen() bool {
	return len(m.ModalStack) > 0
}

// ModalDepth returns the current modal stack depth (0 = no modal)
func (m Model) ModalDepth() int {
	return len(m.ModalStack)
}

// CurrentModal returns a pointer to the current (top) modal entry, or nil if none
func (m *Model) CurrentModal() *ModalEntry {
	if len(m.ModalStack) == 0 {
		return nil
	}
	return &m.ModalStack[len(m.ModalStack)-1]
}

// ModalSourcePanel returns the source panel of the base modal (depth 1)
func (m Model) ModalSourcePanel() Panel {
	if len(m.ModalStack) == 0 {
		return PanelCurrentWork
	}
	return m.ModalStack[0].SourcePanel
}

// ModalBreadcrumb returns a breadcrumb string for the modal stack
func (m Model) ModalBreadcrumb() string {
	if len(m.ModalStack) <= 1 {
		return ""
	}
	var parts []string
	for _, entry := range m.ModalStack {
		if entry.Issue != nil {
			parts = append(parts, string(entry.Issue.Type)+": "+entry.IssueID)
		} else {
			parts = append(parts, entry.IssueID)
		}
	}
	return strings.Join(parts, " > ")
}

// renderMarkdownAsync returns a command that renders markdown in background
func (m Model) renderMarkdownAsync(issueID, desc, accept string, width int) tea.Cmd {
	return func() tea.Msg {
		return MarkdownRenderedMsg{
			IssueID:      issueID,
			DescRender:   preRenderMarkdown(desc, width),
			AcceptRender: preRenderMarkdown(accept, width),
		}
	}
}

// preRenderMarkdown renders markdown once (expensive operation)
func preRenderMarkdown(text string, width int) string {
	if text == "" {
		return ""
	}

	// Use dark style directly (avoid expensive auto-detection)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text // fallback to plain text
	}

	rendered, err := renderer.Render(text)
	if err != nil {
		return text // fallback to plain text
	}

	// Glamour adds lots of trailing newlines - strip them all
	return strings.TrimRight(rendered, "\n\r\t ")
}

// openStatsModal opens the stats modal and fetches stats data
func (m Model) openStatsModal() (tea.Model, tea.Cmd) {
	m.StatsOpen = true
	m.StatsScroll = 0
	m.StatsLoading = true
	m.StatsError = nil
	m.StatsData = nil

	return m, m.fetchStats()
}

// closeStatsModal closes the stats modal and clears transient state
func (m *Model) closeStatsModal() {
	m.StatsOpen = false
	m.StatsScroll = 0
	m.StatsLoading = false
	m.StatsError = nil
	m.StatsData = nil
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

		// Fetch parent epic if this issue has a parent
		if issue.ParentID != "" {
			if parent, err := m.DB.GetIssue(issue.ParentID); err == nil && parent.Type == models.TypeEpic {
				msg.ParentEpic = parent
			}
			// Silently ignore errors - parent may have been deleted
		}

		// Fetch dependencies (blocked by)
		depIDs, _ := m.DB.GetDependencies(issueID)
		for _, depID := range depIDs {
			if depIssue, err := m.DB.GetIssue(depID); err == nil {
				msg.BlockedBy = append(msg.BlockedBy, *depIssue)
			}
		}

		// Fetch dependents (issues blocked by this one)
		blockedIDs, _ := m.DB.GetBlockedBy(issueID)
		for _, blockedID := range blockedIDs {
			if blockedIssue, err := m.DB.GetIssue(blockedID); err == nil {
				msg.Blocks = append(msg.Blocks, *blockedIssue)
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

// buildCurrentWorkRows builds the flattened list of current work panel rows
func (m *Model) buildCurrentWorkRows() {
	m.CurrentWorkRows = nil
	if m.FocusedIssue != nil {
		m.CurrentWorkRows = append(m.CurrentWorkRows, m.FocusedIssue.ID)
	}
	for _, issue := range m.InProgress {
		// Skip focused issue if it's also in progress (avoid duplicate)
		if m.FocusedIssue != nil && issue.ID == m.FocusedIssue.ID {
			continue
		}
		m.CurrentWorkRows = append(m.CurrentWorkRows, issue.ID)
	}
}

// buildTaskListRows builds the flattened list of task list rows with category metadata
func (m *Model) buildTaskListRows() {
	m.TaskListRows = nil
	// Order: Reviewable, Ready, Blocked, Closed (matches display order)
	for _, issue := range m.TaskList.Reviewable {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryReviewable})
	}
	for _, issue := range m.TaskList.Ready {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryReady})
	}
	for _, issue := range m.TaskList.Blocked {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryBlocked})
	}
	for _, issue := range m.TaskList.Closed {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: issue, Category: CategoryClosed})
	}
}

// restoreCursors restores cursor positions from saved issue IDs after data refresh
func (m *Model) restoreCursors() {
	// Current Work panel
	if savedID := m.SelectedID[PanelCurrentWork]; savedID != "" {
		found := false
		for i, id := range m.CurrentWorkRows {
			if id == savedID {
				m.Cursor[PanelCurrentWork] = i
				found = true
				break
			}
		}
		if !found {
			m.clampCursor(PanelCurrentWork)
		}
	} else {
		m.clampCursor(PanelCurrentWork)
	}

	// Task List panel
	if savedID := m.SelectedID[PanelTaskList]; savedID != "" {
		found := false
		for i, row := range m.TaskListRows {
			if row.Issue.ID == savedID {
				m.Cursor[PanelTaskList] = i
				found = true
				break
			}
		}
		if !found {
			m.clampCursor(PanelTaskList)
		}
	} else {
		m.clampCursor(PanelTaskList)
	}

	// Activity panel
	m.clampCursor(PanelActivity)

	// Ensure scroll offsets keep the cursor visible after refresh
	m.ensureCursorVisible(PanelCurrentWork)
	m.ensureCursorVisible(PanelTaskList)
	m.ensureCursorVisible(PanelActivity)
}

// clampCursor ensures cursor is within valid bounds for a panel
func (m *Model) clampCursor(panel Panel) {
	count := m.rowCount(panel)
	if count == 0 {
		m.Cursor[panel] = 0
		return
	}
	if m.Cursor[panel] >= count {
		m.Cursor[panel] = count - 1
	}
	if m.Cursor[panel] < 0 {
		m.Cursor[panel] = 0
	}
}

// rowCount returns the number of selectable rows in a panel
func (m Model) rowCount(panel Panel) int {
	switch panel {
	case PanelCurrentWork:
		return len(m.CurrentWorkRows)
	case PanelActivity:
		return len(m.Activity)
	case PanelTaskList:
		return len(m.TaskListRows)
	}
	return 0
}

// moveCursor moves the cursor in the active panel by delta, clamping to bounds
func (m *Model) moveCursor(delta int) {
	panel := m.ActivePanel
	count := m.rowCount(panel)
	if count == 0 {
		return
	}

	newPos := m.Cursor[panel] + delta
	if newPos < 0 {
		newPos = 0
	}
	if newPos >= count {
		newPos = count - 1
	}
	m.Cursor[panel] = newPos

	// Save the selected issue ID for persistence across refresh
	m.saveSelectedID(panel)

	// Auto-scroll viewport to keep cursor visible
	m.ensureCursorVisible(panel)
}

// ensureCursorVisible adjusts ScrollOffset to keep cursor in viewport
func (m *Model) ensureCursorVisible(panel Panel) {
	cursor := m.Cursor[panel]
	offset := m.ScrollOffset[panel]
	visibleHeight := m.visibleHeightForPanel(panel)

	if visibleHeight <= 0 {
		return
	}

	// Calculate effective height accounting for dynamic factors
	effectiveHeight := visibleHeight

	// For task list panel, account for category header lines
	if panel == PanelTaskList {
		headerLines := m.categoryHeaderLinesBetween(offset, cursor)
		effectiveHeight -= headerLines
	}

	if effectiveHeight < 1 {
		effectiveHeight = 1
	}

	// Scroll down if cursor below viewport
	if cursor >= offset+effectiveHeight {
		// After scrolling, "▲ more above" will appear taking 1 line,
		// so we need to scroll 1 extra to compensate
		newOffset := cursor - effectiveHeight + 1
		if offset == 0 && newOffset > 0 {
			newOffset++ // Compensate for "more above" indicator appearing
		}
		m.ScrollOffset[panel] = newOffset
	}
	// Scroll up if cursor above viewport
	if cursor < offset {
		m.ScrollOffset[panel] = cursor
	}
}

// categoryHeaderLinesBetween counts how many lines are consumed by category
// headers between two row indices in TaskListRows. Each category transition
// adds a blank line (if not first) plus a header line.
func (m Model) categoryHeaderLinesBetween(startIdx, endIdx int) int {
	if len(m.TaskListRows) == 0 || startIdx >= endIdx {
		return 0
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > len(m.TaskListRows) {
		endIdx = len(m.TaskListRows)
	}

	lines := 0
	var currentCategory TaskListCategory
	if startIdx > 0 {
		currentCategory = m.TaskListRows[startIdx-1].Category
	}

	for i := startIdx; i < endIdx; i++ {
		row := m.TaskListRows[i]
		if row.Category != currentCategory {
			if i > startIdx || startIdx > 0 {
				lines++ // blank line before category (except first visible)
			}
			lines++ // header line
			currentCategory = row.Category
		}
	}
	return lines
}

// visibleHeightForPanel calculates visible rows for a panel
func (m Model) visibleHeightForPanel(panel Panel) int {
	if m.Height == 0 {
		return 10 // Default fallback
	}

	// Match calculation from renderView()
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight
	panelHeight := availableHeight / 3

	// Account for: title (1) + border (2) + scroll indicators (2)
	// Scroll indicators: "▲ more above" and "▼ more below" each take 1 line
	// when the list is scrollable. Reserve space for both to ensure cursor
	// stays visible during navigation.
	return panelHeight - 5
}

// saveSelectedID saves the currently selected issue ID for a panel
func (m *Model) saveSelectedID(panel Panel) {
	switch panel {
	case PanelCurrentWork:
		if m.Cursor[panel] < len(m.CurrentWorkRows) {
			m.SelectedID[panel] = m.CurrentWorkRows[m.Cursor[panel]]
		}
	case PanelTaskList:
		if m.Cursor[panel] < len(m.TaskListRows) {
			m.SelectedID[panel] = m.TaskListRows[m.Cursor[panel]].Issue.ID
		}
	case PanelActivity:
		if m.Cursor[panel] < len(m.Activity) && m.Activity[m.Cursor[panel]].IssueID != "" {
			m.SelectedID[panel] = m.Activity[m.Cursor[panel]].IssueID
		}
	}
}

// SelectedIssueID returns the issue ID of the currently selected row in a panel
func (m Model) SelectedIssueID(panel Panel) string {
	switch panel {
	case PanelCurrentWork:
		if m.Cursor[panel] < len(m.CurrentWorkRows) {
			return m.CurrentWorkRows[m.Cursor[panel]]
		}
	case PanelTaskList:
		if m.Cursor[panel] < len(m.TaskListRows) {
			return m.TaskListRows[m.Cursor[panel]].Issue.ID
		}
	case PanelActivity:
		if m.Cursor[panel] < len(m.Activity) {
			return m.Activity[m.Cursor[panel]].IssueID
		}
	}
	return ""
}

// markForReview marks the selected in-progress issue for review
func (m Model) markForReview() (tea.Model, tea.Cmd) {
	issueID := m.SelectedIssueID(PanelCurrentWork)
	if issueID == "" {
		return m, nil
	}

	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		return m, nil
	}

	// Only allow marking in_progress issues for review
	if issue.Status != models.StatusInProgress {
		return m, nil
	}

	// Update status
	issue.Status = models.StatusInReview
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionReview,
		EntityType: "issue",
		EntityID:   issueID,
	})

	return m, m.fetchData()
}

// confirmDelete opens confirmation dialog for deleting selected issue
func (m Model) confirmDelete() (tea.Model, tea.Cmd) {
	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}

	issue, err := m.DB.GetIssue(issueID)
	if err != nil || issue == nil {
		return m, nil
	}

	m.ConfirmOpen = true
	m.ConfirmAction = "delete"
	m.ConfirmIssueID = issueID
	m.ConfirmTitle = issue.Title

	return m, nil
}

// executeDelete performs the actual deletion after confirmation
func (m Model) executeDelete() (tea.Model, tea.Cmd) {
	if m.ConfirmIssueID == "" {
		m.ConfirmOpen = false
		return m, nil
	}

	// Delete issue
	if err := m.DB.DeleteIssue(m.ConfirmIssueID); err != nil {
		m.ConfirmOpen = false
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionDelete,
		EntityType: "issue",
		EntityID:   m.ConfirmIssueID,
	})

	m.ConfirmOpen = false
	m.ConfirmIssueID = ""
	m.ConfirmTitle = ""
	m.ConfirmAction = ""

	return m, m.fetchData()
}

// approveIssue approves/closes the selected reviewable issue
func (m Model) approveIssue() (tea.Model, tea.Cmd) {
	// Must be in Task List panel
	if m.ActivePanel != PanelTaskList {
		return m, nil
	}

	cursor := m.Cursor[PanelTaskList]
	if cursor >= len(m.TaskListRows) {
		return m, nil
	}

	row := m.TaskListRows[cursor]
	// Only allow approving reviewable issues
	if row.Category != CategoryReviewable {
		return m, nil
	}

	issue, err := m.DB.GetIssue(row.Issue.ID)
	if err != nil || issue == nil {
		return m, nil
	}

	// Can't approve your own issues
	if issue.ImplementerSession == m.SessionID {
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ReviewerSession = m.SessionID
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionApprove,
		EntityType: "issue",
		EntityID:   issue.ID,
	})

	// Clear the saved ID so cursor stays at the same position after refresh
	// The item will move to Closed, and we want cursor at same index for next item
	m.SelectedID[PanelTaskList] = ""

	return m, m.fetchData()
}
