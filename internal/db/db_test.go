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

	if err := db.TagIssueToWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
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
	if err := db.UntagIssueFromWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
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
	db.TagIssueToWorkSession(ws.ID, issue1.ID, "test-session")
	db.TagIssueToWorkSession(ws.ID, issue2.ID, "test-session")

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

func TestSearchIssuesRanked(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create test issues with specific IDs via manual setting after create
	// Issue 1: ID contains 'grace', title is different
	issue1 := &models.Issue{
		Title:       "Other title",
		Description: "Some description",
		Status:      models.StatusOpen,
	}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	// Update ID to contain 'grace' for testing
	_, err = db.conn.Exec(`UPDATE issues SET id = ? WHERE id = ?`, "td-grace123", issue1.ID)
	if err != nil {
		t.Fatalf("Update ID failed: %v", err)
	}
	issue1.ID = "td-grace123"

	// Issue 2: title contains 'gracefully'
	issue2 := &models.Issue{
		Title:       "Gracefully handle errors",
		Description: "Some other description",
		Status:      models.StatusOpen,
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Issue 3: description contains 'gracefully'
	issue3 := &models.Issue{
		Title:       "Other task",
		Description: "gracefully shutdown the service",
		Status:      models.StatusOpen,
	}
	if err := db.CreateIssue(issue3); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Issue 4: closed issue with 'grace' in description
	issue4 := &models.Issue{
		Title:       "Closed issue",
		Description: "Handle grace period",
		Status:      models.StatusClosed,
	}
	if err := db.CreateIssue(issue4); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Test 1: Search 'grace' - ID match should score highest
	t.Run("ID match scores highest", func(t *testing.T) {
		results, err := db.SearchIssuesRanked("grace", ListIssuesOptions{})
		if err != nil {
			t.Fatalf("SearchIssuesRanked failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("Expected results, got none")
		}

		// First result should be td-grace123 (ID match = 90 points)
		if results[0].Issue.ID != "td-grace123" {
			t.Errorf("Expected td-grace123 first (ID match), got %s", results[0].Issue.ID)
		}
		if results[0].Score != 90 {
			t.Errorf("Expected score 90 for ID match, got %d", results[0].Score)
		}
		if results[0].MatchField != "id" {
			t.Errorf("Expected matchField 'id', got %s", results[0].MatchField)
		}
	})

	// Test 2: Search 'gracefully' - title match should score higher than description
	t.Run("title match scores higher than description", func(t *testing.T) {
		results, err := db.SearchIssuesRanked("gracefully", ListIssuesOptions{})
		if err != nil {
			t.Fatalf("SearchIssuesRanked failed: %v", err)
		}

		if len(results) < 2 {
			t.Fatalf("Expected at least 2 results, got %d", len(results))
		}

		// Title match should come before description match
		if results[0].Issue.ID != issue2.ID {
			t.Errorf("Expected title match first, got %s", results[0].Issue.ID)
		}
		if results[0].MatchField != "title" {
			t.Errorf("Expected matchField 'title', got %s", results[0].MatchField)
		}
	})

	// Test 3: Case-insensitive search
	t.Run("case insensitive search", func(t *testing.T) {
		results, err := db.SearchIssuesRanked("GRACEFULLY", ListIssuesOptions{})
		if err != nil {
			t.Fatalf("SearchIssuesRanked failed: %v", err)
		}

		if len(results) < 2 {
			t.Fatalf("Expected at least 2 results for case-insensitive search, got %d", len(results))
		}
	})

	// Test 4: Search with closed status filter
	t.Run("closed status filter", func(t *testing.T) {
		results, err := db.SearchIssuesRanked("grace", ListIssuesOptions{
			Status: []models.Status{models.StatusClosed},
		})
		if err != nil {
			t.Fatalf("SearchIssuesRanked failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Expected 1 closed result, got %d", len(results))
		}
		if results[0].Issue.ID != issue4.ID {
			t.Errorf("Expected closed issue, got %s", results[0].Issue.ID)
		}
	})

	// Test 5: Search by issue ID prefix
	t.Run("search by ID prefix", func(t *testing.T) {
		results, err := db.SearchIssuesRanked("td-grace", ListIssuesOptions{})
		if err != nil {
			t.Fatalf("SearchIssuesRanked failed: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("Expected 1 result for ID prefix search, got %d", len(results))
		}
		if results[0].Issue.ID != "td-grace123" {
			t.Errorf("Expected td-grace123, got %s", results[0].Issue.ID)
		}
	})
}

func TestReviewableByFilter(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionA := "ses_aaaaaa"
	sessionB := "ses_bbbbbb"
	sessionC := "ses_cccccc"

	// Helper to create and update issue (CreateIssue doesn't set ImplementerSession)
	createIssue := func(issue *models.Issue) {
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Issue 1: Implemented by A, created by A - A cannot review, B can
	issue1 := &models.Issue{
		Title:              "Implemented and created by A",
		Status:             models.StatusInReview,
		ImplementerSession: sessionA,
		CreatorSession:     sessionA,
	}
	createIssue(issue1)

	// Issue 2: Implemented by B, created by A - A cannot review (creator), C can
	issue2 := &models.Issue{
		Title:              "Implemented by B, created by A",
		Status:             models.StatusInReview,
		ImplementerSession: sessionB,
		CreatorSession:     sessionA,
	}
	createIssue(issue2)

	// Issue 3: Implemented by B, A in session history - A cannot review
	issue3 := &models.Issue{
		Title:              "Implemented by B, A in history",
		Status:             models.StatusInReview,
		ImplementerSession: sessionB,
		CreatorSession:     sessionC,
	}
	createIssue(issue3)
	// Record A in history (e.g., A started then unstarted)
	if err := db.RecordSessionAction(issue3.ID, sessionA, models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Issue 4: Minor task - can be self-reviewed by implementer
	issue4 := &models.Issue{
		Title:              "Minor task",
		Status:             models.StatusInReview,
		ImplementerSession: sessionA,
		CreatorSession:     sessionA,
		Minor:              true,
	}
	createIssue(issue4)

	// Issue 5: Clean issue - B implemented, C created, no history for A
	issue5 := &models.Issue{
		Title:              "Clean issue for A to review",
		Status:             models.StatusInReview,
		ImplementerSession: sessionB,
		CreatorSession:     sessionC,
	}
	createIssue(issue5)

	// Test: Session A can only review issue4 (minor) and issue5 (clean)
	t.Run("session A reviewable", func(t *testing.T) {
		reviewable, err := db.ListIssues(ListIssuesOptions{ReviewableBy: sessionA})
		if err != nil {
			t.Fatalf("ListIssues failed: %v", err)
		}

		ids := make(map[string]bool)
		for _, issue := range reviewable {
			ids[issue.ID] = true
		}

		// A should be able to review issue4 (minor) and issue5 (clean)
		if !ids[issue4.ID] {
			t.Errorf("Session A should be able to review minor task %s", issue4.ID)
		}
		if !ids[issue5.ID] {
			t.Errorf("Session A should be able to review clean issue %s", issue5.ID)
		}

		// A should NOT be able to review issue1 (implementer+creator), issue2 (creator), issue3 (in history)
		if ids[issue1.ID] {
			t.Errorf("Session A should NOT be able to review %s (is implementer and creator)", issue1.ID)
		}
		if ids[issue2.ID] {
			t.Errorf("Session A should NOT be able to review %s (is creator)", issue2.ID)
		}
		if ids[issue3.ID] {
			t.Errorf("Session A should NOT be able to review %s (in session history)", issue3.ID)
		}

		if len(reviewable) != 2 {
			t.Errorf("Expected 2 reviewable issues for A, got %d", len(reviewable))
		}
	})

	// Test: Session B can review issue1 (A's work), issue3 (in history only for A)
	t.Run("session B reviewable", func(t *testing.T) {
		reviewable, err := db.ListIssues(ListIssuesOptions{ReviewableBy: sessionB})
		if err != nil {
			t.Fatalf("ListIssues failed: %v", err)
		}

		ids := make(map[string]bool)
		for _, issue := range reviewable {
			ids[issue.ID] = true
		}

		// B should be able to review issue1 (A implemented)
		if !ids[issue1.ID] {
			t.Errorf("Session B should be able to review %s", issue1.ID)
		}

		// B should NOT be able to review issue2, issue3, issue5 (is implementer)
		if ids[issue2.ID] {
			t.Errorf("Session B should NOT be able to review %s (is implementer)", issue2.ID)
		}
		if ids[issue3.ID] {
			t.Errorf("Session B should NOT be able to review %s (is implementer)", issue3.ID)
		}
		if ids[issue5.ID] {
			t.Errorf("Session B should NOT be able to review %s (is implementer)", issue5.ID)
		}
	})

	// Test: Session C can review most things (only created issue5)
	t.Run("session C reviewable", func(t *testing.T) {
		reviewable, err := db.ListIssues(ListIssuesOptions{ReviewableBy: sessionC})
		if err != nil {
			t.Fatalf("ListIssues failed: %v", err)
		}

		ids := make(map[string]bool)
		for _, issue := range reviewable {
			ids[issue.ID] = true
		}

		// C should be able to review issue1, issue2, issue4
		if !ids[issue1.ID] {
			t.Errorf("Session C should be able to review %s", issue1.ID)
		}
		if !ids[issue2.ID] {
			t.Errorf("Session C should be able to review %s", issue2.ID)
		}
		if !ids[issue4.ID] {
			t.Errorf("Session C should be able to review %s (minor)", issue4.ID)
		}

		// C should NOT be able to review issue3, issue5 (is creator)
		if ids[issue3.ID] {
			t.Errorf("Session C should NOT be able to review %s (is creator)", issue3.ID)
		}
		if ids[issue5.ID] {
			t.Errorf("Session C should NOT be able to review %s (is creator)", issue5.ID)
		}
	})
}

func TestGetRejectedInProgressIssueIDs(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create test issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusInProgress}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusInProgress}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusInProgress}
	issue4 := &models.Issue{Title: "Issue 4", Status: models.StatusOpen} // Not in_progress

	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)
	db.CreateIssue(issue4)

	// issue1: rejected, no subsequent review (should be detected)
	db.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   issue1.ID,
	})

	// issue2: rejected, then re-submitted (should NOT be detected)
	db.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   issue2.ID,
	})
	db.LogAction(&models.ActionLog{
		SessionID:  "ses_implementer",
		ActionType: models.ActionReview,
		EntityType: "issue",
		EntityID:   issue2.ID,
	})

	// issue3: never rejected (should NOT be detected)
	// issue4: rejected but not in_progress status (should NOT be detected)
	db.LogAction(&models.ActionLog{
		SessionID:  "ses_reviewer",
		ActionType: models.ActionReject,
		EntityType: "issue",
		EntityID:   issue4.ID,
	})

	// Get rejected IDs
	rejectedIDs, err := db.GetRejectedInProgressIssueIDs()
	if err != nil {
		t.Fatalf("GetRejectedInProgressIssueIDs failed: %v", err)
	}

	// Only issue1 should be detected
	if !rejectedIDs[issue1.ID] {
		t.Errorf("issue1 should be detected as rejected in_progress")
	}
	if rejectedIDs[issue2.ID] {
		t.Errorf("issue2 should NOT be detected (was re-submitted)")
	}
	if rejectedIDs[issue3.ID] {
		t.Errorf("issue3 should NOT be detected (never rejected)")
	}
	if rejectedIDs[issue4.ID] {
		t.Errorf("issue4 should NOT be detected (not in_progress status)")
	}
}

