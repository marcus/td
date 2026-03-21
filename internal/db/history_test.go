package db

import (
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestGetIssueTimeline_Empty(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Timeline test issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	events, err := db.GetIssueTimeline(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	// CreateIssue (without Logged) doesn't write action_log, so 0 events is correct
	if len(events) != 0 {
		t.Errorf("expected 0 events for bare issue, got %d", len(events))
	}
}

func TestGetIssueTimeline_ChronologicalOrder(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Chronological test"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add a log
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "First progress",
		Type:      models.LogTypeProgress,
	}
	if err := db.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Add a comment
	comment := &models.Comment{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Text:      "A review comment",
	}
	if err := db.AddComment(comment); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	// Add a handoff
	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"implemented feature"},
		Remaining: []string{"write tests"},
	}
	if err := db.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	events, err := db.GetIssueTimeline(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	// Verify chronological order
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Before(events[i-1].Timestamp) {
			t.Errorf("event %d (%s at %v) is before event %d (%s at %v)",
				i, events[i].EventType, events[i].Timestamp,
				i-1, events[i-1].EventType, events[i-1].Timestamp)
		}
	}

	// Should have at least: log + comment + handoff (CreateIssue doesn't log to action_log)
	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d", len(events))
	}
}

func TestGetIssueTimeline_EventTypes(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Event types test"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add a status change via action_log
	if err := db.LogAction(&models.ActionLog{
		SessionID: "ses_a", ActionType: models.ActionStart,
		EntityType: "issue", EntityID: issue.ID,
		NewData: `{"status":"in_progress"}`,
	}); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Add log
	if err := db.AddLog(&models.Log{
		IssueID: issue.ID, SessionID: "ses_a",
		Message: "progress note", Type: models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Add comment
	if err := db.AddComment(&models.Comment{
		IssueID: issue.ID, SessionID: "ses_b",
		Text: "looks good",
	}); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	// Add git snapshot
	if err := db.AddGitSnapshot(&models.GitSnapshot{
		IssueID: issue.ID, Event: "start",
		CommitSHA: "abc1234567890", Branch: "main", DirtyFiles: 0,
	}); err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}

	// Add handoff
	if err := db.AddHandoff(&models.Handoff{
		IssueID: issue.ID, SessionID: "ses_a",
		Done: []string{"done"}, Remaining: []string{"more"},
	}); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	events, err := db.GetIssueTimeline(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	// Check that we have diverse event types
	typeSet := make(map[models.EventType]bool)
	for _, ev := range events {
		typeSet[ev.EventType] = true
	}

	expectedTypes := []models.EventType{
		models.EventStatusChange,
		models.EventLog,
		models.EventComment,
		models.EventGitSnapshot,
		models.EventHandoff,
	}
	for _, et := range expectedTypes {
		if !typeSet[et] {
			t.Errorf("expected event type %s not found in timeline", et)
		}
	}
}

func TestGetIssueTimeline_Limit(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Limit test"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add several logs
	for i := 0; i < 5; i++ {
		if err := db.AddLog(&models.Log{
			IssueID: issue.ID, SessionID: "ses_test",
			Message: "log entry", Type: models.LogTypeProgress,
		}); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
		time.Sleep(time.Millisecond) // ensure distinct timestamps
	}

	events, err := db.GetIssueTimeline(issue.ID, 3)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events with limit, got %d", len(events))
	}
}

func TestGetIssueTimeline_SessionAttribution(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Session test"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := db.AddLog(&models.Log{
		IssueID: issue.ID, SessionID: "ses_alpha",
		Message: "from alpha", Type: models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	if err := db.AddComment(&models.Comment{
		IssueID: issue.ID, SessionID: "ses_beta",
		Text: "from beta",
	}); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	events, err := db.GetIssueTimeline(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	sessionSeen := make(map[string]bool)
	for _, ev := range events {
		if ev.SessionID != "" {
			sessionSeen[ev.SessionID] = true
		}
	}

	if !sessionSeen["ses_alpha"] {
		t.Error("expected ses_alpha in timeline events")
	}
	if !sessionSeen["ses_beta"] {
		t.Error("expected ses_beta in timeline events")
	}
}

func TestGetIssueTimeline_StatusChangeFromActionLog(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Status change test"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate a start action in action_log
	if err := db.LogAction(&models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionStart,
		EntityType: "issue",
		EntityID:   issue.ID,
		NewData:    `{"status":"in_progress"}`,
	}); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	events, err := db.GetIssueTimeline(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}

	found := false
	for _, ev := range events {
		if ev.EventType == models.EventStatusChange && ev.Summary == "Status changed to in_progress" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected status_change event for start action")
		for _, ev := range events {
			t.Logf("  event: type=%s summary=%q", ev.EventType, ev.Summary)
		}
	}
}
