// Package serve provides the HTTP API layer for td serve, including
// response envelopes, DTOs with explicit JSON serialization, and
// request validation helpers.
package serve

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/pkg/monitor"
)

// ============================================================================
// Response Envelope
// ============================================================================

// Envelope is the standard response wrapper for all API responses.
// Success: {"ok": true, "data": {...}}
// Error:   {"ok": false, "error": {"code": "...", "message": "...", "details": ...}}
type Envelope struct {
	OK    bool          `json:"ok"`
	Data  interface{}   `json:"data,omitempty"`
	Error *ErrorPayload `json:"error,omitempty"`
}

// ErrorPayload holds structured error information.
type ErrorPayload struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

// FieldError describes a single validation failure on a request field.
type FieldError struct {
	Field    string      `json:"field"`
	Rule     string      `json:"rule"`
	Value    interface{} `json:"value,omitempty"`
	Expected interface{} `json:"expected,omitempty"`
	Message  string      `json:"message"`
}

// ValidationDetails wraps field-level validation errors in error.details.
type ValidationDetails struct {
	Fields []FieldError `json:"fields"`
}

// Standard error codes mapped to HTTP status codes.
const (
	ErrValidation   = "validation_error" // 400
	ErrNotFound     = "not_found"        // 404
	ErrConflict     = "conflict"         // 409
	ErrUnauthorized = "unauthorized"     // 401
	ErrForbidden    = "forbidden"        // 403
	ErrInternal     = "internal"         // 500
)

// WriteSuccess writes a JSON success envelope with the given data and status.
func WriteSuccess(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Envelope{OK: true, Data: data}); err != nil {
		slog.Error("write success response", "err", err)
	}
}

// WriteError writes a JSON error envelope.
func WriteError(w http.ResponseWriter, code, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Envelope{
		OK: false,
		Error: &ErrorPayload{
			Code:    code,
			Message: message,
		},
	}); err != nil {
		slog.Error("write error response", "err", err)
	}
}

// WriteValidation writes a 400 validation_error response with field-level details.
func WriteValidation(w http.ResponseWriter, fields []FieldError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(Envelope{
		OK: false,
		Error: &ErrorPayload{
			Code:    ErrValidation,
			Message: "Validation failed",
			Details: ValidationDetails{Fields: fields},
		},
	}); err != nil {
		slog.Error("write validation response", "err", err)
	}
}

// ============================================================================
// Issue DTO
// ============================================================================

// IssueDTO is the API representation of an issue.
// All documented fields are always present (no omitempty for documented fields).
// Nullable fields use *string so they serialize as JSON null when nil.
// Collections serialize as [] when empty, never null.
type IssueDTO struct {
	ID                       string   `json:"id"`
	Title                    string   `json:"title"`
	Description              string   `json:"description"`
	Status                   string   `json:"status"`
	Type                     string   `json:"type"`
	Priority                 string   `json:"priority"`
	Points                   int      `json:"points"`
	Labels                   []string `json:"labels"`
	ParentID                 *string  `json:"parent_id"`
	Acceptance               string   `json:"acceptance"`
	Sprint                   string   `json:"sprint"`
	ImplementerSession       *string  `json:"implementer_session"`
	CreatorSession           *string  `json:"creator_session"`
	ReviewerSession          *string  `json:"reviewer_session"`
	ReviewRequestedBySession *string  `json:"review_requested_by_session"`
	ClosedBySession          *string  `json:"closed_by_session"`
	CreatedAt                string   `json:"created_at"`
	UpdatedAt                string   `json:"updated_at"`
	ReviewedAt               *string  `json:"reviewed_at"`
	ClosedAt                 *string  `json:"closed_at"`
	DeletedAt                *string  `json:"deleted_at"`
	Minor                    bool     `json:"minor"`
	CreatedBranch            *string  `json:"created_branch"`
	DeferUntil               *string  `json:"defer_until"`
	DueDate                  *string  `json:"due_date"`
	DeferCount               int      `json:"defer_count"`

	// ActiveReview is populated by GET /v1/issues/{id} and transition-response
	// payloads when the issue carries a non-superseded approval. Clients can
	// key off this field to distinguish "reviewed, awaiting close" from
	// "not yet reviewed". Omitted for issues without an active approval.
	ActiveReview *IssueReviewSummary `json:"active_review,omitempty"`

	// Reviews is populated by GET /v1/issues/{id}?with=reviews only. Kept
	// off the default response to keep list payloads compact.
	Reviews []IssueReviewDTO `json:"reviews,omitempty"`

	// AvailableTransitions lists the transition action names the requesting
	// session can perform on this issue right now (start/review/approve/reject/
	// block/unblock/close/reopen). Populated by GET /v1/issues/{id} only — it
	// requires session context and per-issue policy evaluation, so it is omitted
	// from list payloads. Clients should render exactly these actions and treat
	// an absent field as "unknown" (fall back to a status-based default).
	AvailableTransitions []string `json:"available_transitions,omitempty"`

	// DependencySummary is populated ONLY on board + list paths and carries a
	// compact view of this issue's unresolved blockers (the issues it depends on
	// whose status is not closed). It powers a "blocked" indicator without a
	// per-issue detail fetch. Left nil/omitted when the issue has zero unresolved
	// blockers, so the single-issue detail response and dependency-free issues
	// are unaffected.
	DependencySummary *DependencySummaryDTO `json:"dependency_summary,omitempty"`
}

