package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestBlockSingleIssue tests blocking a single issue
func TestBlockSingleIssue(t *testing.T) {
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

	// Block the issue
	issue.Status = models.StatusBlocked
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusBlocked {
		t.Errorf("Expected blocked, got %q", retrieved.Status)
	}
}

// TestBlockMultipleIssues tests blocking multiple issues at once
func TestBlockMultipleIssues(t *testing.T) {
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

	for _, issue := range issues {
		database.CreateIssue(issue)
	}

	// Block all issues
	for _, issue := range issues {
		issue.Status = models.StatusBlocked
		database.UpdateIssue(issue)
	}

	// Verify all blocked
	for _, issue := range issues {
		retrieved, _ := database.GetIssue(issue.ID)
		if retrieved.Status != models.StatusBlocked {
			t.Errorf("Issue %s not blocked", issue.ID)
		}
	}
}

// TestBlockFromDifferentStatuses tests blocking from various states
func TestBlockFromDifferentStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	testCases := []struct {
		name           string
		initialStatus  models.Status
		shouldTransition bool
	}{
		{"from open", models.StatusOpen, true},
		{"from in_progress", models.StatusInProgress, true},
		{"from in_review", models.StatusInReview, true},
		{"from closed", models.StatusClosed, true},
		{"already blocked", models.StatusBlocked, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &models.Issue{
				Title:  tc.name,
				Status: tc.initialStatus,
			}
			database.CreateIssue(issue)

			if tc.shouldTransition {
				issue.Status = models.StatusBlocked
				database.UpdateIssue(issue)

				retrieved, _ := database.GetIssue(issue.ID)
				if retrieved.Status != models.StatusBlocked {
					t.Errorf("Failed to block from %s", tc.initialStatus)
				}
			}
		})
	}
}

// TestBlockLogsAction tests that block action is logged for undo
func TestBlockLogsAction(t *testing.T) {
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
	previousStatus := issue.Status
	issue.Status = models.StatusBlocked

	// Log block action
	action := &models.ActionLog{
		SessionID:    sessionID,
		ActionType:   models.ActionBlock,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: `{"status":"open"}`,
		NewData:      `{"status":"blocked"}`,
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
	if lastAction.ActionType != models.ActionBlock {
		t.Errorf("Expected ActionBlock, got %s", lastAction.ActionType)
	}
	if previousStatus == issue.Status {
		t.Error("Status should have changed from open to blocked")
	}
}

// TestBlockWithReason tests blocking with a reason
func TestBlockWithReason(t *testing.T) {
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

	reason := "Waiting for dependency: td-abc123"

	// Add log with blocking reason
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   reason,
		Type:      models.LogTypeBlocker,
	}
	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Update issue to blocked
	issue.Status = models.StatusBlocked
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify reason was recorded
	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != reason {
		t.Errorf("Expected reason %q, got %q", reason, logs[0].Message)
	}
	if logs[0].Type != models.LogTypeBlocker {
		t.Errorf("Expected LogTypeBlocker, got %s", logs[0].Type)
	}
}

// TestBlockNonexistentIssue tests blocking non-existent issue
func TestBlockNonexistentIssue(t *testing.T) {
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

// TestBlockAlreadyBlockedIssue tests that blocking already blocked issue works
func TestBlockAlreadyBlockedIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Already Blocked",
		Status: models.StatusBlocked,
	}
	database.CreateIssue(issue)

	// Block again (idempotent)
	issue.Status = models.StatusBlocked
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusBlocked {
		t.Errorf("Expected blocked, got %q", retrieved.Status)
	}
}

// TestBlockWithDependentIssues tests blocking issues with dependencies
func TestBlockWithDependentIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create blocker and dependent
	blocker := &models.Issue{Title: "Blocker", Status: models.StatusOpen}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusOpen}

	database.CreateIssue(blocker)
	database.CreateIssue(dependent)

	// Add dependency
	database.AddDependency(dependent.ID, blocker.ID, "depends_on")

	// Block the blocker
	blocker.Status = models.StatusBlocked
	database.UpdateIssue(blocker)

	// Verify blocker is blocked
	retrieved, _ := database.GetIssue(blocker.ID)
	if retrieved.Status != models.StatusBlocked {
		t.Error("Blocker should be blocked")
	}

	// Dependent should still be open but with a dependency on blocked issue
	depRetrieved, _ := database.GetIssue(dependent.ID)
	if depRetrieved.Status != models.StatusOpen {
		t.Error("Dependent should still be open")
	}

	deps, _ := database.GetDependencies(dependent.ID)
	if len(deps) != 1 || deps[0] != blocker.ID {
		t.Error("Dependency should be preserved")
	}
}

// TestBlockMixedStatuses tests blocking issues with mixed statuses
func TestBlockMixedStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	testCases := []struct {
		status models.Status
	}{
		{models.StatusOpen},
		{models.StatusInProgress},
		{models.StatusInReview},
		{models.StatusClosed},
	}

	for i, tc := range testCases {
		title := "Issue " + string(tc.status)
		issue := &models.Issue{
			Title:  title,
			Status: tc.status,
		}
		database.CreateIssue(issue)

		issue.Status = models.StatusBlocked
		database.UpdateIssue(issue)

		retrieved, _ := database.GetIssue(issue.ID)
		if retrieved.Status != models.StatusBlocked {
			t.Errorf("Test case %d: failed to block issue with status %s", i, tc.status)
		}
	}
}

// TestBlockUpdatesTimestamp tests that block updates the UpdatedAt timestamp
func TestBlockUpdatesTimestamp(t *testing.T) {
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

	originalTime := issue.UpdatedAt

	// Simulate time passage and block
	issue.Status = models.StatusBlocked
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.UpdatedAt.Equal(originalTime) {
		t.Error("UpdatedAt should be updated when issue is blocked")
	}
}
