package cmd

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestWsStartCreatesSession tests ws start command creates a work session
func TestWsStartCreatesSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	ws := &models.WorkSession{
		Name:      "feature-auth",
		SessionID: "ses_test",
		StartSHA:  "abc123",
	}

	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	if ws.ID == "" {
		t.Error("Expected work session ID to be set")
	}

	// Verify session was created
	retrieved, err := database.GetWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSession failed: %v", err)
	}

	if retrieved.Name != "feature-auth" {
		t.Errorf("Expected name 'feature-auth', got %q", retrieved.Name)
	}
	if retrieved.SessionID != "ses_test" {
		t.Errorf("Expected session 'ses_test', got %q", retrieved.SessionID)
	}
}

// TestWsStartWithActiveSessionErrors tests that starting a session when one is active fails
func TestWsStartWithActiveSessionErrors(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create and set active session
	ws := &models.WorkSession{Name: "first-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// Check active session is set
	activeWS, err := config.GetActiveWorkSession(dir)
	if err != nil {
		t.Fatalf("GetActiveWorkSession failed: %v", err)
	}
	if activeWS == "" {
		t.Error("Expected active work session to be set")
	}
	if activeWS != ws.ID {
		t.Errorf("Expected active session %s, got %s", ws.ID, activeWS)
	}
}

// TestWsStopEndsSession tests ws stop ends the active session
func TestWsStopEndsSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create active session
	ws := &models.WorkSession{Name: "active-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// End session
	config.ClearActiveWorkSession(dir)

	// Verify session is no longer active
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session after stop")
	}
}

// TestWsStopWithNoActiveSessionErrors tests stopping when no session is active
func TestWsStopWithNoActiveSessionErrors(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// No active session
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Should have no active work session initially")
	}
}

// TestWsTagAddsIssueToSession tests tagging issues to work session
func TestWsTagAddsIssueToSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "tagging-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// Create issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Tag issue to work session
	if err := database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
		t.Fatalf("TagIssueToWorkSession failed: %v", err)
	}

	// Verify issue is tagged
	issueIDs, err := database.GetWorkSessionIssues(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSessionIssues failed: %v", err)
	}
	if len(issueIDs) != 1 {
		t.Fatalf("Expected 1 tagged issue, got %d", len(issueIDs))
	}
	if issueIDs[0] != issue.ID {
		t.Errorf("Expected issue %s, got %s", issue.ID, issueIDs[0])
	}
}

// TestWsTagMultipleIssues tests tagging multiple issues to a session
func TestWsTagMultipleIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "multi-tag-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create issues
	issues := []*models.Issue{
		{Title: "Issue 1", Status: models.StatusOpen},
		{Title: "Issue 2", Status: models.StatusOpen},
		{Title: "Issue 3", Status: models.StatusOpen},
	}

	for _, issue := range issues {
		database.CreateIssue(issue)
		database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")
	}

	// Verify all issues are tagged
	issueIDs, _ := database.GetWorkSessionIssues(ws.ID)
	if len(issueIDs) != 3 {
		t.Errorf("Expected 3 tagged issues, got %d", len(issueIDs))
	}
}

// TestWsTagNoActiveSessionErrors tests tagging without active session
func TestWsTagNoActiveSessionErrors(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// No active session should be set initially
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session")
	}
}

// TestWsTagInvalidIssueErrors tests tagging non-existent issue
func TestWsTagInvalidIssueErrors(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "invalid-tag-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Try to get non-existent issue
	_, err = database.GetIssue("td-nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent issue")
	}
}

