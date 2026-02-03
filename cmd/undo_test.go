package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"just now", 30 * time.Second, "just now"},
		{"1 minute", 1 * time.Minute, "1m ago"},
		{"5 minutes", 5 * time.Minute, "5m ago"},
		{"59 minutes", 59 * time.Minute, "59m ago"},
		{"1 hour", 1 * time.Hour, "1h ago"},
		{"5 hours", 5 * time.Hour, "5h ago"},
		{"23 hours", 23 * time.Hour, "23h ago"},
		{"1 day", 24 * time.Hour, "1d ago"},
		{"3 days", 72 * time.Hour, "3d ago"},
		{"7 days", 168 * time.Hour, "7d ago"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			timestamp := time.Now().Add(-tc.duration)
			got := formatTimeAgo(timestamp)
			if got != tc.want {
				t.Errorf("formatTimeAgo() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatTimeAgoBoundaries(t *testing.T) {
	// Test boundary between just now and minutes
	t.Run("59 seconds is just now", func(t *testing.T) {
		timestamp := time.Now().Add(-59 * time.Second)
		got := formatTimeAgo(timestamp)
		if got != "just now" {
			t.Errorf("formatTimeAgo(59s) = %q, want %q", got, "just now")
		}
	})

	t.Run("61 seconds is 1m ago", func(t *testing.T) {
		timestamp := time.Now().Add(-61 * time.Second)
		got := formatTimeAgo(timestamp)
		if got != "1m ago" {
			t.Errorf("formatTimeAgo(61s) = %q, want %q", got, "1m ago")
		}
	})

	// Test boundary between minutes and hours
	t.Run("59 minutes is minutes", func(t *testing.T) {
		timestamp := time.Now().Add(-59 * time.Minute)
		got := formatTimeAgo(timestamp)
		if got != "59m ago" {
			t.Errorf("formatTimeAgo(59m) = %q, want %q", got, "59m ago")
		}
	})

	t.Run("61 minutes is hours", func(t *testing.T) {
		timestamp := time.Now().Add(-61 * time.Minute)
		got := formatTimeAgo(timestamp)
		if got != "1h ago" {
			t.Errorf("formatTimeAgo(61m) = %q, want %q", got, "1h ago")
		}
	})

	// Test boundary between hours and days
	t.Run("23 hours is hours", func(t *testing.T) {
		timestamp := time.Now().Add(-23 * time.Hour)
		got := formatTimeAgo(timestamp)
		if got != "23h ago" {
			t.Errorf("formatTimeAgo(23h) = %q, want %q", got, "23h ago")
		}
	})

	t.Run("25 hours is days", func(t *testing.T) {
		timestamp := time.Now().Add(-25 * time.Hour)
		got := formatTimeAgo(timestamp)
		if got != "1d ago" {
			t.Errorf("formatTimeAgo(25h) = %q, want %q", got, "1d ago")
		}
	})
}

// TestUndoIssueCreate tests undoing issue creation (deletes the issue)
func TestUndoIssueCreate(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify issue exists
	retrieved, err := database.GetIssue(issue.ID)
	if err != nil || retrieved == nil {
		t.Fatalf("Issue not found after creation")
	}

	// Create action log for create
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   issue.ID,
	}

	// Undo the create (should delete)
	if err := undoIssueAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoIssueAction failed: %v", err)
	}

	// Verify issue is deleted
	retrieved, _ = database.GetIssue(issue.ID)
	if retrieved != nil && retrieved.DeletedAt == nil {
		t.Error("Issue should be deleted after undo create")
	}
}

// TestUndoIssueDelete tests undoing issue deletion (restores the issue)
func TestUndoIssueDelete(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create and delete an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.DeleteIssue(issue.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Create action log for delete
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionDelete,
		EntityType: "issue",
		EntityID:   issue.ID,
	}

	// Undo the delete (should restore)
	if err := undoIssueAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoIssueAction failed: %v", err)
	}

	// Verify issue is restored
	retrieved, err := database.GetIssue(issue.ID)
	if err != nil || retrieved == nil {
		t.Error("Issue should be restored after undo delete")
	}
}