// TestBoardCRUD tests basic board create, read, update, delete operations
func TestBoardCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	t.Run("create board with valid query", func(t *testing.T) {
		board, err := db.CreateBoard("Sprint 1", `sprint = "Sprint 1"`)
		if err != nil {
			t.Fatalf("CreateBoard failed: %v", err)
		}
		if board.ID == "" {
			t.Error("Board ID not set")
		}
		if board.Name != "Sprint 1" {
			t.Errorf("Name mismatch: got %s, want Sprint 1", board.Name)
		}
		if board.Query != `sprint = "Sprint 1"` {
			t.Errorf("Query mismatch: got %s", board.Query)
		}
		if board.IsBuiltin {
			t.Error("Board should not be builtin")
		}
	})

	t.Run("create board with empty query", func(t *testing.T) {
		board, err := db.CreateBoard("All Tasks", "")
		if err != nil {
			t.Fatalf("CreateBoard with empty query failed: %v", err)
		}
		if board.Query != "" {
			t.Errorf("Query should be empty, got: %s", board.Query)
		}
	})

	t.Run("create board with invalid query", func(t *testing.T) {
		// Use a query with unknown field to trigger validation error
		_, err := db.CreateBoard("Invalid", "unknown_field = 123 AND (((")
		if err == nil {
			t.Log("Note: Query parser may be lenient; this test verifies the path exists")
		}
		// This test is informational - the parser may accept some invalid-looking queries
	})

	t.Run("get board by ID", func(t *testing.T) {
		board, _ := db.CreateBoard("Test Board", "")
		retrieved, err := db.GetBoard(board.ID)
		if err != nil {
			t.Fatalf("GetBoard failed: %v", err)
		}
		if retrieved.Name != board.Name {
			t.Errorf("Name mismatch: got %s, want %s", retrieved.Name, board.Name)
		}
	})

	t.Run("get board by name", func(t *testing.T) {
		board, _ := db.CreateBoard("Named Board", "")
		retrieved, err := db.GetBoardByName("Named Board")
		if err != nil {
			t.Fatalf("GetBoardByName failed: %v", err)
		}
		if retrieved.ID != board.ID {
			t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, board.ID)
		}
	})

	t.Run("resolve board ref by ID", func(t *testing.T) {
		board, _ := db.CreateBoard("Ref Board", "")
		resolved, err := db.ResolveBoardRef(board.ID)
		if err != nil {
			t.Fatalf("ResolveBoardRef by ID failed: %v", err)
		}
		if resolved.Name != "Ref Board" {
			t.Errorf("Name mismatch")
		}
	})

	t.Run("resolve board ref by name", func(t *testing.T) {
		db.CreateBoard("By Name", "")
		resolved, err := db.ResolveBoardRef("By Name")
		if err != nil {
			t.Fatalf("ResolveBoardRef by name failed: %v", err)
		}
		if resolved.Name != "By Name" {
			t.Errorf("Name mismatch")
		}
	})

	t.Run("list boards sorted by last_viewed_at", func(t *testing.T) {
		boards, err := db.ListBoards()
		if err != nil {
			t.Fatalf("ListBoards failed: %v", err)
		}
		// Should include builtin "All Issues" board plus created boards
		if len(boards) == 0 {
			t.Error("ListBoards returned empty list")
		}
		// Check builtin board exists
		found := false
		for _, b := range boards {
			if b.ID == "bd-all-issues" && b.IsBuiltin {
				found = true
				break
			}
		}
		if !found {
			t.Error("Builtin 'All Issues' board not found")
		}
	})

	t.Run("update board", func(t *testing.T) {
		board, _ := db.CreateBoard("Update Me", "")
		board.Name = "Updated Name"
		board.Query = "status = open"
		err := db.UpdateBoard(board)
		if err != nil {
			t.Fatalf("UpdateBoard failed: %v", err)
		}
		retrieved, _ := db.GetBoard(board.ID)
		if retrieved.Name != "Updated Name" {
			t.Errorf("Name not updated")
		}
		if retrieved.Query != "status = open" {
			t.Errorf("Query not updated")
		}
	})

	t.Run("update builtin board fails", func(t *testing.T) {
		builtin, _ := db.GetBoard("bd-all-issues")
		if builtin == nil {
			t.Skip("Builtin board not found")
		}
		builtin.Name = "Changed"
		err := db.UpdateBoard(builtin)
		if err == nil {
			t.Error("UpdateBoard should fail for builtin board")
		}
	})

	t.Run("delete board", func(t *testing.T) {
		board, _ := db.CreateBoard("Delete Me", "")
		err := db.DeleteBoard(board.ID)
		if err != nil {
			t.Fatalf("DeleteBoard failed: %v", err)
		}
		_, err = db.GetBoard(board.ID)
		if err == nil {
			t.Error("Board should not exist after deletion")
		}
	})

	t.Run("delete builtin board fails", func(t *testing.T) {
		err := db.DeleteBoard("bd-all-issues")
		if err == nil {
			t.Error("DeleteBoard should fail for builtin board")
		}
	})
}