// BlockerRefDTO is a compact reference to a single unresolved blocker — an
// issue that the card issue depends on (depends_on direction) whose status is
// not closed.
type BlockerRefDTO struct {
	DepID        string `json:"dep_id"`
	IssueID      string `json:"issue_id"` // the BLOCKER's id (= depends_on_id)
	Title        string `json:"title"`
	Status       string `json:"status"`        // never "closed" (filtered out)
	RelationType string `json:"relation_type"` // "depends_on"
}

// DependencySummaryDTO wraps the unresolved blockers for an issue.
type DependencySummaryDTO struct {
	Blockers []BlockerRefDTO `json:"blockers"`
}

// IssueToDTO converts a models.Issue to an IssueDTO with proper null/empty
// handling for the API layer.
func IssueToDTO(issue *models.Issue) IssueDTO {
	dto := IssueDTO{
		ID:          issue.ID,
		Title:       issue.Title,
		Description: issue.Description,
		Status:      string(issue.Status),
		Type:        string(issue.Type),
		Priority:    string(issue.Priority),
		Points:      issue.Points,
		Labels:      issue.Labels,
		Acceptance:  issue.Acceptance,
		Sprint:      issue.Sprint,
		Minor:       issue.Minor,
		DeferCount:  issue.DeferCount,
		CreatedAt:   issue.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   issue.UpdatedAt.Format(time.RFC3339),
	}

	// Ensure labels is always an array, never null
	if dto.Labels == nil {
		dto.Labels = []string{}
	}

	// Nullable string fields: use *string so empty means null
	dto.ParentID = nullableString(issue.ParentID)
	dto.ImplementerSession = nullableString(issue.ImplementerSession)
	dto.CreatorSession = nullableString(issue.CreatorSession)
	dto.ReviewerSession = nullableString(issue.ReviewerSession)
	dto.ReviewRequestedBySession = nullableString(issue.ReviewRequestedBySession)
	dto.ClosedBySession = nullableString(issue.ClosedBySession)
	dto.CreatedBranch = nullableString(issue.CreatedBranch)

	// Nullable *string fields (already pointers in model)
	dto.DeferUntil = issue.DeferUntil
	dto.DueDate = issue.DueDate

	// Nullable *time.Time fields
	dto.ReviewedAt = nullableTime(issue.ReviewedAt)
	dto.ClosedAt = nullableTime(issue.ClosedAt)
	dto.DeletedAt = nullableTime(issue.DeletedAt)

	return dto
}

