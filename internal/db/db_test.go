package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestInitialize(t *testing.T) {
	dir := t.TempDir()

	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Check database file exists
	dbPath := filepath.Join(dir, ".todos", "issues.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file not created")
	}
}

func TestCreateAndGetIssue(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:       "Test Issue",
		Description: "Test description",
		Type:        models.TypeBug,
		Priority:    models.PriorityP1,
		Points:      5,
		Labels:      []string{"test", "bug"},
	}

	err = db.CreateIssue(issue)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if issue.ID == "" {
		t.Error("Issue ID not set")
	}

	// Retrieve issue
	retrieved, err := db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if retrieved.Title != issue.Title {
		t.Errorf("Title mismatch: got %s, want %s", retrieved.Title, issue.Title)
	}

	if retrieved.Type != issue.Type {
		t.Errorf("Type mismatch: got %s, want %s", retrieved.Type, issue.Type)
	}

	if retrieved.Priority != issue.Priority {
		t.Errorf("Priority mismatch: got %s, want %s", retrieved.Priority, issue.Priority)
	}

	if len(retrieved.Labels) != 2 {
		t.Errorf("Labels count mismatch: got %d, want 2", len(retrieved.Labels))
	}
}

func TestListIssues(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create test issues
	issues := []struct {
		title    string
		status   models.Status
		priority models.Priority
	}{
		{"Issue 1", models.StatusOpen, models.PriorityP1},
		{"Issue 2", models.StatusOpen, models.PriorityP2},
		{"Issue 3", models.StatusInProgress, models.PriorityP1},
		{"Issue 4", models.StatusClosed, models.PriorityP3},
	}

	for _, tc := range issues {
		issue := &models.Issue{
			Title:    tc.title,
			Status:   tc.status,
			Priority: tc.priority,
		}
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// Test listing all
	all, err := db.ListIssues(ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("Expected 4 issues, got %d", len(all))
	}

	// Test status filter
	open, err := db.ListIssues(ListIssuesOptions{
		Status: []models.Status{models.StatusOpen},
	})
	if err != nil {
		t.Fatalf("ListIssues with status filter failed: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("Expected 2 open issues, got %d", len(open))
	}

	// Test priority filter
	p1, err := db.ListIssues(ListIssuesOptions{
		Priority: "P1",
	})
	if err != nil {
		t.Fatalf("ListIssues with priority filter failed: %v", err)
	}
	if len(p1) != 2 {
		t.Errorf("Expected 2 P1 issues, got %d", len(p1))
	}
}

func TestDeleteAndRestore(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "To Delete"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Delete
	if err := db.DeleteIssue(issue.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	// Should not appear in normal list
	all, _ := db.ListIssues(ListIssuesOptions{})
	if len(all) != 0 {
		t.Error("Deleted issue still appears in list")
	}

	// Should appear in deleted list
	deleted, _ := db.ListIssues(ListIssuesOptions{OnlyDeleted: true})
	if len(deleted) != 1 {
		t.Error("Deleted issue not in deleted list")
	}

	// Restore
	if err := db.RestoreIssue(issue.ID); err != nil {
		t.Fatalf("RestoreIssue failed: %v", err)
	}

	// Should appear in normal list again
	all, _ = db.ListIssues(ListIssuesOptions{})
	if len(all) != 1 {
		t.Error("Restored issue not in list")
	}
}

func TestEpicFilter(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create epic
	epic := &models.Issue{
		Title: "Epic Issue",
		Type:  models.TypeEpic,
	}
	if err := db.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create direct children of epic
	child1 := &models.Issue{
		Title:    "Child 1",
		ParentID: epic.ID,
	}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		ParentID: epic.ID,
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create grandchild (nested under child1)
	grandchild := &models.Issue{
		Title:    "Grandchild",
		ParentID: child1.ID,
	}
	if err := db.CreateIssue(grandchild); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create unrelated issue
	unrelated := &models.Issue{
		Title: "Unrelated",
	}
	if err := db.CreateIssue(unrelated); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Test epic filter - should return all descendants
	results, err := db.ListIssues(ListIssuesOptions{
		EpicID: epic.ID,
	})
	if err != nil {
		t.Fatalf("ListIssues with epic filter failed: %v", err)
	}

	// Should return child1, child2, and grandchild (3 total)
	if len(results) != 3 {
		t.Errorf("Expected 3 issues in epic, got %d", len(results))
	}

	// Verify IDs are correct
	foundIDs := make(map[string]bool)
	for _, issue := range results {
		foundIDs[issue.ID] = true
	}

	if !foundIDs[child1.ID] {
		t.Error("Child 1 not found in epic results")
	}
	if !foundIDs[child2.ID] {
		t.Error("Child 2 not found in epic results")
	}
	if !foundIDs[grandchild.ID] {
		t.Error("Grandchild not found in epic results")
	}
	if foundIDs[epic.ID] {
		t.Error("Epic itself should not be in results")
	}
	if foundIDs[unrelated.ID] {
		t.Error("Unrelated issue should not be in epic results")
	}
}

func TestEpicFilterNoChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create epic with no children
	epic := &models.Issue{
		Title: "Empty Epic",
		Type:  models.TypeEpic,
	}
	if err := db.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Test epic filter on empty epic
	results, err := db.ListIssues(ListIssuesOptions{
		EpicID: epic.ID,
	})
	if err != nil {
		t.Fatalf("ListIssues with epic filter failed: %v", err)
	}

	// Should return empty list
	if len(results) != 0 {
		t.Errorf("Expected 0 issues in empty epic, got %d", len(results))
	}
}

func TestLogs(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add logs
	log1 := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "First log",
		Type:      models.LogTypeProgress,
	}
	if err := db.AddLog(log1); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	log2 := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Second log",
		Type:      models.LogTypeHypothesis,
	}
	if err := db.AddLog(log2); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Get logs
	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	if len(logs) != 2 {
		t.Errorf("Expected 2 logs, got %d", len(logs))
	}

	// Test limit
	limited, _ := db.GetLogs(issue.ID, 1)
	if len(limited) != 1 {
		t.Errorf("Expected 1 log with limit, got %d", len(limited))
	}
}

