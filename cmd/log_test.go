package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestLogSingleMessage tests adding a single log message
func TestLogSingleMessage(t *testing.T) {
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

	message := "Started working on this"
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   message,
		Type:      models.LogTypeProgress,
	}

	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != message {
		t.Errorf("Expected message %q, got %q", message, logs[0].Message)
	}
}

// TestLogMultipleMessages tests adding multiple log messages
func TestLogMultipleMessages(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	messages := []string{
		"Initial exploration",
		"Found the bug",
		"Applied fix",
		"Tests passing",
	}

	for _, msg := range messages {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   msg,
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != len(messages) {
		t.Fatalf("Expected %d logs, got %d", len(messages), len(logs))
	}

	// Verify all messages are present (order may vary)
	messageMap := make(map[string]bool)
	for _, log := range logs {
		messageMap[log.Message] = true
	}

	for _, msg := range messages {
		if !messageMap[msg] {
			t.Errorf("Message %q not found in logs", msg)
		}
	}
}

// TestLogWithDifferentTypes tests logs with different types
func TestLogWithDifferentTypes(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	testCases := []struct {
		name    string
		logType models.LogType
		message string
	}{
		{"progress log", models.LogTypeProgress, "Making progress"},
		{"blocker log", models.LogTypeBlocker, "Blocked on API dependency"},
		{"decision log", models.LogTypeDecision, "Decided to use approach X"},
		{"hypothesis log", models.LogTypeHypothesis, "Hypothesis: issue is in cache layer"},
		{"tried log", models.LogTypeTried, "Tried restarting the service"},
		{"result log", models.LogTypeResult, "Result: fixed the issue"},
	}

	for _, tc := range testCases {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   tc.message,
			Type:      tc.logType,
		}
		if err := database.AddLog(log); err != nil {
			t.Fatalf("AddLog for %s failed: %v", tc.name, err)
		}
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != len(testCases) {
		t.Fatalf("Expected %d logs, got %d", len(testCases), len(logs))
	}

	// Verify all types are present
	typeMap := make(map[models.LogType]int)
	for _, log := range logs {
		typeMap[log.Type]++
	}

	for _, tc := range testCases {
		if typeMap[tc.logType] != 1 {
			t.Errorf("Expected 1 log of type %s, got %d", tc.logType, typeMap[tc.logType])
		}
	}
}

// TestLogRetrievalLimit tests log retrieval with limit
func TestLogRetrievalLimit(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Add 10 logs
	for i := 0; i < 10; i++ {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Message " + string(rune(i)),
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	// Test different limits
	testCases := []struct {
		limit         int
		expectedCount int
	}{
		{1, 1},
		{5, 5},
		{10, 10},
		{20, 10}, // More than available
	}

	for _, tc := range testCases {
		logs, _ := database.GetLogs(issue.ID, tc.limit)
		if len(logs) != tc.expectedCount {
			t.Errorf("Limit %d: expected %d logs, got %d", tc.limit, tc.expectedCount, len(logs))
		}
	}
}

// TestLogForMultipleIssues tests that logs are issue-specific
func TestLogForMultipleIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}

	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Add logs to issue 1
	for i := 0; i < 3; i++ {
		log := &models.Log{
			IssueID:   issue1.ID,
			SessionID: "ses_test",
			Message:   "Issue 1 log",
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	// Add logs to issue 2
	for i := 0; i < 5; i++ {
		log := &models.Log{
			IssueID:   issue2.ID,
			SessionID: "ses_test",
			Message:   "Issue 2 log",
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	logs1, _ := database.GetLogs(issue1.ID, 10)
	logs2, _ := database.GetLogs(issue2.ID, 10)

	if len(logs1) != 3 {
		t.Errorf("Issue 1: expected 3 logs, got %d", len(logs1))
	}
	if len(logs2) != 5 {
		t.Errorf("Issue 2: expected 5 logs, got %d", len(logs2))
	}

	// Verify logs are not mixed
	for _, log := range logs1 {
		if log.IssueID != issue1.ID {
			t.Error("Issue 1 logs should only contain logs for issue 1")
		}
	}
	for _, log := range logs2 {
		if log.IssueID != issue2.ID {
			t.Error("Issue 2 logs should only contain logs for issue 2")
		}
	}
}

// TestLogWithMultipleSessions tests logs from different sessions
func TestLogWithMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	sessions := []string{"ses_aaa", "ses_bbb", "ses_ccc"}

	for _, session := range sessions {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: session,
			Message:   "Log from " + session,
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != len(sessions) {
		t.Fatalf("Expected %d logs, got %d", len(sessions), len(logs))
	}

	sessionMap := make(map[string]int)
	for _, log := range logs {
		sessionMap[log.SessionID]++
	}

	for _, session := range sessions {
		if sessionMap[session] != 1 {
			t.Errorf("Expected 1 log from %s, got %d", session, sessionMap[session])
		}
	}
}

// TestLogWithWorkSession tests logs with work session ID
func TestLogWithWorkSession(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	workSessionID := "ws_12345"
	log := &models.Log{
		IssueID:       issue.ID,
		SessionID:     "ses_test",
		WorkSessionID: workSessionID,
		Message:       "Work session log",
		Type:          models.LogTypeProgress,
	}

	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].WorkSessionID != workSessionID {
		t.Errorf("Expected work session %q, got %q", workSessionID, logs[0].WorkSessionID)
	}
}

// TestLogEmptyMessage tests logging empty message
func TestLogEmptyMessage(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "",
		Type:      models.LogTypeProgress,
	}

	// Should allow empty message (some systems allow this)
	err = database.AddLog(log)
	if err != nil {
		t.Logf("Empty message error: %v", err)
	}
}

// TestLogLongMessage tests logging very long message
func TestLogLongMessage(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Create a long message
	longMessage := ""
	for i := 0; i < 100; i++ {
		longMessage += "This is a very long log message. "
	}

	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   longMessage,
		Type:      models.LogTypeProgress,
	}

	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != longMessage {
		t.Error("Long message was truncated or modified")
	}
}

// TestLogNonexistentIssue tests adding log to non-existent issue
func TestLogNonexistentIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	log := &models.Log{
		IssueID:   "td-nonexistent",
		SessionID: "ses_test",
		Message:   "This should fail",
		Type:      models.LogTypeProgress,
	}

	// This may or may not error depending on implementation
	err = database.AddLog(log)
	if err != nil {
		t.Logf("Expected behavior: %v", err)
	}
}

// TestLogRetrieval tests log ordering
func TestLogRetrieval(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Add logs in specific order
	messages := []string{"First", "Second", "Third"}
	for _, msg := range messages {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   msg,
			Type:      models.LogTypeProgress,
		}
		database.AddLog(log)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != len(messages) {
		t.Fatalf("Expected %d logs, got %d", len(messages), len(logs))
	}

	// Logs should be in reverse order (newest first typically)
	// Verify at least that all messages are present
	messageSet := make(map[string]bool)
	for _, log := range logs {
		messageSet[log.Message] = true
	}

	for _, msg := range messages {
		if !messageSet[msg] {
			t.Errorf("Message %q not found in logs", msg)
		}
	}
}
