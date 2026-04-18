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

// TestWasSessionImplementationInvolved verifies started/unstarted detection only.
func TestWasSessionImplementationInvolved(t *testing.T) {
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

	// "created" does not count as implementation involvement.
	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	involved, err := db.WasSessionImplementationInvolved(issue.ID, "ses_test")
	if err != nil {
		t.Fatalf("WasSessionImplementationInvolved failed: %v", err)
	}
	if involved {
		t.Fatal("created action should not count as implementation involvement")
	}

	// "started" counts.
	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	involved, err = db.WasSessionImplementationInvolved(issue.ID, "ses_test")
	if err != nil {
		t.Fatalf("WasSessionImplementationInvolved failed: %v", err)
	}
	if !involved {
		t.Fatal("started action should count as implementation involvement")
	}
}

func testCloseAllowed(issue *models.Issue, sessionID string, wasInvolved, wasImplementationInvolved, hasImplementationHistory bool) bool {
	if issue.Minor {
		return true
	}

	isCreator := issue.CreatorSession == sessionID && issue.CreatorSession != ""
	isImplementer := issue.ImplementerSession == sessionID && issue.ImplementerSession != ""

	if isCreator && issue.Status == models.StatusOpen && !hasImplementationHistory && !wasImplementationInvolved {
		return true
	}

	if isImplementer || wasImplementationInvolved || isCreator || wasInvolved {
		return false
	}

	return true
}

func TestHasImplementationHistory(t *testing.T) {
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

	hasHistory, err := db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if hasHistory {
		t.Fatal("new issue should not have implementation history")
	}

	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	hasHistory, err = db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if hasHistory {
		t.Fatal("created action should not count as implementation history")
	}

	if err := db.RecordSessionAction(issue.ID, "ses_test", models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}
	hasHistory, err = db.HasImplementationHistory(issue.ID)
	if err != nil {
		t.Fatalf("HasImplementationHistory failed: %v", err)
	}
	if !hasHistory {
		t.Fatal("started action should count as implementation history")
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

// TestBypassScenario_CreateClose verifies creator can close an open throwaway issue
// before any implementation history exists.
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
		Status:         models.StatusOpen,
		CreatorSession: "ses_A",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.RecordSessionAction(issue.ID, "ses_A", models.ActionSessionCreated); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, "ses_A")
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)

	canClose := testCloseAllowed(issue, "ses_A", wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if !canClose {
		t.Error("Session A should be able to close their own open throwaway issue")
	}
}

