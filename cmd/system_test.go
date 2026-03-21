package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestImportJSON_RoundTrip(t *testing.T) {
	// Set up source DB with fully populated issues
	srcDir := t.TempDir()
	srcDB, err := db.Initialize(srcDir)
	if err != nil {
		t.Fatalf("Initialize source DB: %v", err)
	}
	defer srcDB.Close()

	closedAt := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	deferUntil := "2026-04-01"
	dueDate := "2026-05-01"

	// Issue 1: in_review with all fields populated
	issue1 := &models.Issue{
		ID:                 "td-111111",
		Title:              "Feature A",
		Description:        "Full description",
		Status:             models.StatusInReview,
		Type:               models.TypeFeature,
		Priority:           models.PriorityP1,
		Points:             5,
		Labels:             []string{"backend", "urgent"},
		ParentID:           "td-222222",
		Acceptance:         "All tests pass",
		Sprint:             "sprint-3",
		ImplementerSession: "sess-impl",
		CreatorSession:     "sess-creator",
		ReviewerSession:    "sess-reviewer",
		CreatedAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		ClosedAt:           &closedAt,
		Minor:              true,
		CreatedBranch:      "feature/a",
		DeferUntil:         &deferUntil,
		DueDate:            &dueDate,
		DeferCount:         2,
	}

	// Issue 2: closed
	issue2 := &models.Issue{
		ID:        "td-222222",
		Title:     "Bug B",
		Status:    models.StatusClosed,
		Type:      models.TypeBug,
		Priority:  models.PriorityP0,
		CreatedAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
	}

	// Insert both issues
	if err := srcDB.UpsertIssueRaw(issue1); err != nil {
		t.Fatalf("UpsertIssueRaw issue1: %v", err)
	}
	if err := srcDB.UpsertIssueRaw(issue2); err != nil {
		t.Fatalf("UpsertIssueRaw issue2: %v", err)
	}

	// Add a log to issue1
	log1 := &models.Log{
		ID:            "log-001",
		IssueID:       "td-111111",
		SessionID:     "sess-impl",
		WorkSessionID: "ws-1",
		Message:       "Started implementation",
		Type:          models.LogTypeProgress,
		Timestamp:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := srcDB.InsertLogRaw(log1); err != nil {
		t.Fatalf("InsertLogRaw: %v", err)
	}

	// Add a handoff to issue1
	handoff := &models.Handoff{
		ID:        "handoff-001",
		IssueID:   "td-111111",
		SessionID: "sess-impl",
		Done:      []string{"implemented API"},
		Remaining: []string{"write tests"},
		Decisions: []string{"use REST"},
		Uncertain: []string{"caching"},
		Timestamp: time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC),
	}
	if err := srcDB.InsertHandoffRaw(handoff); err != nil {
		t.Fatalf("InsertHandoffRaw: %v", err)
	}

	// Add a dependency: issue1 depends on issue2
	if err := srcDB.AddDependency("td-111111", "td-222222", "depends_on"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Add a linked file to issue1
	file := &models.IssueFile{
		ID:        "file-001",
		IssueID:   "td-111111",
		FilePath:  "cmd/system.go",
		Role:      models.FileRoleImplementation,
		LinkedSHA: "abc123",
		LinkedAt:  time.Date(2026, 1, 20, 14, 0, 0, 0, time.UTC),
	}
	if err := srcDB.InsertIssueFileRaw(file); err != nil {
		t.Fatalf("InsertIssueFileRaw: %v", err)
	}

	// Build export JSON (mirrors what the export command produces)
	exportData := buildExportJSON(t, srcDB, []string{"td-111111", "td-222222"})

	// Import into a fresh DB
	dstDir := t.TempDir()
	dstDB, err := db.Initialize(dstDir)
	if err != nil {
		t.Fatalf("Initialize dest DB: %v", err)
	}
	defer dstDB.Close()

	imported, err := importJSON(dstDB, exportData, false, false)
	if err != nil {
		t.Fatalf("importJSON failed: %v", err)
	}
	if imported != 2 {
		t.Errorf("imported count: got %d, want 2", imported)
	}

	// Verify issue1 — all 21 fields
	got1, err := dstDB.GetIssue("td-111111")
	if err != nil {
		t.Fatalf("GetIssue td-111111: %v", err)
	}
	assertIssueEqual(t, got1, issue1)

	// Verify issue2
	got2, err := dstDB.GetIssue("td-222222")
	if err != nil {
		t.Fatalf("GetIssue td-222222: %v", err)
	}
	if got2.Status != models.StatusClosed {
		t.Errorf("Issue2 Status: got %s, want %s", got2.Status, models.StatusClosed)
	}

	// Verify log was imported
	logs, err := dstDB.GetLogs("td-111111", 0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].ID != "log-001" {
		t.Errorf("Log ID: got %s, want log-001", logs[0].ID)
	}
	if logs[0].Message != "Started implementation" {
		t.Errorf("Log Message: got %s", logs[0].Message)
	}

	// Verify handoff was imported
	gotHandoff, err := dstDB.GetLatestHandoff("td-111111")
	if err != nil {
		t.Fatalf("GetLatestHandoff: %v", err)
	}
	if gotHandoff == nil {
		t.Fatal("Expected handoff, got nil")
	}
	if gotHandoff.ID != "handoff-001" {
		t.Errorf("Handoff ID: got %s, want handoff-001", gotHandoff.ID)
	}
	if len(gotHandoff.Done) != 1 || gotHandoff.Done[0] != "implemented API" {
		t.Errorf("Handoff Done: got %v", gotHandoff.Done)
	}

	// Verify dependency was imported
	deps, err := dstDB.GetDependencies("td-111111")
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 1 || deps[0] != "td-222222" {
		t.Errorf("Dependencies: got %v, want [td-222222]", deps)
	}

	// Verify linked file was imported
	files, err := dstDB.GetLinkedFiles("td-111111")
	if err != nil {
		t.Fatalf("GetLinkedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(files))
	}
	if files[0].FilePath != "cmd/system.go" {
		t.Errorf("FilePath: got %s, want cmd/system.go", files[0].FilePath)
	}
}

func TestImportJSON_ForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Create an existing issue with open status
	existing := &models.Issue{
		ID:        "td-aaaaaa",
		Title:     "Original",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		Priority:  models.PriorityP2,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := database.UpsertIssueRaw(existing); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}

	// Import data with the same ID but in_review status
	items := []exportedItem{{
		Issue: models.Issue{
			ID:        "td-aaaaaa",
			Title:     "Updated",
			Status:    models.StatusInReview,
			Type:      models.TypeFeature,
			Priority:  models.PriorityP1,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	data, _ := json.Marshal(items)

	// Without force: should skip
	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON without force: %v", err)
	}
	if imported != 0 {
		t.Errorf("Expected 0 imported without force, got %d", imported)
	}
	got, _ := database.GetIssue("td-aaaaaa")
	if got.Title != "Original" {
		t.Errorf("Should not have overwritten: got title %s", got.Title)
	}

	// With force: should overwrite
	imported, err = importJSON(database, data, false, true)
	if err != nil {
		t.Fatalf("importJSON with force: %v", err)
	}
	if imported != 1 {
		t.Errorf("Expected 1 imported with force, got %d", imported)
	}
	got, _ = database.GetIssue("td-aaaaaa")
	if got.Title != "Updated" {
		t.Errorf("Title: got %s, want Updated", got.Title)
	}
	if got.Status != models.StatusInReview {
		t.Errorf("Status: got %s, want %s", got.Status, models.StatusInReview)
	}
}

func TestImportJSON_DryRun(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	items := []exportedItem{{
		Issue: models.Issue{
			ID:     "td-dryrun",
			Title:  "Dry run test",
			Status: models.StatusOpen,
			Type:   models.TypeTask,
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, true, false)
	if err != nil {
		t.Fatalf("importJSON dry-run: %v", err)
	}
	if imported != 1 {
		t.Errorf("Expected 1 counted in dry-run, got %d", imported)
	}

	// Verify nothing was actually written
	got, err := database.GetIssue("td-dryrun")
	if err == nil && got != nil {
		t.Error("Dry-run should not create issues")
	}
}

func TestImportJSON_SkipsEmptyID(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	items := []exportedItem{{
		Issue: models.Issue{
			Title:  "No ID issue",
			Status: models.StatusOpen,
			Type:   models.TypeTask,
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 0 {
		t.Errorf("Expected 0 imported for empty ID, got %d", imported)
	}
}

func TestImportJSON_SkipsEmptyTitle(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	items := []exportedItem{{
		Issue: models.Issue{
			ID:     "td-notitle",
			Status: models.StatusOpen,
			Type:   models.TypeTask,
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 0 {
		t.Errorf("Expected 0 imported for empty title, got %d", imported)
	}
}

func TestImportJSON_DryRunExisting(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Create existing issue
	existing := &models.Issue{
		ID:     "td-dryexist",
		Title:  "Existing",
		Status: models.StatusOpen,
		Type:   models.TypeTask,
	}
	if err := database.UpsertIssueRaw(existing); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}

	// Dry-run import with force — should report "Would overwrite"
	items := []exportedItem{{
		Issue: models.Issue{
			ID:     "td-dryexist",
			Title:  "Updated",
			Status: models.StatusInReview,
			Type:   models.TypeTask,
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, true, true)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("Expected 1 counted in dry-run, got %d", imported)
	}

	// Verify nothing was changed
	got, _ := database.GetIssue("td-dryexist")
	if got.Title != "Existing" {
		t.Errorf("Dry-run should not modify: got title %s", got.Title)
	}
}

func TestImportJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Completely invalid JSON
	_, err = importJSON(database, []byte("not json"), false, false)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}

	// Valid array but malformed item — triggers UnmarshalJSON error path
	_, err = importJSON(database, []byte(`[{"issue": "not an object"}]`), false, false)
	if err == nil {
		t.Fatal("Expected error for malformed item")
	}
}

// buildExportJSON mirrors the export command's JSON output structure
func buildExportJSON(t *testing.T, database *db.DB, issueIDs []string) []byte {
	t.Helper()
	var exportData []map[string]interface{}
	for _, id := range issueIDs {
		issue, err := database.GetIssue(id)
		if err != nil {
			t.Fatalf("GetIssue %s: %v", id, err)
		}
		logs, _ := database.GetLogs(id, 0)
		handoffs, _ := database.GetHandoffs(id)
		deps, _ := database.GetIssueDependencyRelations(id)
		files, _ := database.GetLinkedFiles(id)

		item := map[string]interface{}{
			"issue":        issue,
			"logs":         logs,
			"handoffs":     handoffs,
			"dependencies": deps,
			"files":        files,
		}
		exportData = append(exportData, item)
	}
	data, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		t.Fatalf("Marshal export: %v", err)
	}
	return data
}

func assertIssueEqual(t *testing.T, got, want *models.Issue) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID: got %s, want %s", got.ID, want.ID)
	}
	if got.Title != want.Title {
		t.Errorf("Title: got %s, want %s", got.Title, want.Title)
	}
	if got.Description != want.Description {
		t.Errorf("Description: got %s, want %s", got.Description, want.Description)
	}
	if got.Status != want.Status {
		t.Errorf("Status: got %s, want %s", got.Status, want.Status)
	}
	if got.Type != want.Type {
		t.Errorf("Type: got %s, want %s", got.Type, want.Type)
	}
	if got.Priority != want.Priority {
		t.Errorf("Priority: got %s, want %s", got.Priority, want.Priority)
	}
	if got.Points != want.Points {
		t.Errorf("Points: got %d, want %d", got.Points, want.Points)
	}
	if len(got.Labels) != len(want.Labels) {
		t.Errorf("Labels length: got %d, want %d", len(got.Labels), len(want.Labels))
	}
	if got.ParentID != want.ParentID {
		t.Errorf("ParentID: got %s, want %s", got.ParentID, want.ParentID)
	}
	if got.Acceptance != want.Acceptance {
		t.Errorf("Acceptance: got %s, want %s", got.Acceptance, want.Acceptance)
	}
	if got.Sprint != want.Sprint {
		t.Errorf("Sprint: got %s, want %s", got.Sprint, want.Sprint)
	}
	if got.ImplementerSession != want.ImplementerSession {
		t.Errorf("ImplementerSession: got %s, want %s", got.ImplementerSession, want.ImplementerSession)
	}
	if got.CreatorSession != want.CreatorSession {
		t.Errorf("CreatorSession: got %s, want %s", got.CreatorSession, want.CreatorSession)
	}
	if got.ReviewerSession != want.ReviewerSession {
		t.Errorf("ReviewerSession: got %s, want %s", got.ReviewerSession, want.ReviewerSession)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
	if (got.ClosedAt == nil) != (want.ClosedAt == nil) {
		t.Errorf("ClosedAt nil mismatch: got %v, want %v", got.ClosedAt, want.ClosedAt)
	} else if got.ClosedAt != nil && !got.ClosedAt.Equal(*want.ClosedAt) {
		t.Errorf("ClosedAt: got %v, want %v", got.ClosedAt, want.ClosedAt)
	}
	if got.Minor != want.Minor {
		t.Errorf("Minor: got %v, want %v", got.Minor, want.Minor)
	}
	if got.CreatedBranch != want.CreatedBranch {
		t.Errorf("CreatedBranch: got %s, want %s", got.CreatedBranch, want.CreatedBranch)
	}
	if (got.DeferUntil == nil) != (want.DeferUntil == nil) {
		t.Errorf("DeferUntil nil mismatch: got %v, want %v", got.DeferUntil, want.DeferUntil)
	} else if got.DeferUntil != nil && *got.DeferUntil != *want.DeferUntil {
		t.Errorf("DeferUntil: got %s, want %s", *got.DeferUntil, *want.DeferUntil)
	}
	if (got.DueDate == nil) != (want.DueDate == nil) {
		t.Errorf("DueDate nil mismatch: got %v, want %v", got.DueDate, want.DueDate)
	} else if got.DueDate != nil && *got.DueDate != *want.DueDate {
		t.Errorf("DueDate: got %s, want %s", *got.DueDate, *want.DueDate)
	}
	if got.DeferCount != want.DeferCount {
		t.Errorf("DeferCount: got %d, want %d", got.DeferCount, want.DeferCount)
	}
	if (got.DeletedAt == nil) != (want.DeletedAt == nil) {
		t.Errorf("DeletedAt nil mismatch: got %v, want %v", got.DeletedAt, want.DeletedAt)
	} else if got.DeletedAt != nil && !got.DeletedAt.Equal(*want.DeletedAt) {
		t.Errorf("DeletedAt: got %v, want %v", got.DeletedAt, want.DeletedAt)
	}
}

func TestImportJSON_DeletedAtRoundTrip(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	deletedAt := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	items := []exportedItem{{
		Issue: models.Issue{
			ID:        "td-deleted",
			Title:     "Soft deleted issue",
			Status:    models.StatusClosed,
			Type:      models.TypeTask,
			Priority:  models.PriorityP2,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
			DeletedAt: &deletedAt,
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	got, err := database.GetIssue("td-deleted")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("DeletedAt should not be nil")
	}
	if !got.DeletedAt.Equal(deletedAt) {
		t.Errorf("DeletedAt: got %v, want %v", got.DeletedAt, deletedAt)
	}
}

func TestImportJSON_ForceOverwriteCleansAssociatedData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Create an issue with a log
	issue := &models.Issue{
		ID:        "td-cleanup",
		Title:     "Original",
		Status:    models.StatusOpen,
		Type:      models.TypeTask,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := database.UpsertIssueRaw(issue); err != nil {
		t.Fatalf("UpsertIssueRaw: %v", err)
	}
	oldLog := &models.Log{
		ID:        "log-old",
		IssueID:   "td-cleanup",
		SessionID: "sess-1",
		Message:   "Old log",
		Type:      models.LogTypeProgress,
		Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
	}
	if err := database.InsertLogRaw(oldLog); err != nil {
		t.Fatalf("InsertLogRaw: %v", err)
	}

	// Force-import with a different log
	items := []exportedItem{{
		Issue: models.Issue{
			ID:        "td-cleanup",
			Title:     "Updated",
			Status:    models.StatusInReview,
			Type:      models.TypeTask,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		Logs: []models.Log{{
			ID:        "log-new",
			IssueID:   "td-cleanup",
			SessionID: "sess-2",
			Message:   "New log",
			Type:      models.LogTypeProgress,
			Timestamp: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		}},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, true)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	// Old log should be gone, only new log present
	logs, err := database.GetLogs("td-cleanup", 0)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log after force overwrite, got %d", len(logs))
	}
	if logs[0].ID != "log-new" {
		t.Errorf("Log ID: got %s, want log-new", logs[0].ID)
	}
}

func TestImportJSON_MultipleHandoffs(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	items := []exportedItem{{
		Issue: models.Issue{
			ID:        "td-multi-ho",
			Title:     "Multiple handoffs",
			Status:    models.StatusOpen,
			Type:      models.TypeTask,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Handoffs: []models.Handoff{
			{
				ID:        "ho-1",
				IssueID:   "td-multi-ho",
				SessionID: "sess-1",
				Done:      []string{"first pass"},
				Remaining: []string{"more work"},
				Timestamp: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:        "ho-2",
				IssueID:   "td-multi-ho",
				SessionID: "sess-2",
				Done:      []string{"second pass"},
				Remaining: []string{"final review"},
				Timestamp: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
			},
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	handoffs, err := database.GetHandoffs("td-multi-ho")
	if err != nil {
		t.Fatalf("GetHandoffs: %v", err)
	}
	if len(handoffs) != 2 {
		t.Fatalf("Expected 2 handoffs, got %d", len(handoffs))
	}
}

func TestImportJSON_OldHandoffFormat(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Old export format: "handoff" singular with a single object
	oldFormatJSON := `[{
		"issue": {
			"id": "td-oldfmt",
			"title": "Old format",
			"status": "open",
			"type": "task",
			"priority": "P2",
			"created_at": "2026-01-01T00:00:00Z",
			"updated_at": "2026-01-01T00:00:00Z"
		},
		"logs": [],
		"handoff": {
			"id": "ho-old",
			"issue_id": "td-oldfmt",
			"session_id": "sess-1",
			"done": ["completed work"],
			"remaining": ["more work"],
			"timestamp": "2026-01-10T00:00:00Z"
		},
		"dependencies": [],
		"files": []
	}]`

	imported, err := importJSON(database, []byte(oldFormatJSON), false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	handoffs, err := database.GetHandoffs("td-oldfmt")
	if err != nil {
		t.Fatalf("GetHandoffs: %v", err)
	}
	if len(handoffs) != 1 {
		t.Fatalf("Expected 1 handoff from old format, got %d", len(handoffs))
	}
	if handoffs[0].ID != "ho-old" {
		t.Errorf("Handoff ID: got %s, want ho-old", handoffs[0].ID)
	}
	if len(handoffs[0].Done) != 1 || handoffs[0].Done[0] != "completed work" {
		t.Errorf("Handoff Done: got %v", handoffs[0].Done)
	}
}

func TestImportJSON_DanglingParentID(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Import an issue whose parent doesn't exist in the DB or import set
	items := []exportedItem{{
		Issue: models.Issue{
			ID:        "td-child",
			Title:     "Child issue",
			Status:    models.StatusOpen,
			Type:      models.TypeTask,
			ParentID:  "td-nonexistent",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	data, _ := json.Marshal(items)

	// Should still import successfully (warning only, not error)
	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	// Issue should exist with its parent_id preserved
	got, err := database.GetIssue("td-child")
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.ParentID != "td-nonexistent" {
		t.Errorf("ParentID: got %s, want td-nonexistent", got.ParentID)
	}
}

func TestImportJSON_ValidParentID(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Import parent and child together — no warning expected
	items := []exportedItem{
		{Issue: models.Issue{
			ID: "td-parent", Title: "Parent",
			Status: models.StatusOpen, Type: models.TypeTask,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
		{Issue: models.Issue{
			ID: "td-child2", Title: "Child",
			Status: models.StatusOpen, Type: models.TypeTask,
			ParentID:  "td-parent",
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 2 {
		t.Errorf("imported: got %d, want 2", imported)
	}

	got, _ := database.GetIssue("td-child2")
	if got.ParentID != "td-parent" {
		t.Errorf("ParentID: got %s, want td-parent", got.ParentID)
	}
}

func TestImportMarkdown_Status(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	md := `## Task One
- Status: in_progress
- Type: feature
- Priority: P1

## Task Two
- Status: blocked
- Type: bug

## Task Three
- Type: task
`

	imported, err := importMarkdown(database, md, false, false, "test-session")
	if err != nil {
		t.Fatalf("importMarkdown: %v", err)
	}
	if imported != 3 {
		t.Errorf("imported: got %d, want 3", imported)
	}

	// Verify statuses
	issues, err := database.ListIssues(db.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}

	statusByTitle := make(map[string]models.Status)
	for _, issue := range issues {
		statusByTitle[issue.Title] = issue.Status
	}

	if statusByTitle["Task One"] != models.StatusInProgress {
		t.Errorf("Task One status: got %s, want %s", statusByTitle["Task One"], models.StatusInProgress)
	}
	if statusByTitle["Task Two"] != models.StatusBlocked {
		t.Errorf("Task Two status: got %s, want %s", statusByTitle["Task Two"], models.StatusBlocked)
	}
	// Task Three has no status line — should default to open
	if statusByTitle["Task Three"] != models.StatusOpen {
		t.Errorf("Task Three status: got %s, want %s", statusByTitle["Task Three"], models.StatusOpen)
	}
}

func TestImportJSON_OldDependencyFormat(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Old export format: dependencies as []string
	oldFormatJSON := `[{
		"issue": {
			"id": "td-olddep",
			"title": "Old deps format",
			"status": "open",
			"type": "task",
			"priority": "P2",
			"created_at": "2026-01-01T00:00:00Z",
			"updated_at": "2026-01-01T00:00:00Z"
		},
		"logs": [],
		"handoffs": [],
		"dependencies": ["td-dep1", "td-dep2"],
		"files": []
	}]`

	imported, err := importJSON(database, []byte(oldFormatJSON), false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	deps, err := database.GetDependencies("td-olddep")
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("Expected 2 dependencies, got %d", len(deps))
	}
}

func TestImportJSON_DependencyRelationTypePreserved(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	items := []exportedItem{{
		Issue: models.Issue{
			ID: "td-deptype", Title: "Dep type test",
			Status: models.StatusOpen, Type: models.TypeTask,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Dependencies: []models.IssueDependency{
			{DependsOnID: "td-a", RelationType: "depends_on"},
			{DependsOnID: "td-b", RelationType: "blocks"},
		},
	}}
	data, _ := json.Marshal(items)

	imported, err := importJSON(database, data, false, false)
	if err != nil {
		t.Fatalf("importJSON: %v", err)
	}
	if imported != 1 {
		t.Errorf("imported: got %d, want 1", imported)
	}

	// Verify both relation types stored
	allDeps, err := database.GetIssueDependencyRelations("td-deptype")
	if err != nil {
		t.Fatalf("GetIssueDependencyRelations: %v", err)
	}
	if len(allDeps) != 2 {
		t.Fatalf("Expected 2 dependency relations, got %d", len(allDeps))
	}

	typeMap := make(map[string]string)
	for _, dep := range allDeps {
		typeMap[dep.DependsOnID] = dep.RelationType
	}
	if typeMap["td-a"] != "depends_on" {
		t.Errorf("td-a relation: got %s, want depends_on", typeMap["td-a"])
	}
	if typeMap["td-b"] != "blocks" {
		t.Errorf("td-b relation: got %s, want blocks", typeMap["td-b"])
	}
}