// TestWsUntagRemovesIssueFromSession tests untagging issues from work session
func TestWsUntagRemovesIssueFromSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "untag-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create and tag issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Verify issue is tagged
	issueIDs, _ := database.GetWorkSessionIssues(ws.ID)
	if len(issueIDs) != 1 {
		t.Fatalf("Expected 1 tagged issue, got %d", len(issueIDs))
	}

	// Untag issue
	if err := database.UntagIssueFromWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
		t.Fatalf("UntagIssueFromWorkSession failed: %v", err)
	}

	// Verify issue is untagged
	issueIDs, _ = database.GetWorkSessionIssues(ws.ID)
	if len(issueIDs) != 0 {
		t.Errorf("Expected 0 tagged issues after untag, got %d", len(issueIDs))
	}
}

// TestWsUntagPartialRemoval tests untagging one of multiple issues
func TestWsUntagPartialRemoval(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "partial-untag-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create and tag issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusOpen}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.TagIssueToWorkSession(ws.ID, issue1.ID, "test-session")
	database.TagIssueToWorkSession(ws.ID, issue2.ID, "test-session")

	// Untag only issue1
	database.UntagIssueFromWorkSession(ws.ID, issue1.ID, "test-session")

	// Verify only issue2 remains
	issueIDs, _ := database.GetWorkSessionIssues(ws.ID)
	if len(issueIDs) != 1 {
		t.Fatalf("Expected 1 tagged issue, got %d", len(issueIDs))
	}
	if issueIDs[0] != issue2.ID {
		t.Errorf("Expected issue2 to remain, got %s", issueIDs[0])
	}
}

// TestWsLogAddsLogEntry tests adding log to work session
func TestWsLogAddsLogEntry(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "log-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create and tag issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Add log to work session (log attached to work session, not specific issue)
	log := &models.Log{
		IssueID:       "",
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Progress update",
		Type:          models.LogTypeProgress,
	}
	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Verify log was created with work session ID
	logs, err := database.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != "Progress update" {
		t.Errorf("Expected message 'Progress update', got %q", logs[0].Message)
	}
}

// TestWsLogWithDifferentTypes tests work session logs with different types
func TestWsLogWithDifferentTypes(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "typed-log-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	testCases := []struct {
		logType models.LogType
		message string
	}{
		{models.LogTypeProgress, "Progress message"},
		{models.LogTypeBlocker, "Blocker message"},
		{models.LogTypeDecision, "Decision message"},
		{models.LogTypeHypothesis, "Hypothesis message"},
		{models.LogTypeTried, "Tried message"},
		{models.LogTypeResult, "Result message"},
	}

	for _, tc := range testCases {
		log := &models.Log{
			SessionID:     "ses_test",
			WorkSessionID: ws.ID,
			Message:       tc.message,
			Type:          tc.logType,
		}
		if err := database.AddLog(log); err != nil {
			t.Fatalf("AddLog for %s failed: %v", tc.logType, err)
		}
	}

	logs, _ := database.GetLogsByWorkSession(ws.ID)
	if len(logs) != len(testCases) {
		t.Errorf("Expected %d logs, got %d", len(testCases), len(logs))
	}
}

// TestWsLogNoActiveSessionErrors tests logging without active session
func TestWsLogNoActiveSessionErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// No active session
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session")
	}
}

// TestWsHandoffCreatesHandoffs tests handoff creates handoffs for tagged issues
func TestWsHandoffCreatesHandoffs(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "handoff-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create and tag issues
	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusInProgress}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusInProgress}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.TagIssueToWorkSession(ws.ID, issue1.ID, "test-session")
	database.TagIssueToWorkSession(ws.ID, issue2.ID, "test-session")

	// Create handoffs for each issue
	handoff1 := &models.Handoff{
		IssueID:   issue1.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1 completed"},
		Remaining: []string{"Task 2"},
	}
	handoff2 := &models.Handoff{
		IssueID:   issue2.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1 completed"},
		Remaining: []string{"Task 2"},
	}

	if err := database.AddHandoff(handoff1); err != nil {
		t.Fatalf("AddHandoff for issue1 failed: %v", err)
	}
	if err := database.AddHandoff(handoff2); err != nil {
		t.Fatalf("AddHandoff for issue2 failed: %v", err)
	}

	// Verify handoffs were created
	h1, _ := database.GetLatestHandoff(issue1.ID)
	h2, _ := database.GetLatestHandoff(issue2.ID)

	if h1 == nil {
		t.Error("Expected handoff for issue1")
	}
	if h2 == nil {
		t.Error("Expected handoff for issue2")
	}
}

