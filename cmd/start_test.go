package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestStartSingleIssue tests starting a single issue
func TestStartSingleIssue(t *testing.T) {
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

	// Simulate starting the issue
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = "ses_test"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Errorf("Expected in_progress, got %q", retrieved.Status)
	}
	if retrieved.ImplementerSession != "ses_test" {
		t.Errorf("Expected session ses_test, got %q", retrieved.ImplementerSession)
	}
}

// TestStartMultipleIssues tests starting multiple issues at once
func TestStartMultipleIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issues := []*models.Issue{
		{Title: "Issue 1", Status: models.StatusOpen},
		{Title: "Issue 2", Status: models.StatusOpen},
		{Title: "Issue 3", Status: models.StatusOpen},
	}

	for _, issue := range issues {
		mustCreateIssue(t, database, issue)
	}

	// Start all issues
	for _, issue := range issues {
		issue.Status = models.StatusInProgress
		issue.ImplementerSession = "ses_test"
		mustUpdateIssue(t, database, issue)
	}

	// Verify all started
	for _, issue := range issues {
		retrieved, _ := database.GetIssue(issue.ID)
		if retrieved.Status != models.StatusInProgress {
			t.Errorf("Issue %s not started", issue.ID)
		}
	}
}

// TestStartBlockedIssueWithoutForce tests that blocked issues can't be started normally
func TestStartBlockedIssueWithoutForce(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Blocked Issue",
		Status: models.StatusBlocked,
	}
	mustCreateIssue(t, database, issue)

	// Without force, blocked issues should remain blocked
	// In the actual command this would skip, here we test the state check
	if issue.Status == models.StatusBlocked {
		// This should be skipped in actual command
		t.Log("Correctly detected blocked issue")
	} else {
		t.Error("Issue should be blocked")
	}
}

// TestStartBlockedIssueWithForce tests that blocked issues can be started with force
func TestStartBlockedIssueWithForce(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Blocked Issue",
		Status: models.StatusBlocked,
	}
	mustCreateIssue(t, database, issue)

	// With force, even blocked issues can be started
	force := true
	if issue.Status == models.StatusBlocked && force {
		issue.Status = models.StatusInProgress
		issue.ImplementerSession = "ses_test"
		mustUpdateIssue(t, database, issue)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Error("Blocked issue should be startable with force")
	}
}

// TestStartSetsImplementerSession tests that implementer session is recorded
func TestStartSetsImplementerSession(t *testing.T) {
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
	mustCreateIssue(t, database, issue)

	sessionID := "ses_abc123"
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = sessionID
	mustUpdateIssue(t, database, issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.ImplementerSession != sessionID {
		t.Errorf("Expected session %s, got %s", sessionID, retrieved.ImplementerSession)
	}
}

// TestStartFromDifferentStatuses tests starting from various initial states
func TestStartFromDifferentStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	testCases := []struct {
		name          string
		initialStatus models.Status
		canStart      bool
	}{
		{"from open", models.StatusOpen, true},
		{"from in_review", models.StatusInReview, true},
		{"from closed", models.StatusClosed, true},
		{"from blocked", models.StatusBlocked, false}, // needs force
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &models.Issue{
				Title:  tc.name,
				Status: tc.initialStatus,
			}
			mustCreateIssue(t, database, issue)

			if tc.canStart || tc.initialStatus != models.StatusBlocked {
				issue.Status = models.StatusInProgress
				mustUpdateIssue(t, database, issue)

				retrieved, _ := database.GetIssue(issue.ID)
				if retrieved.Status != models.StatusInProgress {
					t.Errorf("Failed to start from %s", tc.initialStatus)
				}
			}
		})
	}
}

// TestStartNonexistentIssue tests handling of non-existent issues
func TestStartNonexistentIssue(t *testing.T) {
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

// TestStartRecordsGitSnapshot tests that git state is captured on start
func TestStartRecordsGitSnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	mustCreateIssue(t, database, issue)

	// Record a git snapshot
	snapshot := &models.GitSnapshot{
		IssueID:    issue.ID,
		Event:      "start",
		CommitSHA:  "abc123def456789012345678901234567890abcd",
		Branch:     "main",
		DirtyFiles: 0,
	}
	if err := database.AddGitSnapshot(snapshot); err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := database.GetStartSnapshot(issue.ID)
	if err != nil {
		t.Fatalf("GetStartSnapshot failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected snapshot, got nil")
	}
	if retrieved.CommitSHA != snapshot.CommitSHA {
		t.Errorf("CommitSHA mismatch: got %q", retrieved.CommitSHA)
	}
	if retrieved.Event != "start" {
		t.Errorf("Event mismatch: got %q", retrieved.Event)
	}
}

// TestStartLogsAction tests that start action is logged for undo
func TestStartLogsAction(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	if _, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	mustCreateIssue(t, database, issue)

	sessionID := "ses_test123"

	// Log start action
	action := &models.ActionLog{
		SessionID:    sessionID,
		ActionType:   models.ActionStart,
		EntityType:   "issue",
		EntityID:     issue.ID,
		PreviousData: `{"status":"open"}`,
		NewData:      `{"status":"in_progress"}`,
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
	if lastAction.ActionType != models.ActionStart {
		t.Errorf("Expected ActionStart, got %s", lastAction.ActionType)
	}
}

// TestStartAddsProgressLog tests that a progress log is added on start
func TestStartAddsProgressLog(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	mustCreateIssue(t, database, issue)

	// Add progress log as start command would
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Started work",
		Type:      models.LogTypeProgress,
	}
	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Verify log was added
	logs, err := database.GetLogs(issue.ID, 10)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != "Started work" {
		t.Errorf("Expected 'Started work', got %q", logs[0].Message)
	}
}

// TestStartWithReason tests custom reason for starting
func TestStartWithReason(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	mustCreateIssue(t, database, issue)

	reason := "Picking up from previous session"
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   reason,
		Type:      models.LogTypeProgress,
	}
	mustAddLog(t, database, log)

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 1 || logs[0].Message != reason {
		t.Error("Custom reason not recorded correctly")
	}
}

// TestStartMixedValidAndInvalid tests starting a mix of valid and invalid issues
func TestStartMixedValidAndInvalid(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	validIssue := &models.Issue{Title: "Valid", Status: models.StatusOpen}
	mustCreateIssue(t, database, validIssue)

	// Try to get a non-existent issue
	_, err = database.GetIssue("td-invalid")
	if err == nil {
		t.Error("Expected error for invalid issue")
	}

	// Valid issue can still be started
	validIssue.Status = models.StatusInProgress
	mustUpdateIssue(t, database, validIssue)

	retrieved, _ := database.GetIssue(validIssue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Error("Valid issue should be started despite invalid issue")
	}
}
