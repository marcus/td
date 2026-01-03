package monitor

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/version"
	"github.com/marcus/td/pkg/monitor/keymap"
)

// Panel represents which panel is active
type Panel int

const (
	PanelCurrentWork Panel = iota
	PanelTaskList
	PanelActivity
)

// Rect represents a rectangular region for hit-testing
type Rect struct {
	X, Y, W, H int
}

// Contains returns true if the point (x, y) is within the rectangle
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// HitTestPanel returns which panel contains the point (x, y), or -1 if none
func (m Model) HitTestPanel(x, y int) Panel {
	for panel, bounds := range m.PanelBounds {
		if bounds.Contains(x, y) {
			return panel
		}
	}
	return -1
}

// HitTestRow returns the row index within a panel for a given y coordinate, or -1 if none.
// Accounts for scroll indicators, category headers, and separator lines.
func (m Model) HitTestRow(panel Panel, y int) int {
	bounds, ok := m.PanelBounds[panel]
	if !ok {
		return -1
	}

	// Content starts after top border (1 line), title (1 line), and title underline/gap (1 line)
	contentY := bounds.Y + 3
	if y < contentY {
		return -1
	}

	// Calculate relative position within content area
	relY := y - contentY

	// Delegate to panel-specific hit testing
	switch panel {
	case PanelTaskList:
		return m.hitTestTaskListRow(relY)
	case PanelCurrentWork:
		return m.hitTestCurrentWorkRow(relY)
	case PanelActivity:
		return m.hitTestActivityRow(relY)
	}
	return -1
}

// hitTestTaskListRow maps a y position to a TaskListRows index, accounting for headers
func (m Model) hitTestTaskListRow(relY int) int {
	if len(m.TaskListRows) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelTaskList]
	height := m.visibleHeightForPanel(PanelTaskList)

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1 // Clicked on scroll indicator
		}
		linePos = 1
	}

	// Walk through visible rows, tracking line position
	var currentCategory TaskListCategory
	if offset > 0 && offset <= len(m.TaskListRows) {
		currentCategory = m.TaskListRows[offset-1].Category
	}

	for i := offset; i < len(m.TaskListRows); i++ {
		row := m.TaskListRows[i]

		// Category header takes lines
		if row.Category != currentCategory {
			if i > offset {
				linePos++ // Blank separator line
			}
			if relY == linePos {
				return -1 // Clicked on header
			}
			linePos++ // Header line
			currentCategory = row.Category
		}

		// Check if this row matches
		if relY == linePos {
			return i
		}
		linePos++

		// Stop if we've gone past visible area
		if linePos > height {
			break
		}
	}

	return -1
}

// hitTestCurrentWorkRow maps a y position to a CurrentWorkRows index
// Accounts for blank line + "IN PROGRESS:" header between focused and in-progress sections
func (m Model) hitTestCurrentWorkRow(relY int) int {
	if len(m.CurrentWorkRows) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelCurrentWork]

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1
		}
		linePos = 1
	}

	// Count in-progress issues (excluding focused if duplicate)
	inProgressCount := len(m.InProgress)
	if m.FocusedIssue != nil {
		for _, issue := range m.InProgress {
			if issue.ID == m.FocusedIssue.ID {
				inProgressCount--
				break
			}
		}
	}

	// Track position through the panel layout
	rowIdx := 0

	// Focused issue row (if present)
	if m.FocusedIssue != nil {
		if rowIdx >= offset {
			if relY == linePos {
				return rowIdx
			}
			linePos++
		}
		rowIdx++
	}

	// "IN PROGRESS:" section header (blank line + header = 2 lines)
	if inProgressCount > 0 {
		// Only show header if we're past offset or at start
		if rowIdx >= offset || (m.FocusedIssue != nil && offset == 0) {
			// Blank line
			if relY == linePos {
				return -1 // clicked on blank line
			}
			linePos++
			// Header line
			if relY == linePos {
				return -1 // clicked on header
			}
			linePos++
		}

		// In-progress issue rows
		for i := 0; i < inProgressCount; i++ {
			if rowIdx >= offset {
				if relY == linePos {
					return rowIdx
				}
				linePos++
			}
			rowIdx++
		}
	}

	return -1
}

// hitTestActivityRow maps a y position to an Activity index
func (m Model) hitTestActivityRow(relY int) int {
	if len(m.Activity) == 0 {
		return -1
	}

	offset := m.ScrollOffset[PanelActivity]

	// Account for "▲ more above" indicator
	linePos := 0
	if offset > 0 {
		if relY == 0 {
			return -1
		}
		linePos = 1
	}

	// Simple 1:1 mapping for activity (no headers)
	rowIdx := relY - linePos + offset
	if rowIdx >= 0 && rowIdx < len(m.Activity) {
		return rowIdx
	}
	return -1
}