// IssuesToDTOs converts a slice of issues to DTOs.
func IssuesToDTOs(issues []models.Issue) []IssueDTO {
	dtos := make([]IssueDTO, len(issues))
	for i := range issues {
		dtos[i] = IssueToDTO(&issues[i])
	}
	return dtos
}

// ============================================================================
// Log DTO
// ============================================================================

// LogDTO is the API representation of a session log entry.
type LogDTO struct {
	ID            string `json:"id"`
	IssueID       string `json:"issue_id"`
	SessionID     string `json:"session_id"`
	WorkSessionID string `json:"work_session_id"`
	Message       string `json:"message"`
	Type          string `json:"type"`
	Timestamp     string `json:"timestamp"`
}

// LogToDTO converts a models.Log to a LogDTO.
func LogToDTO(log *models.Log) LogDTO {
	return LogDTO{
		ID:            log.ID,
		IssueID:       log.IssueID,
		SessionID:     log.SessionID,
		WorkSessionID: log.WorkSessionID,
		Message:       log.Message,
		Type:          string(log.Type),
		Timestamp:     log.Timestamp.Format(time.RFC3339),
	}
}

// LogsToDTOs converts a slice of logs to DTOs.
func LogsToDTOs(logs []models.Log) []LogDTO {
	dtos := make([]LogDTO, len(logs))
	for i := range logs {
		dtos[i] = LogToDTO(&logs[i])
	}
	return dtos
}

// ============================================================================
// Comment DTO
// ============================================================================

// CommentDTO is the API representation of a comment.
type CommentDTO struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// CommentToDTO converts a models.Comment to a CommentDTO.
func CommentToDTO(comment *models.Comment) CommentDTO {
	return CommentDTO{
		ID:        comment.ID,
		IssueID:   comment.IssueID,
		SessionID: comment.SessionID,
		Text:      comment.Text,
		CreatedAt: comment.CreatedAt.Format(time.RFC3339),
	}
}

// CommentsToDTOs converts a slice of comments to DTOs.
func CommentsToDTOs(comments []models.Comment) []CommentDTO {
	dtos := make([]CommentDTO, len(comments))
	for i := range comments {
		dtos[i] = CommentToDTO(&comments[i])
	}
	return dtos
}

// ============================================================================
// Handoff DTO
// ============================================================================

// HandoffDTO is the API representation of a handoff.
type HandoffDTO struct {
	ID        string   `json:"id"`
	IssueID   string   `json:"issue_id"`
	SessionID string   `json:"session_id"`
	Done      []string `json:"done"`
	Remaining []string `json:"remaining"`
	Decisions []string `json:"decisions"`
	Uncertain []string `json:"uncertain"`
	Timestamp string   `json:"timestamp"`
}

// HandoffToDTO converts a models.Handoff to a HandoffDTO.
func HandoffToDTO(handoff *models.Handoff) HandoffDTO {
	dto := HandoffDTO{
		ID:        handoff.ID,
		IssueID:   handoff.IssueID,
		SessionID: handoff.SessionID,
		Done:      handoff.Done,
		Remaining: handoff.Remaining,
		Decisions: handoff.Decisions,
		Uncertain: handoff.Uncertain,
		Timestamp: handoff.Timestamp.Format(time.RFC3339),
	}
	// Ensure collections are never null
	if dto.Done == nil {
		dto.Done = []string{}
	}
	if dto.Remaining == nil {
		dto.Remaining = []string{}
	}
	if dto.Decisions == nil {
		dto.Decisions = []string{}
	}
	if dto.Uncertain == nil {
		dto.Uncertain = []string{}
	}
	return dto
}

// ============================================================================
// Dependency DTO
// ============================================================================

// DependencyDTO is the API representation of an issue dependency.
type DependencyDTO struct {
	DepID        string `json:"dep_id"`
	IssueID      string `json:"issue_id"`
	DependsOnID  string `json:"depends_on_id"`
	RelationType string `json:"relation_type"`
}

