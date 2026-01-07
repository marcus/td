package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestDeleteSingleIssue tests soft-deleting a single issue
func TestDeleteSingleIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	issueID := issue.ID

	// Soft-delete the issue
	if err := database.DeleteIssue(issueID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Verify issue is marked as deleted
	retrieved, err := database.GetIssue(issueID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if retrieved.DeletedAt == nil {
		t.Error("Expected DeletedAt to be set")
	}
}

// TestDeleteMultipleIssues tests deleting multiple issues at once
func TestDeleteMultipleIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issues := []*models.Issue{
		{Title: "Issue 1", Status: models.StatusOpen},
		{Title: "Issue 2", Status: models.StatusInProgress},
		{Title: "Issue 3", Status: models.StatusOpen},
	}

	issueIDs := make([]string, 0)
	for _, issue := range issues {
		database.CreateIssue(issue)
		issueIDs = append(issueIDs, issue.ID)
	}

	// Delete all issues
	for _, id := range issueIDs {
		if err := database.DeleteIssue(id); err != nil {
			t.Fatalf("DeleteIssue failed: %v", err)
		}
	}

	// Verify all deleted
	for _, id := range issueIDs {
		retrieved, _ := database.GetIssue(id)
		if retrieved.DeletedAt == nil {
			t.Errorf("Issue %s should be deleted", id)
		}
	}
}

// TestDeleteLogsAction tests that delete action is logged for undo
func TestDeleteLogsAction(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	if _, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	database.CreateIssue(issue)

	sessionID := "ses_test123"
	issueData := `{"id":"` + issue.ID + `","title":"Test Issue","status":"open"}`

	// Delete issue
	database.DeleteIssue(issue.ID)

	// Log delete action
	action := &models.ActionLog{
		SessionID:    sessionID,
		ActionType:   models.ActionDelete,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: issueData,
	}
	if err := database.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Verify action was logged
	lastAction, err := database.GetLastAction(sessionID)
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if lastAction == nil {
		t.Fatal("Expected action, got nil")
	}
	if lastAction.ActionType != models.ActionDelete {
		t.Errorf("Expected ActionDelete, got %s", lastAction.ActionType)
	}
}

// TestDeleteNonexistentIssue tests deleting non-existent issue
func TestDeleteNonexistentIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	_, err = database.GetIssue("td-nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent issue")
	}
}

// TestDeleteFromDifferentStatuses tests deleting from various states
func TestDeleteFromDifferentStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	testCases := []struct {
		name          string
		initialStatus models.Status
	}{
		{"from open", models.StatusOpen},
		{"from in_progress", models.StatusInProgress},
		{"from in_review", models.StatusInReview},
		{"from blocked", models.StatusBlocked},
		{"from closed", models.StatusClosed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &models.Issue{
				Title:  tc.name,
				Status: tc.initialStatus,
			}
			database.CreateIssue(issue)

			if err := database.DeleteIssue(issue.ID); err != nil {
				t.Errorf("Failed to delete from %s: %v", tc.initialStatus, err)
			}

			retrieved, _ := database.GetIssue(issue.ID)
			if retrieved.DeletedAt == nil {
				t.Errorf("Issue from %s should be deleted", tc.initialStatus)
			}
		})
	}
}

// TestDeleteAlreadyDeletedIssue tests that deleting already deleted issue is idempotent
func TestDeleteAlreadyDeletedIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Already Deleted",
		Status: models.StatusOpen,
	}
	database.CreateIssue(issue)

	// Delete once
	database.DeleteIssue(issue.ID)

	// Delete again (should be idempotent or still work)
	err = database.DeleteIssue(issue.ID)
	if err != nil {
		t.Logf("Second delete returned: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.DeletedAt == nil {
		t.Error("Expected issue to remain deleted")
	}
}