// ActivityItem represents a unified activity item (log, action, or comment)
type ActivityItem struct {
	Timestamp  time.Time
	SessionID  string
	Type       string // "log", "action", "comment"
	IssueID    string
	IssueTitle string // title of the associated issue
	Message    string
	LogType    models.LogType    // for logs
	Action     models.ActionType // for actions
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

// ToSortClause returns the TDQ sort clause string for this mode
func (s SortMode) ToSortClause() string {
	switch s {
	case SortByCreatedDesc:
		return "sort:-created"
	case SortByUpdatedDesc:
		return "sort:-updated"
	default:
		return "sort:priority"
	}
}

// updateQuerySort updates or appends sort clause to a query string
func updateQuerySort(query string, sortMode SortMode) string {
	sortClause := sortMode.ToSortClause()
	query = strings.TrimSpace(query)

	// Remove existing sort clause if present
	// Match sort:word or sort:-word patterns
	words := strings.Fields(query)
	var filtered []string
	for _, word := range words {
		if !strings.HasPrefix(strings.ToLower(word), "sort:") {
			filtered = append(filtered, word)
		}
	}

	// Rebuild query with new sort clause
	if len(filtered) == 0 {
		return sortClause
	}
	return strings.Join(filtered, " ") + " " + sortClause
}

// TypeFilterMode represents type filtering for the task list
type TypeFilterMode int

const (
	TypeFilterNone    TypeFilterMode = iota // No type filter
	TypeFilterEpic                          // type=epic
	TypeFilterTask                          // type=task
	TypeFilterBug                           // type=bug
	TypeFilterFeature                       // type=feature
	TypeFilterChore                         // type=chore
)

// String returns display name for type filter mode
func (t TypeFilterMode) String() string {
	switch t {
	case TypeFilterEpic:
		return "epic"
	case TypeFilterTask:
		return "task"
	case TypeFilterBug:
		return "bug"
	case TypeFilterFeature:
		return "feature"
	case TypeFilterChore:
		return "chore"
	default:
		return ""
	}
}

// ToTypeClause returns the TDQ type clause string for this mode
func (t TypeFilterMode) ToTypeClause() string {
	switch t {
	case TypeFilterEpic:
		return "type=epic"
	case TypeFilterTask:
		return "type=task"
	case TypeFilterBug:
		return "type=bug"
	case TypeFilterFeature:
		return "type=feature"
	case TypeFilterChore:
		return "type=chore"
	default:
		return ""
	}
}

// updateQueryType updates or appends type clause to a query string
func updateQueryType(query string, typeMode TypeFilterMode) string {
	query = strings.TrimSpace(query)

	// Remove existing type= clause if present
	words := strings.Fields(query)
	var filtered []string
	for _, word := range words {
		if !strings.HasPrefix(strings.ToLower(word), "type=") {
			filtered = append(filtered, word)
		}
	}

	// Rebuild query with new type clause (if any)
	typeClause := typeMode.ToTypeClause()
	if typeClause == "" {
		if len(filtered) == 0 {
			return ""
		}
		return strings.Join(filtered, " ")
	}

	if len(filtered) == 0 {
		return typeClause
	}
	return strings.Join(filtered, " ") + " " + typeClause
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
	Scroll       int
	ContentLines int // Cached content line count for scroll clamping

	// Async data
	Loading      bool
	Error        error
	Issue        *models.Issue
	Handoff      *models.Handoff
	Logs         []models.Log
	Comments     []models.Comment
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
	SearchMode     bool           // Whether search mode is active
	SearchQuery    string         // Current search query
	IncludeClosed  bool           // Whether to include closed tasks
	SortMode       SortMode       // Task list sort order
	TypeFilterMode TypeFilterMode // Type filter (epic, task, bug, etc.)

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

	// Handoffs modal state
	HandoffsOpen    bool
	HandoffsLoading bool
	HandoffsData    []models.Handoff
	HandoffsCursor  int
	HandoffsScroll  int
	HandoffsError   error

	// Form modal state
	FormOpen  bool
	FormState *FormState

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
	Comments   []models.Comment
	BlockedBy  []models.Issue // Dependencies (issues blocking this one)
	Blocks     []models.Issue // Dependents (issues blocked by this one)
	EpicTasks  []models.Issue // Child tasks (when issue is an epic)
	ParentEpic *models.Issue  // Parent epic (when issue.ParentID is set)
	Error      error
}

// MarkdownRenderedMsg carries pre-rendered markdown for the modal
type MarkdownRenderedMsg struct {
	IssueID      string
	DescRender   string
	AcceptRender string
}

// HandoffsDataMsg carries fetched handoffs data for the modal
type HandoffsDataMsg struct {
	Data  []models.Handoff
	Error error
}

// ClearStatusMsg clears the status message
type ClearStatusMsg struct{}

// PaneHeightsSavedMsg is sent after pane heights are persisted to config
type PaneHeightsSavedMsg struct {
	Error error
}

// EditorField identifies which form field is being edited externally
type EditorField int

const (
	EditorFieldDescription EditorField = iota
	EditorFieldAcceptance
)

// EditorFinishedMsg carries the result from external editor
type EditorFinishedMsg struct {
	Field   EditorField
	Content string
	Error   error
}

// NewModel creates a new monitor model
func NewModel(database *db.DB, sessionID string, interval time.Duration, ver string, baseDir string) Model {
	// Initialize keymap with default bindings
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)

	// Load pane heights from config (or use defaults)
	paneHeights, _ := config.GetPaneHeights(baseDir)

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
		Version:         ver,
		PanelBounds:     make(map[Panel]Rect),
		HoverPanel:      -1,
		LastClickPanel:  -1,
		LastClickRow:    -1,
		PaneHeights:     paneHeights,
		DraggingDivider: -1,
		DividerHover:    -1,
		BaseDir:         baseDir,
	}
}

// NewEmbedded creates a monitor model for embedding in external applications.
// It opens the database and creates/gets a session automatically.
// The caller must call Close() when done to release resources.
func NewEmbedded(baseDir string, interval time.Duration, ver string) (*Model, error) {
	database, err := db.Open(baseDir)
	if err != nil {
		return nil, err
	}

	sess, err := session.GetOrCreate(baseDir)
	if err != nil {
		database.Close()
		return nil, err
	}

	m := NewModel(database, sess.ID, interval, ver, baseDir)
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
	cmds := []tea.Cmd{
		m.fetchData(),
		m.scheduleTick(),
	}

	// Start async version check (non-blocking)
	if m.Version != "" && !version.IsDevelopmentVersion(m.Version) {
		cmds = append(cmds, version.CheckAsync(m.Version))
	}

	return tea.Batch(cmds...)
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Form mode: forward all messages to huh form first
	if m.FormOpen && m.FormState != nil && m.FormState.Form != nil {
		return m.handleFormUpdate(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.updatePanelBounds()
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

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
			modal.Comments = msg.Comments
			modal.BlockedBy = msg.BlockedBy
			modal.Blocks = msg.Blocks
			modal.EpicTasks = msg.EpicTasks
			modal.ParentEpic = msg.ParentEpic
			modal.ParentEpicFocused = false // Reset focus on load

			// Calculate content lines for scroll clamping
			modal.ContentLines = m.estimateModalContentLines(modal)

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
		}
		return m, nil

	case HandoffsDataMsg:
		// Only update if handoffs modal is open
		if m.HandoffsOpen {
			m.HandoffsLoading = false
			m.HandoffsError = msg.Error
			m.HandoffsData = msg.Data
		}
		return m, nil

	case ClearStatusMsg:
		m.StatusMessage = ""
		m.StatusIsError = false
		return m, nil

	case version.UpdateAvailableMsg:
		m.UpdateAvail = &msg
		return m, nil

	case PaneHeightsSavedMsg:
		// Pane heights saved (or failed) - just ignore errors silently
		return m, nil
	}

	return m, nil
}