// DependencyToDTO converts a models.IssueDependency to a DependencyDTO.
func DependencyToDTO(dep *models.IssueDependency) DependencyDTO {
	return DependencyDTO{
		DepID:        db.DependencyID(dep.IssueID, dep.DependsOnID, dep.RelationType),
		IssueID:      dep.IssueID,
		DependsOnID:  dep.DependsOnID,
		RelationType: dep.RelationType,
	}
}

// DependenciesToDTOs converts a slice of dependencies to DTOs.
func DependenciesToDTOs(deps []models.IssueDependency) []DependencyDTO {
	dtos := make([]DependencyDTO, len(deps))
	for i := range deps {
		dtos[i] = DependencyToDTO(&deps[i])
	}
	return dtos
}

// ============================================================================
// Board DTO
// ============================================================================

// BoardDTO is the API representation of a board.
type BoardDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Query        string  `json:"query"`
	IsBuiltin    bool    `json:"is_builtin"`
	ViewMode     string  `json:"view_mode"`
	LastViewedAt *string `json:"last_viewed_at"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// BoardToDTO converts a models.Board to a BoardDTO.
func BoardToDTO(board *models.Board) BoardDTO {
	return BoardDTO{
		ID:           board.ID,
		Name:         board.Name,
		Query:        board.Query,
		IsBuiltin:    board.IsBuiltin,
		ViewMode:     board.ViewMode,
		LastViewedAt: nullableTime(board.LastViewedAt),
		CreatedAt:    board.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    board.UpdatedAt.Format(time.RFC3339),
	}
}

// BoardsToDTOs converts a slice of boards to DTOs.
func BoardsToDTOs(boards []models.Board) []BoardDTO {
	dtos := make([]BoardDTO, len(boards))
	for i := range boards {
		dtos[i] = BoardToDTO(&boards[i])
	}
	return dtos
}

// ============================================================================
// Session DTO
// ============================================================================

// SessionDTO is the API representation of a session.
type SessionDTO struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Branch            string  `json:"branch"`
	AgentType         string  `json:"agent_type"`
	AgentPID          int     `json:"agent_pid"`
	ContextID         string  `json:"context_id"`
	PreviousSessionID *string `json:"previous_session_id"`
	StartedAt         string  `json:"started_at"`
	LastActivity      string  `json:"last_activity"`
}

// SessionToDTO converts a session.Session to a SessionDTO.
func SessionToDTO(sess *session.Session) SessionDTO {
	return SessionDTO{
		ID:                sess.ID,
		Name:              sess.Name,
		Branch:            sess.Branch,
		AgentType:         sess.AgentType,
		AgentPID:          sess.AgentPID,
		ContextID:         sess.ContextID,
		PreviousSessionID: nullableString(sess.PreviousSessionID),
		StartedAt:         sess.StartedAt.Format(time.RFC3339),
		LastActivity:      sess.LastActivity.Format(time.RFC3339),
	}
}

// SessionsToDTOs converts a slice of sessions to DTOs.
func SessionsToDTOs(sessions []session.Session) []SessionDTO {
	dtos := make([]SessionDTO, len(sessions))
	for i := range sessions {
		dtos[i] = SessionToDTO(&sessions[i])
	}
	return dtos
}

// ============================================================================
// Activity Item DTO
// ============================================================================

// ActivityItemDTO is the API representation of a unified activity feed item.
type ActivityItemDTO struct {
	Timestamp    string `json:"timestamp"`
	SessionID    string `json:"session_id"`
	Type         string `json:"type"`
	IssueID      string `json:"issue_id"`
	IssueTitle   string `json:"issue_title"`
	Message      string `json:"message"`
	LogType      string `json:"log_type"`
	Action       string `json:"action"`
	EntityID     string `json:"entity_id"`
	EntityType   string `json:"entity_type"`
	PreviousData string `json:"previous_data"`
	NewData      string `json:"new_data"`
}

// ActivityItemToDTO converts a monitor.ActivityItem to an ActivityItemDTO.
func ActivityItemToDTO(item *monitor.ActivityItem) ActivityItemDTO {
	return ActivityItemDTO{
		Timestamp:    item.Timestamp.Format(time.RFC3339),
		SessionID:    item.SessionID,
		Type:         item.Type,
		IssueID:      item.IssueID,
		IssueTitle:   item.IssueTitle,
		Message:      item.Message,
		LogType:      string(item.LogType),
		Action:       string(item.Action),
		EntityID:     item.EntityID,
		EntityType:   item.EntityType,
		PreviousData: item.PreviousData,
		NewData:      item.NewData,
	}
}

// ActivityItemsToDTOs converts a slice of activity items to DTOs.
func ActivityItemsToDTOs(items []monitor.ActivityItem) []ActivityItemDTO {
	dtos := make([]ActivityItemDTO, len(items))
	for i := range items {
		dtos[i] = ActivityItemToDTO(&items[i])
	}
	return dtos
}

// ============================================================================
// Monitor DTO (GET /v1/monitor response)
// ============================================================================

// MonitorDTO is the API representation of the full monitor state.
type MonitorDTO struct {
	FocusedIssue   *IssueDTO          `json:"focused_issue"`
	InProgress     []IssueDTO         `json:"in_progress"`
	Activity       []ActivityItemDTO  `json:"activity"`
	TaskList       TaskListDTO        `json:"task_list"`
	RecentHandoffs []RecentHandoffDTO `json:"recent_handoffs"`
	ActiveSessions []string           `json:"active_sessions"`
	Timestamp      string             `json:"timestamp"`
}

// TaskListDTO is the API representation of categorized task lists.
type TaskListDTO struct {
	Reviewable    []IssueDTO `json:"reviewable"`
	NeedsRework   []IssueDTO `json:"needs_rework"`
	InProgress    []IssueDTO `json:"in_progress"`
	Ready         []IssueDTO `json:"ready"`
	PendingReview []IssueDTO `json:"pending_review"`
	Blocked       []IssueDTO `json:"blocked"`
	Closed        []IssueDTO `json:"closed"`
}

// RecentHandoffDTO is the API representation of a recent handoff summary.
type RecentHandoffDTO struct {
	IssueID   string `json:"issue_id"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
}