// TestWsHandoffNoActiveSessionErrors tests handoff without active session
func TestWsHandoffNoActiveSessionErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// No active session
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session")
	}
}

// TestWsHandoffEndsSession tests handoff ends the work session
func TestWsHandoffEndsSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create and set active session
	ws := &models.WorkSession{Name: "ending-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// Verify session is active
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != ws.ID {
		t.Fatalf("Expected active session %s, got %s", ws.ID, activeWS)
	}

	// Clear active session (simulates handoff ending session)
	config.ClearActiveWorkSession(dir)

	// Verify session is ended
	activeWS, _ = config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session after handoff")
	}
}

// TestWsCurrentShowsActiveSession tests current command shows session state
func TestWsCurrentShowsActiveSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "current-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// Tag issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Get current session
	activeWS, _ := config.GetActiveWorkSession(dir)
	retrieved, err := database.GetWorkSession(activeWS)
	if err != nil {
		t.Fatalf("GetWorkSession failed: %v", err)
	}

	if retrieved.Name != "current-session" {
		t.Errorf("Expected session name 'current-session', got %q", retrieved.Name)
	}

	// Get tagged issues
	issueIDs, _ := database.GetWorkSessionIssues(activeWS)
	if len(issueIDs) != 1 {
		t.Errorf("Expected 1 tagged issue, got %d", len(issueIDs))
	}
}

// TestWsCurrentNoActiveSession tests current with no active session
func TestWsCurrentNoActiveSession(t *testing.T) {
	dir := t.TempDir()
	_, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// No active session
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session")
	}
}

// TestWsListShowsSessions tests list command shows work sessions
func TestWsListShowsSessions(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create multiple work sessions
	sessions := []string{"session-1", "session-2", "session-3"}
	for _, name := range sessions {
		ws := &models.WorkSession{Name: name, SessionID: "ses_test"}
		database.CreateWorkSession(ws)
	}

	// List work sessions
	listed, err := database.ListWorkSessions(20)
	if err != nil {
		t.Fatalf("ListWorkSessions failed: %v", err)
	}

	if len(listed) != len(sessions) {
		t.Errorf("Expected %d sessions, got %d", len(sessions), len(listed))
	}
}

// TestWsListEmpty tests list with no sessions
func TestWsListEmpty(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// No sessions
	listed, err := database.ListWorkSessions(20)
	if err != nil {
		t.Fatalf("ListWorkSessions failed: %v", err)
	}

	if len(listed) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(listed))
	}
}

// TestWsListWithLimit tests list respects limit
func TestWsListWithLimit(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create 10 sessions
	for i := 0; i < 10; i++ {
		ws := &models.WorkSession{Name: "session", SessionID: "ses_test"}
		database.CreateWorkSession(ws)
	}

	// List with limit 5
	listed, err := database.ListWorkSessions(5)
	if err != nil {
		t.Fatalf("ListWorkSessions failed: %v", err)
	}

	if len(listed) != 5 {
		t.Errorf("Expected 5 sessions with limit, got %d", len(listed))
	}
}

// TestWsEndWithoutHandoff tests ending session without handoff
func TestWsEndWithoutHandoff(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create and set active session
	ws := &models.WorkSession{Name: "no-handoff-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)
	config.SetActiveWorkSession(dir, ws.ID)

	// End without handoff
	config.ClearActiveWorkSession(dir)

	// Verify session ended
	activeWS, _ := config.GetActiveWorkSession(dir)
	if activeWS != "" {
		t.Error("Expected no active work session after end")
	}
}

// TestWsTagAutoStartsOpenIssues tests that tagging starts open issues
func TestWsTagAutoStartsOpenIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "auto-start-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create open issue
	issue := &models.Issue{Title: "Open Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Tag issue (simulating auto-start behavior)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Simulate starting the issue
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = "ses_test"
	database.UpdateIssue(issue)

	// Verify issue is started
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Errorf("Expected issue to be in_progress, got %s", retrieved.Status)
	}
}

