package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/version"
	"github.com/marcus/td/pkg/monitor/keymap"
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
	ActivePanel  Panel
	ScrollOffset map[Panel]int
	Cursor            map[Panel]int    // Per-panel cursor position (selected row)
	SelectedID        map[Panel]string // Per-panel selected issue ID (preserved across refresh)
	ScrollIndependent map[Panel]bool   // True when user scrolled viewport away from cursor
	HelpOpen       bool // Whether help modal is open
	HelpScroll     int  // Current scroll position in help
	HelpTotalLines int  // Cached total line count in help
	ShowTDQHelp    bool // Show TDQ query syntax help (when in search mode)
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
	SearchMode     bool              // Whether search mode is active
	SearchQuery    string            // Current search query
	SearchInput    textinput.Model   // Text input for search (cursor support)
	IncludeClosed  bool           // Whether to include closed tasks
	SortMode       SortMode       // Task list sort order
	TypeFilterMode TypeFilterMode // Type filter (epic, task, bug, etc.)

	// Confirmation dialog state
	ConfirmOpen    bool
	ConfirmAction  string // "delete"
	ConfirmIssueID string
	ConfirmTitle   string

	// Close confirmation dialog state
	CloseConfirmOpen    bool
	CloseConfirmIssueID string
	CloseConfirmTitle   string
	CloseConfirmInput   textinput.Model

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
	searchInput.Prompt = ""  // No prompt, we show triangle icon separately
	searchInput.Width = 50   // Reasonable width for search queries
	searchInput.CharLimit = 200

	return Model{
		DB:              database,
		SessionID:       sessionID,
		RefreshInterval: interval,
		ScrollOffset:      make(map[Panel]int),
		Cursor:            make(map[Panel]int),
		SelectedID:        make(map[Panel]string),
		ScrollIndependent: make(map[Panel]bool),
		ActivePanel:     PanelCurrentWork,
		StartedAt:       time.Now(),
		SearchMode:      false,
		SearchQuery:     "",
		SearchInput:     searchInput,
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

	// Close confirmation mode: forward key messages to textinput
	if m.CloseConfirmOpen {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter":
				return m.executeCloseWithReason()
			case "esc":
				m.CloseConfirmOpen = false
				m.CloseConfirmIssueID = ""
				m.CloseConfirmTitle = ""
				return m, nil
			default:
				var cmd tea.Cmd
				m.CloseConfirmInput, cmd = m.CloseConfirmInput.Update(msg)
				return m, cmd
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

// fetchHandoffs returns a command that fetches all handoffs
func (m Model) fetchHandoffs() tea.Cmd {
	return func() tea.Msg {
		handoffs, err := m.DB.GetRecentHandoffs(50, time.Time{})
		return HandoffsDataMsg{Data: handoffs, Error: err}
	}
}
