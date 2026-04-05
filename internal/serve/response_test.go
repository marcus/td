package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/pkg/monitor"
)

// ============================================================================
// Response Envelope Tests
// ============================================================================

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSuccess(w, map[string]string{"id": "td-abc123"}, http.StatusCreated)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !env.OK {
		t.Error("ok = false, want true")
	}
	if env.Error != nil {
		t.Errorf("error should be nil, got %+v", env.Error)
	}

	// Data should be present
	dataMap, ok := env.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data type = %T, want map[string]interface{}", env.Data)
	}
	if dataMap["id"] != "td-abc123" {
		t.Errorf("data.id = %v, want td-abc123", dataMap["id"])
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, ErrNotFound, "issue not found: td-xyz", http.StatusNotFound)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Data != nil {
		t.Errorf("data should be nil, got %+v", env.Data)
	}
	if env.Error == nil {
		t.Fatal("error should not be nil")
	}
	if env.Error.Code != ErrNotFound {
		t.Errorf("error.code = %q, want %q", env.Error.Code, ErrNotFound)
	}
	if env.Error.Message != "issue not found: td-xyz" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "issue not found: td-xyz")
	}
}

func TestWriteValidation(t *testing.T) {
	w := httptest.NewRecorder()
	fields := []FieldError{
		{Field: "title", Rule: "required", Message: "title is required"},
		{Field: "type", Rule: "enum", Value: "invalid", Expected: []string{"bug", "feature"}, Message: "invalid type"},
	}
	WriteValidation(w, fields)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil {
		t.Fatal("error should not be nil")
	}
	if env.Error.Code != ErrValidation {
		t.Errorf("error.code = %q, want %q", env.Error.Code, ErrValidation)
	}

	// Details should be wrapped as {"fields":[...]}
	detailsJSON, err := json.Marshal(env.Error.Details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	var parsed ValidationDetails
	if err := json.Unmarshal(detailsJSON, &parsed); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if len(parsed.Fields) != 2 {
		t.Errorf("len(details.fields) = %d, want 2", len(parsed.Fields))
	}
	if parsed.Fields[0].Field != "title" {
		t.Errorf("details.fields[0].field = %q, want title", parsed.Fields[0].Field)
	}
}

func TestEnvelopeJSONShape(t *testing.T) {
	// Verify that the JSON output has the exact keys we expect
	t.Run("success has no error key", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteSuccess(w, "hello", http.StatusOK)

		var raw map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if _, exists := raw["error"]; exists {
			t.Error("success response should not have 'error' key")
		}
		if _, exists := raw["ok"]; !exists {
			t.Error("response should have 'ok' key")
		}
		if _, exists := raw["data"]; !exists {
			t.Error("success response should have 'data' key")
		}
	})

	t.Run("error has no data key", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteError(w, ErrInternal, "fail", http.StatusInternalServerError)

		var raw map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if _, exists := raw["data"]; exists {
			t.Error("error response should not have 'data' key")
		}
		if _, exists := raw["error"]; !exists {
			t.Error("error response should have 'error' key")
		}
	})
}

// ============================================================================
// Issue DTO Conversion Tests
// ============================================================================