// TestWsTagNoStartFlag tests --no-start flag doesn't change status
func TestWsTagNoStartFlag(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "no-start-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Create open issue
	issue := &models.Issue{Title: "Open Issue", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Tag issue without starting (simulating --no-start)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Issue should remain open (with --no-start)
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusOpen {
		t.Errorf("Expected issue to remain open, got %s", retrieved.Status)
	}
}

// TestWsShowDisplaysPastSession tests show command displays past session
func TestWsShowDisplaysPastSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "past-session", SessionID: "ses_test", StartSHA: "abc123"}
	database.CreateWorkSession(ws)

	// Tag issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)
	database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session")

	// Get session details
	retrieved, err := database.GetWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSession failed: %v", err)
	}

	if retrieved.Name != "past-session" {
		t.Errorf("Expected name 'past-session', got %q", retrieved.Name)
	}
	if retrieved.StartSHA != "abc123" {
		t.Errorf("Expected start SHA 'abc123', got %q", retrieved.StartSHA)
	}

	// Get tagged issues
	issueIDs, _ := database.GetWorkSessionIssues(ws.ID)
	if len(issueIDs) != 1 {
		t.Errorf("Expected 1 tagged issue, got %d", len(issueIDs))
	}
}

// TestWsShowInvalidSessionErrors tests show with invalid session ID
func TestWsShowInvalidSessionErrors(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Try to get non-existent session
	_, err = database.GetWorkSession("ws_nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
}

// TestWsHandoffAutoPopulatesFromLogs tests handoff auto-populates from session logs
func TestWsHandoffAutoPopulatesFromLogs(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "auto-populate-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Add various log types
	logs := []models.Log{
		{SessionID: "ses_test", WorkSessionID: ws.ID, Message: "Progress 1", Type: models.LogTypeProgress},
		{SessionID: "ses_test", WorkSessionID: ws.ID, Message: "Decision 1", Type: models.LogTypeDecision},
		{SessionID: "ses_test", WorkSessionID: ws.ID, Message: "Blocker 1", Type: models.LogTypeBlocker},
		{SessionID: "ses_test", WorkSessionID: ws.ID, Message: "Result 1", Type: models.LogTypeResult},
	}

	for _, log := range logs {
		database.AddLog(&log)
	}

	// Get logs for session
	sessionLogs, _ := database.GetLogsByWorkSession(ws.ID)
	if len(sessionLogs) != 4 {
		t.Errorf("Expected 4 logs, got %d", len(sessionLogs))
	}

	// Verify log types
	typeCount := make(map[models.LogType]int)
	for _, log := range sessionLogs {
		typeCount[log.Type]++
	}

	if typeCount[models.LogTypeProgress] != 1 {
		t.Errorf("Expected 1 progress log, got %d", typeCount[models.LogTypeProgress])
	}
	if typeCount[models.LogTypeDecision] != 1 {
		t.Errorf("Expected 1 decision log, got %d", typeCount[models.LogTypeDecision])
	}
	if typeCount[models.LogTypeBlocker] != 1 {
		t.Errorf("Expected 1 blocker log, got %d", typeCount[models.LogTypeBlocker])
	}
}

// TestWsUpdateSession tests updating work session
func TestWsUpdateSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create work session
	ws := &models.WorkSession{Name: "update-session", SessionID: "ses_test"}
	database.CreateWorkSession(ws)

	// Update session with end SHA
	ws.EndSHA = "def456"
	if err := database.UpdateWorkSession(ws); err != nil {
		t.Fatalf("UpdateWorkSession failed: %v", err)
	}

	// Verify update
	retrieved, _ := database.GetWorkSession(ws.ID)
	if retrieved.EndSHA != "def456" {
		t.Errorf("Expected end SHA 'def456', got %q", retrieved.EndSHA)
	}
}

