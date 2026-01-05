package monitor

import (
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
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

// TaskListCategory represents the category of a task list row
type TaskListCategory string

const (
	CategoryReviewable TaskListCategory = "REVIEW"
	CategoryReady      TaskListCategory = "READY"
	CategoryBlocked    TaskListCategory = "BLOCKED"
	CategoryClosed     TaskListCategory = "CLOSED"
)

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

	// Navigation scope - when set, l/r navigates within this list instead of source panel
	// Used when opening issues from within an epic to scope navigation to siblings
	NavigationScope []models.Issue

	// Blocked-by section (dependencies blocking this issue)
	BlockedBySectionFocused bool
	BlockedByCursor         int

	// Blocks section (issues blocked by this one)
	BlocksSectionFocused bool
	BlocksCursor         int

	// Line tracking for mouse click support (set during render)
	BlockedByStartLine int // Line index where blocked-by section starts
	BlockedByEndLine   int // Line index where blocked-by section ends
	BlocksStartLine    int // Line index where blocks section starts
	BlocksEndLine      int // Line index where blocks section ends
}

// Minimum dimensions for the monitor
const (
	MinWidth  = 40
	MinHeight = 15
)

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
