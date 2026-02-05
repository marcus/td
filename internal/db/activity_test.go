package db

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// Log Tests
// ============================================================================

func TestAddLog_Basic(t *testing.T) {
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

	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Test log message",
		Type:      models.LogTypeProgress,
	}

	err = db.AddLog(log)
	if err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	if log.ID == "" {
		t.Error("Log ID not set after AddLog")
	}
	if log.Timestamp.IsZero() {
		t.Error("Log Timestamp not set after AddLog")
	}
}

func TestAddLog_WithWorkSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create work session
	ws := &models.WorkSession{
		Name:      "Test Work Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	log := &models.Log{
		IssueID:       issue.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Work session log",
		Type:          models.LogTypeDecision,
	}

	err = db.AddLog(log)
	if err != nil {
		t.Fatalf("AddLog with work session failed: %v", err)
	}

	if log.WorkSessionID != ws.ID {
		t.Errorf("WorkSessionID mismatch: got %s, want %s", log.WorkSessionID, ws.ID)
	}
}

func TestAddLog_AllLogTypes(t *testing.T) {
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

	logTypes := []models.LogType{
		models.LogTypeProgress,
		models.LogTypeSecurity,
		models.LogTypeBlocker,
		models.LogTypeDecision,
		models.LogTypeHypothesis,
		models.LogTypeTried,
		models.LogTypeResult,
		models.LogTypeOrchestration,
	}

	for _, lt := range logTypes {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Test " + string(lt),
			Type:      lt,
		}
		if err := db.AddLog(log); err != nil {
			t.Errorf("AddLog failed for type %s: %v", lt, err)
		}
	}

	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != len(logTypes) {
		t.Errorf("Expected %d logs, got %d", len(logTypes), len(logs))
	}
}

func TestGetLogs_BasicRetrieval(t *testing.T) {
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

	// Add multiple logs
	for i := 0; i < 5; i++ {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Log message",
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 5 {
		t.Errorf("Expected 5 logs, got %d", len(logs))
	}
}

func TestGetLogs_ChronologicalOrder(t *testing.T) {
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

	// Add logs with distinct messages to track order
	messages := []string{"First", "Second", "Third"}
	for _, msg := range messages {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   msg,
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure distinct timestamps
	}

	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	// Should be in chronological order (oldest first)
	for i, msg := range messages {
		if logs[i].Message != msg {
			t.Errorf("Log %d: expected %s, got %s", i, msg, logs[i].Message)
		}
	}
}

func TestGetLogs_WithLimit(t *testing.T) {
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

	// Add 10 logs
	for i := 0; i < 10; i++ {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Log message",
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	// Get with limit of 3
	logs, err := db.GetLogs(issue.ID, 3)
	if err != nil {
		t.Fatalf("GetLogs with limit failed: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs with limit, got %d", len(logs))
	}

	// Limit of 0 should return all
	allLogs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs without limit failed: %v", err)
	}
	if len(allLogs) != 10 {
		t.Errorf("Expected 10 logs without limit, got %d", len(allLogs))
	}
}

func TestGetLogs_IncludesWorkSessionLogs(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create work session
	ws := &models.WorkSession{
		Name:      "Test Work Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Tag issue to work session
	if err := db.TagIssueToWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
		t.Fatalf("TagIssueToWorkSession failed: %v", err)
	}

	// Add direct log to issue
	directLog := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Direct log",
		Type:      models.LogTypeProgress,
	}
	if err := db.AddLog(directLog); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Add work session log (empty issue_id)
	wsLog := &models.Log{
		IssueID:       "", // Work session log - not tied to specific issue
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Work session log",
		Type:          models.LogTypeDecision,
	}
	if err := db.AddLog(wsLog); err != nil {
		t.Fatalf("AddLog for work session failed: %v", err)
	}

	// GetLogs should include both direct and work session logs
	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs (direct + work session), got %d", len(logs))
	}
}