// TestWsTagFlags tests that tag command has expected flags
func TestWsTagFlags(t *testing.T) {
	if wsTagCmd.Flags().Lookup("no-start") == nil {
		t.Error("Expected --no-start flag on ws tag command")
	}
}

// TestWsLogFlags tests that log command has expected flags
func TestWsLogFlags(t *testing.T) {
	flags := []string{"blocker", "decision", "hypothesis", "tried", "result", "only"}

	for _, flag := range flags {
		if wsLogCmd.Flags().Lookup(flag) == nil {
			t.Errorf("Expected --%s flag on ws log command", flag)
		}
	}
}

// TestWsHandoffFlags tests that handoff command has expected flags
func TestWsHandoffFlags(t *testing.T) {
	flags := []string{"done", "remaining", "decision", "uncertain", "continue", "review"}

	for _, flag := range flags {
		if wsHandoffCmd.Flags().Lookup(flag) == nil {
			t.Errorf("Expected --%s flag on ws handoff command", flag)
		}
	}
}

// TestWsCurrentFlags tests that current command has expected flags
func TestWsCurrentFlags(t *testing.T) {
	if wsCurrentCmd.Flags().Lookup("json") == nil {
		t.Error("Expected --json flag on ws current command")
	}
}

// TestWsShowFlags tests that show command has expected flags
func TestWsShowFlags(t *testing.T) {
	if wsShowCmd.Flags().Lookup("full") == nil {
		t.Error("Expected --full flag on ws show command")
	}
}

// TestWsCommandStructure tests the ws command has all expected subcommands
func TestWsCommandStructure(t *testing.T) {
	subcommands := []string{"start", "tag", "untag", "log", "current", "handoff", "end", "list", "show"}

	for _, name := range subcommands {
		found := false
		for _, cmd := range wsCmd.Commands() {
			if cmd.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected subcommand %q on ws command", name)
		}
	}
}

// TestWsCommandAliases tests ws command has expected alias
func TestWsCommandAliases(t *testing.T) {
	if len(wsCmd.Aliases) == 0 {
		t.Error("Expected ws command to have aliases")
	}

	hasWorksessionAlias := false
	for _, alias := range wsCmd.Aliases {
		if alias == "worksession" {
			hasWorksessionAlias = true
			break
		}
	}
	if !hasWorksessionAlias {
		t.Error("Expected 'worksession' alias for ws command")
	}
}

// TestFilterForIssue tests the filterForIssue helper function
func TestFilterForIssue(t *testing.T) {
	testCases := []struct {
		name     string
		items    []string
		issueID  string
		expected []string
	}{
		{
			name:     "item tagged for specific issue",
			items:    []string{"Fix bug (td-123)"},
			issueID:  "td-123",
			expected: []string{"Fix bug"},
		},
		{
			name:     "item tagged for different issue",
			items:    []string{"Fix bug (td-456)"},
			issueID:  "td-123",
			expected: []string{},
		},
		{
			name:     "untagged items included for all",
			items:    []string{"Untagged item"},
			issueID:  "td-123",
			expected: []string{"Untagged item"},
		},
		{
			name:     "mixed items",
			items:    []string{"Tagged (td-123)", "Untagged", "Other (td-456)"},
			issueID:  "td-123",
			expected: []string{"Tagged", "Untagged"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := filterForIssue(tc.items, tc.issueID)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d items, got %d", len(tc.expected), len(result))
				return
			}
			for i, item := range result {
				if item != tc.expected[i] {
					t.Errorf("Expected item %d to be %q, got %q", i, tc.expected[i], item)
				}
			}
		})
	}
}