// MonitorDataToDTO converts a RefreshDataMsg to a MonitorDTO.
func MonitorDataToDTO(msg *monitor.RefreshDataMsg) MonitorDTO {
	dto := MonitorDTO{
		Timestamp:      msg.Timestamp.Format(time.RFC3339),
		ActiveSessions: msg.ActiveSessions,
	}

	// Ensure active sessions is never null
	if dto.ActiveSessions == nil {
		dto.ActiveSessions = []string{}
	}

	// Focused issue
	if msg.FocusedIssue != nil {
		focused := IssueToDTO(msg.FocusedIssue)
		dto.FocusedIssue = &focused
	}

	// In-progress issues
	dto.InProgress = issuesToDTOsNonNil(msg.InProgress)

	// Activity
	dto.Activity = activityToDTOsNonNil(msg.Activity)

	// Task list
	dto.TaskList = taskListDataToDTO(&msg.TaskList)

	// Recent handoffs
	dto.RecentHandoffs = make([]RecentHandoffDTO, len(msg.RecentHandoffs))
	for i, h := range msg.RecentHandoffs {
		dto.RecentHandoffs[i] = RecentHandoffDTO{
			IssueID:   h.IssueID,
			SessionID: h.SessionID,
			Timestamp: h.Timestamp.Format(time.RFC3339),
		}
	}
	if dto.RecentHandoffs == nil {
		dto.RecentHandoffs = []RecentHandoffDTO{}
	}

	return dto
}

