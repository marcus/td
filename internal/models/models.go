// Package models defines the core domain types (Issue, Log, Handoff,
// WorkSession, Board, etc.) and their validation helpers.
package models

import (
	"strings"
	"time"
)

// Status represents issue status
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusInReview   Status = "in_review"
	StatusClosed     Status = "closed"
)

// Type represents issue type
type Type string

const (
	TypeBug     Type = "bug"
	TypeFeature Type = "feature"
	TypeTask    Type = "task"
	TypeEpic    Type = "epic"
	TypeChore   Type = "chore"
)

// Priority represents issue priority
type Priority string

const (
	PriorityP0 Priority = "P0" // critical
	PriorityP1 Priority = "P1" // high
	PriorityP2 Priority = "P2" // medium (default)
	PriorityP3 Priority = "P3" // low
	PriorityP4 Priority = "P4" // none
)

// LogType represents the type of log entry
type LogType string

const (
	LogTypeProgress      LogType = "progress"
	LogTypeSecurity      LogType = "security"
	LogTypeBlocker       LogType = "blocker"
	LogTypeDecision      LogType = "decision"
	LogTypeHypothesis    LogType = "hypothesis"
	LogTypeTried         LogType = "tried"
	LogTypeResult        LogType = "result"
	LogTypeOrchestration LogType = "orchestration"
)

// IssueSessionAction represents actions a session can take on an issue
type IssueSessionAction string

const (
	ActionSessionCreated   IssueSessionAction = "created"
	ActionSessionStarted   IssueSessionAction = "started"
	ActionSessionUnstarted IssueSessionAction = "unstarted"
	ActionSessionReviewed  IssueSessionAction = "reviewed"
)

// FileRole represents the role of a linked file
type FileRole string

const (
	FileRoleImplementation FileRole = "implementation"
	FileRoleTest           FileRole = "test"
	FileRoleReference      FileRole = "reference"
	FileRoleConfig         FileRole = "config"
)

// Issue represents a task/issue in the system
type Issue struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Description        string     `json:"description,omitempty"`
	Status             Status     `json:"status"`
	Type               Type       `json:"type"`
	Priority           Priority   `json:"priority"`
	Points             int        `json:"points"`
	Labels             []string   `json:"labels,omitempty"`
	ParentID           string     `json:"parent_id,omitempty"`
	Acceptance         string     `json:"acceptance,omitempty"`
	Sprint             string     `json:"sprint,omitempty"`
	ImplementerSession string     `json:"implementer_session"`
	CreatorSession     string     `json:"creator_session"`
	ReviewerSession    string     `json:"reviewer_session"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
	Minor              bool       `json:"minor"`
	CreatedBranch      string     `json:"created_branch,omitempty"`
}

// Log represents a session log entry
type Log struct {
	ID            string    `json:"id"`
	IssueID       string    `json:"issue_id"`
	SessionID     string    `json:"session_id"`
	WorkSessionID string    `json:"work_session_id,omitempty"`
	Message       string    `json:"message"`
	Type          LogType   `json:"type"`
	Timestamp     time.Time `json:"timestamp"`
}

// Handoff represents a structured handoff state
type Handoff struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	SessionID string    `json:"session_id"`
	Done      []string  `json:"done,omitempty"`
	Remaining []string  `json:"remaining,omitempty"`
	Decisions []string  `json:"decisions,omitempty"`
	Uncertain []string  `json:"uncertain,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GitSnapshot captures git state at a point in time
type GitSnapshot struct {
	ID         string    `json:"id"`
	IssueID    string    `json:"issue_id"`
	Event      string    `json:"event"` // start, handoff
	CommitSHA  string    `json:"commit_sha"`
	Branch     string    `json:"branch"`
	DirtyFiles int       `json:"dirty_files"`
	Timestamp  time.Time `json:"timestamp"`
}

// IssueFile represents a linked file
type IssueFile struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	FilePath  string    `json:"file_path"`
	Role      FileRole  `json:"role"`
	LinkedSHA string    `json:"linked_sha"`
	LinkedAt  time.Time `json:"linked_at"`
}