// CurrentContextString returns the current keymap context as a sidecar-formatted string.
// This is used by sidecar's TD plugin to determine which shortcuts to display.
func (m Model) CurrentContextString() string {
	return keymap.ContextToSidecar(m.currentContext())
}

// currentContext returns the keymap context based on current UI state
func (m Model) currentContext() keymap.Context {
	if m.ConfirmOpen {
		return keymap.ContextConfirm
	}
	if m.FormOpen {
		return keymap.ContextForm
	}
	if m.HandoffsOpen {
		return keymap.ContextHandoffs
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

// handleFormUpdate handles all messages when form is open
func (m Model) handleFormUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle our custom key bindings first
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case keyMsg.Type == tea.KeyCtrlS:
			return m.executeCommand(keymap.CmdFormSubmit)
		case keyMsg.Type == tea.KeyEsc:
			return m.executeCommand(keymap.CmdFormCancel)
		case keyMsg.Type == tea.KeyCtrlX:
			return m.executeCommand(keymap.CmdFormToggleExtend)
		case keyMsg.Type == tea.KeyCtrlO:
			return m.executeCommand(keymap.CmdFormOpenEditor)
		}
	}

	// Handle editor finished message
	if editorMsg, ok := msg.(EditorFinishedMsg); ok {
		return m.handleEditorFinished(editorMsg)
	}

	// Handle window resize
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = sizeMsg.Width
		m.Height = sizeMsg.Height
		m.updatePanelBounds()
	}

	// Forward message to huh form
	form, cmd := m.FormState.Form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.FormState.Form = f
	}

	// Check if form completed (user pressed enter on last field)
	if m.FormState.Form.State == huh.StateCompleted {
		return m.executeCommand(keymap.CmdFormSubmit)
	}

	return m, cmd
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
		if m.HandoffsOpen {
			return m, m.fetchHandoffs()
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

	// Cursor movement
	case keymap.CmdCursorDown, keymap.CmdScrollDown:
		if modal := m.CurrentModal(); modal != nil {
			if modal.ParentEpicFocused {
				// Unfocus parent epic, move past epic zone so next j scrolls
				modal.ParentEpicFocused = false
				modal.Scroll = 1
			} else if modal.TaskSectionFocused {
				// Move epic task cursor, or transition to scroll at end
				if modal.EpicTasksCursor < len(modal.EpicTasks)-1 {
					modal.EpicTasksCursor++
				} else {
					// At last task, try to scroll. If can scroll, unfocus tasks first.
					maxScroll := m.modalMaxScroll(modal)
					if modal.Scroll < maxScroll {
						modal.TaskSectionFocused = false
						modal.Scroll++
					}
					// If can't scroll, stay at last task
				}
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top with parent epic, focus it first before scrolling
				modal.ParentEpicFocused = true
			} else {
				// Scroll down, clamped to max
				maxScroll := m.modalMaxScroll(modal)
				if modal.Scroll < maxScroll {
					modal.Scroll++
				}
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor < len(m.HandoffsData)-1 {
				m.HandoffsCursor++
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
				// At first task, stay there (user can use Tab to unfocus)
			} else if modal.Scroll == 0 && modal.ParentEpic != nil {
				// At top of scroll with parent epic, focus it
				modal.ParentEpicFocused = true
			} else if modal.Scroll > 0 {
				modal.Scroll--
			}
		} else if m.HandoffsOpen {
			if m.HandoffsCursor > 0 {
				m.HandoffsCursor--
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
		} else if m.HandoffsOpen {
			m.HandoffsCursor = 0
			m.HandoffsScroll = 0
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
			modal.Scroll = m.modalMaxScroll(modal)
		} else if m.HandoffsOpen {
			if len(m.HandoffsData) > 0 {
				m.HandoffsCursor = len(m.HandoffsData) - 1
			}
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
			maxScroll := m.modalMaxScroll(modal)
			modal.Scroll += pageSize
			if modal.Scroll > maxScroll {
				modal.Scroll = maxScroll
			}
		} else if m.HandoffsOpen {
			m.HandoffsCursor += pageSize
			if m.HandoffsCursor >= len(m.HandoffsData) {
				m.HandoffsCursor = len(m.HandoffsData) - 1
			}
			if m.HandoffsCursor < 0 {
				m.HandoffsCursor = 0
			}
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
		} else if m.HandoffsOpen {
			m.HandoffsCursor -= pageSize
			if m.HandoffsCursor < 0 {
				m.HandoffsCursor = 0
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
			maxScroll := m.modalMaxScroll(modal)
			modal.Scroll += pageSize
			if modal.Scroll > maxScroll {
				modal.Scroll = maxScroll
			}
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
		} else if m.HandoffsOpen {
			m.closeHandoffsModal()
		} else if m.StatsOpen {
			m.closeStatsModal()
		}
		return m, nil

	// Actions
	case keymap.CmdOpenDetails:
		if m.HandoffsOpen {
			return m.openIssueFromHandoffs()
		}
		return m.openModal()

	case keymap.CmdOpenStats:
		return m.openStatsModal()

	case keymap.CmdOpenHandoffs:
		return m.openHandoffsModal()

	case keymap.CmdSearch:
		m.SearchMode = true
		m.SearchQuery = ""
		m.updatePanelBounds() // Recalc bounds for search bar
		return m, nil

	case keymap.CmdToggleClosed:
		m.IncludeClosed = !m.IncludeClosed
		return m, m.fetchData()

	case keymap.CmdCycleSortMode:
		m.SortMode = (m.SortMode + 1) % 3
		m.SearchQuery = updateQuerySort(m.SearchQuery, m.SortMode)
		return m, m.fetchData()

	case keymap.CmdCycleTypeFilter:
		m.TypeFilterMode = (m.TypeFilterMode + 1) % 6 // 6 modes: none + 5 types
		m.SearchQuery = updateQueryType(m.SearchQuery, m.TypeFilterMode)
		if m.TypeFilterMode == TypeFilterNone {
			m.StatusMessage = "Type filter: all"
		} else {
			m.StatusMessage = "Type filter: " + m.TypeFilterMode.String()
		}
		return m, tea.Batch(m.fetchData(), tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		}))

	case keymap.CmdMarkForReview:
		// Mark for review works from modal, TaskList, or CurrentWork panel
		if m.ModalOpen() {
			return m.markForReview()
		}
		if m.ActivePanel == PanelCurrentWork || m.ActivePanel == PanelTaskList {
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

	case keymap.CmdCloseIssue:
		return m.closeIssue()

	// Search commands
	case keymap.CmdSearchConfirm:
		m.SearchMode = false
		m.ShowTDQHelp = false
		m.updatePanelBounds() // Recalc bounds after search bar closes
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
		m.updatePanelBounds() // Recalc bounds after search bar closes
		return m, m.fetchData()

	case keymap.CmdSearchClear:
		if m.SearchQuery == "" {
			return m, nil // Nothing to clear
		}
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
				// Don't reset TaskSectionFocused - preserve parent modal state for when we return
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

	case keymap.CmdCopyToClipboard:
		return m.copyCurrentIssueToClipboard()

	case keymap.CmdCopyIDToClipboard:
		return m.copyIssueIDToClipboard()

	// Form commands
	case keymap.CmdNewIssue:
		return m.openNewIssueForm()

	case keymap.CmdEditIssue:
		return m.openEditIssueForm()

	case keymap.CmdFormSubmit:
		return m.submitForm()

	case keymap.CmdFormCancel:
		m.closeForm()
		return m, nil

	case keymap.CmdFormToggleExtend:
		if m.FormState != nil {
			m.FormState.ToggleExtended()
		}
		return m, nil

	case keymap.CmdFormOpenEditor:
		return m.openExternalEditor()
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

// estimateModalContentLines estimates the number of content lines in a modal
// This is used to clamp scroll values and prevent over-scrolling
func (m Model) estimateModalContentLines(modal *ModalEntry) int {
	if modal == nil || modal.Issue == nil {
		return 10 // Minimal default
	}

	lines := 0
	issue := modal.Issue

	// Header + status section
	lines += 5 // ID, title, blank, status, blank

	// Parent epic
	if modal.ParentEpic != nil {
		lines += 2
	}

	// Labels, implementer, reviewer
	if len(issue.Labels) > 0 {
		lines++
	}
	if issue.ImplementerSession != "" {
		lines++
	}
	if issue.ReviewerSession != "" {
		lines++
	}
	lines++ // Blank

	// Epic tasks
	if issue.Type == models.TypeEpic && len(modal.EpicTasks) > 0 {
		lines += 1 + len(modal.EpicTasks) + 1 // Header + tasks + blank
	}

	// Description - use rendered version if available, otherwise estimate from raw
	if issue.Description != "" {
		lines++ // Header
		if modal.DescRender != "" {
			lines += strings.Count(modal.DescRender, "\n") + 1
		} else {
			lines += strings.Count(issue.Description, "\n") + 1
		}
		lines++ // Blank
	}

	// Acceptance criteria
	if issue.Acceptance != "" {
		lines++ // Header
		if modal.AcceptRender != "" {
			lines += strings.Count(modal.AcceptRender, "\n") + 1
		} else {
			lines += strings.Count(issue.Acceptance, "\n") + 1
		}
		lines++ // Blank
	}

	// Dependencies and blockers
	lines += len(modal.BlockedBy) + len(modal.Blocks)
	if len(modal.BlockedBy) > 0 {
		lines += 2 // Header + blank
	}
	if len(modal.Blocks) > 0 {
		lines += 2 // Header + blank
	}

	// Handoff
	if modal.Handoff != nil {
		lines += 2 // Header + blank
		lines += len(modal.Handoff.Done)
		lines += len(modal.Handoff.Remaining)
		lines += len(modal.Handoff.Decisions)
		lines += len(modal.Handoff.Uncertain)
	}

	// Logs
	if len(modal.Logs) > 0 {
		lines += 1 + len(modal.Logs) // Header + logs
	}

	// Comments
	if len(modal.Comments) > 0 {
		lines += 1 + len(modal.Comments) // Header + comments
	}

	return lines
}

// modalMaxScroll returns the maximum scroll value for a modal
func (m Model) modalMaxScroll(modal *ModalEntry) int {
	if modal == nil {
		return 0
	}

	// Calculate visible height (same as view)
	modalHeight := m.Height * 80 / 100
	if modalHeight > 40 {
		modalHeight = 40
	}
	if modalHeight < 15 {
		modalHeight = 15
	}
	visibleHeight := modalHeight - 4 // Account for border and footer

	maxScroll := modal.ContentLines - visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	return maxScroll
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

// openHandoffsModal opens the handoffs modal and fetches data
func (m Model) openHandoffsModal() (tea.Model, tea.Cmd) {
	m.HandoffsOpen = true
	m.HandoffsCursor = 0
	m.HandoffsScroll = 0
	m.HandoffsLoading = true
	m.HandoffsError = nil
	m.HandoffsData = nil

	return m, m.fetchHandoffs()
}

// closeHandoffsModal closes the handoffs modal and clears state
func (m *Model) closeHandoffsModal() {
	m.HandoffsOpen = false
	m.HandoffsCursor = 0
	m.HandoffsScroll = 0
	m.HandoffsLoading = false
	m.HandoffsError = nil
	m.HandoffsData = nil
}

// openIssueFromHandoffs opens the issue detail modal for the selected handoff
func (m Model) openIssueFromHandoffs() (tea.Model, tea.Cmd) {
	if m.HandoffsCursor >= len(m.HandoffsData) {
		return m, nil
	}
	issueID := m.HandoffsData[m.HandoffsCursor].IssueID
	m.closeHandoffsModal()
	return m.pushModal(issueID, PanelCurrentWork)
}

// fetchHandoffs returns a command that fetches all handoffs
func (m Model) fetchHandoffs() tea.Cmd {
	return func() tea.Msg {
		handoffs, err := m.DB.GetRecentHandoffs(50, time.Time{})
		return HandoffsDataMsg{Data: handoffs, Error: err}
	}
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

	// Clamp scroll offset to valid range
	maxOffset := m.maxScrollOffset(panel)
	if m.ScrollOffset[panel] > maxOffset {
		m.ScrollOffset[panel] = maxOffset
	}
	if m.ScrollOffset[panel] < 0 {
		m.ScrollOffset[panel] = 0
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

	// Get panel height based on dynamic pane ratios
	var panelHeight int
	switch panel {
	case PanelCurrentWork:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[0])
	case PanelTaskList:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[1])
	case PanelActivity:
		panelHeight = int(float64(availableHeight) * m.PaneHeights[2])
	default:
		panelHeight = availableHeight / 3
	}

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

// markForReview marks the selected issue for review
// Works from modal view, CurrentWork panel, or TaskList panel
// Accepts both in_progress and open (ready) issues
func (m Model) markForReview() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.IssueID
		issue = modal.Issue
	} else {
		// Otherwise, use the selected issue from the active panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Only allow marking in_progress or open issues for review
	if issue.Status != models.StatusInProgress && issue.Status != models.StatusOpen {
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

	// Cascade up to parent epic if all siblings are ready
	m.DB.CascadeUpParentStatus(issueID, models.StatusInReview, m.SessionID)

	// If we're in a modal, close it since the issue moved to review
	if m.ModalOpen() {
		m.closeModal()
	}

	return m, m.fetchData()
}

// confirmDelete opens confirmation dialog for deleting selected issue
// Works from both main panel selection and modal view
func (m Model) confirmDelete() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.IssueID
		issue = modal.Issue
	} else {
		// Otherwise, use the selected issue from the panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
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

	deletedID := m.ConfirmIssueID

	// Delete issue
	if err := m.DB.DeleteIssue(deletedID); err != nil {
		m.ConfirmOpen = false
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionDelete,
		EntityType: "issue",
		EntityID:   deletedID,
	})

	m.ConfirmOpen = false
	m.ConfirmIssueID = ""
	m.ConfirmTitle = ""
	m.ConfirmAction = ""

	// Close modal if we just deleted the issue being viewed
	if modal := m.CurrentModal(); modal != nil && modal.IssueID == deletedID {
		m.closeModal()
	}

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

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issue.ID, models.StatusClosed, m.SessionID)

	// Clear the saved ID so cursor stays at the same position after refresh
	// The item will move to Closed, and we want cursor at same index for next item
	m.SelectedID[PanelTaskList] = ""

	return m, m.fetchData()
}

// closeIssue closes the selected issue directly (workflow shortcut)
// Works from both main panel selection and modal view
func (m Model) closeIssue() (tea.Model, tea.Cmd) {
	var issueID string
	var issue *models.Issue

	// Check if a modal is open - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.IssueID
		issue = modal.Issue
	} else {
		// Otherwise, use the selected issue from the panel
		issueID = m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Can't close already-closed issues
	if issue.Status == models.StatusClosed {
		return m, nil
	}

	// Update status
	now := time.Now()
	issue.Status = models.StatusClosed
	issue.ClosedAt = &now
	if err := m.DB.UpdateIssue(issue); err != nil {
		return m, nil
	}

	// Log action for undo
	m.DB.LogAction(&models.ActionLog{
		SessionID:  m.SessionID,
		ActionType: models.ActionClose,
		EntityType: "issue",
		EntityID:   issueID,
	})

	// Cascade up to parent epic if all siblings are closed
	m.DB.CascadeUpParentStatus(issueID, models.StatusClosed, m.SessionID)

	// If we're in a modal, close it since the issue is now closed
	if m.ModalOpen() {
		m.closeModal()
	}

	return m, m.fetchData()
}

// copyCurrentIssueToClipboard copies the current issue to clipboard as markdown
// Works from modal view or list views (PanelCurrentWork, PanelTaskList)
func (m Model) copyCurrentIssueToClipboard() (tea.Model, tea.Cmd) {
	var issue *models.Issue
	var epicTasks []models.Issue

	// Check if modal is open first - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issue = modal.Issue
		epicTasks = modal.EpicTasks
	} else {
		// Otherwise get the issue from the selected row in the active panel
		issueID := m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
		// For epics in list view, fetch tasks
		if issue.Type == models.TypeEpic {
			epicTasks, _ = m.DB.ListIssues(db.ListIssuesOptions{EpicID: issue.ID})
		}
	}

	var markdown string
	if issue.Type == models.TypeEpic {
		markdown = formatEpicAsMarkdown(issue, epicTasks)
	} else {
		markdown = formatIssueAsMarkdown(issue)
	}

	if err := copyToClipboard(markdown); err != nil {
		m.StatusMessage = "Copy failed: " + err.Error()
		m.StatusIsError = true
	} else {
		m.StatusMessage = "Yanked to clipboard"
		m.StatusIsError = false
	}

	// Clear status after 2 seconds
	return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// copyIssueIDToClipboard copies just the issue ID to clipboard
// Works from modal view or list views
func (m Model) copyIssueIDToClipboard() (tea.Model, tea.Cmd) {
	var issueID string

	// Check if modal is open first - use that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issueID = modal.Issue.ID
	} else {
		// Otherwise get the issue ID from the selected row in the active panel
		issueID = m.SelectedIssueID(m.ActivePanel)
	}

	if issueID == "" {
		return m, nil
	}

	if err := copyToClipboard(issueID); err != nil {
		m.StatusMessage = "Copy failed: " + err.Error()
		m.StatusIsError = true
	} else {
		m.StatusMessage = "Yanked ID: " + issueID
		m.StatusIsError = false
	}

	// Clear status after 2 seconds
	return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// updatePanelBounds recalculates panel bounds based on current dimensions.
// Called when window size changes to enable accurate mouse hit-testing.
func (m *Model) updatePanelBounds() {
	if m.Width == 0 || m.Height == 0 {
		return
	}

	// Match layout calculation from renderView()
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight

	// Calculate panel heights from ratios
	panelHeights := [3]int{
		int(float64(availableHeight) * m.PaneHeights[0]),
		int(float64(availableHeight) * m.PaneHeights[1]),
		int(float64(availableHeight) * m.PaneHeights[2]),
	}
	// Adjust last panel to absorb rounding errors
	panelHeights[2] = availableHeight - panelHeights[0] - panelHeights[1]

	// Calculate Y positions for each panel (stacked vertically)
	// Order: search bar (optional) → Current Work → Task List → Activity → footer
	y := searchBarHeight

	m.PanelBounds[PanelCurrentWork] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[0]}
	y += panelHeights[0]

	// First divider (between Current Work and Task List)
	// 3px hit region centered on the border
	m.DividerBounds[0] = Rect{X: 0, Y: y - 1, W: m.Width, H: 3}

	m.PanelBounds[PanelTaskList] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[1]}
	y += panelHeights[1]

	// Second divider (between Task List and Activity)
	m.DividerBounds[1] = Rect{X: 0, Y: y - 1, W: m.Width, H: 3}

	m.PanelBounds[PanelActivity] = Rect{X: 0, Y: y, W: m.Width, H: panelHeights[2]}
}