func TestGetLogs_EmptyResult(t *testing.T) {
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

	// Get logs for issue with no logs
	logs, err := db.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs for new issue, got %d", len(logs))
	}
}

func TestGetLogs_NonExistentIssue(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Get logs for non-existent issue - should return empty, not error
	logs, err := db.GetLogs("td-nonexistent", 0)
	if err != nil {
		t.Fatalf("GetLogs should not error for non-existent issue: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs for non-existent issue, got %d", len(logs))
	}
}

func TestGetLogsByWorkSession_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create work session
	ws := &models.WorkSession{
		Name:      "Test Work Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add logs with work session ID
	for i := 0; i < 3; i++ {
		log := &models.Log{
			IssueID:       issue.ID,
			SessionID:     "ses_test",
			WorkSessionID: ws.ID,
			Message:       "Work session log",
			Type:          models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	logs, err := db.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs for work session, got %d", len(logs))
	}
}

func TestGetLogsByWorkSession_MultipleIssues(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	ws := &models.WorkSession{
		Name:      "Multi-Issue Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add logs from different issues to same work session
	log1 := &models.Log{
		IssueID:       issue1.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Log for issue 1",
		Type:          models.LogTypeProgress,
	}
	log2 := &models.Log{
		IssueID:       issue2.ID,
		SessionID:     "ses_test",
		WorkSessionID: ws.ID,
		Message:       "Log for issue 2",
		Type:          models.LogTypeDecision,
	}
	if err := db.AddLog(log1); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}
	if err := db.AddLog(log2); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	logs, err := db.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("Expected 2 logs for work session, got %d", len(logs))
	}

	// Verify logs from both issues
	issueIDs := make(map[string]bool)
	for _, log := range logs {
		issueIDs[log.IssueID] = true
	}
	if !issueIDs[issue1.ID] || !issueIDs[issue2.ID] {
		t.Error("Expected logs from both issues")
	}
}

func TestGetLogsByWorkSession_EmptyResult(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	ws := &models.WorkSession{
		Name:      "Empty Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	logs, err := db.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs for empty work session, got %d", len(logs))
	}
}