// IssueDependency represents issue relationships
type IssueDependency struct {
	IssueID      string `json:"issue_id"`
	DependsOnID  string `json:"depends_on_id"`
	RelationType string `json:"relation_type"` // blocks, depends_on
}

// WorkSession represents a multi-issue work session
type WorkSession struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	SessionID string     `json:"session_id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	StartSHA  string     `json:"start_sha,omitempty"`
	EndSHA    string     `json:"end_sha,omitempty"`
}

// IssueSessionHistory tracks all sessions that touched an issue
type IssueSessionHistory struct {
	ID        string             `json:"id"`
	IssueID   string             `json:"issue_id"`
	SessionID string             `json:"session_id"`
	Action    IssueSessionAction `json:"action"`
	CreatedAt time.Time          `json:"created_at"`
}

// WorkSessionIssue links a work session to an issue
type WorkSessionIssue struct {
	WorkSessionID string    `json:"work_session_id"`
	IssueID       string    `json:"issue_id"`
	TaggedAt      time.Time `json:"tagged_at"`
}

// Board represents a named view into issues with custom ordering
type Board struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Query        string     `json:"query"`      // TDQ query defining which issues appear
	IsBuiltin    bool       `json:"is_builtin"` // Cannot delete builtin boards
	ViewMode     string     `json:"view_mode"`  // "swimlanes" or "backlog"
	LastViewedAt *time.Time `json:"last_viewed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// BoardIssue represents board membership with ordering
type BoardIssue struct {
	BoardID  string    `json:"board_id"`
	IssueID  string    `json:"issue_id"`
	Position int       `json:"position"`
	AddedAt  time.Time `json:"added_at"`
}

// BoardIssueView joins BoardIssue with Issue data
type BoardIssueView struct {
	BoardID     string `json:"board_id"`
	Position    int    `json:"position"`     // Valid only when HasPosition is true
	HasPosition bool   `json:"has_position"` // True if explicitly positioned
	Issue       Issue  `json:"issue"`
	Category    string `json:"category"` // Computed category (ready/blocked/reviewable/etc)
}

// Comment represents a comment on an issue
type Comment struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	SessionID string    `json:"session_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Note represents a freeform note (synced via sidecar)
type Note struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Pinned    bool       `json:"pinned"`
	Archived  bool       `json:"archived"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// Config represents the local config state
type Config struct {
	FocusedIssueID    string          `json:"focused_issue_id,omitempty"`
	ActiveWorkSession string          `json:"active_work_session,omitempty"`
	PaneHeights       [3]float64      `json:"pane_heights,omitempty"`  // Ratios for 3 horizontal panes (sum=1.0)
	FeatureFlags      map[string]bool `json:"feature_flags,omitempty"` // Experimental feature gates
	// Filter state for monitor
	SearchQuery   string `json:"search_query,omitempty"`
	SortMode      string `json:"sort_mode,omitempty"`   // "priority", "created", "updated"
	TypeFilter    string `json:"type_filter,omitempty"` // "epic", "task", "bug", "feature", "chore", ""
	IncludeClosed bool   `json:"include_closed,omitempty"`
	// Title validation limits
	TitleMinLength int `json:"title_min_length,omitempty"` // Default: 15
	TitleMaxLength int `json:"title_max_length,omitempty"` // Default: 100
}

// ActionType represents the type of action that was performed
type ActionType string

const (
	ActionCreate           ActionType = "create"
	ActionUpdate           ActionType = "update"
	ActionDelete           ActionType = "delete"
	ActionRestore          ActionType = "restore"
	ActionStart            ActionType = "start"
	ActionReview           ActionType = "review"
	ActionApprove          ActionType = "approve"
	ActionReject           ActionType = "reject"
	ActionBlock            ActionType = "block"
	ActionUnblock          ActionType = "unblock"
	ActionClose            ActionType = "close"
	ActionReopen           ActionType = "reopen"
	ActionAddDep           ActionType = "add_dependency"
	ActionRemoveDep        ActionType = "remove_dependency"
	ActionLinkFile         ActionType = "link_file"
	ActionUnlinkFile       ActionType = "unlink_file"
	ActionHandoff          ActionType = "handoff"
	ActionBoardCreate      ActionType = "board_create"
	ActionBoardDelete      ActionType = "board_delete"
	ActionBoardUpdate      ActionType = "board_update"
	ActionBoardAddIssue    ActionType = "board_add_issue"
	ActionBoardRemoveIssue ActionType = "board_remove_issue"
	ActionBoardMoveIssue   ActionType = "board_move_issue"
	ActionBoardSetPosition ActionType = "board_set_position"
	ActionBoardUnposition  ActionType = "board_unposition"
	ActionWorkSessionTag   ActionType = "work_session_tag"
	ActionWorkSessionUntag ActionType = "work_session_untag"
)