// TestUndoIssueUpdate tests undoing status changes (restores previous state)
func TestUndoIssueUpdate(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Capture original state
	originalJSON, _ := json.Marshal(issue)

	// Change status
	issue.Status = models.StatusInProgress
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Create action log with previous state
	action := &models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionUpdate,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: string(originalJSON),
	}

	// Undo the update
	if err := undoIssueAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoIssueAction failed: %v", err)
	}

	// Verify status is restored
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusOpen {
		t.Errorf("Status not restored: got %q, want %q", retrieved.Status, models.StatusOpen)
	}
}

// TestUndoIssueStart tests undoing start action (reverts to open)
func TestUndoIssueStart(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue in open state
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	originalJSON, _ := json.Marshal(issue)

	// Start the issue
	issue.Status = models.StatusInProgress
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Create action log
	action := &models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionStart,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: string(originalJSON),
	}

	// Undo start
	if err := undoIssueAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoIssueAction failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusOpen {
		t.Errorf("Status not reverted: got %q, want %q", retrieved.Status, models.StatusOpen)
	}
}

// TestUndoDependencyAdd tests undoing adding a dependency
func TestUndoDependencyAdd(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Add dependency
	if err := database.AddDependency(issue1.ID, issue2.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Create action log for add dependency
	depInfo := struct {
		IssueID     string `json:"issue_id"`
		DependsOnID string `json:"depends_on_id"`
	}{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
	}
	depJSON, _ := json.Marshal(depInfo)

	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionAddDep,
		EntityType: "dependency",
		EntityID:   issue1.ID + ":" + issue2.ID,
		NewData:    string(depJSON),
	}

	// Undo add dependency (should remove it)
	if err := undoDependencyAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoDependencyAction failed: %v", err)
	}

	// Verify dependency is removed
	deps, _ := database.GetDependencies(issue1.ID)
	if len(deps) != 0 {
		t.Error("Dependency should be removed after undo")
	}
}

// TestUndoDependencyRemove tests undoing removing a dependency
func TestUndoDependencyRemove(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Create action log for remove dependency (dependency was removed)
	depInfo := struct {
		IssueID     string `json:"issue_id"`
		DependsOnID string `json:"depends_on_id"`
	}{
		IssueID:     issue1.ID,
		DependsOnID: issue2.ID,
	}
	depJSON, _ := json.Marshal(depInfo)

	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionRemoveDep,
		EntityType: "dependency",
		EntityID:   issue1.ID + ":" + issue2.ID,
		NewData:    string(depJSON),
	}

	// Undo remove dependency (should add it back)
	if err := undoDependencyAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoDependencyAction failed: %v", err)
	}

	// Verify dependency is restored
	deps, _ := database.GetDependencies(issue1.ID)
	if len(deps) != 1 {
		t.Errorf("Expected 1 dependency, got %d", len(deps))
	}
}

// TestUndoFileLinkAdd tests undoing linking a file
func TestUndoFileLinkAdd(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Link a file
	if err := database.LinkFile(issue.ID, "test.go", models.FileRoleImplementation, "abc123"); err != nil {
		t.Fatalf("LinkFile failed: %v", err)
	}

	// Create action log
	linkInfo := struct {
		IssueID  string `json:"issue_id"`
		FilePath string `json:"file_path"`
		Role     string `json:"role"`
		SHA      string `json:"sha"`
	}{
		IssueID:  issue.ID,
		FilePath: "test.go",
		Role:     string(models.FileRoleImplementation),
		SHA:      "abc123",
	}
	linkJSON, _ := json.Marshal(linkInfo)

	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionLinkFile,
		EntityType: "file_link",
		EntityID:   issue.ID + ":test.go",
		NewData:    string(linkJSON),
	}

	// Undo link file (should unlink it)
	if err := undoFileLinkAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoFileLinkAction failed: %v", err)
	}

	// Verify file is unlinked
	files, _ := database.GetLinkedFiles(issue.ID)
	if len(files) != 0 {
		t.Error("File should be unlinked after undo")
	}
}

