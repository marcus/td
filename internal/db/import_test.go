package db

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestUpsertIssueRaw(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	deferUntil := "2026-04-01"
	dueDate := "2026-05-01"
	closedAt := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)

	issue := &models.Issue{
		ID:                 "td-aaaaaa",
		Title:              "Test issue",
		Description:        "Full description",
		Status:             models.StatusInReview,
		Type:               models.TypeFeature,
		Priority:           models.PriorityP1,
		Points:             5,
		Labels:             []string{"backend", "urgent"},
		ParentID:           "td-parent1",
		Acceptance:         "All tests pass",
		Sprint:             "sprint-3",
		ImplementerSession: "sess-impl",
		CreatorSession:     "sess-creator",
		ReviewerSession:    "sess-reviewer",
		CreatedAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		ClosedAt:           &closedAt,
		Minor:              true,
		CreatedBranch:      "feature/test",
		DeferUntil:         &deferUntil,
		DueDate:            &dueDate,
		DeferCount:         2,
	}

	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw failed: %v", err)
	}

	got, err := database.GetIssue("td-aaaaaa")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if got.ID != "td-aaaaaa" {
		t.Errorf("ID: got %s, want td-aaaaaa", got.ID)
	}
	if got.Title != "Test issue" {
		t.Errorf("Title: got %s, want Test issue", got.Title)
	}
	if got.Description != "Full description" {
		t.Errorf("Description: got %s, want Full description", got.Description)
	}
	if got.Status != models.StatusInReview {
		t.Errorf("Status: got %s, want %s", got.Status, models.StatusInReview)
	}
	if got.Type != models.TypeFeature {
		t.Errorf("Type: got %s, want %s", got.Type, models.TypeFeature)
	}
	if got.Priority != models.PriorityP1 {
		t.Errorf("Priority: got %s, want %s", got.Priority, models.PriorityP1)
	}
	if got.Points != 5 {
		t.Errorf("Points: got %d, want 5", got.Points)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "backend" || got.Labels[1] != "urgent" {
		t.Errorf("Labels: got %v, want [backend urgent]", got.Labels)
	}
	if got.ParentID != "td-parent1" {
		t.Errorf("ParentID: got %s, want td-parent1", got.ParentID)
	}
	if got.Acceptance != "All tests pass" {
		t.Errorf("Acceptance: got %s, want All tests pass", got.Acceptance)
	}
	if got.Sprint != "sprint-3" {
		t.Errorf("Sprint: got %s, want sprint-3", got.Sprint)
	}
	if got.ImplementerSession != "sess-impl" {
		t.Errorf("ImplementerSession: got %s, want sess-impl", got.ImplementerSession)
	}
	if got.CreatorSession != "sess-creator" {
		t.Errorf("CreatorSession: got %s, want sess-creator", got.CreatorSession)
	}
	if got.ReviewerSession != "sess-reviewer" {
		t.Errorf("ReviewerSession: got %s, want sess-reviewer", got.ReviewerSession)
	}
	if !got.CreatedAt.Equal(issue.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, issue.CreatedAt)
	}
	if !got.UpdatedAt.Equal(issue.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, issue.UpdatedAt)
	}
	if got.ClosedAt == nil || !got.ClosedAt.Equal(closedAt) {
		t.Errorf("ClosedAt: got %v, want %v", got.ClosedAt, closedAt)
	}
	if !got.Minor {
		t.Error("Minor: got false, want true")
	}
	if got.CreatedBranch != "feature/test" {
		t.Errorf("CreatedBranch: got %s, want feature/test", got.CreatedBranch)
	}
	if got.DeferUntil == nil || *got.DeferUntil != "2026-04-01" {
		t.Errorf("DeferUntil: got %v, want 2026-04-01", got.DeferUntil)
	}
	if got.DueDate == nil || *got.DueDate != "2026-05-01" {
		t.Errorf("DueDate: got %v, want 2026-05-01", got.DueDate)
	}
	if got.DeferCount != 2 {
		t.Errorf("DeferCount: got %d, want 2", got.DeferCount)
	}

	// Verify no action_log entry was created
	var count int
	err = database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if err != nil {
		t.Fatalf("Query action_log failed: %v", err)
	}
	if count != 0 {
		t.Errorf("action_log should be empty, got %d entries", count)
	}
}