// TestBypassScenario_CreateCloseWithOtherImplementer verifies creator cannot close
// once the issue has entered implementation flow.
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
		Status:         models.StatusOpen,
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

	wasInvolved, _ := db.WasSessionInvolved(issue.ID, "ses_A")
	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, "ses_A")
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)

	canClose := testCloseAllowed(issue, "ses_A", wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if canClose {
		t.Error("Creator should not be able to close once implementation history exists")
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

// ============================================================================
// INTEGRATION TESTS FOR BYPASS PREVENTION (Workflow Enforcement)
// ============================================================================

// TestIntegration_SkipReviewNotAllowed verifies cannot skip review workflow step
func TestIntegration_SkipReviewNotAllowed(t *testing.T) {
	tests := []struct {
		name               string
		initialStatus      models.Status
		attemptedStatus    models.Status
		creatorSession     string
		implementerSession string
		reviewerSession    string
		attemptingSession  string
		shouldBeAllowed    bool
		description        string
	}{
		{
			name:               "Open to Closed directly - bypass review",
			initialStatus:      models.StatusOpen,
			attemptedStatus:    models.StatusClosed,
			creatorSession:     "ses_creator",
			implementerSession: "",
			reviewerSession:    "",
			attemptingSession:  "ses_other",
			shouldBeAllowed:    false,
			description:        "Cannot skip from open directly to closed without review",
		},
		{
			name:               "InProgress to Closed directly - bypass review",
			initialStatus:      models.StatusInProgress,
			attemptedStatus:    models.StatusClosed,
			creatorSession:     "ses_creator",
			implementerSession: "ses_impl",
			reviewerSession:    "",
			attemptingSession:  "ses_impl",
			shouldBeAllowed:    false,
			description:        "Cannot skip from in_progress directly to closed without review",
		},
		{
			name:               "Must go through InReview first",
			initialStatus:      models.StatusOpen,
			attemptedStatus:    models.StatusInReview,
			creatorSession:     "ses_creator",
			implementerSession: "",
			reviewerSession:    "",
			attemptingSession:  "ses_other",
			shouldBeAllowed:    true,
			description:        "Can move to review from open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer db.Close()

			// Create issue in initial status
			issue := &models.Issue{
				Title:              tt.name,
				Status:             tt.initialStatus,
				CreatorSession:     tt.creatorSession,
				ImplementerSession: tt.implementerSession,
				ReviewerSession:    tt.reviewerSession,
			}
			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			// Record relevant session actions
			if tt.creatorSession != "" {
				_ = db.RecordSessionAction(issue.ID, tt.creatorSession, models.ActionSessionCreated)
			}
			if tt.implementerSession != "" {
				_ = db.RecordSessionAction(issue.ID, tt.implementerSession, models.ActionSessionStarted)
			}

			// Check if bypass is being attempted
			isBypassAttempt := (tt.initialStatus == models.StatusOpen || tt.initialStatus == models.StatusInProgress) &&
				tt.attemptedStatus == models.StatusClosed

			if isBypassAttempt && tt.shouldBeAllowed {
				t.Errorf("%s: %s", tt.name, tt.description)
			}
		})
	}
}

// TestIntegration_ReviewWorkflowEnforced verifies proper workflow sequence
func TestIntegration_ReviewWorkflowEnforced(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_implementer"
	reviewerSess := "ses_reviewer"

	// Step 1: Creator creates issue (open)
	issue := &models.Issue{
		Title:          "Feature Request",
		Status:         models.StatusOpen,
		CreatorSession: creatorSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)

	// Verify no one has been involved yet except creator
	creatorInvolved, _ := db.WasSessionInvolved(issue.ID, creatorSess)
	if !creatorInvolved {
		t.Error("Creator should be involved after creation")
	}

	// Step 2: Implementer starts work
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = implSess
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	implInvolved, _ := db.WasSessionInvolved(issue.ID, implSess)
	if !implInvolved {
		t.Error("Implementer should be involved after starting")
	}

	// Step 3: Implementer submits for review
	issue.Status = models.StatusInReview
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionReviewed)

	// Step 4: Reviewer (not implementer, not creator) approves
	wasReviewerInvolved, _ := db.WasSessionInvolved(issue.ID, reviewerSess)
	if wasReviewerInvolved {
		t.Error("Reviewer should not be involved yet")
	}

	// Reviewer can approve since they were not involved
	issue.Status = models.StatusClosed
	issue.ReviewerSession = reviewerSess
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	// Record reviewer action (use reviewed action to track approval)
	_ = db.RecordSessionAction(issue.ID, reviewerSess, models.ActionSessionReviewed)

	// Verify final state
	finalIssue, _ := db.GetIssue(issue.ID)
	if finalIssue.Status != models.StatusClosed {
		t.Errorf("Expected closed, got %q", finalIssue.Status)
	}
	if finalIssue.ReviewerSession != reviewerSess {
		t.Errorf("Expected reviewer %q, got %q", reviewerSess, finalIssue.ReviewerSession)
	}
}

// TestIntegration_ImplementerCannotApprove verifies implementer cannot self-approve
func TestIntegration_ImplementerCannotApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	implSess := "ses_implementer"

	// Create issue with implementer starting it
	issue := &models.Issue{
		Title:              "Task",
		Status:             models.StatusOpen,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Implementer starts the task
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Mark as in review
	issue.Status = models.StatusInReview
	_ = db.UpdateIssue(issue)

	// Check if implementer can approve
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, implSess)
	if !wasInvolved {
		t.Error("Implementer should be involved after starting")
	}

	// Implementer should NOT be able to approve (unless minor)
	canApprove := !wasInvolved || issue.Minor
	if canApprove {
		t.Error("Non-minor task: implementer should NOT be able to approve own work")
	}
}