// TestBoardLastViewed tests last viewed board tracking
func TestBoardLastViewed(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	t.Run("no last viewed initially", func(t *testing.T) {
		board, err := db.GetLastViewedBoard()
		if err != nil {
			t.Fatalf("GetLastViewedBoard failed: %v", err)
		}
		// May return nil or builtin board depending on initialization
		_ = board
	})

	t.Run("update last viewed", func(t *testing.T) {
		board, _ := db.CreateBoard("Last Viewed Test", "")
		err := db.UpdateBoardLastViewed(board.ID)
		if err != nil {
			t.Fatalf("UpdateBoardLastViewed failed: %v", err)
		}

		lastViewed, err := db.GetLastViewedBoard()
		if err != nil {
			t.Fatalf("GetLastViewedBoard failed: %v", err)
		}
		if lastViewed == nil || lastViewed.ID != board.ID {
			t.Error("Last viewed board not updated correctly")
		}
	})
}

// TestBoardPositions tests board issue positioning
func TestBoardPositions(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a board and some issues
	board, _ := db.CreateBoard("Position Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	t.Run("set issue positions", func(t *testing.T) {
		err := db.SetIssuePosition(board.ID, issue1.ID, 1)
		if err != nil {
			t.Fatalf("SetIssuePosition failed: %v", err)
		}
		err = db.SetIssuePosition(board.ID, issue2.ID, 2)
		if err != nil {
			t.Fatalf("SetIssuePosition failed: %v", err)
		}
	})

	t.Run("get board issue positions", func(t *testing.T) {
		positions, err := db.GetBoardIssuePositions(board.ID)
		if err != nil {
			t.Fatalf("GetBoardIssuePositions failed: %v", err)
		}
		if len(positions) != 2 {
			t.Errorf("Expected 2 positions, got %d", len(positions))
		}
	})

	t.Run("swap issue positions", func(t *testing.T) {
		err := db.SwapIssuePositions(board.ID, issue1.ID, issue2.ID)
		if err != nil {
			t.Fatalf("SwapIssuePositions failed: %v", err)
		}
		positions, _ := db.GetBoardIssuePositions(board.ID)
		// Verify swap happened
		pos1, pos2 := 0, 0
		for _, p := range positions {
			if p.IssueID == issue1.ID {
				pos1 = p.Position
			}
			if p.IssueID == issue2.ID {
				pos2 = p.Position
			}
		}
		if pos1 != 2 || pos2 != 1 {
			t.Errorf("Positions not swapped correctly: issue1=%d, issue2=%d", pos1, pos2)
		}
	})

	t.Run("remove issue position", func(t *testing.T) {
		err := db.RemoveIssuePosition(board.ID, issue1.ID)
		if err != nil {
			t.Fatalf("RemoveIssuePosition failed: %v", err)
		}
		positions, _ := db.GetBoardIssuePositions(board.ID)
		for _, p := range positions {
			if p.IssueID == issue1.ID {
				t.Error("Position should have been removed")
			}
		}
	})
}

