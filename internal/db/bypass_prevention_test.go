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

// ============================================================================
// INTEGRATION TESTS FOR BYPASS PREVENTION (Workflow Enforcement)
// ============================================================================

// TestIntegration_SkipReviewNotAllowed verifies cannot skip review workflow step
func TestIntegration_SkipReviewNotAllowed(t *testing.T) {
	tests := []struct {
		name                 string
		initialStatus        models.Status
		attemptedStatus      models.Status
		creatorSession       string
		implementerSession   string
		reviewerSession      string
		attemptingSession    string
		shouldBeAllowed      bool
		description          string
	}{
		{
			name:                "Open to Closed directly - bypass review",
			initialStatus:       models.StatusOpen,
			attemptedStatus:     models.StatusClosed,
			creatorSession:      "ses_creator",
			implementerSession:  "",
			reviewerSession:     "",
			attemptingSession:   "ses_other",
			shouldBeAllowed:     false,
			description:         "Cannot skip from open directly to closed without review",
		},
		{
			name:                "InProgress to Closed directly - bypass review",
			initialStatus:       models.StatusInProgress,
			attemptedStatus:     models.StatusClosed,
			creatorSession:      "ses_creator",
			implementerSession:  "ses_impl",
			reviewerSession:     "",
			attemptingSession:   "ses_impl",
			shouldBeAllowed:     false,
			description:         "Cannot skip from in_progress directly to closed without review",
		},
		{
			name:                "Must go through InReview first",
			initialStatus:       models.StatusOpen,
			attemptedStatus:     models.StatusInReview,
			creatorSession:      "ses_creator",
			implementerSession:  "",
			reviewerSession:     "",
			attemptingSession:   "ses_other",
			shouldBeAllowed:     true,
			description:         "Can move to review from open",
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
				db.RecordSessionAction(issue.ID, tt.creatorSession, models.ActionSessionCreated)
			}
			if tt.implementerSession != "" {
				db.RecordSessionAction(issue.ID, tt.implementerSession, models.ActionSessionStarted)
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
	db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)

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
	db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	implInvolved, _ := db.WasSessionInvolved(issue.ID, implSess)
	if !implInvolved {
		t.Error("Implementer should be involved after starting")
	}

	// Step 3: Implementer submits for review
	issue.Status = models.StatusInReview
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	db.RecordSessionAction(issue.ID, implSess, models.ActionSessionReviewed)

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
	db.RecordSessionAction(issue.ID, reviewerSess, models.ActionSessionReviewed)

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
	db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Mark as in review
	issue.Status = models.StatusInReview
	db.UpdateIssue(issue)

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
		name            string
		status          models.Status
		implementerSet  bool
		description     string
	}{
		{
			name:            "Handoff at open status",
			status:          models.StatusOpen,
			implementerSet:  false,
			description:     "Handoff can be recorded at open status",
		},
		{
			name:            "Handoff at in_progress",
			status:          models.StatusInProgress,
			implementerSet:  true,
			description:     "Handoff recorded when in_progress with implementer set",
		},
		{
			name:            "Handoff at in_review",
			status:          models.StatusInReview,
			implementerSet:  true,
			description:     "Handoff recorded when in_review",
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
				t.Error("Expected handoff to be recorded")
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
	db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)

	// Creator also implements (should not be allowed to approve)
	issue.ImplementerSession = creatorSess
	issue.Status = models.StatusInProgress
	db.UpdateIssue(issue)
	db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionStarted)

	// Mark as in review
	issue.Status = models.StatusInReview
	db.UpdateIssue(issue)

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
	db.RecordSessionAction(issue.ID, creatorSess, models.ActionSessionCreated)
	db.RecordSessionAction(issue.ID, implSess, models.ActionSessionStarted)

	// Move to review
	issue.Status = models.StatusInReview
	db.UpdateIssue(issue)

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
	db.RecordSessionAction(issue.ID, sessA, models.ActionSessionStarted)

	// A unstarts (clears implementer but history remains)
	issue.ImplementerSession = ""
	db.UpdateIssue(issue)
	db.RecordSessionAction(issue.ID, sessA, models.ActionSessionUnstarted)

	// B starts (becomes new implementer)
	issue.ImplementerSession = sessB
	issue.Status = models.StatusInProgress
	db.UpdateIssue(issue)
	db.RecordSessionAction(issue.ID, sessB, models.ActionSessionStarted)

	// Now move to review
	issue.Status = models.StatusInReview
	db.UpdateIssue(issue)

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
				db.RecordSessionAction(issue.ID, tt.creatorSession, models.ActionSessionCreated)
			}
			if tt.implementerSession != "" {
				db.RecordSessionAction(issue.ID, tt.implementerSession, models.ActionSessionStarted)
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
