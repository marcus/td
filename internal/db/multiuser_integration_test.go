package db

import (
	"os"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// TestMultiUserIssueIndependence verifies multiple users can work on same repo independently
func TestMultiUserIssueIndependence(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Simulate User A
	userASession := "ses_user_a"
	issueA := &models.Issue{
		Title:           "Task for User A",
		Status:          models.StatusOpen,
		CreatorSession:  userASession,
		Type:            models.TypeTask,
		Priority:        models.PriorityP1,
	}
	if err := db.CreateIssue(issueA); err != nil {
		t.Fatalf("User A: CreateIssue failed: %v", err)
	}

	// Simulate User B
	userBSession := "ses_user_b"
	issueB := &models.Issue{
		Title:           "Task for User B",
		Status:          models.StatusOpen,
		CreatorSession:  userBSession,
		Type:            models.TypeTask,
		Priority:        models.PriorityP2,
	}
	if err := db.CreateIssue(issueB); err != nil {
		t.Fatalf("User B: CreateIssue failed: %v", err)
	}

	// User A and B should have different issue IDs
	if issueA.ID == issueB.ID {
		t.Errorf("Issues should have different IDs: %s vs %s", issueA.ID, issueB.ID)
	}

	// Verify both issues exist and are distinct
	retrievedA, err := db.GetIssue(issueA.ID)
	if err != nil {
		t.Fatalf("GetIssue for A failed: %v", err)
	}
	if retrievedA.CreatorSession != userASession {
		t.Errorf("User A issue creator session mismatch: %s != %s", retrievedA.CreatorSession, userASession)
	}

	retrievedB, err := db.GetIssue(issueB.ID)
	if err != nil {
		t.Fatalf("GetIssue for B failed: %v", err)
	}
	if retrievedB.CreatorSession != userBSession {
		t.Errorf("User B issue creator session mismatch: %s != %s", retrievedB.CreatorSession, userBSession)
	}
}

// TestSessionFingerprinting verifies session fingerprints distinguish users
func TestSessionFingerprinting(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantAgent string
	}{
		{
			name:      "explicit session ID",
			sessionID: "my-test-session",
			wantAgent: "explicit",
		},
		{
			name:      "explicit session with special chars",
			sessionID: "user@example.com-session",
			wantAgent: "explicit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set explicit session ID
			oldVal := os.Getenv("TD_SESSION_ID")
			defer func() {
				if oldVal != "" {
					os.Setenv("TD_SESSION_ID", oldVal)
				} else {
					os.Unsetenv("TD_SESSION_ID")
				}
			}()

			os.Setenv("TD_SESSION_ID", tc.sessionID)

			// Get fingerprint
			fp := session.GetAgentFingerprint()
			fpString := fp.String()

			if fp.Type != session.AgentType(tc.wantAgent) {
				t.Errorf("agent type mismatch: got %s, want %s", fp.Type, tc.wantAgent)
			}

			// Verify fingerprint string is filesystem-safe
			if !isFilesystemSafe(fpString) {
				t.Errorf("fingerprint string not filesystem-safe: %q", fpString)
			}
		})
	}
}