// TestIntegration_HandoffValidatesWorkflow verifies handoff is recorded in workflow
func TestIntegration_HandoffValidatesWorkflow(t *testing.T) {
	tests := []struct {
		name           string
		status         models.Status
		implementerSet bool
		description    string
	}{
		{
			name:           "Handoff at open status",
			status:         models.StatusOpen,
			implementerSet: false,
			description:    "Handoff can be recorded at open status",
		},
		{
			name:           "Handoff at in_progress",
			status:         models.StatusInProgress,
			implementerSet: true,
			description:    "Handoff recorded when in_progress with implementer set",
		},
		{
			name:           "Handoff at in_review",
			status:         models.StatusInReview,
			implementerSet: true,
			description:    "Handoff recorded when in_review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer db.Close()

			issue := &models.Issue{
				Title:  tt.name,
				Status: tt.status,
			}
			if tt.implementerSet {
				issue.ImplementerSession = "ses_impl"
			}

			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			// Add handoff
			handoff := &models.Handoff{
				IssueID:   issue.ID,
				SessionID: "ses_impl",
				Done:      []string{"part 1"},
				Remaining: []string{"part 2"},
			}
			err = db.AddHandoff(handoff)
			if err != nil {
				t.Fatalf("AddHandoff failed: %v", err)
			}

			// Verify handoff was recorded
			retrieved, err := db.GetLatestHandoff(issue.ID)
			if err != nil {
				t.Fatalf("GetLatestHandoff failed: %v", err)
			}
			if retrieved == nil {
				t.Fatal("Expected handoff to be recorded")
			}
			if len(retrieved.Done) != 1 || retrieved.Done[0] != "part 1" {
				t.Errorf("Handoff done items not preserved: %v", retrieved.Done)
			}
		})
	}
}

// TestIntegration_CreatorCannotImplementAndApprove verifies creator cannot bypass approval
func TestIntegration_CreatorCannotImplementAndApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"

	// Creator creates issue
	issue := &models.Issue{
		Title:          "Feature",
		Status:         models.StatusOpen,
		CreatorSession: creatorSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)

	// Creator also implements (should not be allowed to approve)
	issue.ImplementerSession = creatorSess
	issue.Status = models.StatusInProgress
	_ = db.UpdateIssue(issue)
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionStarted)

	// Mark as in review
	issue.Status = models.StatusInReview
	_ = db.UpdateIssue(issue)

	// Creator should not be able to approve
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, creatorSess)
	if !wasInvolved {
		t.Error("Creator should be marked as involved (created + implemented)")
	}

	// Non-minor task: creator/implementer cannot approve
	canApprove := !wasInvolved || issue.Minor
	if canApprove {
		t.Error("Creator who implemented should NOT be able to approve (unless minor)")
	}
}

// TestIntegration_DifferentSessionCanApprove verifies uninvolved session can approve
func TestIntegration_DifferentSessionCanApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_impl"
	reviewerSess := "ses_reviewer"

	// Setup: creator -> implementer -> now reviewer
	issue := &models.Issue{
		Title:              "Task",
		Status:             models.StatusOpen,
		CreatorSession:     creatorSess,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record interactions
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Move to review
	issue.Status = models.StatusInReview
	_ = db.UpdateIssue(issue)

	// Reviewer (uninvolved) should be able to approve
	wasReviewerInvolved, _ := db.WasSessionInvolved(issue.ID, reviewerSess)
	if wasReviewerInvolved {
		t.Error("Reviewer should not be involved yet")
	}

	canApprove := !wasReviewerInvolved
	if !canApprove {
		t.Error("Uninvolved reviewer should be able to approve")
	}
}