func TestUpsertIssueRaw_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	original := &models.Issue{
		ID:     "td-bbbbbb",
		Title:  "Original",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	if err := database.UpsertIssueRaw(original); err != nil {
		t.Fatalf("First upsert failed: %v", err)
	}

	updated := &models.Issue{
		ID:     "td-bbbbbb",
		Title:  "Updated",
		Status: models.StatusClosed,
		Type:   models.TypeBug,
	}
	if err := database.UpsertIssueRaw(updated); err != nil {
		t.Fatalf("Second upsert failed: %v", err)
	}

	got, err := database.GetIssue("td-bbbbbb")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Title != "Updated" {
		t.Errorf("Title: got %s, want Updated", got.Title)
	}
	if got.Status != models.StatusClosed {
		t.Errorf("Status: got %s, want %s", got.Status, models.StatusClosed)
	}
}

func TestInsertLogRaw(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue first (logs reference issues)
	issue := &models.Issue{ID: "td-cccccc", Title: "Log test", Status: models.StatusOpen}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw failed: %v", err)
	}

	ts := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	log := &models.Log{
		ID:            "log-001",
		IssueID:       "td-cccccc",
		SessionID:     "sess-1",
		WorkSessionID: "ws-1",
		Message:       "Made progress",
		Type:          models.LogTypeProgress,
		Timestamp:     ts,
	}

	if err := database.InsertLogRaw(log); err != nil {
		t.Fatalf("InsertLogRaw failed: %v", err)
	}

	// Read back
	logs, err := database.GetLogs("td-cccccc", 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].ID != "log-001" {
		t.Errorf("Log ID: got %s, want log-001", logs[0].ID)
	}
	if logs[0].Message != "Made progress" {
		t.Errorf("Log Message: got %s, want Made progress", logs[0].Message)
	}
	if logs[0].WorkSessionID != "ws-1" {
		t.Errorf("Log WorkSessionID: got %s, want ws-1", logs[0].WorkSessionID)
	}

	// Duplicate insert should be silently ignored
	if err := database.InsertLogRaw(log); err != nil {
		t.Fatalf("Duplicate InsertLogRaw should not error: %v", err)
	}
	logs, _ = database.GetLogs("td-cccccc", 0)
	if len(logs) != 1 {
		t.Errorf("Expected 1 log after duplicate insert, got %d", len(logs))
	}

	// Verify no action_log entry
	var count int
	database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if count != 0 {
		t.Errorf("action_log should be empty, got %d entries", count)
	}
}

func TestInsertHandoffRaw(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{ID: "td-dddddd", Title: "Handoff test", Status: models.StatusOpen}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw failed: %v", err)
	}

	ts := time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC)
	handoff := &models.Handoff{
		ID:        "handoff-001",
		IssueID:   "td-dddddd",
		SessionID: "sess-2",
		Done:      []string{"implemented API", "wrote tests"},
		Remaining: []string{"deploy"},
		Decisions: []string{"use REST not gRPC"},
		Uncertain: []string{"caching strategy"},
		Timestamp: ts,
	}

	if err := database.InsertHandoffRaw(handoff); err != nil {
		t.Fatalf("InsertHandoffRaw failed: %v", err)
	}

	got, err := database.GetLatestHandoff("td-dddddd")
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if got == nil {
		t.Fatal("Expected handoff, got nil")
	}
	if got.ID != "handoff-001" {
		t.Errorf("Handoff ID: got %s, want handoff-001", got.ID)
	}
	if len(got.Done) != 2 || got.Done[0] != "implemented API" {
		t.Errorf("Handoff Done: got %v", got.Done)
	}
	if len(got.Remaining) != 1 || got.Remaining[0] != "deploy" {
		t.Errorf("Handoff Remaining: got %v", got.Remaining)
	}
	if len(got.Decisions) != 1 || got.Decisions[0] != "use REST not gRPC" {
		t.Errorf("Handoff Decisions: got %v", got.Decisions)
	}
	if len(got.Uncertain) != 1 || got.Uncertain[0] != "caching strategy" {
		t.Errorf("Handoff Uncertain: got %v", got.Uncertain)
	}

	// Duplicate insert should be silently ignored
	if err := database.InsertHandoffRaw(handoff); err != nil {
		t.Fatalf("Duplicate InsertHandoffRaw should not error: %v", err)
	}

	// Verify no action_log entry
	var count int
	database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if count != 0 {
		t.Errorf("action_log should be empty, got %d entries", count)
	}
}