// taskListDataToDTO converts monitor.TaskListData to TaskListDTO.
func taskListDataToDTO(data *monitor.TaskListData) TaskListDTO {
	return TaskListDTO{
		Reviewable:    issuesToDTOsNonNil(data.Reviewable),
		NeedsRework:   issuesToDTOsNonNil(data.NeedsRework),
		InProgress:    issuesToDTOsNonNil(data.InProgress),
		Ready:         issuesToDTOsNonNil(data.Ready),
		PendingReview: issuesToDTOsNonNil(data.PendingReview),
		Blocked:       issuesToDTOsNonNil(data.Blocked),
		Closed:        issuesToDTOsNonNil(data.Closed),
	}
}

// ============================================================================
// Stats DTO (GET /v1/stats response)
// ============================================================================

// StatsDTO is the API representation of extended project statistics.
type StatsDTO struct {
	Total      int            `json:"total"`
	ByStatus   map[string]int `json:"by_status"`
	ByType     map[string]int `json:"by_type"`
	ByPriority map[string]int `json:"by_priority"`

	OldestOpen      *IssueDTO `json:"oldest_open"`
	NewestTask      *IssueDTO `json:"newest_task"`
	LastClosed      *IssueDTO `json:"last_closed"`
	CreatedToday    int       `json:"created_today"`
	CreatedThisWeek int       `json:"created_this_week"`

	TotalPoints      int     `json:"total_points"`
	AvgPointsPerTask float64 `json:"avg_points_per_task"`
	CompletionRate   float64 `json:"completion_rate"`

	TotalLogs         int    `json:"total_logs"`
	TotalHandoffs     int    `json:"total_handoffs"`
	MostActiveSession string `json:"most_active_session"`
}

// StatsToDTO converts a models.ExtendedStats to a StatsDTO.
func StatsToDTO(stats *models.ExtendedStats) StatsDTO {
	dto := StatsDTO{
		Total:             stats.Total,
		ByStatus:          make(map[string]int),
		ByType:            make(map[string]int),
		ByPriority:        make(map[string]int),
		CreatedToday:      stats.CreatedToday,
		CreatedThisWeek:   stats.CreatedThisWeek,
		TotalPoints:       stats.TotalPoints,
		AvgPointsPerTask:  stats.AvgPointsPerTask,
		CompletionRate:    stats.CompletionRate,
		TotalLogs:         stats.TotalLogs,
		TotalHandoffs:     stats.TotalHandoffs,
		MostActiveSession: stats.MostActiveSession,
	}

	for status, count := range stats.ByStatus {
		dto.ByStatus[string(status)] = count
	}
	for typ, count := range stats.ByType {
		dto.ByType[string(typ)] = count
	}
	for prio, count := range stats.ByPriority {
		dto.ByPriority[string(prio)] = count
	}

	if stats.OldestOpen != nil {
		issue := IssueToDTO(stats.OldestOpen)
		dto.OldestOpen = &issue
	}
	if stats.NewestTask != nil {
		issue := IssueToDTO(stats.NewestTask)
		dto.NewestTask = &issue
	}
	if stats.LastClosed != nil {
		issue := IssueToDTO(stats.LastClosed)
		dto.LastClosed = &issue
	}

	return dto
}

// ============================================================================
// Pagination DTO
// ============================================================================

// PaginationDTO describes the pagination state returned alongside list results.
type PaginationDTO struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// PaginatedResponse wraps a list result with pagination metadata.
type PaginatedResponse struct {
	Items      interface{}   `json:"items"`
	Pagination PaginationDTO `json:"pagination"`
}

// ============================================================================
// Validation Helpers
// ============================================================================

// IssueCreateBody represents the expected JSON body for creating an issue.
type IssueCreateBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Priority    string   `json:"priority"`
	Points      int      `json:"points"`
	Labels      []string `json:"labels"`
	ParentID    string   `json:"parent_id"`
	Acceptance  string   `json:"acceptance"`
	Sprint      string   `json:"sprint"`
	Minor       bool     `json:"minor"`
	DeferUntil  string   `json:"defer_until"`
	DueDate     string   `json:"due_date"`
}