// ActionLog represents a logged action that can be undone
type ActionLog struct {
	ID           string     `json:"id"`
	SessionID    string     `json:"session_id"`
	ActionType   ActionType `json:"action_type"`
	EntityType   string     `json:"entity_type"` // issue, dependency, file_link
	EntityID     string     `json:"entity_id"`
	PreviousData string     `json:"previous_data"` // JSON snapshot before action
	NewData      string     `json:"new_data"`      // JSON snapshot after action
	Timestamp    time.Time  `json:"timestamp"`
	Undone       bool       `json:"undone"`
}

// ValidPoints returns valid Fibonacci story points
func ValidPoints() []int {
	return []int{1, 2, 3, 5, 8, 13, 21}
}

// IsValidPoints checks if a point value is valid
func IsValidPoints(p int) bool {
	for _, v := range ValidPoints() {
		if v == p {
			return true
		}
	}
	return false
}

// IsValidStatus checks if a status is valid
func IsValidStatus(s Status) bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusInReview, StatusClosed:
		return true
	}
	return false
}

// IsValidType checks if a type is valid
func IsValidType(t Type) bool {
	switch t {
	case TypeBug, TypeFeature, TypeTask, TypeEpic, TypeChore:
		return true
	}
	return false
}

// IsValidPriority checks if a priority is valid
func IsValidPriority(p Priority) bool {
	switch p {
	case PriorityP0, PriorityP1, PriorityP2, PriorityP3, PriorityP4:
		return true
	}
	return false
}

// NormalizePriority converts alternate priority formats to canonical form
// Accepts: "0"-"4" as aliases, case-insensitive "p0"-"p4" or "P0"-"P4"
// Also accepts word forms: critical/highest→P0, high→P1, medium/normal→P2, low→P3, lowest/none→P4
func NormalizePriority(p string) Priority {
	// Normalize to lowercase for comparison
	lower := strings.ToLower(p)
	switch lower {
	case "0", "p0", "critical", "highest":
		return PriorityP0
	case "1", "p1", "high":
		return PriorityP1
	case "2", "p2", "medium", "normal", "default":
		return PriorityP2
	case "3", "p3", "low":
		return PriorityP3
	case "4", "p4", "lowest", "none":
		return PriorityP4
	default:
		return Priority(strings.ToUpper(p)) // Return uppercase for consistent error messages
	}
}

// NormalizeType converts alternate type names to canonical form
// Accepts: "story" as alias for "feature"
func NormalizeType(t string) Type {
	switch t {
	case "story":
		return TypeFeature
	default:
		return Type(t)
	}
}

// NormalizeStatus converts alternate status names to canonical form
// Accepts: "review" as alias for "in_review", hyphens converted to underscores
func NormalizeStatus(s string) Status {
	// Convert hyphens to underscores (in-progress → in_progress)
	normalized := strings.ReplaceAll(s, "-", "_")

	switch normalized {
	case "review":
		return StatusInReview
	default:
		return Status(normalized)
	}
}

// ExtendedStats holds detailed statistics for dashboard/stats displays
type ExtendedStats struct {
	// Counts
	Total      int
	ByStatus   map[Status]int
	ByType     map[Type]int
	ByPriority map[Priority]int

	// Timeline
	OldestOpen      *Issue
	NewestTask      *Issue
	LastClosed      *Issue
	CreatedToday    int
	CreatedThisWeek int

	// Points/velocity
	TotalPoints      int
	AvgPointsPerTask float64
	CompletionRate   float64 // closed / total created (or created + closed)

	// Activity
	TotalLogs         int
	TotalHandoffs     int
	MostActiveSession string
}