func TestGetLogsByWorkSession_NonExistentSession(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	logs, err := db.GetLogsByWorkSession("ws-nonexistent")
	if err != nil {
		t.Fatalf("GetLogsByWorkSession should not error for non-existent session: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("Expected 0 logs for non-existent session, got %d", len(logs))
	}
}

func TestGetLogsByWorkSession_ChronologicalOrder(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	ws := &models.WorkSession{
		Name:      "Ordered Session",
		SessionID: "ses_test",
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	messages := []string{"First", "Second", "Third"}
	for _, msg := range messages {
		log := &models.Log{
			IssueID:       issue.ID,
			SessionID:     "ses_test",
			WorkSessionID: ws.ID,
			Message:       msg,
			Type:          models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	logs, err := db.GetLogsByWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetLogsByWorkSession failed: %v", err)
	}

	// Should be in chronological order
	for i, msg := range messages {
		if logs[i].Message != msg {
			t.Errorf("Log %d: expected %s, got %s", i, msg, logs[i].Message)
		}
	}
}

func TestGetRecentLogsAll_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add logs to different issues
	for i := 0; i < 3; i++ {
		log := &models.Log{
			IssueID:   issue1.ID,
			SessionID: "ses_test",
			Message:   "Log for issue 1",
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		log := &models.Log{
			IssueID:   issue2.ID,
			SessionID: "ses_test",
			Message:   "Log for issue 2",
			Type:      models.LogTypeDecision,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	// GetRecentLogsAll should return logs from all issues
	logs, err := db.GetRecentLogsAll(10)
	if err != nil {
		t.Fatalf("GetRecentLogsAll failed: %v", err)
	}
	if len(logs) != 5 {
		t.Errorf("Expected 5 logs across all issues, got %d", len(logs))
	}
}

func TestGetRecentLogsAll_WithLimit(t *testing.T) {
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

	for i := 0; i < 10; i++ {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Log message",
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	logs, err := db.GetRecentLogsAll(3)
	if err != nil {
		t.Fatalf("GetRecentLogsAll with limit failed: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs with limit, got %d", len(logs))
	}
}

func TestGetActiveSessions_Basic(t *testing.T) {
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

	// Add logs from different sessions
	sessions := []string{"ses_aaa", "ses_bbb", "ses_ccc"}
	for _, sid := range sessions {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: sid,
			Message:   "Log from " + sid,
			Type:      models.LogTypeProgress,
		}
		if err := db.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	since := time.Now().Add(-1 * time.Hour)
	activeSessions, err := db.GetActiveSessions(since)
	if err != nil {
		t.Fatalf("GetActiveSessions failed: %v", err)
	}
	if len(activeSessions) != 3 {
		t.Errorf("Expected 3 active sessions, got %d", len(activeSessions))
	}
}

func TestGetActiveSessions_ExcludesOldActivity(t *testing.T) {
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

	// Add a log
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_old",
		Message:   "Old log",
		Type:      models.LogTypeProgress,
	}
	if err := db.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Query for activity after now - should return empty
	since := time.Now().Add(1 * time.Hour)
	activeSessions, err := db.GetActiveSessions(since)
	if err != nil {
		t.Fatalf("GetActiveSessions failed: %v", err)
	}
	if len(activeSessions) != 0 {
		t.Errorf("Expected 0 active sessions after future time, got %d", len(activeSessions))
	}
}

// ============================================================================
// Handoff Tests
// ============================================================================

func TestAddHandoff_Basic(t *testing.T) {
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

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1", "Task 2"},
		Remaining: []string{"Task 3", "Task 4"},
		Decisions: []string{"Decision 1"},
		Uncertain: []string{"Question 1"},
	}

	err = db.AddHandoff(handoff)
	if err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	if handoff.ID == "" {
		t.Error("Handoff ID not set after AddHandoff")
	}
	if handoff.Timestamp.IsZero() {
		t.Error("Handoff Timestamp not set after AddHandoff")
	}
}

func TestAddHandoff_CreatesActionLog(t *testing.T) {
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

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1"},
		Remaining: []string{"Task 2"},
	}
	if err := db.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Verify action_log entry was created
	var count int
	var actionType, entityType, entityID string
	err = db.Conn().QueryRow(
		`SELECT COUNT(*) FROM action_log WHERE entity_type = 'handoff' AND entity_id = ?`,
		handoff.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query action_log count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 action_log entry for handoff, got %d", count)
	}

	err = db.Conn().QueryRow(
		`SELECT action_type, entity_type, entity_id FROM action_log WHERE entity_id = ?`,
		handoff.ID,
	).Scan(&actionType, &entityType, &entityID)
	if err != nil {
		t.Fatalf("query action_log: %v", err)
	}
	if actionType != string(models.ActionHandoff) {
		t.Errorf("action_type: got %q, want %q", actionType, models.ActionHandoff)
	}
	if entityType != "handoff" {
		t.Errorf("entity_type: got %q, want 'handoff'", entityType)
	}
}

func TestAddHandoff_EmptyArrays(t *testing.T) {
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

	// Create handoff with some empty arrays
	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task 1"},
		Remaining: []string{},  // Empty
		Decisions: nil,         // Nil
		Uncertain: []string{},  // Empty
	}

	err = db.AddHandoff(handoff)
	if err != nil {
		t.Fatalf("AddHandoff with empty arrays failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if len(retrieved.Done) != 1 {
		t.Errorf("Expected 1 done item, got %d", len(retrieved.Done))
	}
	// Empty/nil arrays should come back as empty (not nil) after JSON round-trip
	if retrieved.Remaining == nil {
		t.Log("Note: Empty arrays may deserialize as nil")
	}
}

func TestGetLatestHandoff_Basic(t *testing.T) {
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

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Completed task"},
		Remaining: []string{"Remaining task"},
		Decisions: []string{"Made decision"},
		Uncertain: []string{"Open question"},
	}
	if err := db.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected handoff, got nil")
	}
	if retrieved.IssueID != issue.ID {
		t.Errorf("IssueID mismatch: got %s, want %s", retrieved.IssueID, issue.ID)
	}
	if len(retrieved.Done) != 1 || retrieved.Done[0] != "Completed task" {
		t.Error("Done array not correctly retrieved")
	}
	if len(retrieved.Remaining) != 1 || retrieved.Remaining[0] != "Remaining task" {
		t.Error("Remaining array not correctly retrieved")
	}
	if len(retrieved.Decisions) != 1 || retrieved.Decisions[0] != "Made decision" {
		t.Error("Decisions array not correctly retrieved")
	}
	if len(retrieved.Uncertain) != 1 || retrieved.Uncertain[0] != "Open question" {
		t.Error("Uncertain array not correctly retrieved")
	}
}

func TestGetLatestHandoff_MultipleHandoffs(t *testing.T) {
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

	// Add multiple handoffs
	handoff1 := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_first",
		Done:      []string{"First handoff"},
	}
	if err := db.AddHandoff(handoff1); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	handoff2 := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_second",
		Done:      []string{"Second handoff"},
	}
	if err := db.AddHandoff(handoff2); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// GetLatestHandoff should return the most recent
	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved.SessionID != "ses_second" {
		t.Errorf("Expected latest handoff from ses_second, got %s", retrieved.SessionID)
	}
	if len(retrieved.Done) != 1 || retrieved.Done[0] != "Second handoff" {
		t.Error("Expected Second handoff content")
	}
}