// IssueUpdateBody represents the expected JSON body for updating an issue.
// All fields are optional; only present fields are applied.
type IssueUpdateBody struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Type        *string  `json:"type"`
	Priority    *string  `json:"priority"`
	Points      *int     `json:"points"`
	Labels      []string `json:"labels"`
	ParentID    *string  `json:"parent_id"`
	Acceptance  *string  `json:"acceptance"`
	Sprint      *string  `json:"sprint"`
	Minor       *bool    `json:"minor"`
	DeferUntil  *string  `json:"defer_until"`
	DueDate     *string  `json:"due_date"`
}

// ValidateIssueCreate validates an IssueCreateBody and returns any field errors.
// The titleMin and titleMax parameters allow callers to pass configured limits.
func ValidateIssueCreate(body *IssueCreateBody, titleMin, titleMax int) []FieldError {
	var errs []FieldError

	// Title is required
	if body.Title == "" {
		errs = append(errs, FieldError{
			Field:   "title",
			Rule:    "required",
			Message: "title is required",
		})
	} else {
		if fe := validateTitleField(body.Title, titleMin, titleMax); fe != nil {
			errs = append(errs, *fe)
		}
	}

	// Type (optional, defaults to task)
	if body.Type != "" {
		normalized := models.NormalizeType(body.Type)
		if !models.IsValidType(normalized) {
			errs = append(errs, FieldError{
				Field:    "type",
				Rule:     "enum",
				Value:    body.Type,
				Expected: []string{"bug", "feature", "task", "epic", "chore"},
				Message:  fmt.Sprintf("invalid type: %s", body.Type),
			})
		}
	}

	// Priority (optional, defaults to P2)
	if body.Priority != "" {
		normalized := models.NormalizePriority(body.Priority)
		if !models.IsValidPriority(normalized) {
			errs = append(errs, FieldError{
				Field:    "priority",
				Rule:     "enum",
				Value:    body.Priority,
				Expected: []string{"P0", "P1", "P2", "P3", "P4"},
				Message:  fmt.Sprintf("invalid priority: %s", body.Priority),
			})
		}
	}

	// Points (optional)
	if body.Points != 0 {
		if !models.IsValidPoints(body.Points) {
			errs = append(errs, FieldError{
				Field:    "points",
				Rule:     "enum",
				Value:    body.Points,
				Expected: models.ValidPoints(),
				Message:  fmt.Sprintf("invalid points: %d (must be Fibonacci: 1,2,3,5,8,13,21)", body.Points),
			})
		}
	}

	// Dates
	if body.DeferUntil != "" {
		if fe := validateDateField("defer_until", body.DeferUntil); fe != nil {
			errs = append(errs, *fe)
		}
	}
	if body.DueDate != "" {
		if fe := validateDateField("due_date", body.DueDate); fe != nil {
			errs = append(errs, *fe)
		}
	}

	return errs
}