func TestInsertIssueFileRaw(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{ID: "td-eeeeee", Title: "File test", Status: models.StatusOpen}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw failed: %v", err)
	}

	linkedAt := time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC)
	file := &models.IssueFile{
		ID:        "file-001",
		IssueID:   "td-eeeeee",
		FilePath:  "cmd/system.go",
		Role:      models.FileRoleImplementation,
		LinkedSHA: "abc123",
		LinkedAt:  linkedAt,
	}

	if err := database.InsertIssueFileRaw(file); err != nil {
		t.Fatalf("InsertIssueFileRaw failed: %v", err)
	}

	files, err := database.GetLinkedFiles("td-eeeeee")
	if err != nil {
		t.Fatalf("GetLinkedFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(files))
	}
	if files[0].ID != "file-001" {
		t.Errorf("File ID: got %s, want file-001", files[0].ID)
	}
	if files[0].FilePath != "cmd/system.go" {
		t.Errorf("FilePath: got %s, want cmd/system.go", files[0].FilePath)
	}
	if files[0].Role != models.FileRoleImplementation {
		t.Errorf("Role: got %s, want %s", files[0].Role, models.FileRoleImplementation)
	}
	if files[0].LinkedSHA != "abc123" {
		t.Errorf("LinkedSHA: got %s, want abc123", files[0].LinkedSHA)
	}

	// Duplicate insert should be silently ignored
	if err := database.InsertIssueFileRaw(file); err != nil {
		t.Fatalf("Duplicate InsertIssueFileRaw should not error: %v", err)
	}
	files, _ = database.GetLinkedFiles("td-eeeeee")
	if len(files) != 1 {
		t.Errorf("Expected 1 file after duplicate insert, got %d", len(files))
	}

	// Verify no action_log entry
	var count int
	database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if count != 0 {
		t.Errorf("action_log should be empty, got %d entries", count)
	}
}

func TestGetHandoffs(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{ID: "td-ffffff", Title: "Handoffs test", Status: models.StatusOpen}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw failed: %v", err)
	}

	h1 := &models.Handoff{
		ID:        "ho-1",
		IssueID:   "td-ffffff",
		SessionID: "sess-1",
		Done:      []string{"first"},
		Timestamp: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	}
	h2 := &models.Handoff{
		ID:        "ho-2",
		IssueID:   "td-ffffff",
		SessionID: "sess-2",
		Done:      []string{"second"},
		Timestamp: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
	}
	if err := database.InsertHandoffRaw(h1); err != nil {
		t.Fatalf("InsertHandoffRaw h1: %v", err)
	}
	if err := database.InsertHandoffRaw(h2); err != nil {
		t.Fatalf("InsertHandoffRaw h2: %v", err)
	}

	handoffs, err := database.GetHandoffs("td-ffffff")
	if err != nil {
		t.Fatalf("GetHandoffs failed: %v", err)
	}
	if len(handoffs) != 2 {
		t.Fatalf("Expected 2 handoffs, got %d", len(handoffs))
	}
	// Ordered by timestamp DESC
	if handoffs[0].ID != "ho-2" {
		t.Errorf("First handoff should be latest: got %s, want ho-2", handoffs[0].ID)
	}
	if handoffs[1].ID != "ho-1" {
		t.Errorf("Second handoff should be earliest: got %s, want ho-1", handoffs[1].ID)
	}

	// No handoffs for non-existent issue
	empty, err := database.GetHandoffs("td-nonexistent")
	if err != nil {
		t.Fatalf("GetHandoffs non-existent: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 handoffs for non-existent issue, got %d", len(empty))
	}
}