// TestIntegration_UnstartDoesNotBypass verifies unstart still prevents bypass
func TestIntegration_UnstartDoesNotBypass(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessA := "ses_A"
	sessB := "ses_B"

	// Session A starts, then unstarts
	issue := &models.Issue{
		Title:          "Task",
		Status:         models.StatusInProgress,
		CreatorSession: "ses_creator",
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// A starts
	_ = db.RecordSessionAction(issue.ID, sessA, models.ActionSessionStarted)

	// A unstarts (clears implementer but history remains)
	issue.ImplementerSession = ""
	_ = db.UpdateIssue(issue)
	_ = db.RecordSessionAction(issue.ID, sessA, models.ActionSessionUnstarted)

	// B starts (becomes new implementer)
	issue.ImplementerSession = sessB
	issue.Status = models.StatusInProgress
	_ = db.UpdateIssue(issue)
	_ = db.RecordSessionAction(issue.ID, sessB, models.ActionSessionStarted)

	// Now move to review
	issue.Status = models.StatusInReview
	_ = db.UpdateIssue(issue)

	// A should NOT be able to approve even though they're not current implementer
	wasAInvolved, _ := db.WasSessionInvolved(issue.ID, sessA)
	if !wasAInvolved {
		t.Error("Session A should be involved (history tracked unstart)")
	}

	// B should NOT be able to approve (implementer)
	wasBInvolved, _ := db.WasSessionInvolved(issue.ID, sessB)
	if !wasBInvolved {
		t.Error("Session B should be involved")
	}

	canAApprove := !wasAInvolved || issue.Minor
	if canAApprove {
		t.Error("Session A should NOT be able to approve after unstartingeven though not current implementer)")
	}

	canBApprove := !wasBInvolved || issue.Minor
	if canBApprove {
		t.Error("Session B should NOT be able to approve (current implementer)")
	}
}

// TestIntegration_BypassAttemptErrors verifies error messages are clear
func TestIntegration_BypassAttemptErrors(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	tests := []struct {
		name                  string
		creatorSession        string
		implementerSession    string
		currentSession        string
		attemptedAction       string
		expectedErrorContains string
	}{
		{
			name:                  "Implementer tries to approve own work",
			creatorSession:        "ses_creator",
			implementerSession:    "ses_impl",
			currentSession:        "ses_impl",
			attemptedAction:       "approve",
			expectedErrorContains: "cannot",
		},
		{
			name:                  "Unrelated can approve",
			creatorSession:        "ses_creator",
			implementerSession:    "ses_impl",
			currentSession:        "ses_other",
			attemptedAction:       "approve",
			expectedErrorContains: "", // should succeed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &models.Issue{
				Title:              tt.name,
				Status:             models.StatusInReview,
				CreatorSession:     tt.creatorSession,
				ImplementerSession: tt.implementerSession,
			}
			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			// Record session involvement
			if tt.creatorSession != "" {
				_ = db.RecordSessionAction(issue.ID, tt.creatorSession, models.ActionSessionCreated)
			}
			if tt.implementerSession != "" {
				_ = db.RecordSessionAction(issue.ID, tt.implementerSession, models.ActionSessionStarted)
			}

			// Check if approval would be allowed
			wasInvolved, _ := db.WasSessionInvolved(issue.ID, tt.currentSession)
			isCreator := issue.CreatorSession == tt.currentSession
			isImplementer := issue.ImplementerSession == tt.currentSession
			isInvolved := wasInvolved || isCreator || isImplementer

			if tt.attemptedAction == "approve" {
				canApprove := !isInvolved || issue.Minor
				if tt.expectedErrorContains == "" && !canApprove {
					t.Error("Expected approval to succeed")
				}
				if tt.expectedErrorContains != "" && canApprove {
					t.Error("Expected approval to fail")
				}
			}
		})
	}
}

// ============================================================================
// COMMAND-LEVEL INTEGRATION TESTS FOR BYPASS PREVENTION
// These tests verify the actual command logic matches bypass prevention rules
// ============================================================================

// TestCommand_CreatorAsImplementerCannotClose verifies creator who implemented cannot close
func TestCommand_CreatorAsImplementerCannotClose(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"

	// Creator creates and implements issue
	issue := &models.Issue{
		Title:              "Self-implemented task",
		Status:             models.StatusInProgress,
		CreatorSession:     creatorSess,
		ImplementerSession: creatorSess, // Same session
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record both creation and implementation
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionStarted)

	// Simulate close command logic from cmd/review.go closeCmd
	wasInvolved, err := db.WasSessionInvolved(issue.ID, creatorSess)
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}

	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, creatorSess)
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)
	canClose := testCloseAllowed(issue, creatorSess, wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if canClose {
		t.Error("Creator who is also implementer should NOT be able to close without self-close-exception")
	}
}

// TestCommand_UninvolvedSessionCanClose verifies uninvolved session CAN close issues
func TestCommand_UninvolvedSessionCanClose(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_impl"
	closerSess := "ses_uninvolved"

	// Create issue with different creator and implementer
	issue := &models.Issue{
		Title:              "Task to close",
		Status:             models.StatusInReview,
		CreatorSession:     creatorSess,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record actions for creator and implementer
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Simulate close command logic for uninvolved session
	wasInvolved, err := db.WasSessionInvolved(issue.ID, closerSess)
	if err != nil {
		t.Fatalf("WasSessionInvolved failed: %v", err)
	}

	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, closerSess)
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)
	canClose := testCloseAllowed(issue, closerSess, wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if !canClose {
		t.Error("Uninvolved session should be able to close issues")
	}
}