// ValidateIssueUpdate validates an IssueUpdateBody and returns any field errors.
// The titleMin and titleMax parameters allow callers to pass configured limits.
func ValidateIssueUpdate(body *IssueUpdateBody, titleMin, titleMax int) []FieldError {
	var errs []FieldError

	// Title (optional, but if present must be valid)
	if body.Title != nil {
		if *body.Title == "" {
			errs = append(errs, FieldError{
				Field:   "title",
				Rule:    "required",
				Message: "title cannot be empty",
			})
		} else {
			if fe := validateTitleField(*body.Title, titleMin, titleMax); fe != nil {
				errs = append(errs, *fe)
			}
		}
	}

	// Type
	if body.Type != nil && *body.Type != "" {
		normalized := models.NormalizeType(*body.Type)
		if !models.IsValidType(normalized) {
			errs = append(errs, FieldError{
				Field:    "type",
				Rule:     "enum",
				Value:    *body.Type,
				Expected: []string{"bug", "feature", "task", "epic", "chore"},
				Message:  fmt.Sprintf("invalid type: %s", *body.Type),
			})
		}
	}

	// Priority
	if body.Priority != nil && *body.Priority != "" {
		normalized := models.NormalizePriority(*body.Priority)
		if !models.IsValidPriority(normalized) {
			errs = append(errs, FieldError{
				Field:    "priority",
				Rule:     "enum",
				Value:    *body.Priority,
				Expected: []string{"P0", "P1", "P2", "P3", "P4"},
				Message:  fmt.Sprintf("invalid priority: %s", *body.Priority),
			})
		}
	}

	// Points
	if body.Points != nil && *body.Points != 0 {
		if !models.IsValidPoints(*body.Points) {
			errs = append(errs, FieldError{
				Field:    "points",
				Rule:     "enum",
				Value:    *body.Points,
				Expected: models.ValidPoints(),
				Message:  fmt.Sprintf("invalid points: %d (must be Fibonacci: 1,2,3,5,8,13,21)", *body.Points),
			})
		}
	}

	// Dates
	if body.DeferUntil != nil && *body.DeferUntil != "" {
		if fe := validateDateField("defer_until", *body.DeferUntil); fe != nil {
			errs = append(errs, *fe)
		}
	}
	if body.DueDate != nil && *body.DueDate != "" {
		if fe := validateDateField("due_date", *body.DueDate); fe != nil {
			errs = append(errs, *fe)
		}
	}

	return errs
}

// ValidatePagination validates limit and offset query parameters.
func ValidatePagination(limit, offset int) []FieldError {
	var errs []FieldError

	if limit < 1 || limit > 1000 {
		errs = append(errs, FieldError{
			Field:    "limit",
			Rule:     "range",
			Value:    limit,
			Expected: "1-1000",
			Message:  fmt.Sprintf("limit must be between 1 and 1000, got %d", limit),
		})
	}

	if offset < 0 {
		errs = append(errs, FieldError{
			Field:   "offset",
			Rule:    "min",
			Value:   offset,
			Message: fmt.Sprintf("offset must be >= 0, got %d", offset),
		})
	}

	return errs
}

// ============================================================================
// Internal Helpers
// ============================================================================

// nullableString converts a string to *string, returning nil for empty strings.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullableTime converts a *time.Time to *string (RFC3339), returning nil when input is nil.
func nullableTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// validateTitleField validates a title string and returns a FieldError or nil.
func validateTitleField(title string, minLen, maxLen int) *FieldError {
	trimmed := strings.TrimSpace(title)
	runeCount := utf8.RuneCountInString(trimmed)

	if runeCount < minLen {
		return &FieldError{
			Field:    "title",
			Rule:     "min_length",
			Value:    title,
			Expected: minLen,
			Message:  fmt.Sprintf("title too short (%d chars, min %d)", runeCount, minLen),
		}
	}
	if runeCount > maxLen {
		return &FieldError{
			Field:    "title",
			Rule:     "max_length",
			Value:    title,
			Expected: maxLen,
			Message:  fmt.Sprintf("title too long (%d chars, max %d)", runeCount, maxLen),
		}
	}

	return nil
}

// validateDateField validates a date string in YYYY-MM-DD format.
func validateDateField(field, value string) *FieldError {
	_, err := time.Parse("2006-01-02", value)
	if err != nil {
		return &FieldError{
			Field:    field,
			Rule:     "date_format",
			Value:    value,
			Expected: "YYYY-MM-DD",
			Message:  fmt.Sprintf("invalid date format for %s: expected YYYY-MM-DD", field),
		}
	}
	return nil
}

// issuesToDTOsNonNil converts issues to DTOs, returning empty slice instead of nil.
func issuesToDTOsNonNil(issues []models.Issue) []IssueDTO {
	if len(issues) == 0 {
		return []IssueDTO{}
	}
	return IssuesToDTOs(issues)
}

// activityToDTOsNonNil converts activity items to DTOs, returning empty slice instead of nil.
func activityToDTOsNonNil(items []monitor.ActivityItem) []ActivityItemDTO {
	if len(items) == 0 {
		return []ActivityItemDTO{}
	}
	return ActivityItemsToDTOs(items)
}