func TestHandoff(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add handoff
	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1", "Task 2"},
		Remaining: []string{"Task 3"},
		Decisions: []string{"Decision 1"},
		Uncertain: []string{"Question 1"},
	}
	if err := db.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Get handoff
	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}

	if len(retrieved.Done) != 2 {
		t.Errorf("Expected 2 done items, got %d", len(retrieved.Done))
	}

	if len(retrieved.Remaining) != 1 {
		t.Errorf("Expected 1 remaining item, got %d", len(retrieved.Remaining))
	}
}

func TestDependencies(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issues
	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add dependency: issue2 depends on issue1
	if err := db.AddDependency(issue2.ID, issue1.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Check dependencies
	deps, err := db.GetDependencies(issue2.ID)
	if err != nil {
		t.Fatalf("GetDependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0] != issue1.ID {
		t.Error("Dependency not correctly stored")
	}

	// Check blocked by
	blocked, err := db.GetBlockedBy(issue1.ID)
	if err != nil {
		t.Fatalf("GetBlockedBy failed: %v", err)
	}
	if len(blocked) != 1 || blocked[0] != issue2.ID {
		t.Error("Blocked by not correctly retrieved")
	}

	// Remove dependency
	if err := db.RemoveDependency(issue2.ID, issue1.ID); err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	deps, _ = db.GetDependencies(issue2.ID)
	if len(deps) != 0 {
		t.Error("Dependency not removed")
	}
}

func TestWorkSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create work session
	ws := &models.WorkSession{
		Name:      "Test Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	if ws.ID == "" {
		t.Error("Work session ID not set")
	}

	// Create issue and tag it
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := db.TagIssueToWorkSession(ws.ID, issue.ID); err != nil {
		t.Fatalf("TagIssueToWorkSession failed: %v", err)
	}

	// Get tagged issues
	issues, err := db.GetWorkSessionIssues(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSessionIssues failed: %v", err)
	}
	if len(issues) != 1 || issues[0] != issue.ID {
		t.Error("Issue not correctly tagged to work session")
	}

	// Untag
	if err := db.UntagIssueFromWorkSession(ws.ID, issue.ID); err != nil {
		t.Fatalf("UntagIssueFromWorkSession failed: %v", err)
	}

	issues, _ = db.GetWorkSessionIssues(ws.ID)
	if len(issues) != 0 {
		t.Error("Issue not untagged from work session")
	}
}

func TestGetLogsByWorkSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create work session
	ws := &models.WorkSession{
		Name:      "Test Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	// Create issues and tag them
	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	db.TagIssueToWorkSession(ws.ID, issue1.ID)
	db.TagIssueToWorkSession(ws.ID, issue2.ID)

	// Add logs with work session ID
	log1 := &models.Log{
		IssueID:       issue1.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Progress on issue 1",
		Type:          models.LogTypeProgress,
	}
	log2 := &models.Log{
		IssueID:       issue2.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Progress on issue 2",
		Type:          models.LogTypeProgress,
	}
	log3 := &models.Log{
		IssueID:       issue1.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Decision made",
		Type:          models.LogTypeDecision,
	}

	if err := db.AddLog(log1); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}
	if err := db.AddLog(log2); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}
	if err := db.AddLog(log3); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Get logs by work session
	logs, err := db.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}

	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}

	// Verify logs are from both issues
	issueIDs := make(map[string]bool)
	for _, log := range logs {
		issueIDs[log.IssueID] = true
	}
	if len(issueIDs) != 2 {
		t.Errorf("Expected logs from 2 issues, got %d", len(issueIDs))
	}
}

