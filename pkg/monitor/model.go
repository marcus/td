package monitor

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
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
	LastRefresh  time.Time
	StartedAt    time.Time // When monitor started, to track new handoffs
	Err          error     // Last error, if any

	// Flattened rows for selection
	TaskListRows    []TaskListRow // Flattened task list for selection
	CurrentWorkRows []string      // Issue IDs for current work panel (focused + in-progress)

	// Modal state for issue details
	ModalOpen        bool
	ModalIssueID     string
	ModalSourcePanel Panel // Panel the modal was opened from (for navigation)
	ModalScroll      int
	ModalLoading     bool
	ModalError       error
	ModalIssue       *models.Issue
	ModalHandoff     *models.Handoff
	ModalLogs        []models.Log
	ModalBlockedBy   []models.Issue // Dependencies (issues blocking this one)
	ModalBlocks      []models.Issue // Dependents (issues blocked by this one)
	ModalDescRender  string         // Pre-rendered description markdown
	ModalAcceptRender string        // Pre-rendered acceptance criteria markdown

	// Search state
	SearchMode    bool   // Whether search mode is active
	SearchQuery   string // Current search query
	IncludeClosed bool   // Whether to include closed tasks

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
	IssueID   string
	Issue     *models.Issue
	Handoff   *models.Handoff
	Logs      []models.Log
	BlockedBy []models.Issue // Dependencies (issues blocking this one)
	Blocks    []models.Issue // Dependents (issues blocked by this one)
	Error     error
}

// MarkdownRenderedMsg carries pre-rendered markdown for the modal
type MarkdownRenderedMsg struct {
	IssueID    string
	DescRender string
	AcceptRender string
}

// NewModel creates a new monitor model
func NewModel(database *db.DB, sessionID string, interval time.Duration) Model {
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
		if m.ModalOpen && msg.IssueID == m.ModalIssueID {
			m.ModalLoading = false
			m.ModalError = msg.Error
			m.ModalIssue = msg.Issue
			m.ModalHandoff = msg.Handoff
			m.ModalLogs = msg.Logs
			m.ModalBlockedBy = msg.BlockedBy
			m.ModalBlocks = msg.Blocks

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
		if m.ModalOpen && msg.IssueID == m.ModalIssueID {
			m.ModalDescRender = msg.DescRender
			m.ModalAcceptRender = msg.AcceptRender
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

// handleKey processes key input
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirmation dialog key handling
	if m.ConfirmOpen {
		return m.handleConfirmKey(msg)
	}

	// Stats modal key handling
	if m.StatsOpen {
		return m.handleStatsKey(msg)
	}

	// Modal-specific key handling
	if m.ModalOpen {
		return m.handleModalKey(msg)
	}

	// Search mode key handling
	if m.SearchMode {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "/":
		m.SearchMode = true
		m.SearchQuery = ""
		return m, nil

	case "c":
		m.IncludeClosed = !m.IncludeClosed
		return m, m.fetchData()

	case "tab":
		m.ActivePanel = (m.ActivePanel + 1) % 3
		return m, nil

	case "shift+tab":
		m.ActivePanel = (m.ActivePanel + 2) % 3
		return m, nil

	case "1":
		m.ActivePanel = PanelCurrentWork
		return m, nil

	case "2":
		m.ActivePanel = PanelTaskList
		return m, nil

	case "3":
		m.ActivePanel = PanelActivity
		return m, nil

	case "down":
		m.moveCursor(1)
		return m, nil

	case "up":
		m.moveCursor(-1)
		return m, nil

	case "j":
		m.ScrollOffset[m.ActivePanel]++
		return m, nil

	case "k":
		if m.ScrollOffset[m.ActivePanel] > 0 {
			m.ScrollOffset[m.ActivePanel]--
		}
		return m, nil

	case "r":
		// Mark for review if in Current Work panel, otherwise refresh
		if m.ActivePanel == PanelCurrentWork {
			return m.markForReview()
		}
		return m, m.fetchData()

	case "a":
		// Approve issue if in Task List panel
		if m.ActivePanel == PanelTaskList {
			return m.approveIssue()
		}
		return m, nil

	case "x":
		// Delete with confirmation
		return m.confirmDelete()

	case "?":
		m.ShowHelp = !m.ShowHelp
		return m, nil

	case "s":
		return m.openStatsModal()

	case "enter":
		return m.openModal()
	}

	return m, nil
}

// handleSearchKey processes key input when in search mode
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Exit search mode and reset
		m.SearchMode = false
		m.SearchQuery = ""
		return m, m.fetchData()

	case "enter":
		// Exit search mode but keep the query applied
		m.SearchMode = false
		return m, nil

	case "backspace":
		// Remove last character from query
		if len(m.SearchQuery) > 0 {
			m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-1]
			return m, m.fetchData()
		}
		return m, nil

	case "ctrl+u", "ctrl+w":
		// Clear entire query
		m.SearchQuery = ""
		return m, m.fetchData()

	case "space":
		m.SearchQuery += " "
		return m, m.fetchData()

	default:
		// Append printable characters to search query
		keyStr := msg.String()
		if len(keyStr) == 1 && keyStr >= " " && keyStr <= "~" {
			m.SearchQuery += keyStr
			return m, m.fetchData()
		}
	}

	return m, nil
}