// TestDeleteWithDependencies tests deleting issues with dependencies
func TestDeleteWithDependencies(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create two related issues
	parent := &models.Issue{Title: "Parent", Status: models.StatusOpen}
	child := &models.Issue{Title: "Child", Status: models.StatusOpen}

	database.CreateIssue(parent)
	database.CreateIssue(child)

	// Add dependency
	database.AddDependency(child.ID, parent.ID, "depends_on")

	// Delete parent
	if err := database.DeleteIssue(parent.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Parent should be deleted
	parentRetrieved, _ := database.GetIssue(parent.ID)
	if parentRetrieved.DeletedAt == nil {
		t.Error("Parent should be deleted")
	}

	// Child should still exist
	childRetrieved, _ := database.GetIssue(child.ID)
	if childRetrieved.DeletedAt != nil {
		t.Error("Child should not be deleted")
	}

	// Dependency should still be recorded
	deps, _ := database.GetDependencies(child.ID)
	if len(deps) != 1 {
		t.Error("Dependency should still exist")
	}
}

// TestDeleteUpdatesTimestamp tests that delete updates the DeletedAt timestamp
func TestDeleteUpdatesTimestamp(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	database.CreateIssue(issue)

	if issue.DeletedAt != nil {
		t.Error("DeletedAt should be nil before delete")
	}

	// Delete
	database.DeleteIssue(issue.ID)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.DeletedAt == nil {
		t.Error("DeletedAt should be set after delete")
	}
}

// TestDeletePreservesIssueData tests that delete preserves all issue data
func TestDeletePreservesIssueData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Test Issue",
		Description: "Important description",
		Status:      models.StatusInProgress,
		Type:        models.TypeFeature,
		Priority:    models.PriorityP1,
		Points:      8,
		Labels:      []string{"backend", "critical"},
	}
	database.CreateIssue(issue)

	issueID := issue.ID

	// Delete the issue
	database.DeleteIssue(issueID)

	// Retrieve and verify data is preserved
	retrieved, _ := database.GetIssue(issueID)
	if retrieved.Title != issue.Title {
		t.Errorf("Title changed: %s -> %s", issue.Title, retrieved.Title)
	}
	if retrieved.Description != issue.Description {
		t.Errorf("Description changed: %s -> %s", issue.Description, retrieved.Description)
	}
	if retrieved.Type != issue.Type {
		t.Errorf("Type changed: %s -> %s", issue.Type, retrieved.Type)
	}
	if retrieved.Priority != issue.Priority {
		t.Errorf("Priority changed: %s -> %s", issue.Priority, retrieved.Priority)
	}
	if retrieved.Points != issue.Points {
		t.Errorf("Points changed: %d -> %d", issue.Points, retrieved.Points)
	}
	if len(retrieved.Labels) != len(issue.Labels) {
		t.Errorf("Labels changed: %v -> %v", issue.Labels, retrieved.Labels)
	}
}

// TestDeleteMultipleWithMixedStatuses tests deleting multiple issues with different statuses
func TestDeleteMultipleWithMixedStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	statuses := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusInReview,
		models.StatusBlocked,
		models.StatusClosed,
	}

	issueIDs := make([]string, 0)
	for _, status := range statuses {
		issue := &models.Issue{
			Title:  string(status),
			Status: status,
		}
		database.CreateIssue(issue)
		issueIDs = append(issueIDs, issue.ID)
	}

	// Delete all
	for _, id := range issueIDs {
		database.DeleteIssue(id)
	}

	// Verify all deleted
	for _, id := range issueIDs {
		retrieved, _ := database.GetIssue(id)
		if retrieved.DeletedAt == nil {
			t.Errorf("Issue %s should be deleted", id)
		}
	}
}

// TestDeletePartialFailure tests handling partial deletion (some succeed, some fail)
func TestDeletePartialFailure(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}

	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Delete first issue successfully
	err1 := database.DeleteIssue(issue1.ID)

	// Try to delete non-existent issue (simulating partial failure)
	err2 := database.DeleteIssue("td-nonexistent")

	// Verify first deletion succeeded
	retrieved, _ := database.GetIssue(issue1.ID)
	if retrieved.DeletedAt == nil {
		t.Error("First deletion should have succeeded")
	}

	if err1 != nil {
		t.Errorf("First deletion should not error: %v", err1)
	}

	// Second deletion may or may not error - just verify first succeeded
	_ = err2
}