func TestIssueToDTO_FullyPopulated(t *testing.T) {
	now := time.Now()
	closedAt := now.Add(-time.Hour)
	defer1 := "2026-03-01"
	due1 := "2026-04-01"

	issue := &models.Issue{
		ID:                 "td-abc123",
		Title:              "Fix the login bug",
		Description:        "Users cannot log in",
		Status:             models.StatusClosed,
		Type:               models.TypeBug,
		Priority:           models.PriorityP0,
		Points:             5,
		Labels:             []string{"urgent", "auth"},
		ParentID:           "td-parent1",
		Acceptance:         "Login works",
		Sprint:             "sprint-1",
		ImplementerSession: "ses_impl1",
		CreatorSession:     "ses_creator1",
		ReviewerSession:    "ses_reviewer1",
		CreatedAt:          now,
		UpdatedAt:          now,
		ClosedAt:           &closedAt,
		Minor:              true,
		CreatedBranch:      "fix/login-bug",
		DeferUntil:         &defer1,
		DueDate:            &due1,
		DeferCount:         2,
	}

	dto := IssueToDTO(issue)

	if dto.ID != "td-abc123" {
		t.Errorf("ID = %q, want td-abc123", dto.ID)
	}
	if dto.Status != "closed" {
		t.Errorf("Status = %q, want closed", dto.Status)
	}
	if dto.Type != "bug" {
		t.Errorf("Type = %q, want bug", dto.Type)
	}
	if dto.Priority != "P0" {
		t.Errorf("Priority = %q, want P0", dto.Priority)
	}
	if dto.Points != 5 {
		t.Errorf("Points = %d, want 5", dto.Points)
	}
	if len(dto.Labels) != 2 || dto.Labels[0] != "urgent" {
		t.Errorf("Labels = %v, want [urgent auth]", dto.Labels)
	}
	if dto.ParentID == nil || *dto.ParentID != "td-parent1" {
		t.Errorf("ParentID = %v, want td-parent1", dto.ParentID)
	}
	if dto.ImplementerSession == nil || *dto.ImplementerSession != "ses_impl1" {
		t.Errorf("ImplementerSession = %v, want ses_impl1", dto.ImplementerSession)
	}
	if dto.CreatorSession == nil || *dto.CreatorSession != "ses_creator1" {
		t.Errorf("CreatorSession = %v, want ses_creator1", dto.CreatorSession)
	}
	if dto.ReviewerSession == nil || *dto.ReviewerSession != "ses_reviewer1" {
		t.Errorf("ReviewerSession = %v, want ses_reviewer1", dto.ReviewerSession)
	}
	if dto.CreatedBranch == nil || *dto.CreatedBranch != "fix/login-bug" {
		t.Errorf("CreatedBranch = %v, want fix/login-bug", dto.CreatedBranch)
	}
	if dto.ClosedAt == nil {
		t.Error("ClosedAt should not be nil")
	}
	if dto.DeletedAt != nil {
		t.Error("DeletedAt should be nil")
	}
	if dto.DeferUntil == nil || *dto.DeferUntil != "2026-03-01" {
		t.Errorf("DeferUntil = %v, want 2026-03-01", dto.DeferUntil)
	}
	if dto.DueDate == nil || *dto.DueDate != "2026-04-01" {
		t.Errorf("DueDate = %v, want 2026-04-01", dto.DueDate)
	}
	if dto.DeferCount != 2 {
		t.Errorf("DeferCount = %d, want 2", dto.DeferCount)
	}
	if !dto.Minor {
		t.Error("Minor = false, want true")
	}
}