// handleMouse processes mouse events for panel selection and row clicking
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Ignore mouse events when modals/overlays are open
	if m.ModalOpen() || m.StatsOpen || m.HandoffsOpen || m.ConfirmOpen || m.ShowHelp || m.ShowTDQHelp {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Check divider hit first (highest priority)
			divider := m.HitTestDivider(msg.X, msg.Y)
			if divider >= 0 {
				return m.startDividerDrag(divider, msg.Y)
			}
			return m.handleMouseClick(msg.X, msg.Y)
		}
		// Handle mouse wheel (reported as button press)
		if msg.Button == tea.MouseButtonWheelUp {
			return m.handleMouseWheel(msg.X, msg.Y, -3)
		}
		if msg.Button == tea.MouseButtonWheelDown {
			return m.handleMouseWheel(msg.X, msg.Y, 3)
		}

	case tea.MouseActionRelease:
		if m.DraggingDivider >= 0 {
			return m.endDividerDrag()
		}

	case tea.MouseActionMotion:
		// Handle divider dragging
		if m.DraggingDivider >= 0 {
			return m.updateDividerDrag(msg.Y)
		}

		// Track divider hover for visual feedback
		divider := m.HitTestDivider(msg.X, msg.Y)
		if divider != m.DividerHover {
			m.DividerHover = divider
		}

		// Track panel hover for visual feedback
		panel := m.HitTestPanel(msg.X, msg.Y)
		if panel != m.HoverPanel {
			m.HoverPanel = panel
		}
		return m, nil
	}

	return m, nil
}