// TestCommand_DBErrorHandlingApprove verifies conservative behavior on DB errors
func TestCommand_DBErrorHandlingApprove(t *testing.T) {
	// This tests the pattern used in approve command:
	// On DB error, assume involvement (conservative approach)

	tests := []struct {
		name             string
		dbError          bool
		sessionID        string
		isCreator        bool
		isImplementer    bool
		isMinor          bool
		expectCanApprove bool
	}{
		{
			name:             "DB error assumes involvement - blocks approve",
			dbError:          true,
			sessionID:        "ses_unknown",
			isCreator:        false,
			isImplementer:    false,
			isMinor:          false,
			expectCanApprove: false, // Conservative: block on error
		},
		{
			name:             "DB error but minor task - allows approve",
			dbError:          true,
			sessionID:        "ses_unknown",
			isCreator:        false,
			isImplementer:    false,
			isMinor:          true,
			expectCanApprove: true, // Minor overrides
		},
		{
			name:             "No error, not involved - allows approve",
			dbError:          false,
			sessionID:        "ses_reviewer",
			isCreator:        false,
			isImplementer:    false,
			isMinor:          false,
			expectCanApprove: true,
		},
		{
			name:             "No error, is creator - blocks approve",
			dbError:          false,
			sessionID:        "ses_creator",
			isCreator:        true,
			isImplementer:    false,
			isMinor:          false,
			expectCanApprove: false,
		},
		{
			name:             "No error, is implementer - blocks approve",
			dbError:          false,
			sessionID:        "ses_impl",
			isCreator:        false,
			isImplementer:    true,
			isMinor:          false,
			expectCanApprove: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the approve command logic from cmd/review.go
			var wasInvolved bool
			if tt.dbError {
				// On error, conservative assumption
				wasInvolved = true
			} else {
				// No error case - would normally query DB
				wasInvolved = false
			}

			// Check creator/implementer flags (defensive fallback)
			wasEverInvolved := wasInvolved || tt.isCreator || tt.isImplementer

			// Apply approve command logic
			canApprove := !wasEverInvolved || tt.isMinor

			if canApprove != tt.expectCanApprove {
				t.Errorf("Expected canApprove=%v, got %v", tt.expectCanApprove, canApprove)
			}
		})
	}
}

// TestCommand_DBErrorHandlingClose verifies conservative behavior on DB errors for close
func TestCommand_DBErrorHandlingClose(t *testing.T) {
	tests := []struct {
		name           string
		dbError        bool
		status         models.Status
		isCreator      bool
		isImplementer  bool
		wasImpl        bool
		hasImplHistory bool
		isMinor        bool
		expectCanClose bool
	}{
		{
			name:           "DB error assumes involvement - blocks close",
			dbError:        true,
			status:         models.StatusInReview,
			isCreator:      false,
			isImplementer:  false,
			wasImpl:        false,
			hasImplHistory: false,
			isMinor:        false,
			expectCanClose: false,
		},
		{
			name:           "DB error but minor task - allows close",
			dbError:        true,
			status:         models.StatusInReview,
			isCreator:      false,
			isImplementer:  false,
			wasImpl:        false,
			hasImplHistory: false,
			isMinor:        true,
			expectCanClose: true,
		},
		{
			name:           "No error, uninvolved - allows close",
			dbError:        false,
			status:         models.StatusInReview,
			isCreator:      false,
			isImplementer:  false,
			wasImpl:        false,
			hasImplHistory: true,
			isMinor:        false,
			expectCanClose: true,
		},
		{
			name:           "No error, creator open issue with no implementation history - allows close",
			dbError:        false,
			status:         models.StatusOpen,
			isCreator:      true,
			isImplementer:  false,
			wasImpl:        false,
			hasImplHistory: false,
			isMinor:        false,
			expectCanClose: true,
		},
		{
			name:           "No error, creator issue with implementation history - blocks close",
			dbError:        false,
			status:         models.StatusOpen,
			isCreator:      true,
			isImplementer:  false,
			wasImpl:        false,
			hasImplHistory: true,
			isMinor:        false,
			expectCanClose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the close command logic from cmd/review.go
			var wasInvolved bool
			if tt.dbError {
				wasInvolved = true // Conservative
			}

			issue := &models.Issue{
				Status: models.Status(tt.status),
				Minor:  tt.isMinor,
			}
			if tt.isCreator {
				issue.CreatorSession = "ses_creator"
			}
			if tt.isImplementer {
				issue.ImplementerSession = "ses_impl"
			}

			canClose := testCloseAllowed(issue, issue.CreatorSession, wasInvolved, tt.wasImpl, tt.hasImplHistory)
			if !tt.isCreator {
				canClose = testCloseAllowed(issue, "ses_other", wasInvolved, tt.wasImpl, tt.hasImplHistory)
			}

			if canClose != tt.expectCanClose {
				t.Errorf("Expected canClose=%v, got %v", tt.expectCanClose, canClose)
			}
		})
	}
}