// TestIssuesVisibleAcrossUsers verifies created issues are visible to all users
func TestIssuesVisibleAcrossUsers(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// User A creates an issue
	userASession := "ses_creator_a"
	issue := &models.Issue{
		Title:           "Shared task",
		Status:          models.StatusOpen,
		CreatorSession:  userASession,
		Type:            models.TypeFeature,
		Priority:        models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// User B should be able to see the issue
	userBSession := "ses_viewer_b"
	retrieved, err := db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if retrieved.CreatorSession != userASession {
		t.Errorf("Creator session should be %s, got %s", userASession, retrieved.CreatorSession)
	}

	// Both users should see the issue in listings
	all, err := db.ListIssues(ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	found := false
	for _, i := range all {
		if i.ID == issue.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Issue not found in listing by both users")
	}

	_ = userBSession // User B can see the issue
}

// TestSessionHistoryTracking verifies session interactions are recorded
func TestSessionHistoryTracking(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:    "Tracked issue",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate different sessions interacting
	sessions := []string{"ses_user_1", "ses_user_2", "ses_user_3"}
	actions := []models.IssueSessionAction{
		models.ActionSessionCreated,
		models.ActionSessionStarted,
		models.ActionSessionReviewed,
	}

	// Record interactions
	for i, sessionID := range sessions {
		action := actions[i%len(actions)]
		if err := db.RecordSessionAction(issue.ID, sessionID, action); err != nil {
			t.Fatalf("RecordSessionAction failed for session %s: %v", sessionID, err)
		}
	}

	// Retrieve history
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != len(sessions) {
		t.Errorf("History length mismatch: got %d, want %d", len(history), len(sessions))
	}

	// Verify all sessions are recorded
	sessionMap := make(map[string]int)
	for _, h := range history {
		sessionMap[h.SessionID]++
	}

	for _, sessionID := range sessions {
		if sessionMap[sessionID] == 0 {
			t.Errorf("Session %s not found in history", sessionID)
		}
	}
}

// TestConcurrentUserOperations verifies concurrent operations don't conflict
func TestConcurrentUserOperations(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a shared issue
	shared := &models.Issue{
		Title:    "Concurrent work",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(shared); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate concurrent operations
	done := make(chan error, 3)

	// User A starts the issue
	go func() {
		userASession := "ses_concurrent_a"
		issue, err := db.GetIssue(shared.ID)
		if err != nil {
			done <- err
			return
		}
		issue.ImplementerSession = userASession
		issue.Status = models.StatusInProgress
		done <- db.UpdateIssue(issue)
	}()

	// User B records activity
	go func() {
		userBSession := "ses_concurrent_b"
		done <- db.RecordSessionAction(shared.ID, userBSession, models.ActionSessionReviewed)
	}()

	// User C records activity
	go func() {
		userCSession := "ses_concurrent_c"
		done <- db.RecordSessionAction(shared.ID, userCSession, models.ActionSessionCreated)
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent operation failed: %v", err)
		}
	}

	// Verify final state
	final, err := db.GetIssue(shared.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if final.Status != models.StatusInProgress {
		t.Errorf("Status should be in_progress, got %s", final.Status)
	}

	history, err := db.GetSessionHistory(shared.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) < 2 {
		t.Errorf("Expected at least 2 history entries, got %d", len(history))
	}
}

// TestReviewableByLogic verifies multi-user review workflows
func TestReviewableByLogic(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionA := "ses_reviewer_a"
	sessionB := "ses_impl_b"
	sessionC := "ses_creator_c"

	// Helper to create issue and set implementer
	createIssueWithImpl := func(issue *models.Issue) {
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Test 1: Non-implementer, non-creator can review clean task
	issue1 := &models.Issue{
		Title:              "Clean task for A",
		Status:             models.StatusInReview,
		ImplementerSession: sessionB,
		CreatorSession:     sessionC,
		Type:               models.TypeTask,
		Priority:           models.PriorityP1,
		Minor:              false,
	}
	createIssueWithImpl(issue1)

	// Test 2: Creator cannot review
	issue2 := &models.Issue{
		Title:              "Creator is reviewer",
		Status:             models.StatusInReview,
		ImplementerSession: sessionB,
		CreatorSession:     sessionA,
		Type:               models.TypeTask,
		Priority:           models.PriorityP1,
		Minor:              false,
	}
	createIssueWithImpl(issue2)

	// Test 3: Implementer cannot review non-minor
	issue3 := &models.Issue{
		Title:              "Impl cannot review",
		Status:             models.StatusInReview,
		ImplementerSession: sessionA,
		CreatorSession:     sessionC,
		Type:               models.TypeTask,
		Priority:           models.PriorityP1,
		Minor:              false,
	}
	createIssueWithImpl(issue3)

	// Test 4: Minor task - implementer can self-review
	issue4 := &models.Issue{
		Title:              "Minor self-review",
		Status:             models.StatusInReview,
		ImplementerSession: sessionA,
		CreatorSession:     sessionC,
		Type:               models.TypeTask,
		Priority:           models.PriorityP3,
		Minor:              true,
	}
	createIssueWithImpl(issue4)

	// Verify Session A can review exactly issue1 and issue4
	reviewable, err := db.ListIssues(ListIssuesOptions{ReviewableBy: sessionA})
	if err != nil {
		t.Fatalf("ListIssues with ReviewableBy failed: %v", err)
	}

	reviewableMap := make(map[string]bool)
	for _, issue := range reviewable {
		reviewableMap[issue.ID] = true
	}

	testCases := []struct {
		issueID         string
		shouldReviewable bool
		description     string
	}{
		{issue1.ID, true, "Clean task should be reviewable"},
		{issue2.ID, false, "Creator should not be able to review"},
		{issue3.ID, false, "Implementer should not be able to review"},
		{issue4.ID, true, "Minor task should be self-reviewable"},
	}

	for _, tc := range testCases {
		if reviewableMap[tc.issueID] != tc.shouldReviewable {
			t.Errorf("%s: expected %v, got %v", tc.description, tc.shouldReviewable, reviewableMap[tc.issueID])
		}
	}
}

// TestMultipleIssuesPerUser verifies users can create and manage multiple issues
func TestMultipleIssuesPerUser(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	userSession := "ses_prolific_user"
	issueCount := 5

	// Create multiple issues from same user
	issueIDs := make([]string, issueCount)
	for i := 0; i < issueCount; i++ {
		issue := &models.Issue{
			Title:           "Issue " + string(rune('A'+i)),
			Status:          models.StatusOpen,
			CreatorSession:  userSession,
			Type:            models.TypeTask,
			Priority:        models.PriorityP2,
		}
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue %d failed: %v", i, err)
		}
		issueIDs[i] = issue.ID
	}

	// Verify all issues exist
	for _, id := range issueIDs {
		retrieved, err := db.GetIssue(id)
		if err != nil {
			t.Errorf("GetIssue failed for %s: %v", id, err)
		}
		if retrieved.CreatorSession != userSession {
			t.Errorf("Creator session mismatch for %s", id)
		}
	}

	// Update different issues with different statuses
	statusProgression := []models.Status{
		models.StatusInProgress,
		models.StatusInReview,
		models.StatusClosed,
		models.StatusBlocked,
		models.StatusOpen,
	}

	for i, id := range issueIDs {
		issue, err := db.GetIssue(id)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		issue.Status = statusProgression[i]
		if err := db.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Verify each issue has correct status
	for i, id := range issueIDs {
		issue, err := db.GetIssue(id)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if issue.Status != statusProgression[i] {
			t.Errorf("Status mismatch for issue %d: got %s, want %s",
				i, issue.Status, statusProgression[i])
		}
	}
}

// TestSessionInvolvementTracking verifies session involvement logic
func TestSessionInvolvementTracking(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:    "Involvement test",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionA := "ses_involved_a"
	sessionB := "ses_uninvolved_b"

	// Record activity for session A
	if err := db.RecordSessionAction(issue.ID, sessionA, models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction failed: %v", err)
	}

	// Check involvement
	involvementTests := []struct {
		sessionID string
		expected  bool
	}{
		{sessionA, true},
		{sessionB, false},
		{sessionA, true}, // Check again for stability
	}

	for _, test := range involvementTests {
		involved, err := db.WasSessionInvolved(issue.ID, test.sessionID)
		if err != nil {
			t.Fatalf("WasSessionInvolved failed: %v", err)
		}
		if involved != test.expected {
			t.Errorf("Session %s involvement: got %v, want %v",
				test.sessionID, involved, test.expected)
		}
	}
}

// TestMultiUserWorkSession verifies work sessions track multiple contributors
func TestMultiUserWorkSession(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a work session
	ws := &models.WorkSession{
		Name:      "Team feature work",
		SessionID: "ses_work_1",
		StartedAt: time.Now(),
	}
	if err := db.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	// Create multiple issues from different users
	issueCount := 3
	creatorIDs := []string{"ses_creator_a", "ses_creator_b", "ses_creator_c"}
	var issueIDs []string

	for i := 0; i < issueCount; i++ {
		issue := &models.Issue{
			Title:          "Feature part " + string(rune('A'+i)),
			Status:         models.StatusOpen,
			CreatorSession: creatorIDs[i],
			Type:           models.TypeFeature,
			Priority:       models.PriorityP1,
		}
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		issueIDs = append(issueIDs, issue.ID)

		// Tag to work session
		if err := db.TagIssueToWorkSession(ws.ID, issue.ID); err != nil {
			t.Fatalf("TagIssueToWorkSession failed: %v", err)
		}
	}

	// Verify work session contains all issues
	wsIssues, err := db.GetWorkSessionIssues(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSessionIssues failed: %v", err)
	}

	if len(wsIssues) != issueCount {
		t.Errorf("Work session issues count: got %d, want %d", len(wsIssues), issueCount)
	}

	// Verify all creator sessions are represented
	sessionSet := make(map[string]bool)
	for _, issueID := range wsIssues {
		issue, err := db.GetIssue(issueID)
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if issue.CreatorSession != "" {
			sessionSet[issue.CreatorSession] = true
		}
	}

	for _, sessionID := range creatorIDs {
		if !sessionSet[sessionID] {
			t.Errorf("Session %s not found in work session contributors", sessionID)
		}
	}
}

// TestReviewerAssignmentAcrossUsers verifies reviewer can be different from implementer
func TestReviewerAssignmentAcrossUsers(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create an issue with different implementer and reviewer
	implSession := "ses_implementer_1"
	reviewSession := "ses_reviewer_1"
	creatorSession := "ses_creator_1"

	issue := &models.Issue{
		Title:              "Code review task",
		Status:             models.StatusInReview,
		ImplementerSession: implSession,
		ReviewerSession:    reviewSession,
		CreatorSession:     creatorSession,
		Type:               models.TypeFeature,
		Priority:           models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Need to update to persist implementer and reviewer sessions
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Retrieve and verify assignments
	retrieved, err := db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if retrieved.ImplementerSession != implSession {
		t.Errorf("Implementer session: got %s, want %s", retrieved.ImplementerSession, implSession)
	}

	if retrieved.ReviewerSession != reviewSession {
		t.Errorf("Reviewer session: got %s, want %s", retrieved.ReviewerSession, reviewSession)
	}

	// Verify reviewer can see issues to review (if they're not implementer, creator, or in history)
	reviewable, err := db.ListIssues(ListIssuesOptions{
		ReviewableBy: reviewSession,
	})
	if err != nil {
		t.Fatalf("ListIssues with ReviewableBy failed: %v", err)
	}

	// Reviewer is different from implementer and creator, so should be reviewable
	found := false
	for _, i := range reviewable {
		if i.ID == issue.ID {
			found = true
			break
		}
	}

	if !found {
		t.Error("Reviewer should be able to see issue for review")
	}
}

// TestLogEntriesPerSession verifies session-specific logging
func TestLogEntriesPerSession(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:    "Logged work",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Different sessions log work
	sessions := []string{"ses_logger_a", "ses_logger_b", "ses_logger_c"}
	logCount := 3

	for _, sessionID := range sessions {
		for j := 0; j < logCount; j++ {
			log := &models.Log{
				IssueID:   issue.ID,
				SessionID: sessionID,
				Message:   "Work by session",
				Type:      models.LogTypeProgress,
				Timestamp: time.Now(),
			}
			if err := db.AddLog(log); err != nil {
				t.Fatalf("AddLog failed: %v", err)
			}
		}
	}

	// Retrieve logs for issue
	logs, err := db.GetLogs(issue.ID, 100)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}

	expectedCount := len(sessions) * logCount
	if len(logs) != expectedCount {
		t.Errorf("Log count: got %d, want %d", len(logs), expectedCount)
	}

	// Count logs per session
	sessionLogCount := make(map[string]int)
	for _, log := range logs {
		sessionLogCount[log.SessionID]++
	}

	for _, sessionID := range sessions {
		if sessionLogCount[sessionID] != logCount {
			t.Errorf("Logs for session %s: got %d, want %d",
				sessionID, sessionLogCount[sessionID], logCount)
		}
	}
}

// Helper function to check if string is filesystem-safe
func isFilesystemSafe(s string) bool {
	dangerousChars := []rune{'/', '\\', ':', '*', '?', '"', '<', '>', '|'}
	for _, char := range s {
		for _, danger := range dangerousChars {
			if char == danger {
				return false
			}
		}
	}
	return true
}

// TestMultiUserIssueListFiltering verifies users can filter issues appropriately
func TestMultiUserIssueListFiltering(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issues with different creators and statuses
	scenarios := []struct {
		title     string
		creator   string
		status    models.Status
		priority  models.Priority
		implementer string
	}{
		{"Issue A1", "ses_user_a", models.StatusOpen, models.PriorityP1, ""},
		{"Issue A2", "ses_user_a", models.StatusInProgress, models.PriorityP2, "ses_user_a"},
		{"Issue B1", "ses_user_b", models.StatusOpen, models.PriorityP1, ""},
		{"Issue B2", "ses_user_b", models.StatusClosed, models.PriorityP3, "ses_user_b"},
		{"Issue C1", "ses_user_c", models.StatusInReview, models.PriorityP2, "ses_user_c"},
	}

	for _, scenario := range scenarios {
		issue := &models.Issue{
			Title:              scenario.title,
			CreatorSession:     scenario.creator,
			Status:             scenario.status,
			Priority:           scenario.priority,
			ImplementerSession: scenario.implementer,
			Type:               models.TypeTask,
		}
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
	}

	// Test various filters
	filterTests := []struct {
		name     string
		opts     ListIssuesOptions
		expected int
	}{
		{
			name: "All open issues",
			opts: ListIssuesOptions{Status: []models.Status{models.StatusOpen}},
			expected: 2,
		},
		{
			name: "P1 priority",
			opts: ListIssuesOptions{Priority: "P1"},
			expected: 2,
		},
		{
			name: "All issues",
			opts: ListIssuesOptions{},
			expected: 5,
		},
	}

	for _, test := range filterTests {
		t.Run(test.name, func(t *testing.T) {
			result, err := db.ListIssues(test.opts)
			if err != nil {
				t.Fatalf("ListIssues failed: %v", err)
			}
			if len(result) != test.expected {
				t.Errorf("Expected %d issues, got %d", test.expected, len(result))
			}
		})
	}
}

// TestBlockedIssueAcrossUsers verifies blocked issues are visible to all
func TestBlockedIssueAcrossUsers(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blockerSession := "ses_blocker_user"
	blockedSession := "ses_blocked_user"

	blocker := &models.Issue{
		Title:          "Blocker issue",
		Status:         models.StatusOpen,
		Type:           models.TypeBug,
		Priority:       models.PriorityP0,
		CreatorSession: blockerSession,
	}
	if err := db.CreateIssue(blocker); err != nil {
		t.Fatalf("CreateIssue blocker failed: %v", err)
	}

	blocked := &models.Issue{
		Title:          "Blocked task",
		Status:         models.StatusBlocked,
		Type:           models.TypeTask,
		Priority:       models.PriorityP1,
		CreatorSession: blockedSession,
	}
	if err := db.CreateIssue(blocked); err != nil {
		t.Fatalf("CreateIssue blocked failed: %v", err)
	}

	// Both issues should be visible
	all, err := db.ListIssues(ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	if len(all) != 2 {
		t.Errorf("Expected 2 issues, got %d", len(all))
	}

	// Verify both users see both issues
	blocked, err = db.GetIssue(blocked.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if blocked.Status != models.StatusBlocked {
		t.Errorf("Blocked status not preserved: got %s", blocked.Status)
	}
}