// HitTestDivider returns which divider (0 or 1) contains the point, or -1 if none
func (m Model) HitTestDivider(x, y int) int {
	for i, bounds := range m.DividerBounds {
		if bounds.Contains(x, y) {
			return i
		}
	}
	return -1
}

// startDividerDrag begins dragging a divider
func (m Model) startDividerDrag(divider int, y int) (tea.Model, tea.Cmd) {
	m.DraggingDivider = divider
	m.DragStartY = y
	m.DragStartHeights = m.PaneHeights
	return m, nil
}

// updateDividerDrag updates pane heights during drag
func (m Model) updateDividerDrag(y int) (tea.Model, tea.Cmd) {
	if m.DraggingDivider < 0 {
		return m, nil
	}

	// Calculate available height
	searchBarHeight := 0
	if m.SearchMode || m.SearchQuery != "" {
		searchBarHeight = 2
	}
	footerHeight := 3
	if m.Embedded {
		footerHeight = 0
	}
	availableHeight := m.Height - footerHeight - searchBarHeight
	if availableHeight <= 0 {
		return m, nil // Terminal too small for resize
	}

	// Calculate delta as a ratio
	deltaY := y - m.DragStartY
	deltaRatio := float64(deltaY) / float64(availableHeight)

	// Get starting heights
	newHeights := m.DragStartHeights

	// Apply delta based on which divider is being dragged
	// Divider 0: between pane 0 and pane 1
	// Divider 1: between pane 1 and pane 2
	if m.DraggingDivider == 0 {
		// Moving divider 0 affects panes 0 and 1
		newHeights[0] = m.DragStartHeights[0] + deltaRatio
		newHeights[1] = m.DragStartHeights[1] - deltaRatio
	} else {
		// Moving divider 1 affects panes 1 and 2
		newHeights[1] = m.DragStartHeights[1] + deltaRatio
		newHeights[2] = m.DragStartHeights[2] - deltaRatio
	}

	// Enforce minimum 10% per pane (only check affected panes)
	const minHeight = 0.1
	p1, p2 := m.DraggingDivider, m.DraggingDivider+1
	for _, i := range []int{p1, p2} {
		if newHeights[i] < minHeight {
			deficit := minHeight - newHeights[i]
			newHeights[i] = minHeight
			// Take from the other affected pane
			other := p2
			if i == p2 {
				other = p1
			}
			newHeights[other] -= deficit
		}
	}

	// Re-check affected panes; abort if constraints can't be satisfied
	if newHeights[p1] < minHeight || newHeights[p2] < minHeight {
		return m, nil
	}

	// Normalize to ensure sum = 1.0
	sum := newHeights[0] + newHeights[1] + newHeights[2]
	for i := range newHeights {
		newHeights[i] /= sum
	}

	m.PaneHeights = newHeights
	m.updatePanelBounds()
	return m, nil
}