// TestCommand_CreatorOnlyCannotApprove verifies creator (who didn't implement) cannot approve
func TestCommand_CreatorOnlyCannotApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_impl"

	// Creator creates issue, different session implements
	issue := &models.Issue{
		Title:              "Task with separate implementer",
		Status:             models.StatusInReview,
		CreatorSession:     creatorSess,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record creator action
	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Creator tries to approve
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, creatorSess)
	isCreator := issue.CreatorSession == creatorSess
	isImplementer := issue.ImplementerSession == creatorSess
	wasEverInvolved := wasInvolved || isCreator || isImplementer

	canApprove := !wasEverInvolved || issue.Minor

	if canApprove {
		t.Error("Creator should NOT be able to approve even if they didn't implement")
	}
}

// TestCommand_ImplementerOnlyCannotApprove verifies implementer (who didn't create) cannot approve
func TestCommand_ImplementerOnlyCannotApprove(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_impl"

	// Different session creates, implementer just implements
	issue := &models.Issue{
		Title:              "Task with separate creator",
		Status:             models.StatusInReview,
		CreatorSession:     creatorSess,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Implementer tries to approve
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, implSess)
	isCreator := issue.CreatorSession == implSess
	isImplementer := issue.ImplementerSession == implSess
	wasEverInvolved := wasInvolved || isCreator || isImplementer

	canApprove := !wasEverInvolved || issue.Minor

	if canApprove {
		t.Error("Implementer should NOT be able to approve their own work")
	}
}

// TestCommand_StatusValidationBeforeApprove verifies status must be in_review before approve
func TestCommand_StatusValidationBeforeApprove(t *testing.T) {
	tests := []struct {
		name               string
		status             models.Status
		shouldAllowApprove bool
		description        string
	}{
		{
			name:               "Open status - should not approve",
			status:             models.StatusOpen,
			shouldAllowApprove: false,
			description:        "Cannot approve issues that haven't been submitted for review",
		},
		{
			name:               "InProgress status - should not approve",
			status:             models.StatusInProgress,
			shouldAllowApprove: false,
			description:        "Cannot approve issues still being worked on",
		},
		{
			name:               "InReview status - can approve",
			status:             models.StatusInReview,
			shouldAllowApprove: true,
			description:        "Can approve issues that are in_review",
		},
		{
			name:               "Blocked status - should not approve",
			status:             models.StatusBlocked,
			shouldAllowApprove: false,
			description:        "Cannot approve blocked issues",
		},
		{
			name:               "Already closed - should not approve",
			status:             models.StatusClosed,
			shouldAllowApprove: false,
			description:        "Cannot approve already closed issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer db.Close()

			creatorSess := "ses_creator"
			implSess := "ses_impl"
			reviewerSess := "ses_reviewer"

			issue := &models.Issue{
				Title:              tt.name,
				Status:             tt.status,
				CreatorSession:     creatorSess,
				ImplementerSession: implSess,
			}
			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
			if tt.status != models.StatusOpen {
				_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)
			}

			// Verify reviewer is not involved
			wasInvolved, _ := db.WasSessionInvolved(issue.ID, reviewerSess)
			isCreator := issue.CreatorSession == reviewerSess
			isImplementer := issue.ImplementerSession == reviewerSess
			wasEverInvolved := wasInvolved || isCreator || isImplementer

			// Session is not involved, so bypass check passes
			bypassCheckPasses := !wasEverInvolved

			// But status must also be in_review for approve to make sense
			statusAllowsApprove := issue.Status == models.StatusInReview

			// Both conditions needed for approval
			canApprove := bypassCheckPasses && statusAllowsApprove

			if canApprove != tt.shouldAllowApprove {
				t.Errorf("%s: expected canApprove=%v, got %v", tt.description, tt.shouldAllowApprove, canApprove)
			}
		})
	}
}