func TestIssueToDTO_EmptyNullableFields(t *testing.T) {
	issue := &models.Issue{
		ID:        "td-empty1",
		Title:     "Minimal issue with no optional fields",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		Priority:  models.PriorityP2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	dto := IssueToDTO(issue)

	// Nullable string fields should be nil (JSON null) when source is empty
	if dto.ParentID != nil {
		t.Errorf("ParentID = %v, want nil", dto.ParentID)
	}
	if dto.ImplementerSession != nil {
		t.Errorf("ImplementerSession = %v, want nil", dto.ImplementerSession)
	}
	if dto.CreatorSession != nil {
		t.Errorf("CreatorSession = %v, want nil", dto.CreatorSession)
	}
	if dto.ReviewerSession != nil {
		t.Errorf("ReviewerSession = %v, want nil", dto.ReviewerSession)
	}
	if dto.CreatedBranch != nil {
		t.Errorf("CreatedBranch = %v, want nil", dto.CreatedBranch)
	}
	if dto.ClosedAt != nil {
		t.Errorf("ClosedAt = %v, want nil", dto.ClosedAt)
	}
	if dto.DeletedAt != nil {
		t.Errorf("DeletedAt = %v, want nil", dto.DeletedAt)
	}
	if dto.DeferUntil != nil {
		t.Errorf("DeferUntil = %v, want nil", dto.DeferUntil)
	}
	if dto.DueDate != nil {
		t.Errorf("DueDate = %v, want nil", dto.DueDate)
	}

	// String fields should be "" (not null)
	if dto.Description != "" {
		t.Errorf("Description = %q, want empty", dto.Description)
	}
	if dto.Acceptance != "" {
		t.Errorf("Acceptance = %q, want empty", dto.Acceptance)
	}
	if dto.Sprint != "" {
		t.Errorf("Sprint = %q, want empty", dto.Sprint)
	}
}

func TestIssueToDTO_LabelsNeverNull(t *testing.T) {
	// Issue with nil labels should serialize as []
	issue := &models.Issue{
		ID:        "td-nolabels",
		Title:     "Issue with nil labels slice",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		Priority:  models.PriorityP2,
		Labels:    nil, // explicitly nil
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	dto := IssueToDTO(issue)

	if dto.Labels == nil {
		t.Fatal("Labels should not be nil")
	}
	if len(dto.Labels) != 0 {
		t.Errorf("len(Labels) = %d, want 0", len(dto.Labels))
	}

	// Verify JSON serialization is [] not null
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	labels, ok := raw["labels"].([]interface{})
	if !ok {
		t.Fatalf("labels should be array, got %T (%v)", raw["labels"], raw["labels"])
	}
	if len(labels) != 0 {
		t.Errorf("JSON labels should be empty array, got %v", labels)
	}
}

func TestIssueToDTO_NullableFieldsSerializeAsNull(t *testing.T) {
	issue := &models.Issue{
		ID:        "td-nulls1",
		Title:     "Issue where nullable fields serialize as null",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		Priority:  models.PriorityP2,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	dto := IssueToDTO(issue)
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	nullFields := []string{"parent_id", "implementer_session", "creator_session", "reviewer_session",
		"created_branch", "defer_until", "due_date", "closed_at", "deleted_at"}
	for _, field := range nullFields {
		val, exists := raw[field]
		if !exists {
			t.Errorf("field %q missing from JSON output", field)
			continue
		}
		if val != nil {
			t.Errorf("field %q = %v, want null", field, val)
		}
	}
}

func TestIssuesToDTOs(t *testing.T) {
	issues := []models.Issue{
		{ID: "td-1", Title: "First issue title here", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP2, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "td-2", Title: "Second issue title here", Status: models.StatusClosed, Type: models.TypeBug, Priority: models.PriorityP0, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	dtos := IssuesToDTOs(issues)
	if len(dtos) != 2 {
		t.Fatalf("len = %d, want 2", len(dtos))
	}
	if dtos[0].ID != "td-1" || dtos[1].ID != "td-2" {
		t.Error("DTO IDs don't match")
	}
}

// ============================================================================
// Log DTO Tests
// ============================================================================

func TestLogToDTO(t *testing.T) {
	now := time.Now()
	log := &models.Log{
		ID:            "log-1",
		IssueID:       "td-abc",
		SessionID:     "ses_1",
		WorkSessionID: "ws-1",
		Message:       "Fixed the thing",
		Type:          models.LogTypeProgress,
		Timestamp:     now,
	}

	dto := LogToDTO(log)
	if dto.ID != "log-1" {
		t.Errorf("ID = %q", dto.ID)
	}
	if dto.Type != "progress" {
		t.Errorf("Type = %q, want progress", dto.Type)
	}
	if dto.Timestamp != now.Format(time.RFC3339) {
		t.Errorf("Timestamp = %q", dto.Timestamp)
	}
}

// ============================================================================
// Comment DTO Tests
// ============================================================================

func TestCommentToDTO(t *testing.T) {
	now := time.Now()
	comment := &models.Comment{
		ID:        "cmt-1",
		IssueID:   "td-abc",
		SessionID: "ses_1",
		Text:      "This looks good",
		CreatedAt: now,
	}

	dto := CommentToDTO(comment)
	if dto.ID != "cmt-1" {
		t.Errorf("ID = %q", dto.ID)
	}
	if dto.Text != "This looks good" {
		t.Errorf("Text = %q", dto.Text)
	}
}

// ============================================================================
// Handoff DTO Tests
// ============================================================================

func TestHandoffToDTO_EmptySlicesNotNull(t *testing.T) {
	handoff := &models.Handoff{
		ID:        "ho-1",
		IssueID:   "td-abc",
		SessionID: "ses_1",
		Done:      nil,
		Remaining: nil,
		Decisions: nil,
		Uncertain: nil,
		Timestamp: time.Now(),
	}

	dto := HandoffToDTO(handoff)
	if dto.Done == nil {
		t.Error("Done should not be nil")
	}
	if dto.Remaining == nil {
		t.Error("Remaining should not be nil")
	}
	if dto.Decisions == nil {
		t.Error("Decisions should not be nil")
	}
	if dto.Uncertain == nil {
		t.Error("Uncertain should not be nil")
	}

	// Verify JSON
	data, _ := json.Marshal(dto)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range []string{"done", "remaining", "decisions", "uncertain"} {
		arr, ok := raw[field].([]interface{})
		if !ok {
			t.Errorf("%s should be array, got %T", field, raw[field])
			continue
		}
		if len(arr) != 0 {
			t.Errorf("%s should be empty array", field)
		}
	}
}

func TestHandoffToDTO_WithData(t *testing.T) {
	handoff := &models.Handoff{
		ID:        "ho-2",
		IssueID:   "td-abc",
		SessionID: "ses_1",
		Done:      []string{"step 1", "step 2"},
		Remaining: []string{"step 3"},
		Decisions: []string{"use approach A"},
		Uncertain: []string{"unclear about edge case"},
		Timestamp: time.Now(),
	}

	dto := HandoffToDTO(handoff)
	if len(dto.Done) != 2 {
		t.Errorf("len(Done) = %d, want 2", len(dto.Done))
	}
	if len(dto.Remaining) != 1 {
		t.Errorf("len(Remaining) = %d, want 1", len(dto.Remaining))
	}
}

// ============================================================================
// Dependency DTO Tests
// ============================================================================

func TestDependencyToDTO(t *testing.T) {
	dep := &models.IssueDependency{
		IssueID:      "td-1",
		DependsOnID:  "td-2",
		RelationType: "depends_on",
	}

	dto := DependencyToDTO(dep)
	if dto.IssueID != "td-1" {
		t.Errorf("IssueID = %q", dto.IssueID)
	}
	if dto.DependsOnID != "td-2" {
		t.Errorf("DependsOnID = %q", dto.DependsOnID)
	}
	if dto.RelationType != "depends_on" {
		t.Errorf("RelationType = %q", dto.RelationType)
	}
}

// ============================================================================
// Board DTO Tests
// ============================================================================

func TestBoardToDTO(t *testing.T) {
	now := time.Now()
	lastViewed := now.Add(-time.Hour)
	board := &models.Board{
		ID:           "bd-abc",
		Name:         "Sprint 1",
		Query:        "status = open",
		IsBuiltin:    false,
		ViewMode:     "swimlanes",
		LastViewedAt: &lastViewed,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	dto := BoardToDTO(board)
	if dto.ID != "bd-abc" {
		t.Errorf("ID = %q", dto.ID)
	}
	if dto.LastViewedAt == nil {
		t.Error("LastViewedAt should not be nil")
	}
}

func TestBoardToDTO_NilLastViewed(t *testing.T) {
	board := &models.Board{
		ID:        "bd-abc",
		Name:      "Sprint 1",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	dto := BoardToDTO(board)
	if dto.LastViewedAt != nil {
		t.Errorf("LastViewedAt = %v, want nil", dto.LastViewedAt)
	}
}

// ============================================================================
// Session DTO Tests
// ============================================================================

func TestSessionToDTO(t *testing.T) {
	sess := &session.Session{
		ID:                "ses_abc123",
		Name:              "my-session",
		Branch:            "main",
		AgentType:         "claude-code",
		AgentPID:          12345,
		ContextID:         "ctx:1",
		PreviousSessionID: "ses_prev",
		StartedAt:         time.Now(),
		LastActivity:      time.Now(),
	}

	dto := SessionToDTO(sess)
	if dto.ID != "ses_abc123" {
		t.Errorf("ID = %q", dto.ID)
	}
	if dto.PreviousSessionID == nil || *dto.PreviousSessionID != "ses_prev" {
		t.Errorf("PreviousSessionID = %v, want ses_prev", dto.PreviousSessionID)
	}
}

func TestSessionToDTO_NoPreviousSession(t *testing.T) {
	sess := &session.Session{
		ID:           "ses_abc123",
		StartedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	dto := SessionToDTO(sess)
	if dto.PreviousSessionID != nil {
		t.Errorf("PreviousSessionID = %v, want nil", dto.PreviousSessionID)
	}
}

// ============================================================================
// ActivityItem DTO Tests
// ============================================================================

func TestActivityItemToDTO(t *testing.T) {
	item := &monitor.ActivityItem{
		Timestamp:  time.Now(),
		SessionID:  "ses_1",
		Type:       "log",
		IssueID:    "td-abc",
		IssueTitle: "Fix login",
		Message:    "Started work",
		LogType:    models.LogTypeProgress,
		EntityID:   "log-1",
	}

	dto := ActivityItemToDTO(item)
	if dto.Type != "log" {
		t.Errorf("Type = %q, want log", dto.Type)
	}
	if dto.LogType != "progress" {
		t.Errorf("LogType = %q, want progress", dto.LogType)
	}
	if dto.IssueTitle != "Fix login" {
		t.Errorf("IssueTitle = %q", dto.IssueTitle)
	}
}

// ============================================================================
// Monitor DTO Tests
// ============================================================================

func TestMonitorDataToDTO_Empty(t *testing.T) {
	msg := &monitor.RefreshDataMsg{
		Timestamp: time.Now(),
	}

	dto := MonitorDataToDTO(msg)
	if dto.FocusedIssue != nil {
		t.Error("FocusedIssue should be nil")
	}
	if dto.InProgress == nil {
		t.Error("InProgress should not be nil (should be empty array)")
	}
	if len(dto.InProgress) != 0 {
		t.Errorf("len(InProgress) = %d, want 0", len(dto.InProgress))
	}
	if dto.Activity == nil {
		t.Error("Activity should not be nil")
	}
	if dto.ActiveSessions == nil {
		t.Error("ActiveSessions should not be nil")
	}
	if dto.RecentHandoffs == nil {
		t.Error("RecentHandoffs should not be nil")
	}
}

func TestMonitorDataToDTO_WithFocusedIssue(t *testing.T) {
	issue := &models.Issue{
		ID:        "td-focused",
		Title:     "This is the focused issue for testing",
		Status:    models.StatusInProgress,
		Type:      models.TypeTask,
		Priority:  models.PriorityP1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	msg := &monitor.RefreshDataMsg{
		FocusedIssue: issue,
		Timestamp:    time.Now(),
	}

	dto := MonitorDataToDTO(msg)
	if dto.FocusedIssue == nil {
		t.Fatal("FocusedIssue should not be nil")
	}
	if dto.FocusedIssue.ID != "td-focused" {
		t.Errorf("FocusedIssue.ID = %q", dto.FocusedIssue.ID)
	}
}

// ============================================================================
// Stats DTO Tests
// ============================================================================

func TestStatsToDTO(t *testing.T) {
	oldest := &models.Issue{ID: "td-oldest", Title: "Oldest open issue for testing", Status: models.StatusOpen, Type: models.TypeTask, Priority: models.PriorityP3, CreatedAt: time.Now(), UpdatedAt: time.Now()}

	stats := &models.ExtendedStats{
		Total: 42,
		ByStatus: map[models.Status]int{
			models.StatusOpen:   10,
			models.StatusClosed: 32,
		},
		ByType: map[models.Type]int{
			models.TypeBug:  15,
			models.TypeTask: 27,
		},
		ByPriority: map[models.Priority]int{
			models.PriorityP0: 5,
			models.PriorityP2: 37,
		},
		OldestOpen:        oldest,
		CreatedToday:      3,
		CreatedThisWeek:   10,
		TotalPoints:       100,
		AvgPointsPerTask:  2.38,
		CompletionRate:    0.76,
		TotalLogs:         200,
		TotalHandoffs:     15,
		MostActiveSession: "ses_active",
	}

	dto := StatsToDTO(stats)
	if dto.Total != 42 {
		t.Errorf("Total = %d, want 42", dto.Total)
	}
	if dto.ByStatus["open"] != 10 {
		t.Errorf("ByStatus[open] = %d, want 10", dto.ByStatus["open"])
	}
	if dto.ByType["bug"] != 15 {
		t.Errorf("ByType[bug] = %d, want 15", dto.ByType["bug"])
	}
	if dto.ByPriority["P0"] != 5 {
		t.Errorf("ByPriority[P0] = %d, want 5", dto.ByPriority["P0"])
	}
	if dto.OldestOpen == nil {
		t.Error("OldestOpen should not be nil")
	}
	if dto.NewestTask != nil {
		t.Error("NewestTask should be nil")
	}
	if dto.CompletionRate != 0.76 {
		t.Errorf("CompletionRate = %f, want 0.76", dto.CompletionRate)
	}
}

func TestStatsToDTO_EmptyMaps(t *testing.T) {
	stats := &models.ExtendedStats{
		ByStatus:   map[models.Status]int{},
		ByType:     map[models.Type]int{},
		ByPriority: map[models.Priority]int{},
	}

	dto := StatsToDTO(stats)
	if dto.ByStatus == nil {
		t.Error("ByStatus should not be nil")
	}
	if len(dto.ByStatus) != 0 {
		t.Errorf("len(ByStatus) = %d, want 0", len(dto.ByStatus))
	}
}

// ============================================================================
// Validation Tests
// ============================================================================

func TestValidateIssueCreate_Valid(t *testing.T) {
	body := &IssueCreateBody{
		Title:    "Fix the authentication timeout bug in login flow",
		Type:     "bug",
		Priority: "P1",
		Points:   5,
	}

	errs := ValidateIssueCreate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d: %+v", len(errs), errs)
	}
}

func TestValidateIssueCreate_MissingTitle(t *testing.T) {
	body := &IssueCreateBody{}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Field != "title" || errs[0].Rule != "required" {
		t.Errorf("error = %+v, want title/required", errs[0])
	}
}

func TestValidateIssueCreate_TitleTooShort(t *testing.T) {
	body := &IssueCreateBody{Title: "ab"}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Rule != "min_length" {
		t.Errorf("rule = %q, want min_length", errs[0].Rule)
	}
}

func TestValidateIssueCreate_TitleTooLong(t *testing.T) {
	body := &IssueCreateBody{Title: string(make([]byte, 201))}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Rule != "max_length" {
		t.Errorf("rule = %q, want max_length", errs[0].Rule)
	}
}

func TestValidateIssueCreate_InvalidType(t *testing.T) {
	body := &IssueCreateBody{
		Title: "Valid title for testing purposes",
		Type:  "invalid",
	}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Field != "type" || errs[0].Rule != "enum" {
		t.Errorf("error = %+v, want type/enum", errs[0])
	}
}

func TestValidateIssueCreate_StoryAliasAccepted(t *testing.T) {
	body := &IssueCreateBody{
		Title: "Valid title for testing purposes",
		Type:  "story", // alias for "feature"
	}
	errs := ValidateIssueCreate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("story should be accepted as alias, got errors: %+v", errs)
	}
}

func TestValidateIssueCreate_InvalidPriority(t *testing.T) {
	body := &IssueCreateBody{
		Title:    "Valid title for testing purposes",
		Priority: "P5",
	}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Field != "priority" {
		t.Errorf("field = %q, want priority", errs[0].Field)
	}
}

func TestValidateIssueCreate_NumericPriorityAccepted(t *testing.T) {
	body := &IssueCreateBody{
		Title:    "Valid title for testing purposes",
		Priority: "2", // alias for P2
	}
	errs := ValidateIssueCreate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("numeric priority should be accepted, got errors: %+v", errs)
	}
}

func TestValidateIssueCreate_InvalidPoints(t *testing.T) {
	body := &IssueCreateBody{
		Title:  "Valid title for testing purposes",
		Points: 7, // not Fibonacci
	}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Field != "points" {
		t.Errorf("field = %q, want points", errs[0].Field)
	}
}

func TestValidateIssueCreate_ValidPoints(t *testing.T) {
	for _, pts := range []int{1, 2, 3, 5, 8, 13, 21} {
		body := &IssueCreateBody{
			Title:  "Valid title for testing purposes",
			Points: pts,
		}
		errs := ValidateIssueCreate(body, 3, 200)
		if len(errs) != 0 {
			t.Errorf("points=%d should be valid, got errors: %+v", pts, errs)
		}
	}
}

func TestValidateIssueCreate_InvalidDate(t *testing.T) {
	body := &IssueCreateBody{
		Title:      "Valid title for testing purposes",
		DeferUntil: "not-a-date",
	}
	errs := ValidateIssueCreate(body, 3, 200)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Field != "defer_until" || errs[0].Rule != "date_format" {
		t.Errorf("error = %+v, want defer_until/date_format", errs[0])
	}
}

func TestValidateIssueCreate_ValidDate(t *testing.T) {
	body := &IssueCreateBody{
		Title:      "Valid title for testing purposes",
		DeferUntil: "2026-03-01",
		DueDate:    "2026-04-15",
	}
	errs := ValidateIssueCreate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %+v", errs)
	}
}

func TestValidateIssueCreate_MultipleErrors(t *testing.T) {
	body := &IssueCreateBody{
		Type:     "invalid",
		Priority: "P9",
		Points:   7,
	}
	errs := ValidateIssueCreate(body, 3, 200)

	// Should get: title required, invalid type, invalid priority, invalid points
	if len(errs) != 4 {
		t.Errorf("expected 4 errors, got %d: %+v", len(errs), errs)
	}
}

func TestValidateIssueUpdate_EmptyBody(t *testing.T) {
	body := &IssueUpdateBody{}
	errs := ValidateIssueUpdate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("empty update body should have no errors, got %+v", errs)
	}
}

func TestValidateIssueUpdate_ValidTitle(t *testing.T) {
	title := "Updated title for an existing issue"
	body := &IssueUpdateBody{Title: &title}
	errs := ValidateIssueUpdate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %+v", errs)
	}
}

func TestValidateIssueUpdate_EmptyTitle(t *testing.T) {
	title := ""
	body := &IssueUpdateBody{Title: &title}
	errs := ValidateIssueUpdate(body, 3, 200)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Rule != "required" {
		t.Errorf("rule = %q, want required", errs[0].Rule)
	}
}

func TestValidateIssueUpdate_InvalidFields(t *testing.T) {
	badType := "invalid"
	badPriority := "P9"
	badPoints := 7
	body := &IssueUpdateBody{
		Type:     &badType,
		Priority: &badPriority,
		Points:   &badPoints,
	}
	errs := ValidateIssueUpdate(body, 3, 200)
	if len(errs) != 3 {
		t.Errorf("expected 3 errors, got %d: %+v", len(errs), errs)
	}
}

func TestValidateIssueUpdate_ClearPoints(t *testing.T) {
	// Setting points to 0 should be allowed (clear points)
	zero := 0
	body := &IssueUpdateBody{Points: &zero}
	errs := ValidateIssueUpdate(body, 3, 200)
	if len(errs) != 0 {
		t.Errorf("points=0 should be allowed (clear), got %+v", errs)
	}
}

func TestValidatePagination_Valid(t *testing.T) {
	errs := ValidatePagination(50, 0)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %+v", errs)
	}
}

func TestValidatePagination_Boundaries(t *testing.T) {
	// Valid boundaries
	if errs := ValidatePagination(1, 0); len(errs) != 0 {
		t.Errorf("limit=1 should be valid")
	}
	if errs := ValidatePagination(1000, 0); len(errs) != 0 {
		t.Errorf("limit=1000 should be valid")
	}

	// Invalid
	if errs := ValidatePagination(0, 0); len(errs) != 1 {
		t.Errorf("limit=0 should be invalid, got %d errors", len(errs))
	}
	if errs := ValidatePagination(1001, 0); len(errs) != 1 {
		t.Errorf("limit=1001 should be invalid, got %d errors", len(errs))
	}
	if errs := ValidatePagination(50, -1); len(errs) != 1 {
		t.Errorf("offset=-1 should be invalid, got %d errors", len(errs))
	}
}

func TestValidatePagination_BothInvalid(t *testing.T) {
	errs := ValidatePagination(0, -1)
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d: %+v", len(errs), errs)
	}
}

// ============================================================================
// Helper Tests
// ============================================================================

func TestNullableString(t *testing.T) {
	if result := nullableString(""); result != nil {
		t.Errorf("empty string should return nil, got %v", result)
	}
	if result := nullableString("hello"); result == nil || *result != "hello" {
		t.Errorf("non-empty string should return pointer")
	}
}

func TestNullableTime(t *testing.T) {
	if result := nullableTime(nil); result != nil {
		t.Errorf("nil time should return nil, got %v", result)
	}

	now := time.Now()
	result := nullableTime(&now)
	if result == nil {
		t.Fatal("non-nil time should return pointer")
	}
	expected := now.Format(time.RFC3339)
	if *result != expected {
		t.Errorf("result = %q, want %q", *result, expected)
	}
}

func TestValidateDateField(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"2026-01-15", true},
		{"2026-12-31", true},
		{"2025-02-28", true},
		{"not-a-date", false},
		{"01-15-2026", false},
		{"2026/01/15", false},
		{"2026-13-01", false},
		{"2026-01-32", false},
	}

	for _, tt := range tests {
		result := validateDateField("test_field", tt.value)
		if tt.valid && result != nil {
			t.Errorf("date %q should be valid, got error: %s", tt.value, result.Message)
		}
		if !tt.valid && result == nil {
			t.Errorf("date %q should be invalid", tt.value)
		}
	}
}