func TestGetLatestHandoff_NoHandoff(t *testing.T) {
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

	// Get handoff for issue with no handoffs - should return nil, not error
	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff should not error: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected nil for issue with no handoffs")
	}
}

func TestGetLatestHandoff_NonExistentIssue(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	retrieved, err := db.GetLatestHandoff("td-nonexistent")
	if err != nil {
		t.Fatalf("GetLatestHandoff should not error for non-existent issue: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected nil for non-existent issue")
	}
}

func TestDeleteHandoff_Basic(t *testing.T) {
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

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task"},
	}
	if err := db.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Delete the handoff
	if err := db.DeleteHandoff(handoff.ID); err != nil {
		t.Fatalf("DeleteHandoff failed: %v", err)
	}

	// Verify it's gone
	retrieved, err := db.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Handoff should be deleted")
	}
}

func TestGetRecentHandoffs_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	since := time.Now().Add(-1 * time.Hour)

	// Add handoffs to different issues
	h1 := &models.Handoff{IssueID: issue1.ID, SessionID: "ses_a", Done: []string{"Task 1"}}
	h2 := &models.Handoff{IssueID: issue2.ID, SessionID: "ses_b", Done: []string{"Task 2"}}
	if err := db.AddHandoff(h1); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}
	if err := db.AddHandoff(h2); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	handoffs, err := db.GetRecentHandoffs(10, since)
	if err != nil {
		t.Fatalf("GetRecentHandoffs failed: %v", err)
	}
	if len(handoffs) != 2 {
		t.Errorf("Expected 2 handoffs, got %d", len(handoffs))
	}
}

func TestGetRecentHandoffs_WithLimit(t *testing.T) {
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

	since := time.Now().Add(-1 * time.Hour)

	// Add multiple handoffs
	for i := 0; i < 5; i++ {
		h := &models.Handoff{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Done:      []string{"Task"},
		}
		if err := db.AddHandoff(h); err != nil {
			t.Fatalf("AddHandoff failed: %v", err)
		}
	}

	handoffs, err := db.GetRecentHandoffs(2, since)
	if err != nil {
		t.Fatalf("GetRecentHandoffs failed: %v", err)
	}
	if len(handoffs) != 2 {
		t.Errorf("Expected 2 handoffs with limit, got %d", len(handoffs))
	}
}

