package db

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestRecordSessionAction verifies session actions are recorded correctly
func TestRecordSessionAction(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a test issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_creator",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record a session action
	err = db.RecordSessionAction(issue.ID, "ses_creator", models.ActionSessionCreated)
	if err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Verify the action was recorded
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != 1 {
		t.Fatalf("Expected 1 history entry, got %d", len(history))
	}

	if history[0].SessionID != "ses_creator" {
		t.Errorf("Expected session_id 'ses_creator', got '%s'", history[0].SessionID)
	}

	if history[0].Action != models.ActionSessionCreated {
		t.Errorf("Expected action 'created', got '%s'", history[0].Action)
	}
}

// TestRecordSessionActionNormalizesID verifies bare IDs are normalized
func TestRecordSessionActionNormalizesID(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a test issue
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record using bare ID (without td- prefix)
	bareID := issue.ID[3:] // Remove "td-" prefix
	err = db.RecordSessionAction(bareID, "ses_test", models.ActionSessionStarted)
	if err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Query using full ID should find it
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != 1 {
		t.Fatalf("Expected 1 history entry, got %d (ID normalization may have failed)", len(history))
	}
}

// TestWasSessionInvolved verifies involvement detection
func TestWasSessionInvolved(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a test issue
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Initially, no session should be involved
	involved, err := db.WasSessionInvolved(issue.ID, "ses_test")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if involved {
		t.Error("Expected session to NOT be involved initially")
	}

	// Record an action
	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Now the session should be involved
	involved, err = db.WasSessionInvolved(issue.ID, "ses_test")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if !involved {
		t.Error("Expected session to be involved after recording action")
	}

	// A different session should still not be involved
	involved, err = db.WasSessionInvolved(issue.ID, "ses_other")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if involved {
		t.Error("Expected different session to NOT be involved")
	}
}

// TestWasSessionInvolvedNormalizesID verifies bare IDs work
func TestWasSessionInvolvedNormalizesID(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a test issue and record action
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Query using bare ID should still find it
	bareID := issue.ID[3:] // Remove "td-" prefix
	involved, err := db.WasSessionInvolved(bareID, "ses_test")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if !involved {
		t.Error("Expected session to be involved (ID normalization may have failed)")
	}
}

// TestGetSessionHistory verifies history retrieval and ordering
func TestGetSessionHistory(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a test issue
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record multiple actions
	actions := []struct {
		session string
		action  models.IssueSessionAction
	}{
		{"ses_creator", models.ActionSessionCreated},
		{"ses_worker", models.ActionSessionStarted},
		{"ses_worker", models.ActionSessionUnstarted},
		{"ses_reviewer", models.ActionSessionReviewed},
	}

	for _, a := range actions {
		if err := db.RecordSessionAction(issue.ID, a.session, a.action); err != nil {
			t.Fatalf("RecordSessionAction failed: %v", err)
		}
	}

	// Get history
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != 4 {
		t.Fatalf("Expected 4 history entries, got %d", len(history))
	}

	// Verify order (should be chronological)
	expectedActions := []models.IssueSessionAction{
		models.ActionSessionCreated,
		models.ActionSessionStarted,
		models.ActionSessionUnstarted,
		models.ActionSessionReviewed,
	}

	for i, expected := range expectedActions {
		if history[i].Action != expected {
			t.Errorf("History[%d]: expected action '%s', got '%s'", i, expected, history[i].Action)
		}
	}
}

// TestUnstartBypassPrevention verifies that unstarting still tracks involvement
func TestUnstartBypassPrevention(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_creator",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Session A starts the issue
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A unstarts (clears ImplementerSession but should still be tracked)
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionUnstarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A should STILL be considered involved (bypass prevention)
	involved, err := db.WasSessionInvolved(issue.ID, "ses_A")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if !involved {
		t.Error("Session A should still be involved after unstart (bypass prevention)")
	}
}

// TestMultipleSessionsTracked verifies all sessions that touched an issue are tracked
func TestMultipleSessionsTracked(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Multiple sessions interact
	sessions := []string{"ses_A", "ses_B", "ses_C"}
	for _, sess := range sessions {
		if err := db.RecordSessionAction(issue.ID, sess, models.ActionSessionStarted); err != nil {
			t.Fatalf("RecordSessionAction failed for %s: %v", sess, err)
		}
	}

	// All sessions should be tracked as involved
	for _, sess := range sessions {
		involved, err := db.WasSessionInvolved(issue.ID, sess)
		if err != nil {
			t.Fatalf("WasSessionInvolved failed for %s: %v", sess, err)
		}
		if !involved {
			t.Errorf("Session %s should be involved", sess)
		}
	}

	// Uninvolved session should not be tracked
	involved, err := db.WasSessionInvolved(issue.ID, "ses_uninvolved")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if involved {
		t.Error("Uninvolved session should NOT be tracked")
	}
}