func TestActionLog(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Run migrations to ensure action_log table exists
	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	sessionID := "ses_test123"

	// Test logging an action
	action := &models.ActionLog{
		SessionID:    sessionID,
		ActionType:   models.ActionCreate,
		EntityType:   "issue",
		EntityID:     "td-test1",
		PreviousData: "",
		NewData:      `{"id":"td-test1","title":"Test Issue"}`,
	}
	if err := db.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Test GetLastAction
	lastAction, err := db.GetLastAction(sessionID)
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if lastAction == nil {
		t.Fatal("Expected action, got nil")
	}
	if lastAction.EntityID != "td-test1" {
		t.Errorf("Expected entity ID td-test1, got %s", lastAction.EntityID)
	}
	if lastAction.ActionType != models.ActionCreate {
		t.Errorf("Expected action type create, got %s", lastAction.ActionType)
	}

	// Log another action
	action2 := &models.ActionLog{
		SessionID:    sessionID,
		ActionType:   models.ActionUpdate,
		EntityType:   "issue",
		EntityID:     "td-test1",
		PreviousData: `{"id":"td-test1","title":"Test Issue"}`,
		NewData:      `{"id":"td-test1","title":"Updated Issue"}`,
	}
	if err := db.LogAction(action2); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Test GetRecentActions
	recent, err := db.GetRecentActions(sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentActions failed: %v", err)
	}
	if len(recent) != 2 {
		t.Errorf("Expected 2 recent actions, got %d", len(recent))
	}
	// Most recent should be first
	if recent[0].ActionType != models.ActionUpdate {
		t.Error("Expected most recent action to be update")
	}

	// Test MarkActionUndone
	if err := db.MarkActionUndone(recent[0].ID); err != nil {
		t.Fatalf("MarkActionUndone failed: %v", err)
	}

	// GetLastAction should now return the first action (create), not the undone one
	lastAction, err = db.GetLastAction(sessionID)
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if lastAction == nil {
		t.Fatal("Expected action, got nil")
	}
	if lastAction.ActionType != models.ActionCreate {
		t.Errorf("Expected first non-undone action (create), got %s", lastAction.ActionType)
	}

	// Test limit on GetRecentActions
	limited, err := db.GetRecentActions(sessionID, 1)
	if err != nil {
		t.Fatalf("GetRecentActions with limit failed: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("Expected 1 action with limit, got %d", len(limited))
	}
}

func TestActionLogDifferentSessions(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Log actions from different sessions
	action1 := &models.ActionLog{
		SessionID:  "ses_session1",
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   "td-abc1",
	}
	action2 := &models.ActionLog{
		SessionID:  "ses_session2",
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   "td-abc2",
	}
	db.LogAction(action1)
	db.LogAction(action2)

	// Each session should only see its own actions
	session1Actions, _ := db.GetRecentActions("ses_session1", 10)
	session2Actions, _ := db.GetRecentActions("ses_session2", 10)

	if len(session1Actions) != 1 {
		t.Errorf("Session 1 should have 1 action, got %d", len(session1Actions))
	}
	if len(session2Actions) != 1 {
		t.Errorf("Session 2 should have 1 action, got %d", len(session2Actions))
	}
	if session1Actions[0].EntityID != "td-abc1" {
		t.Error("Session 1 got wrong action")
	}
	if session2Actions[0].EntityID != "td-abc2" {
		t.Error("Session 2 got wrong action")
	}
}