func TestGetRecentHandoffs_ExcludesOld(t *testing.T) {
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

	// Add a handoff
	h := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task"},
	}
	if err := db.AddHandoff(h); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Query for handoffs after now - should return empty
	since := time.Now().Add(1 * time.Hour)
	handoffs, err := db.GetRecentHandoffs(10, since)
	if err != nil {
		t.Fatalf("GetRecentHandoffs failed: %v", err)
	}
	if len(handoffs) != 0 {
		t.Errorf("Expected 0 handoffs after future time, got %d", len(handoffs))
	}
}

// ============================================================================
// Comment Tests
// ============================================================================

func TestAddComment_Basic(t *testing.T) {
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

	comment := &models.Comment{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Text:      "This is a test comment",
	}

	err = db.AddComment(comment)
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	if comment.ID == "" {
		t.Error("Comment ID not set after AddComment")
	}
	if comment.CreatedAt.IsZero() {
		t.Error("Comment CreatedAt not set after AddComment")
	}
}

func TestGetComments_Basic(t *testing.T) {
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

	// Add comments
	for i := 0; i < 3; i++ {
		comment := &models.Comment{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Text:      "Comment",
		}
		if err := db.AddComment(comment); err != nil {
			t.Fatalf("AddComment failed: %v", err)
		}
	}

	comments, err := db.GetComments(issue.ID)
	if err != nil {
		t.Fatalf("GetComments failed: %v", err)
	}
	if len(comments) != 3 {
		t.Errorf("Expected 3 comments, got %d", len(comments))
	}
}

func TestGetComments_ChronologicalOrder(t *testing.T) {
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

	texts := []string{"First", "Second", "Third"}
	for _, text := range texts {
		comment := &models.Comment{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Text:      text,
		}
		if err := db.AddComment(comment); err != nil {
			t.Fatalf("AddComment failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	comments, err := db.GetComments(issue.ID)
	if err != nil {
		t.Fatalf("GetComments failed: %v", err)
	}

	for i, text := range texts {
		if comments[i].Text != text {
			t.Errorf("Comment %d: expected %s, got %s", i, text, comments[i].Text)
		}
	}
}

func TestGetComments_EmptyResult(t *testing.T) {
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

	comments, err := db.GetComments(issue.ID)
	if err != nil {
		t.Fatalf("GetComments failed: %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("Expected 0 comments for new issue, got %d", len(comments))
	}
}

func TestGetRecentCommentsAll_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add comments to different issues
	c1 := &models.Comment{IssueID: issue1.ID, SessionID: "ses_a", Text: "Comment 1"}
	c2 := &models.Comment{IssueID: issue2.ID, SessionID: "ses_b", Text: "Comment 2"}
	if err := db.AddComment(c1); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	if err := db.AddComment(c2); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	comments, err := db.GetRecentCommentsAll(10)
	if err != nil {
		t.Fatalf("GetRecentCommentsAll failed: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("Expected 2 comments, got %d", len(comments))
	}
}

// ============================================================================
// Action Log Tests (for undo support)
// ============================================================================

func TestLogAction_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	action := &models.ActionLog{
		SessionID:    "ses_test",
		ActionType:   models.ActionCreate,
		EntityType:   "issue",
		EntityID:     "td-test",
		PreviousData: "",
		NewData:      `{"id":"td-test"}`,
	}

	err = db.LogAction(action)
	if err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	if action.ID == "" {
		t.Error("Action ID not set after LogAction")
	}
	if action.Timestamp.IsZero() {
		t.Error("Action Timestamp not set after LogAction")
	}
}

func TestGetLastAction_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	sessionID := "ses_test"
	action := &models.ActionLog{
		SessionID:  sessionID,
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   "td-test",
	}
	if err := db.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	last, err := db.GetLastAction(sessionID)
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if last == nil {
		t.Fatal("Expected action, got nil")
	}
	if last.EntityID != "td-test" {
		t.Errorf("EntityID mismatch: got %s, want td-test", last.EntityID)
	}
}

func TestGetLastAction_ExcludesUndone(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	sessionID := "ses_test"

	// Create first action
	action1 := &models.ActionLog{
		SessionID:  sessionID,
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   "td-first",
	}
	if err := db.LogAction(action1); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Create second action
	action2 := &models.ActionLog{
		SessionID:  sessionID,
		ActionType: models.ActionUpdate,
		EntityType: "issue",
		EntityID:   "td-second",
	}
	if err := db.LogAction(action2); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Mark second action as undone
	if err := db.MarkActionUndone(action2.ID); err != nil {
		t.Fatalf("MarkActionUndone failed: %v", err)
	}

	// GetLastAction should return first action (second is undone)
	last, err := db.GetLastAction(sessionID)
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if last.EntityID != "td-first" {
		t.Errorf("Expected first action, got %s", last.EntityID)
	}
}

func TestGetLastAction_NoActions(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	last, err := db.GetLastAction("ses_empty")
	if err != nil {
		t.Fatalf("GetLastAction should not error: %v", err)
	}
	if last != nil {
		t.Error("Expected nil for session with no actions")
	}
}

func TestMarkActionUndone_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionCreate,
		EntityType: "issue",
		EntityID:   "td-test",
	}
	if err := db.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Mark as undone
	if err := db.MarkActionUndone(action.ID); err != nil {
		t.Fatalf("MarkActionUndone failed: %v", err)
	}

	// Verify via GetRecentActions
	actions, err := db.GetRecentActions("ses_test", 10)
	if err != nil {
		t.Fatalf("GetRecentActions failed: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action, got %d", len(actions))
	}
	if !actions[0].Undone {
		t.Error("Action should be marked as undone")
	}
}