// TestCreatorSessionSet verifies CreatorSession is stored and retrieved
func TestCreatorSessionSet(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue with CreatorSession
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_creator_123",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if retrieved.CreatorSession != "ses_creator_123" {
		t.Errorf("CreatorSession mismatch: got '%s', want 'ses_creator_123'", retrieved.CreatorSession)
	}
}

// TestEmptyHistoryForNewIssue verifies new issues have no history
func TestEmptyHistoryForNewIssue(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue without recording any actions
	issue := &models.Issue{Title: "Test Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// History should be empty
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("Expected empty history, got %d entries", len(history))
	}

	// No session should be involved
	involved, err := db.WasSessionInvolved(issue.ID, "any_session")
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}
	if involved {
		t.Error("No session should be involved for fresh issue")
	}
}

// TestBypassScenario_CreateClose verifies create->close bypass is prevented
// Scenario: Session creates issue, then tries to close without anyone implementing
func TestBypassScenario_CreateClose(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Session A creates issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_A",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A tries to close - should be blocked because:
	// 1. wasInvolved = true (recorded 'created' action)
	// 2. isCreator = true
	// 3. hasOtherImplementer = false (no one implemented)
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	isCreator := issue.CreatorSession == "ses_A"
	hasOtherImplementer := issue.ImplementerSession != "" && issue.ImplementerSession != "ses_A"

	// Apply the same logic as close command
	wasEverInvolved := wasInvolved || isCreator
	canClose := !wasEverInvolved || (isCreator && hasOtherImplementer)

	if canClose {
		t.Error("Session A should NOT be able to close their own creation without another implementer")
	}
}

// TestBypassScenario_CreateCloseWithOtherImplementer verifies creator CAN close if other implemented
func TestBypassScenario_CreateCloseWithOtherImplementer(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Session A creates issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_A",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session B implements the issue
	issue.ImplementerSession = "ses_B"
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_B", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A tries to close - should be ALLOWED because:
	// 1. isCreator = true
	// 2. hasOtherImplementer = true (ses_B implemented)
	// 3. isImplementer = false (ses_A is not the implementer)
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	isCreator := issue.CreatorSession == "ses_A"
	isImplementer := issue.ImplementerSession == "ses_A"
	hasOtherImplementer := issue.ImplementerSession != "" && !isImplementer

	wasEverInvolved := wasInvolved || isCreator || isImplementer
	var canClose bool
	if !wasEverInvolved {
		canClose = true
	} else if isCreator && hasOtherImplementer && !isImplementer {
		canClose = true
	}

	if !canClose {
		t.Error("Creator should be able to close when someone else implemented")
	}
}

// TestBypassScenario_UnstartRestart verifies unstart->restart bypass is prevented
// Scenario: A starts, A unstarts, B starts, A tries to approve
func TestBypassScenario_UnstartRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_creator",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Session A starts
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A unstarts (would clear ImplementerSession, but history remains)
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionUnstarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session B starts (becomes implementer)
	issue.ImplementerSession = "ses_B"
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_B", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A tries to approve - should be BLOCKED because A was previously involved
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	if !wasInvolved {
		t.Error("Session A should still be marked as involved after unstart")
	}

	// Per approve logic: block if wasInvolved && !issue.Minor
	canApprove := !wasInvolved
	if canApprove {
		t.Error("Session A should NOT be able to approve after having started/unstarted")
	}
}

// TestBypassScenario_UnrelatedSessionCanApprove verifies uninvolved sessions CAN approve
func TestBypassScenario_UnrelatedSessionCanApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue
	issue := &models.Issue{
		Title:          "Test Issue",
		CreatorSession: "ses_creator",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_creator", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Implementer works on it
	issue.ImplementerSession = "ses_implementer"
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_implementer", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Unrelated session tries to approve - should be ALLOWED
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_reviewer")
	if wasInvolved {
		t.Error("Unrelated session should NOT be marked as involved")
	}

	canApprove := !wasInvolved
	if !canApprove {
		t.Error("Unrelated session should be able to approve")
	}
}

// TestMinorTaskSelfApprove verifies minor tasks allow self-approve
func TestMinorTaskSelfApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create minor issue
	issue := &models.Issue{
		Title:              "Minor fix",
		CreatorSession:     "ses_A",
		ImplementerSession: "ses_A",
		Minor:              true,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Session A tries to approve their own minor task - should be ALLOWED
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	if !wasInvolved {
		t.Error("Session A should be marked as involved")
	}

	// Per approve logic: allow if minor even if involved
	canApprove := !wasInvolved || issue.Minor
	if !canApprove {
		t.Error("Minor task should allow self-approve")
	}
}