// handleModalKey processes key input when modal is open
func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc", "enter":
		m.closeModal()
		return m, nil

	case "down", "j":
		m.ModalScroll++
		return m, nil

	case "up", "k":
		if m.ModalScroll > 0 {
			m.ModalScroll--
		}
		return m, nil

	case "left", "h":
		return m.navigateModal(-1)

	case "right", "l":
		return m.navigateModal(1)

	case "r":
		// Refresh both dashboard and modal details
		return m, tea.Batch(m.fetchData(), m.fetchIssueDetails(m.ModalIssueID))
	}

	return m, nil
}

// handleStatsKey processes key input when stats modal is open
func (m Model) handleStatsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc", "enter":
		m.closeStatsModal()
		return m, nil

	case "down", "j":
		m.StatsScroll++
		return m, nil

	case "up", "k":
		if m.StatsScroll > 0 {
			m.StatsScroll--
		}
		return m, nil

	case "r":
		// Refresh stats
		return m, m.fetchStats()
	}

	return m, nil
}

// navigateModal moves to the prev/next issue in the source panel's list
func (m Model) navigateModal(delta int) (tea.Model, tea.Cmd) {
	// Get the list of issue IDs for the source panel (panel that opened the modal)
	var issueIDs []string
	switch m.ModalSourcePanel {
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
		if id == m.ModalIssueID {
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

	// Navigate to new issue
	newIssueID := issueIDs[newIdx]
	m.ModalIssueID = newIssueID
	m.ModalScroll = 0
	m.ModalLoading = true
	m.ModalError = nil
	m.ModalIssue = nil
	m.ModalHandoff = nil
	m.ModalLogs = nil
	m.ModalBlockedBy = nil
	m.ModalBlocks = nil
	m.ModalDescRender = ""
	m.ModalAcceptRender = ""

	// Update cursor position to match in source panel
	m.Cursor[m.ModalSourcePanel] = newIdx
	m.saveSelectedID(m.ModalSourcePanel)

	return m, m.fetchIssueDetails(newIssueID)
}

// openModal opens the details modal for the currently selected issue
func (m Model) openModal() (tea.Model, tea.Cmd) {
	issueID := m.SelectedIssueID(m.ActivePanel)
	if issueID == "" {
		return m, nil
	}

	m.ModalOpen = true
	m.ModalIssueID = issueID
	m.ModalSourcePanel = m.ActivePanel // Track which panel opened the modal
	m.ModalScroll = 0
	m.ModalLoading = true
	m.ModalError = nil
	m.ModalIssue = nil
	m.ModalHandoff = nil
	m.ModalLogs = nil
	m.ModalBlockedBy = nil
	m.ModalBlocks = nil
	m.ModalDescRender = ""
	m.ModalAcceptRender = ""

	return m, m.fetchIssueDetails(issueID)
}

// closeModal closes the details modal and clears transient state
func (m *Model) closeModal() {
	m.ModalOpen = false
	m.ModalIssueID = ""
	m.ModalScroll = 0
	m.ModalLoading = false
	m.ModalError = nil
	m.ModalIssue = nil
	m.ModalHandoff = nil
	m.ModalLogs = nil
	m.ModalBlockedBy = nil
	m.ModalBlocks = nil
	m.ModalDescRender = ""
	m.ModalAcceptRender = ""
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
		data := FetchData(m.DB, m.SessionID, m.StartedAt, m.SearchQuery, m.IncludeClosed)
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

	// Scroll down if cursor below viewport
	if cursor >= offset+visibleHeight {
		m.ScrollOffset[panel] = cursor - visibleHeight + 1
	}
	// Scroll up if cursor above viewport
	if cursor < offset {
		m.ScrollOffset[panel] = cursor
	}
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
	availableHeight := m.Height - 3 - searchBarHeight
	panelHeight := availableHeight / 3

	// Account for title + border + category headers overhead
	return panelHeight - 3
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

// handleConfirmKey processes key input when confirmation dialog is open
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.ConfirmAction == "delete" {
			return m.executeDelete()
		}
	case "n", "N", "esc":
		m.ConfirmOpen = false
	}
	return m, nil
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