// endDividerDrag finishes dragging and persists the new heights
func (m Model) endDividerDrag() (tea.Model, tea.Cmd) {
	m.DraggingDivider = -1
	m.DividerHover = -1

	// Persist to config asynchronously
	return m, m.savePaneHeightsAsync()
}

// savePaneHeightsAsync returns a command that saves pane heights to config
func (m Model) savePaneHeightsAsync() tea.Cmd {
	heights := m.PaneHeights
	baseDir := m.BaseDir
	return func() tea.Msg {
		err := config.SetPaneHeights(baseDir, heights)
		return PaneHeightsSavedMsg{Error: err}
	}
}

// handleMouseWheel scrolls the panel under the cursor
func (m Model) handleMouseWheel(x, y, delta int) (tea.Model, tea.Cmd) {
	panel := m.HitTestPanel(x, y)
	if panel < 0 {
		return m, nil
	}

	// Scroll the hovered panel (better UX than requiring active panel)
	count := m.rowCount(panel)
	if count == 0 {
		return m, nil
	}

	// Update scroll offset
	newOffset := m.ScrollOffset[panel] + delta
	if newOffset < 0 {
		newOffset = 0
	}

	// Calculate max offset - for TaskList, account for category headers
	maxOffset := m.maxScrollOffset(panel)
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	m.ScrollOffset[panel] = newOffset

	// Keep cursor visible within the scrolled view
	m.ensureCursorVisible(panel)

	return m, nil
}