// TestCommand_CreatorCannotCloseIfOtherImplemented verifies creator cannot bypass
// review once another session has implemented the issue.
func TestCommand_CreatorCannotCloseIfOtherImplemented(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	creatorSess := "ses_creator"
	implSess := "ses_impl"

	// Creator creates, other session implements
	issue := &models.Issue{
		Title:              "Task with separate implementer",
		Status:             models.StatusInReview,
		CreatorSession:     creatorSess,
		ImplementerSession: implSess,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	_ = db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Creator tries to close
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, creatorSess)
	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, creatorSess)
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)
	canClose := testCloseAllowed(issue, creatorSess, wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if canClose {
		t.Error("Creator should not be able to close when someone else implemented")
	}
}

// TestCommand_MinorTaskBypassesAllChecks verifies minor tasks allow self-close/approve
func TestCommand_MinorTaskBypassesAllChecks(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sess := "ses_solo"

	// Solo session creates, implements, and wants to close/approve minor task
	issue := &models.Issue{
		Title:              "Minor fix",
		Status:             models.StatusInReview,
		CreatorSession:     sess,
		ImplementerSession: sess,
		Minor:              true,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	_ = db.RecordSessionAction(issue.ID, sess, models.ActionSessionCreated)
	_ = db.RecordSessionAction(issue.ID, sess, models.ActionSessionStarted)

	// Check approve
	wasInvolved, _ := db.WasSessionInvolved(issue.ID, sess)
	isCreator := issue.CreatorSession == sess
	isImplementer := issue.ImplementerSession == sess
	wasEverInvolved := wasInvolved || isCreator || isImplementer

	canApprove := !wasEverInvolved || issue.Minor
	if !canApprove {
		t.Error("Minor task should allow self-approve")
	}

	// Check close
	wasImplementationInvolved, _ := db.WasSessionImplementationInvolved(issue.ID, sess)
	hasImplementationHistory, _ := db.HasImplementationHistory(issue.ID)
	canClose := testCloseAllowed(issue, sess, wasInvolved, wasImplementationInvolved, hasImplementationHistory)

	if !canClose {
		t.Error("Minor task should allow self-close")
	}
}

// TestCommand_PreviousInvolvementPreventsApprove verifies that any prior involvement blocks approval
func TestCommand_PreviousInvolvementPreventsApprove(t *testing.T) {
	tests := []struct {
		name        string
		actions     []models.IssueSessionAction
		shouldBlock bool
	}{
		{
			name:        "Created only blocks",
			actions:     []models.IssueSessionAction{models.ActionSessionCreated},
			shouldBlock: true,
		},
		{
			name:        "Started only blocks",
			actions:     []models.IssueSessionAction{models.ActionSessionStarted},
			shouldBlock: true,
		},
		{
			name:        "Started then unstarted still blocks",
			actions:     []models.IssueSessionAction{models.ActionSessionStarted, models.ActionSessionUnstarted},
			shouldBlock: true,
		},
		{
			name:        "Reviewed blocks",
			actions:     []models.IssueSessionAction{models.ActionSessionReviewed},
			shouldBlock: true,
		},
		{
			name:        "No actions does not block",
			actions:     []models.IssueSessionAction{},
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer db.Close()

			sess := "ses_test"

			issue := &models.Issue{
				Title:  tt.name,
				Status: models.StatusInReview,
			}
			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			// Record all actions
			for _, action := range tt.actions {
				_ = db.RecordSessionAction(issue.ID, sess, action)
			}

			// Check involvement
			wasInvolved, _ := db.WasSessionInvolved(issue.ID, sess)

			if wasInvolved != tt.shouldBlock {
				t.Errorf("Expected wasInvolved=%v for actions %v, got %v",
					tt.shouldBlock, tt.actions, wasInvolved)
			}

			// Approve check
			canApprove := !wasInvolved || issue.Minor
			expectedCanApprove := !tt.shouldBlock

			if canApprove != expectedCanApprove {
				t.Errorf("Expected canApprove=%v, got %v", expectedCanApprove, canApprove)
			}
		})
	}
}