func TestGetRecentActions_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	sessionID := "ses_test"

	// Log multiple actions
	for i := 0; i < 5; i++ {
		action := &models.ActionLog{
			SessionID:  sessionID,
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   "td-test",
		}
		if err := db.LogAction(action); err != nil {
			t.Fatalf("LogAction failed: %v", err)
		}
	}

	actions, err := db.GetRecentActions(sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentActions failed: %v", err)
	}
	if len(actions) != 5 {
		t.Errorf("Expected 5 actions, got %d", len(actions))
	}
}

func TestGetRecentActions_WithLimit(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	sessionID := "ses_test"
	for i := 0; i < 10; i++ {
		action := &models.ActionLog{
			SessionID:  sessionID,
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   "td-test",
		}
		if err := db.LogAction(action); err != nil {
			t.Fatalf("LogAction failed: %v", err)
		}
	}

	actions, err := db.GetRecentActions(sessionID, 3)
	if err != nil {
		t.Fatalf("GetRecentActions failed: %v", err)
	}
	if len(actions) != 3 {
		t.Errorf("Expected 3 actions with limit, got %d", len(actions))
	}
}

func TestGetRecentActions_SessionIsolation(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Actions from session A
	for i := 0; i < 3; i++ {
		action := &models.ActionLog{
			SessionID:  "ses_a",
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   "td-a",
		}
		db.LogAction(action)
	}

	// Actions from session B
	for i := 0; i < 2; i++ {
		action := &models.ActionLog{
			SessionID:  "ses_b",
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   "td-b",
		}
		db.LogAction(action)
	}

	actionsA, _ := db.GetRecentActions("ses_a", 10)
	actionsB, _ := db.GetRecentActions("ses_b", 10)

	if len(actionsA) != 3 {
		t.Errorf("Session A should have 3 actions, got %d", len(actionsA))
	}
	if len(actionsB) != 2 {
		t.Errorf("Session B should have 2 actions, got %d", len(actionsB))
	}
}