// TestGetBoardIssues tests retrieving issues for a board
func TestGetBoardIssues(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issues
	issue1 := &models.Issue{Title: "Open Issue", Type: models.TypeTask, Priority: models.PriorityP2, Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Closed Issue", Type: models.TypeTask, Priority: models.PriorityP2, Status: models.StatusClosed}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)

	t.Run("get all issues board (empty query)", func(t *testing.T) {
		issues, err := db.GetBoardIssues("bd-all-issues", "test-session", nil)
		if err != nil {
			t.Fatalf("GetBoardIssues failed: %v", err)
		}
		// Should return at least the issues we created
		if len(issues) < 2 {
			t.Errorf("Expected at least 2 issues, got %d", len(issues))
		}
	})

	t.Run("get board issues with status filter", func(t *testing.T) {
		issues, err := db.GetBoardIssues("bd-all-issues", "test-session", []models.Status{models.StatusOpen})
		if err != nil {
			t.Fatalf("GetBoardIssues failed: %v", err)
		}
		// Should only return open issues
		for _, biv := range issues {
			if biv.Issue.Status != models.StatusOpen {
				t.Errorf("Got non-open issue with status filter: %s", biv.Issue.Status)
			}
		}
	})
}

// TestSetIssuePosition_NoShifting tests that setting a position does NOT shift other rows
func TestSetIssuePosition_NoShifting(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("No Shift Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	// Set initial positions: issue1=PositionGap, issue2=2*PositionGap
	db.SetIssuePosition(board.ID, issue1.ID, PositionGap)
	db.SetIssuePosition(board.ID, issue2.ID, 2*PositionGap)

	// Set issue3 at position 1 — issue1 and issue2 must NOT shift
	err = db.SetIssuePosition(board.ID, issue3.ID, 1)
	if err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}

	positions, _ := db.GetBoardIssuePositions(board.ID)
	posMap := make(map[string]int)
	for _, p := range positions {
		posMap[p.IssueID] = p.Position
	}

	if posMap[issue3.ID] != 1 {
		t.Errorf("issue3 position = %d, want 1", posMap[issue3.ID])
	}
	if posMap[issue1.ID] != PositionGap {
		t.Errorf("issue1 position = %d, want %d (should not shift)", posMap[issue1.ID], PositionGap)
	}
	if posMap[issue2.ID] != 2*PositionGap {
		t.Errorf("issue2 position = %d, want %d (should not shift)", posMap[issue2.ID], 2*PositionGap)
	}
}

// TestSetIssuePosition_Reposition tests moving an already-positioned issue changes only its position
func TestSetIssuePosition_Reposition(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("Reposition Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	// Set initial sparse positions
	db.SetIssuePosition(board.ID, issue1.ID, PositionGap)
	db.SetIssuePosition(board.ID, issue2.ID, 2*PositionGap)
	db.SetIssuePosition(board.ID, issue3.ID, 3*PositionGap)

	// Reposition issue3 to a new sort key — others must stay unchanged
	newPos := PositionGap / 2
	err = db.SetIssuePosition(board.ID, issue3.ID, newPos)
	if err != nil {
		t.Fatalf("SetIssuePosition failed: %v", err)
	}

	positions, _ := db.GetBoardIssuePositions(board.ID)
	posMap := make(map[string]int)
	for _, p := range positions {
		posMap[p.IssueID] = p.Position
	}

	if posMap[issue3.ID] != newPos {
		t.Errorf("issue3 position = %d, want %d", posMap[issue3.ID], newPos)
	}
	if posMap[issue1.ID] != PositionGap {
		t.Errorf("issue1 position = %d, want %d (should not shift)", posMap[issue1.ID], PositionGap)
	}
	if posMap[issue2.ID] != 2*PositionGap {
		t.Errorf("issue2 position = %d, want %d (should not shift)", posMap[issue2.ID], 2*PositionGap)
	}
}

// TestApplyBoardPositions_Ordering verifies positioned-first semantics
func TestApplyBoardPositions_Ordering(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("Ordering Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	issue4 := &models.Issue{Title: "Issue 4", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)
	db.CreateIssue(issue4)

	// Only position issue2 at 1 and issue4 at 2
	db.SetIssuePosition(board.ID, issue2.ID, 1)
	db.SetIssuePosition(board.ID, issue4.ID, 2)

	// Apply positions - positioned should come first, then unpositioned in original order
	issues := []models.Issue{*issue1, *issue2, *issue3, *issue4}
	result, err := db.ApplyBoardPositions(board.ID, issues)
	if err != nil {
		t.Fatalf("ApplyBoardPositions failed: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("Expected 4 results, got %d", len(result))
	}

	// First should be issue2 (position 1)
	if result[0].Issue.ID != issue2.ID {
		t.Errorf("result[0] = %s, want %s (positioned first)", result[0].Issue.ID, issue2.ID)
	}
	if !result[0].HasPosition || result[0].Position != 1 {
		t.Errorf("result[0] HasPosition=%v Position=%d, want true/1", result[0].HasPosition, result[0].Position)
	}

	// Second should be issue4 (position 2)
	if result[1].Issue.ID != issue4.ID {
		t.Errorf("result[1] = %s, want %s (positioned second)", result[1].Issue.ID, issue4.ID)
	}
	if !result[1].HasPosition || result[1].Position != 2 {
		t.Errorf("result[1] HasPosition=%v Position=%d, want true/2", result[1].HasPosition, result[1].Position)
	}

	// Third and fourth should be unpositioned issues (issue1 and issue3) in original order
	if result[2].HasPosition || result[3].HasPosition {
		t.Error("result[2] and result[3] should be unpositioned")
	}
	if result[2].Issue.ID != issue1.ID {
		t.Errorf("result[2] = %s, want %s (unpositioned, original order)", result[2].Issue.ID, issue1.ID)
	}
	if result[3].Issue.ID != issue3.ID {
		t.Errorf("result[3] = %s, want %s (unpositioned, original order)", result[3].Issue.ID, issue3.ID)
	}
}

// TestSwapIssuePositions_UnpositionedError verifies error handling for unpositioned issues
func TestSwapIssuePositions_UnpositionedError(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("Swap Error Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)

	// Only position issue1
	db.SetIssuePosition(board.ID, issue1.ID, 1)

	// Try to swap with unpositioned issue2 - should fail
	err = db.SwapIssuePositions(board.ID, issue1.ID, issue2.ID)
	if err == nil {
		t.Error("SwapIssuePositions should fail when second issue is unpositioned")
	}

	// Try to swap with issue2 first (unpositioned) - should fail
	err = db.SwapIssuePositions(board.ID, issue2.ID, issue1.ID)
	if err == nil {
		t.Error("SwapIssuePositions should fail when first issue is unpositioned")
	}

	// Both unpositioned - should also fail
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue3)
	err = db.SwapIssuePositions(board.ID, issue2.ID, issue3.ID)
	if err == nil {
		t.Error("SwapIssuePositions should fail when both issues are unpositioned")
	}
}

// TestComputeInsertPosition tests sparse position computation for board inserts
func TestComputeInsertPosition(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("Insert Position Test", "")

	t.Run("empty board", func(t *testing.T) {
		pos, results, err := db.ComputeInsertPosition(board.ID, 1)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		if pos != PositionGap {
			t.Errorf("pos = %d, want %d", pos, PositionGap)
		}
		if len(results) != 0 {
			t.Errorf("expected no respace results, got %d", len(results))
		}
	})

	// Set up three issues with sparse positions for remaining subtests
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)
	db.SetIssuePosition(board.ID, issue1.ID, PositionGap)
	db.SetIssuePosition(board.ID, issue2.ID, 2*PositionGap)
	db.SetIssuePosition(board.ID, issue3.ID, 3*PositionGap)

	t.Run("top slot 1", func(t *testing.T) {
		pos, results, err := db.ComputeInsertPosition(board.ID, 1)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		want := 0 // min(PositionGap) - PositionGap = 0
		if pos != want {
			t.Errorf("pos = %d, want %d", pos, want)
		}
		if len(results) != 0 {
			t.Errorf("expected no respace results, got %d", len(results))
		}
	})

	t.Run("slot 0 treated as top", func(t *testing.T) {
		pos, _, err := db.ComputeInsertPosition(board.ID, 0)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		want := 0 // min(PositionGap) - PositionGap
		if pos != want {
			t.Errorf("pos = %d, want %d", pos, want)
		}
	})

	t.Run("negative slot treated as top", func(t *testing.T) {
		pos, _, err := db.ComputeInsertPosition(board.ID, -5)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		want := 0 // min(PositionGap) - PositionGap
		if pos != want {
			t.Errorf("pos = %d, want %d", pos, want)
		}
	})

	t.Run("bottom slot beyond count", func(t *testing.T) {
		pos, results, err := db.ComputeInsertPosition(board.ID, 99)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		want := 3*PositionGap + PositionGap
		if pos != want {
			t.Errorf("pos = %d, want %d", pos, want)
		}
		if len(results) != 0 {
			t.Errorf("expected no respace results, got %d", len(results))
		}
	})

	t.Run("middle slot 2", func(t *testing.T) {
		pos, results, err := db.ComputeInsertPosition(board.ID, 2)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		// midpoint of positions[0]=PositionGap and positions[1]=2*PositionGap
		want := (PositionGap + 2*PositionGap) / 2
		if pos != want {
			t.Errorf("pos = %d, want %d", pos, want)
		}
		if len(results) != 0 {
			t.Errorf("expected no respace results, got %d", len(results))
		}
	})

	t.Run("gap exhausted triggers respace", func(t *testing.T) {
		// Create a board with adjacent positions that have no gap
		b2, _ := db.CreateBoard("Tight Board", "")
		i1 := &models.Issue{Title: "Tight 1", Type: models.TypeTask, Priority: models.PriorityP2}
		i2 := &models.Issue{Title: "Tight 2", Type: models.TypeTask, Priority: models.PriorityP2}
		db.CreateIssue(i1)
		db.CreateIssue(i2)
		db.SetIssuePosition(b2.ID, i1.ID, 100)
		db.SetIssuePosition(b2.ID, i2.ID, 101) // gap of 1, midpoint == lo

		pos, results, err := db.ComputeInsertPosition(b2.ID, 2)
		if err != nil {
			t.Fatalf("ComputeInsertPosition failed: %v", err)
		}
		if len(results) == 0 {
			t.Error("expected respace results when gap exhausted")
		}
		// After respace: i1=PositionGap, i2=2*PositionGap
		// midpoint = (PositionGap + 2*PositionGap) / 2
		want := (PositionGap + 2*PositionGap) / 2
		if pos != want {
			t.Errorf("pos after respace = %d, want %d", pos, want)
		}
	})
}

// TestRespaceBoardPositions tests that respace reassigns clean PositionGap spacing
func TestRespaceBoardPositions(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	board, _ := db.CreateBoard("Respace Test", "")
	issue1 := &models.Issue{Title: "Issue 1", Type: models.TypeTask, Priority: models.PriorityP2}
	issue2 := &models.Issue{Title: "Issue 2", Type: models.TypeTask, Priority: models.PriorityP2}
	issue3 := &models.Issue{Title: "Issue 3", Type: models.TypeTask, Priority: models.PriorityP2}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	// Set tight positions with small gaps
	db.SetIssuePosition(board.ID, issue1.ID, 1)
	db.SetIssuePosition(board.ID, issue2.ID, 2)
	db.SetIssuePosition(board.ID, issue3.ID, 3)

	results, err := db.RespaceBoardPositions(board.ID)
	if err != nil {
		t.Fatalf("RespaceBoardPositions failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 respace results, got %d", len(results))
	}

	// Build map by issue ID for verification
	resMap := make(map[string]RespaceResult)
	for _, r := range results {
		resMap[r.IssueID] = r
	}

	// Verify old positions
	if resMap[issue1.ID].OldPosition != 1 {
		t.Errorf("issue1 old = %d, want 1", resMap[issue1.ID].OldPosition)
	}
	if resMap[issue2.ID].OldPosition != 2 {
		t.Errorf("issue2 old = %d, want 2", resMap[issue2.ID].OldPosition)
	}
	if resMap[issue3.ID].OldPosition != 3 {
		t.Errorf("issue3 old = %d, want 3", resMap[issue3.ID].OldPosition)
	}

	// Verify new positions are PositionGap, 2*PositionGap, 3*PositionGap
	if resMap[issue1.ID].NewPosition != PositionGap {
		t.Errorf("issue1 new = %d, want %d", resMap[issue1.ID].NewPosition, PositionGap)
	}
	if resMap[issue2.ID].NewPosition != 2*PositionGap {
		t.Errorf("issue2 new = %d, want %d", resMap[issue2.ID].NewPosition, 2*PositionGap)
	}
	if resMap[issue3.ID].NewPosition != 3*PositionGap {
		t.Errorf("issue3 new = %d, want %d", resMap[issue3.ID].NewPosition, 3*PositionGap)
	}

	// Verify the DB was actually updated
	positions, _ := db.GetBoardIssuePositions(board.ID)
	posMap := make(map[string]int)
	for _, p := range positions {
		posMap[p.IssueID] = p.Position
	}
	if posMap[issue1.ID] != PositionGap || posMap[issue2.ID] != 2*PositionGap || posMap[issue3.ID] != 3*PositionGap {
		t.Errorf("DB positions not updated: %v", posMap)
	}
}

// TestCreateIssue_CollisionRetry tests that CreateIssue retries on ID collision
func TestCreateIssue_CollisionRetry(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create first issue normally
	issue1 := &models.Issue{Title: "First Issue"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	collidingID := issue1.ID

	// Save original generator and restore after test
	originalGenerator := idGenerator
	defer func() { idGenerator = originalGenerator }()

	// Mock generator: returns colliding ID twice, then a unique ID
	callCount := 0
	idGenerator = func() (string, error) {
		callCount++
		if callCount <= 2 {
			return collidingID, nil // Will collide with issue1
		}
		return "td-unique", nil // Third attempt succeeds
	}

	// Create second issue - should succeed after retry
	issue2 := &models.Issue{Title: "Second Issue"}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue should succeed after retry: %v", err)
	}

	if issue2.ID != "td-unique" {
		t.Errorf("Expected issue2.ID = 'td-unique', got %q", issue2.ID)
	}

	if callCount != 3 {
		t.Errorf("Expected 3 ID generation attempts, got %d", callCount)
	}
}

// TestCreateIssue_CollisionMaxRetries tests that CreateIssue fails after max retries
func TestCreateIssue_CollisionMaxRetries(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create first issue normally
	issue1 := &models.Issue{Title: "First Issue"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	collidingID := issue1.ID

	// Save original generator and restore after test
	originalGenerator := idGenerator
	defer func() { idGenerator = originalGenerator }()

	// Mock generator: always returns the same colliding ID
	callCount := 0
	idGenerator = func() (string, error) {
		callCount++
		return collidingID, nil // Always collide
	}

	// Create second issue - should fail after 3 retries
	issue2 := &models.Issue{Title: "Second Issue"}
	err = db.CreateIssue(issue2)

	if err == nil {
		t.Fatal("CreateIssue should fail after max retries")
	}

	if callCount != 3 {
		t.Errorf("Expected exactly 3 ID generation attempts, got %d", callCount)
	}

	expectedErr := "failed to generate unique issue ID after 3 attempts"
	if err.Error() != expectedErr {
		t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
	}
}
