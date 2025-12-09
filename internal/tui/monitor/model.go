package monitor

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// Panel represents which panel is active
type Panel int

const (
	PanelCurrentWork Panel = iota
	PanelActivity
	PanelTaskList
)

// ActivityItem represents a unified activity item (log, action, or comment)
type ActivityItem struct {
	Timestamp time.Time
	SessionID string
	Type      string // "log", "action", "comment"
	IssueID   string
	Message   string
	LogType   models.LogType   // for logs
	Action    models.ActionType // for actions
}

// TaskListData holds categorized issues for the task list panel
type TaskListData struct {
	Ready      []models.Issue
	Reviewable []models.Issue
	Blocked    []models.Issue
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
	FocusedIssue    *models.Issue
	InProgress      []models.Issue
	Activity        []ActivityItem
	TaskList        TaskListData
	RecentHandoffs  []RecentHandoff // Handoffs since monitor started
	ActiveSessions  []string        // Sessions with recent activity

	// UI state
	ActivePanel  Panel
	ScrollOffset map[Panel]int
	ShowHelp     bool
	LastRefresh  time.Time
	StartedAt    time.Time // When monitor started, to track new handoffs
	Err          error     // Last error, if any

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

// NewModel creates a new monitor model
func NewModel(database *db.DB, sessionID string, interval time.Duration) Model {
	return Model{
		DB:              database,
		SessionID:       sessionID,
		RefreshInterval: interval,
		ScrollOffset:    make(map[Panel]int),
		ActivePanel:     PanelCurrentWork,
		StartedAt:       time.Now(),
	}
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
		return m, nil
	}

	return m, nil
}

// handleKey processes key input
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

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
		m.ActivePanel = PanelActivity
		return m, nil

	case "3":
		m.ActivePanel = PanelTaskList
		return m, nil

	case "j", "down":
		m.ScrollOffset[m.ActivePanel]++
		return m, nil

	case "k", "up":
		if m.ScrollOffset[m.ActivePanel] > 0 {
			m.ScrollOffset[m.ActivePanel]--
		}
		return m, nil

	case "r":
		return m, m.fetchData()

	case "?":
		m.ShowHelp = !m.ShowHelp
		return m, nil
	}

	return m, nil
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
		data := FetchData(m.DB, m.SessionID, m.StartedAt)
		return data
	}
}