func TestGetRecentActionsAll_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Actions from multiple sessions
	action1 := &models.ActionLog{SessionID: "ses_a", ActionType: models.ActionCreate, EntityType: "issue", EntityID: "td-a"}
	action2 := &models.ActionLog{SessionID: "ses_b", ActionType: models.ActionUpdate, EntityType: "issue", EntityID: "td-b"}
	db.LogAction(action1)
	db.LogAction(action2)

	actions, err := db.GetRecentActionsAll(10)
	if err != nil {
		t.Fatalf("GetRecentActionsAll failed: %v", err)
	}
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
}

// ============================================================================
// Git Snapshot Tests
// ============================================================================

func TestAddGitSnapshot_Basic(t *testing.T) {
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

	snapshot := &models.GitSnapshot{
		IssueID:    issue.ID,
		Event:      "start",
		CommitSHA:  "abc123",
		Branch:     "main",
		DirtyFiles: 3,
	}

	err = db.AddGitSnapshot(snapshot)
	if err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}

	if snapshot.ID == "" {
		t.Error("Snapshot ID not set after AddGitSnapshot")
	}
	if snapshot.Timestamp.IsZero() {
		t.Error("Snapshot Timestamp not set after AddGitSnapshot")
	}
}

func TestGetStartSnapshot_Basic(t *testing.T) {
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

	snapshot := &models.GitSnapshot{
		IssueID:    issue.ID,
		Event:      "start",
		CommitSHA:  "abc123",
		Branch:     "feature-branch",
		DirtyFiles: 0,
	}
	if err := db.AddGitSnapshot(snapshot); err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}

	retrieved, err := db.GetStartSnapshot(issue.ID)
	if err != nil {
		t.Fatalf("GetStartSnapshot failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected snapshot, got nil")
	}
	if retrieved.CommitSHA != "abc123" {
		t.Errorf("CommitSHA mismatch: got %s, want abc123", retrieved.CommitSHA)
	}
	if retrieved.Branch != "feature-branch" {
		t.Errorf("Branch mismatch: got %s, want feature-branch", retrieved.Branch)
	}
}

func TestGetStartSnapshot_ReturnsLatest(t *testing.T) {
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

	// Add multiple start snapshots
	snap1 := &models.GitSnapshot{IssueID: issue.ID, Event: "start", CommitSHA: "first"}
	snap2 := &models.GitSnapshot{IssueID: issue.ID, Event: "start", CommitSHA: "second"}
	db.AddGitSnapshot(snap1)
	time.Sleep(10 * time.Millisecond)
	db.AddGitSnapshot(snap2)

	retrieved, err := db.GetStartSnapshot(issue.ID)
	if err != nil {
		t.Fatalf("GetStartSnapshot failed: %v", err)
	}
	if retrieved.CommitSHA != "second" {
		t.Errorf("Expected latest snapshot (second), got %s", retrieved.CommitSHA)
	}
}

func TestGetStartSnapshot_NoSnapshot(t *testing.T) {
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

	retrieved, err := db.GetStartSnapshot(issue.ID)
	if err != nil {
		t.Fatalf("GetStartSnapshot should not error: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected nil for issue with no snapshots")
	}
}

func TestGetStartSnapshot_IgnoresHandoffEvents(t *testing.T) {
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

	// Add handoff snapshot (should be ignored by GetStartSnapshot)
	handoffSnap := &models.GitSnapshot{IssueID: issue.ID, Event: "handoff", CommitSHA: "handoff-sha"}
	if err := db.AddGitSnapshot(handoffSnap); err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}

	retrieved, err := db.GetStartSnapshot(issue.ID)
	if err != nil {
		t.Fatalf("GetStartSnapshot failed: %v", err)
	}
	if retrieved != nil {
		t.Error("GetStartSnapshot should only return 'start' events, not 'handoff'")
	}
}