// maxScrollOffset returns the maximum valid scroll offset for a panel
func (m Model) maxScrollOffset(panel Panel) int {
	count := m.rowCount(panel)
	visibleHeight := m.visibleHeightForPanel(panel)

	if panel == PanelTaskList {
		// TaskList has category headers that consume extra lines
		// Calculate total display lines including headers and separators
		totalLines := m.taskListTotalLines()
		if totalLines <= visibleHeight {
			return 0 // No scrolling needed
		}
		// Find the smallest offset such that content from offset to end
		// fits within visibleHeight. Walk backwards from the end.
		for offset := count - 1; offset >= 0; offset-- {
			linesFromOffset := m.taskListLinesFromOffset(offset)
			if linesFromOffset > visibleHeight {
				// Previous offset was the right one
				if offset+1 < count {
					return offset + 1
				}
				return offset
			}
		}
		return 0
	}

	// For other panels, simple calculation
	maxOffset := count - visibleHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	return maxOffset
}

// taskListTotalLines calculates total display lines for TaskList including headers
func (m Model) taskListTotalLines() int {
	if len(m.TaskListRows) == 0 {
		return 0
	}

	lines := 0
	var currentCategory TaskListCategory
	for i, row := range m.TaskListRows {
		if row.Category != currentCategory {
			if i > 0 {
				lines++ // Blank separator
			}
			lines++ // Category header
			currentCategory = row.Category
		}
		lines++ // The row itself
	}
	return lines
}

// taskListLinesFromOffset calculates display lines needed from a given offset to end
func (m Model) taskListLinesFromOffset(offset int) int {
	if len(m.TaskListRows) == 0 || offset >= len(m.TaskListRows) {
		return 0
	}

	lines := 0
	var currentCategory TaskListCategory
	// Track category from before offset
	if offset > 0 {
		currentCategory = m.TaskListRows[offset-1].Category
	}

	for i := offset; i < len(m.TaskListRows); i++ {
		row := m.TaskListRows[i]
		if row.Category != currentCategory {
			if i > offset || offset > 0 {
				lines++ // Blank separator (not before first visible if at offset 0)
			}
			lines++ // Category header
			currentCategory = row.Category
		}
		lines++ // The row itself
	}
	return lines
}

// handleMouseClick handles left-click events
func (m Model) handleMouseClick(x, y int) (tea.Model, tea.Cmd) {
	panel := m.HitTestPanel(x, y)
	if panel < 0 {
		return m, nil
	}

	row := m.HitTestRow(panel, y)
	now := time.Now()

	// Check for double-click (same panel+row within 400ms)
	isDoubleClick := panel == m.LastClickPanel &&
		row == m.LastClickRow &&
		row >= 0 &&
		now.Sub(m.LastClickTime) < 400*time.Millisecond

	// Update click tracking
	m.LastClickTime = now
	m.LastClickPanel = panel
	m.LastClickRow = row

	// Click on panel: activate it
	if m.ActivePanel != panel {
		m.ActivePanel = panel
		m.clampCursor(panel)
		m.ensureCursorVisible(panel)
	}

	// Select the clicked row
	if row >= 0 && row != m.Cursor[panel] {
		m.Cursor[panel] = row
		m.saveSelectedID(panel)
		m.ensureCursorVisible(panel)
	}

	// Double-click opens issue details
	if isDoubleClick {
		return m.openModal()
	}

	return m, nil
}