func TestReplaceIssueRaw(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue with associated data
	issue := &models.Issue{
		ID:     "td-replace",
		Title:  "Original",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}
	if err := database.InsertLogRaw(&models.Log{
		ID: "log-old", IssueID: "td-replace", SessionID: "s1",
		Message: "old", Type: models.LogTypeProgress,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertLogRaw: %v", err)
	}
	if err := database.InsertHandoffRaw(&models.Handoff{
		ID: "ho-old", IssueID: "td-replace", SessionID: "s1",
		Done:      []string{"old work"},
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertHandoffRaw: %v", err)
	}
	if err := database.AddDependency("td-replace", "td-dep1", "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	if err := database.InsertIssueFileRaw(&models.IssueFile{
		ID: "file-old", IssueID: "td-replace", FilePath: "old.go",
		Role: models.FileRoleImplementation, LinkedSHA: "aaa",
		LinkedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("InsertIssueFileRaw: %v", err)
	}

	// Replace the issue, including DeferUntil and DueDate
	deferUntil := "2026-06-01"
	dueDate := "2026-07-01"
	replacement := &models.Issue{
		ID:         "td-replace",
		Title:      "Replaced",
		Status:     models.StatusInReview,
		Type:       models.TypeFeature,
		DeferUntil: &deferUntil,
		DueDate:    &dueDate,
	}
	if err := database.ReplaceIssueRaw(replacement); err != nil {
		t.Fatalf("ReplaceIssueRaw: %v", err)
	}

	// Issue should be updated
	got, err := database.GetIssue("td-replace")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Title != "Replaced" {
		t.Errorf("Title: got %s, want Replaced", got.Title)
	}
	if got.Status != models.StatusInReview {
		t.Errorf("Status: got %s, want %s", got.Status, models.StatusInReview)
	}
	if got.DeferUntil == nil || *got.DeferUntil != "2026-06-01" {
		t.Errorf("DeferUntil: got %v, want 2026-06-01", got.DeferUntil)
	}
	if got.DueDate == nil || *got.DueDate != "2026-07-01" {
		t.Errorf("DueDate: got %v, want 2026-07-01", got.DueDate)
	}

	// All old associated data should be gone
	logs, _ := database.GetLogs("td-replace", 0)
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs after replace, got %d", len(logs))
	}
	handoffs, _ := database.GetHandoffs("td-replace")
	if len(handoffs) != 0 {
		t.Errorf("Expected 0 handoffs after replace, got %d", len(handoffs))
	}
	deps, _ := database.GetDependencies("td-replace")
	if len(deps) != 0 {
		t.Errorf("Expected 0 dependencies after replace, got %d", len(deps))
	}
	files, _ := database.GetLinkedFiles("td-replace")
	if len(files) != 0 {
		t.Errorf("Expected 0 files after replace, got %d", len(files))
	}

	// Verify no action_log entry
	var actionCount int
	database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&actionCount)
	if actionCount != 0 {
		t.Errorf("action_log should be empty, got %d entries", actionCount)
	}
}

func TestUpsertIssueRaw_NormalizesID(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// ID without prefix should be normalized
	issue := &models.Issue{
		ID:     "aaaaaa",
		Title:  "No prefix",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}
	if issue.ID != "td-aaaaaa" {
		t.Errorf("ID not normalized: got %s, want td-aaaaaa", issue.ID)
	}
	got, err := database.GetIssue("td-aaaaaa")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Title != "No prefix" {
		t.Errorf("Title: got %s", got.Title)
	}
}

func TestReplaceIssueRaw_NormalizesID(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create with normalized ID first
	issue := &models.Issue{ID: "td-bbbbbb", Title: "Original", Status: models.StatusOpen}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}

	// Replace using unnormalized ID
	replacement := &models.Issue{ID: "bbbbbb", Title: "Replaced", Status: models.StatusClosed}
	if err := database.ReplaceIssueRaw(replacement); err != nil {
		t.Fatalf("ReplaceIssueRaw: %v", err)
	}
	if replacement.ID != "td-bbbbbb" {
		t.Errorf("ID not normalized: got %s", replacement.ID)
	}
	got, _ := database.GetIssue("td-bbbbbb")
	if got.Title != "Replaced" {
		t.Errorf("Title: got %s, want Replaced", got.Title)
	}
}