// TestUndoFileLinkRemove tests undoing unlinking a file
func TestUndoFileLinkRemove(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Create action log for unlink
	linkInfo := struct {
		IssueID  string `json:"issue_id"`
		FilePath string `json:"file_path"`
		Role     string `json:"role"`
		SHA      string `json:"sha"`
	}{
		IssueID:  issue.ID,
		FilePath: "test.go",
		Role:     string(models.FileRoleImplementation),
		SHA:      "abc123",
	}
	linkJSON, _ := json.Marshal(linkInfo)

	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionUnlinkFile,
		EntityType: "file_link",
		EntityID:   issue.ID + ":test.go",
		NewData:    string(linkJSON),
	}

	// Undo unlink file (should link it back)
	if err := undoFileLinkAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoFileLinkAction failed: %v", err)
	}

	// Verify file is linked
	files, _ := database.GetLinkedFiles(issue.ID)
	if len(files) != 1 {
		t.Errorf("Expected 1 linked file, got %d", len(files))
	}
}

// TestPerformUndoDispatch tests that performUndo dispatches to correct handler
func TestPerformUndoDispatch(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue for testing
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	tests := []struct {
		name       string
		entityType string
		actionType models.ActionType
		wantError  bool
	}{
		{"issue entity", "issue", models.ActionCreate, false},
		{"unknown entity", "unknown", models.ActionCreate, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action := &models.ActionLog{
				SessionID:  "ses_test",
				ActionType: tc.actionType,
				EntityType: tc.entityType,
				EntityID:   issue.ID,
			}

			err := performUndo(database, action, "ses_test")
			if tc.wantError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestUndoUpdateWithoutPreviousData tests error case when no previous data
func TestUndoUpdateWithoutPreviousData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	action := &models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionUpdate,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: "", // No previous data
	}

	err = undoIssueAction(database, action, "ses_test")
	if err == nil {
		t.Error("Expected error when PreviousData is empty")
	}
}

// TestUndoWithInvalidPreviousData tests error case with invalid JSON
func TestUndoWithInvalidPreviousData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	action := &models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionUpdate,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: "invalid json{",
	}

	err = undoIssueAction(database, action, "ses_test")
	if err == nil {
		t.Error("Expected error when PreviousData is invalid JSON")
	}
}

// TestUndoBoardUnposition tests undoing board unposition restores the position
func TestUndoBoardUnposition(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board and issue
	board, err := database.CreateBoard("test-board", "status:open")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Set position, then remove it
	if err := database.SetIssuePosition(board.ID, issue.ID, 3); err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}
	if err := database.RemoveIssuePosition(board.ID, issue.ID); err != nil {
		t.Fatalf("RemoveIssuePosition failed: %v", err)
	}

	// Create action log with position captured (as the fix does)
	posData, _ := json.Marshal(map[string]any{
		"board_id": board.ID,
		"issue_id": issue.ID,
		"position": 3,
	})
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionBoardUnposition,
		EntityType: "board_issue_positions",
		EntityID:   board.ID + ":" + issue.ID,
		NewData:    string(posData),
	}

	// Undo unposition (should restore position)
	if err := undoBoardPositionAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoBoardPositionAction failed: %v", err)
	}

	// Verify position is restored
	pos, err := database.GetIssuePosition(board.ID, issue.ID)
	if err != nil {
		t.Fatalf("GetIssuePosition failed: %v", err)
	}
	if pos != 3 {
		t.Errorf("Position not restored: got %d, want 3", pos)
	}
}

// TestUndoBoardSetPosition tests undoing board set-position removes the position
func TestUndoBoardSetPosition(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create board and issue
	board, err := database.CreateBoard("test-board", "status:open")
	if err != nil {
		t.Fatalf("CreateBoard failed: %v", err)
	}
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Set position
	if err := database.SetIssuePosition(board.ID, issue.ID, 5); err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}

	// Create action log for set-position
	posData, _ := json.Marshal(map[string]any{
		"board_id": board.ID,
		"issue_id": issue.ID,
		"position": 5,
	})
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionBoardSetPosition,
		EntityType: "board_issue_positions",
		EntityID:   board.ID + ":" + issue.ID,
		NewData:    string(posData),
	}

	// Undo set-position (should remove position)
	if err := undoBoardPositionAction(database, action, "ses_test"); err != nil {
		t.Fatalf("undoBoardPositionAction failed: %v", err)
	}

	// Verify position is removed
	pos, err := database.GetIssuePosition(board.ID, issue.ID)
	if err != nil {
		t.Fatalf("GetIssuePosition failed: %v", err)
	}
	if pos != 0 {
		t.Errorf("Position should be removed: got %d, want 0", pos)
	}
}