// openNewIssueForm opens the new issue form
// If an epic is selected/open, auto-populates parent field
func (m Model) openNewIssueForm() (tea.Model, tea.Cmd) {
	var parentID string

	// Check if we're in a modal viewing an epic
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		if modal.Issue.Type == models.TypeEpic {
			parentID = modal.Issue.ID
		}
	}

	// Create form state
	m.FormState = NewFormState(FormModeCreate, parentID)
	m.FormOpen = true

	// Initialize the form
	return m, m.FormState.Form.Init()
}

// openEditIssueForm opens the edit form for the selected/modal issue
func (m Model) openEditIssueForm() (tea.Model, tea.Cmd) {
	var issue *models.Issue

	// If modal is open, edit that issue
	if modal := m.CurrentModal(); modal != nil && modal.Issue != nil {
		issue = modal.Issue
	} else {
		// Otherwise, edit the selected issue from the panel
		issueID := m.SelectedIssueID(m.ActivePanel)
		if issueID == "" {
			return m, nil
		}
		var err error
		issue, err = m.DB.GetIssue(issueID)
		if err != nil || issue == nil {
			return m, nil
		}
	}

	// Create form state with issue data
	m.FormState = NewFormStateForEdit(issue)
	m.FormOpen = true

	// Initialize the form
	return m, m.FormState.Form.Init()
}

// closeForm closes the form modal and clears state
func (m *Model) closeForm() {
	m.FormOpen = false
	m.FormState = nil
}

// submitForm validates and submits the form
func (m Model) submitForm() (tea.Model, tea.Cmd) {
	if m.FormState == nil {
		return m, nil
	}

	// Get issue data from form
	issue := m.FormState.ToIssue()
	deps := m.FormState.GetDependencies()

	if m.FormState.Mode == FormModeCreate {
		// Create new issue with all fields
		issue.Status = models.StatusOpen
		if err := m.DB.CreateIssue(issue); err != nil {
			m.Err = err
			return m, nil
		}

		// Add dependencies
		for _, depID := range deps {
			if depID != "" {
				_ = m.DB.AddDependency(issue.ID, depID, "depends_on")
			}
		}

		// Log action for undo
		m.DB.LogAction(&models.ActionLog{
			SessionID:  m.SessionID,
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   issue.ID,
		})

		m.closeForm()
		return m, m.fetchData()

	} else if m.FormState.Mode == FormModeEdit {
		// Update existing issue
		existingIssue, err := m.DB.GetIssue(m.FormState.IssueID)
		if err != nil || existingIssue == nil {
			m.Err = err
			return m, nil
		}

		// Update fields
		existingIssue.Title = issue.Title
		existingIssue.Type = issue.Type
		existingIssue.Priority = issue.Priority
		existingIssue.Description = issue.Description
		existingIssue.Labels = issue.Labels
		existingIssue.ParentID = issue.ParentID
		existingIssue.Points = issue.Points
		existingIssue.Acceptance = issue.Acceptance
		existingIssue.Minor = issue.Minor

		if err := m.DB.UpdateIssue(existingIssue); err != nil {
			m.Err = err
			return m, nil
		}

		// Log action for undo
		m.DB.LogAction(&models.ActionLog{
			SessionID:  m.SessionID,
			ActionType: models.ActionUpdate,
			EntityType: "issue",
			EntityID:   existingIssue.ID,
		})

		m.closeForm()

		// Refresh modal if open
		if modal := m.CurrentModal(); modal != nil && modal.IssueID == existingIssue.ID {
			return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(existingIssue.ID))
		}

		return m, m.fetchData()
	}

	return m, nil
}

// openExternalEditor opens the Description field in an external editor
// Uses $VISUAL > $EDITOR > vim fallback
func (m Model) openExternalEditor() (tea.Model, tea.Cmd) {
	if m.FormState == nil {
		return m, nil
	}

	// Get editor from environment
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}

	// Create temp file with .md extension for syntax highlighting
	tmpFile, err := os.CreateTemp("", "td-edit-*.md")
	if err != nil {
		m.StatusMessage = "Failed to create temp file: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	// Write current description content to temp file
	content := m.FormState.Description
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		m.StatusMessage = "Failed to write temp file: " + err.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}
	tmpFile.Close()

	tmpPath := tmpFile.Name()

	// Create editor command
	cmd := exec.Command(editor, tmpPath)

	// Use tea.ExecProcess to suspend TUI and run editor
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		// Read content from temp file
		data, readErr := os.ReadFile(tmpPath)
		os.Remove(tmpPath) // Clean up temp file

		if err != nil {
			return EditorFinishedMsg{
				Field: EditorFieldDescription,
				Error: err,
			}
		}
		if readErr != nil {
			return EditorFinishedMsg{
				Field: EditorFieldDescription,
				Error: readErr,
			}
		}

		return EditorFinishedMsg{
			Field:   EditorFieldDescription,
			Content: string(data),
		}
	})
}

// handleEditorFinished updates the form field after external editor closes
func (m Model) handleEditorFinished(msg EditorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.StatusMessage = "Editor error: " + msg.Error.Error()
		m.StatusIsError = true
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		})
	}

	if m.FormState == nil {
		return m, nil
	}

	// Update the appropriate field based on which was edited
	switch msg.Field {
	case EditorFieldDescription:
		m.FormState.Description = msg.Content
	case EditorFieldAcceptance:
		m.FormState.Acceptance = msg.Content
	}

	// Rebuild the form to reflect the changes
	m.FormState.buildForm()

	m.StatusMessage = "Content updated from editor"
	m.StatusIsError = false
	return m, tea.Batch(
		m.FormState.Form.Init(),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return ClearStatusMsg{}
		}),
	)
}