func TestImportItemRaw_Atomic(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		ID:     "td-atomic1",
		Title:  "Atomic import",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	logs := []models.Log{{
		ID: "log-a1", IssueID: "td-atomic1", SessionID: "s1",
		Message: "progress", Type: models.LogTypeProgress,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	handoffs := []models.Handoff{{
		ID: "ho-a1", IssueID: "td-atomic1", SessionID: "s1",
		Done:      []string{"work"},
		Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}}
	deps := []models.IssueDependency{{
		DependsOnID: "td-dep1", RelationType: "depends_on",
	}}
	files := []models.IssueFile{{
		ID: "file-a1", IssueID: "td-atomic1", FilePath: "main.go",
		Role: models.FileRoleImplementation, LinkedSHA: "abc",
		LinkedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}

	if err := database.ImportItemRaw(issue, logs, handoffs, deps, files, false); err != nil {
		t.Fatalf("ImportItemRaw: %v", err)
	}

	// Verify all data was written
	got, _ := database.GetIssue("td-atomic1")
	if got == nil || got.Title != "Atomic import" {
		t.Fatalf("Issue not imported: %v", got)
	}
	gotLogs, _ := database.GetLogs("td-atomic1", 0)
	if len(gotLogs) != 1 {
		t.Errorf("Logs: got %d, want 1", len(gotLogs))
	}
	gotHandoffs, _ := database.GetHandoffs("td-atomic1")
	if len(gotHandoffs) != 1 {
		t.Errorf("Handoffs: got %d, want 1", len(gotHandoffs))
	}
	gotDeps, _ := database.GetDependencies("td-atomic1")
	if len(gotDeps) != 1 || gotDeps[0] != "td-dep1" {
		t.Errorf("Dependencies: got %v, want [td-dep1]", gotDeps)
	}
	gotFiles, _ := database.GetLinkedFiles("td-atomic1")
	if len(gotFiles) != 1 {
		t.Errorf("Files: got %d, want 1", len(gotFiles))
	}

	// Verify no action_log entry
	var count int
	database.conn.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if count != 0 {
		t.Errorf("action_log should be empty, got %d", count)
	}
}

func TestImportItemRaw_ReplaceIsAtomic(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create initial issue with data
	issue := &models.Issue{ID: "td-replace2", Title: "Original", Status: models.StatusOpen, Type: models.TypeTask}
	oldLogs := []models.Log{{
		ID: "log-old", IssueID: "td-replace2", SessionID: "s1",
		Message: "old log", Type: models.LogTypeProgress,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	if err := database.ImportItemRaw(issue, oldLogs, nil, nil, nil, false); err != nil {
		t.Fatalf("Initial import: %v", err)
	}

	// Replace with new data
	newIssue := &models.Issue{ID: "td-replace2", Title: "Replaced", Status: models.StatusClosed, Type: models.TypeBug}
	newLogs := []models.Log{{
		ID: "log-new", IssueID: "td-replace2", SessionID: "s2",
		Message: "new log", Type: models.LogTypeProgress,
		Timestamp: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}}
	newDeps := []models.IssueDependency{{
		DependsOnID: "td-other", RelationType: "blocks",
	}}
	if err := database.ImportItemRaw(newIssue, newLogs, nil, newDeps, nil, true); err != nil {
		t.Fatalf("Replace import: %v", err)
	}

	// Old data gone, new data present
	got, _ := database.GetIssue("td-replace2")
	if got.Title != "Replaced" {
		t.Errorf("Title: got %s, want Replaced", got.Title)
	}
	logs, _ := database.GetLogs("td-replace2", 0)
	if len(logs) != 1 || logs[0].ID != "log-new" {
		t.Errorf("Logs: got %v, want [log-new]", logs)
	}
	// "blocks" relation should be stored
	var relType string
	database.conn.QueryRow(`SELECT relation_type FROM issue_dependencies WHERE issue_id = ?`, "td-replace2").Scan(&relType)
	if relType != "blocks" {
		t.Errorf("RelationType: got %s, want blocks", relType)
	}
}

func TestImportItemRaw_NormalizesID(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{ID: "cccccc", Title: "Normalize test", Status: models.StatusOpen, Type: models.TypeTask}
	if err := database.ImportItemRaw(issue, nil, nil, nil, nil, false); err != nil {
		t.Fatalf("ImportItemRaw: %v", err)
	}
	if issue.ID != "td-cccccc" {
		t.Errorf("ID not normalized: got %s", issue.ID)
	}
	got, _ := database.GetIssue("td-cccccc")
	if got == nil || got.Title != "Normalize test" {
		t.Fatalf("Issue not found after normalize")
	}
}
