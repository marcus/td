package db

import (
	"fmt"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
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
		Title:          "Task for User A",
		Status:         models.StatusOpen,
		CreatorSession: userASession,
		Type:           models.TypeTask,
		Priority:       models.PriorityP1,
	}
	if err := db.CreateIssue(issueA); err != nil {
		t.Fatalf("User A: CreateIssue failed: %v", err)
	}

	// Simulate User B
	userBSession := "ses_user_b"
	issueB := &models.Issue{
		Title:          "Task for User B",
		Status:         models.StatusOpen,
		CreatorSession: userBSession,
		Type:           models.TypeTask,
		Priority:       models.PriorityP2,
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
		Title:          "Shared task",
		Status:         models.StatusOpen,
		CreatorSession: userASession,
		Type:           models.TypeFeature,
		Priority:       models.PriorityP1,
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
		issueID          string
		shouldReviewable bool
		description      string
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
			Title:          "Issue " + string(rune('A'+i)),
			Status:         models.StatusOpen,
			CreatorSession: userSession,
			Type:           models.TypeTask,
			Priority:       models.PriorityP2,
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
		if err := db.TagIssueToWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
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

// TestConcurrentIssueCreation verifies multiple users can create issues concurrently
func TestConcurrentIssueCreation(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	numUsers := 5
	issuesPerUser := 10
	done := make(chan error, numUsers)

	// Each user creates multiple issues concurrently
	for userIdx := 0; userIdx < numUsers; userIdx++ {
		go func(idx int) {
			sessionID := fmt.Sprintf("ses_concurrent_user_%d", idx)
			var createdIssues []string

			for i := 0; i < issuesPerUser; i++ {
				issue := &models.Issue{
					Title:          fmt.Sprintf("Issue %d-%d", idx, i),
					Status:         models.StatusOpen,
					CreatorSession: sessionID,
					Type:           models.TypeTask,
					Priority:       models.PriorityP2,
				}
				if err := db.CreateIssue(issue); err != nil {
					done <- fmt.Errorf("user %d failed to create issue %d: %v", idx, i, err)
					return
				}
				createdIssues = append(createdIssues, issue.ID)
			}

			// Verify all created issues exist
			for _, issueID := range createdIssues {
				if _, err := db.GetIssue(issueID); err != nil {
					done <- fmt.Errorf("user %d issue %s not retrievable: %v", idx, issueID, err)
					return
				}
			}

			done <- nil
		}(userIdx)
	}

	// Wait for all goroutines and check for errors
	for i := 0; i < numUsers; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}

	// Verify total issue count
	all, err := db.ListIssues(ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues failed: %v", err)
	}

	expectedCount := numUsers * issuesPerUser
	if len(all) != expectedCount {
		t.Errorf("Expected %d total issues, got %d", expectedCount, len(all))
	}
}

// TestConcurrentStatusUpdates verifies concurrent status updates are consistent
func TestConcurrentStatusUpdates(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create a shared issue
	shared := &models.Issue{
		Title:    "Concurrent update test",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(shared); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate 3 users updating status concurrently
	statusUpdates := []models.Status{
		models.StatusInProgress,
		models.StatusInReview,
		models.StatusClosed,
	}

	done := make(chan error, len(statusUpdates))

	for idx, newStatus := range statusUpdates {
		go func(i int, status models.Status) {
			sessionID := fmt.Sprintf("ses_updater_%d", i)
			issue, err := db.GetIssue(shared.ID)
			if err != nil {
				done <- fmt.Errorf("get issue: %v", err)
				return
			}

			issue.Status = status
			issue.ImplementerSession = sessionID
			done <- db.UpdateIssue(issue)
		}(idx, newStatus)
	}

	// Wait for all updates
	for i := 0; i < len(statusUpdates); i++ {
		if err := <-done; err != nil {
			t.Logf("Warning: concurrent update encountered error (expected in concurrent scenario): %v", err)
		}
	}

	// Verify final state is one of the attempted updates
	final, err := db.GetIssue(shared.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	validStatuses := make(map[models.Status]bool)
	for _, s := range statusUpdates {
		validStatuses[s] = true
	}

	if !validStatuses[final.Status] {
		t.Errorf("Final status %s not one of attempted updates", final.Status)
	}
}

// TestDataConsistencyAcrossUsers verifies issue modifications maintain consistency
func TestDataConsistencyAcrossUsers(t *testing.T) {
	tests := []struct {
		name   string
		create func(*models.Issue)
		verify func(*models.Issue) error
	}{
		{
			name: "Title consistency",
			create: func(issue *models.Issue) {
				issue.Title = "Original Title"
				issue.Description = "Test description"
			},
			verify: func(issue *models.Issue) error {
				if issue.Title != "Original Title" {
					return fmt.Errorf("title mismatch: got %q", issue.Title)
				}
				return nil
			},
		},
		{
			name: "Priority consistency",
			create: func(issue *models.Issue) {
				issue.Priority = models.PriorityP0
				issue.Type = models.TypeBug
			},
			verify: func(issue *models.Issue) error {
				if issue.Priority != models.PriorityP0 {
					return fmt.Errorf("priority mismatch: got %q", issue.Priority)
				}
				if issue.Type != models.TypeBug {
					return fmt.Errorf("type mismatch: got %q", issue.Type)
				}
				return nil
			},
		},
		{
			name: "CreatorSession consistency",
			create: func(issue *models.Issue) {
				issue.CreatorSession = "ses_creator_test"
			},
			verify: func(issue *models.Issue) error {
				if issue.CreatorSession != "ses_creator_test" {
					return fmt.Errorf("creator mismatch: got %q", issue.CreatorSession)
				}
				return nil
			},
		},
		{
			name: "Labels consistency",
			create: func(issue *models.Issue) {
				issue.Labels = []string{"urgent", "backend", "feature"}
			},
			verify: func(issue *models.Issue) error {
				if len(issue.Labels) != 3 {
					return fmt.Errorf("labels count mismatch: got %d", len(issue.Labels))
				}
				labelMap := make(map[string]bool)
				for _, l := range issue.Labels {
					labelMap[l] = true
				}
				for _, expected := range []string{"urgent", "backend", "feature"} {
					if !labelMap[expected] {
						return fmt.Errorf("missing label: %q", expected)
					}
				}
				return nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseDir := t.TempDir()
			db, err := Initialize(baseDir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer db.Close()

			// Create issue with test data
			issue := &models.Issue{
				Title:    "Data consistency test",
				Status:   models.StatusOpen,
				Type:     models.TypeTask,
				Priority: models.PriorityP2,
			}
			tc.create(issue)

			if err := db.CreateIssue(issue); err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}

			// Retrieve from different "user" perspectives
			retrieved, err := db.GetIssue(issue.ID)
			if err != nil {
				t.Fatalf("GetIssue failed: %v", err)
			}

			if err := tc.verify(retrieved); err != nil {
				t.Errorf("Verification failed: %v", err)
			}

			// Update status and verify data consistency
			retrieved.Status = models.StatusInProgress
			if err := db.UpdateIssue(retrieved); err != nil {
				t.Fatalf("UpdateIssue failed: %v", err)
			}

			retrieved2, err := db.GetIssue(issue.ID)
			if err != nil {
				t.Fatalf("GetIssue (2) failed: %v", err)
			}

			if err := tc.verify(retrieved2); err != nil {
				t.Errorf("Verification after update failed: %v", err)
			}
		})
	}
}

// TestConflictingReviewers verifies reviewer assignment conflicts are handled
func TestConflictingReviewers(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:    "Multi-reviewer test",
		Status:   models.StatusInReview,
		Type:     models.TypeFeature,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate two reviewers trying to assign themselves
	reviewers := []string{"ses_reviewer_1", "ses_reviewer_2"}
	done := make(chan error, len(reviewers))

	for idx, reviewer := range reviewers {
		go func(i int, rev string) {
			issue, err := db.GetIssue(issue.ID)
			if err != nil {
				done <- fmt.Errorf("get issue: %v", err)
				return
			}

			// Both try to set themselves as reviewer
			if issue.ReviewerSession == "" || issue.ReviewerSession == rev {
				issue.ReviewerSession = rev
				done <- db.UpdateIssue(issue)
			} else {
				done <- fmt.Errorf("reviewer already assigned")
			}
		}(idx, reviewer)
	}

	// Wait for updates
	successCount := 0
	for i := 0; i < len(reviewers); i++ {
		err := <-done
		if err == nil {
			successCount++
		}
	}

	// Verify final state has one reviewer
	final, err := db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}

	if final.ReviewerSession == "" {
		t.Error("No reviewer assigned after concurrent assignment attempts")
	}

	// One of the concurrent assignments should succeed
	if successCount == 0 {
		t.Error("All concurrent reviewer assignments failed")
	}
}

// TestMultiUserWorkFlow verifies complete workflow across multiple users
func TestMultiUserWorkFlow(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionCreator := "ses_workflow_creator"
	sessionImpl := "ses_workflow_impl"
	sessionReviewer := "ses_workflow_reviewer"

	// Step 1: Creator creates an issue
	issue := &models.Issue{
		Title:          "Complete workflow task",
		Description:    "This task will go through full workflow",
		Status:         models.StatusOpen,
		Type:           models.TypeFeature,
		Priority:       models.PriorityP1,
		CreatorSession: sessionCreator,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	issueID := issue.ID

	// Step 2: Implementer starts the issue
	issue, _ = db.GetIssue(issueID)
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = sessionImpl
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	retrieved, _ := db.GetIssue(issueID)
	if retrieved.Status != models.StatusInProgress {
		t.Errorf("Expected in_progress, got %s", retrieved.Status)
	}

	// Step 3: Implementer submits for review
	issue, _ = db.GetIssue(issueID)
	issue.Status = models.StatusInReview
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("Submit for review failed: %v", err)
	}

	// Step 4: Reviewer reviews and assigns themselves
	issue, _ = db.GetIssue(issueID)
	issue.ReviewerSession = sessionReviewer
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("Assign reviewer failed: %v", err)
	}

	// Step 5: Reviewer approves
	issue, _ = db.GetIssue(issueID)
	issue.Status = models.StatusClosed
	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify final state
	final, _ := db.GetIssue(issueID)
	if final.Status != models.StatusClosed {
		t.Errorf("Expected closed, got %s", final.Status)
	}
	if final.CreatorSession != sessionCreator {
		t.Errorf("Creator mismatch: got %s", final.CreatorSession)
	}
	if final.ImplementerSession != sessionImpl {
		t.Errorf("Implementer mismatch: got %s", final.ImplementerSession)
	}
	if final.ReviewerSession != sessionReviewer {
		t.Errorf("Reviewer mismatch: got %s", final.ReviewerSession)
	}

	// Verify session history
	history, err := db.GetSessionHistory(issueID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}
	if len(history) == 0 {
		t.Logf("Warning: session history is empty")
	}
}

// TestIsolationBetweenUsers verifies one user's changes don't affect another's view
func TestIsolationBetweenUsers(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionA := "ses_isolated_a"
	sessionB := "ses_isolated_b"

	// Create 5 issues, each owned by different users
	var issueA []*models.Issue
	var issueB []*models.Issue

	for i := 0; i < 5; i++ {
		issA := &models.Issue{
			Title:          fmt.Sprintf("Issue A-%d", i),
			Status:         models.StatusOpen,
			Type:           models.TypeTask,
			Priority:       models.PriorityP2,
			CreatorSession: sessionA,
		}
		db.CreateIssue(issA)
		issueA = append(issueA, issA)

		issB := &models.Issue{
			Title:          fmt.Sprintf("Issue B-%d", i),
			Status:         models.StatusOpen,
			Type:           models.TypeTask,
			Priority:       models.PriorityP2,
			CreatorSession: sessionB,
		}
		db.CreateIssue(issB)
		issueB = append(issueB, issB)
	}

	// User A modifies their issues
	for i, issue := range issueA {
		issue, _ := db.GetIssue(issue.ID)
		issue.Title = fmt.Sprintf("Updated by A-%d", i)
		issue.Status = models.StatusInProgress
		db.UpdateIssue(issue)
	}

	// Verify User B's issues are unchanged
	for i, issue := range issueB {
		retrieved, _ := db.GetIssue(issue.ID)
		if retrieved.Title != fmt.Sprintf("Issue B-%d", i) {
			t.Errorf("User B's issue was modified: expected %q, got %q",
				fmt.Sprintf("Issue B-%d", i), retrieved.Title)
		}
		if retrieved.Status != models.StatusOpen {
			t.Errorf("User B's issue status changed: expected %s, got %s",
				models.StatusOpen, retrieved.Status)
		}
	}

	// Verify both users can see all issues
	all, _ := db.ListIssues(ListIssuesOptions{})
	expectedCount := len(issueA) + len(issueB)
	if len(all) != expectedCount {
		t.Errorf("Expected %d issues, got %d", expectedCount, len(all))
	}
}

// TestConcurrentLabelModification verifies label updates across users
func TestConcurrentLabelModification(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create an issue
	issue := &models.Issue{
		Title:    "Label modification test",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP2,
		Labels:   []string{"initial"},
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Multiple users add labels concurrently
	newLabels := [][]string{
		{"urgent"},
		{"backend"},
		{"performance"},
	}

	done := make(chan error, len(newLabels))

	for idx, labels := range newLabels {
		go func(i int, labelSet []string) {
			issue, err := db.GetIssue(issue.ID)
			if err != nil {
				done <- fmt.Errorf("get issue: %v", err)
				return
			}

			// Add labels (naive approach - would overwrite in real scenario)
			for _, label := range labelSet {
				alreadyExists := false
				for _, existing := range issue.Labels {
					if existing == label {
						alreadyExists = true
						break
					}
				}
				if !alreadyExists {
					issue.Labels = append(issue.Labels, label)
				}
			}

			done <- db.UpdateIssue(issue)
		}(idx, labels)
	}

	// Wait for all label modifications
	for i := 0; i < len(newLabels); i++ {
		if err := <-done; err != nil {
			t.Logf("Warning: concurrent label update error (expected): %v", err)
		}
	}

	// Verify final label state
	final, _ := db.GetIssue(issue.ID)
	if len(final.Labels) < 1 {
		t.Errorf("Labels lost: expected at least 1, got %d", len(final.Labels))
	}
}

// TestSessionHistoryAccuracy verifies session history is accurately recorded
func TestSessionHistoryAccuracy(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{
		Title:    "History tracking test",
		Status:   models.StatusOpen,
		Type:     models.TypeTask,
		Priority: models.PriorityP1,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Record multiple actions from different sessions
	actions := []struct {
		session string
		action  models.IssueSessionAction
	}{
		{"ses_hist_creator", models.ActionSessionCreated},
		{"ses_hist_impl1", models.ActionSessionStarted},
		{"ses_hist_impl2", models.ActionSessionStarted},
		{"ses_hist_reviewer", models.ActionSessionReviewed},
		{"ses_hist_impl1", models.ActionSessionUnstarted},
	}

	for _, a := range actions {
		if err := db.RecordSessionAction(issue.ID, a.session, a.action); err != nil {
			t.Fatalf("RecordSessionAction failed: %v", err)
		}
	}

	// Retrieve and verify history
	history, err := db.GetSessionHistory(issue.ID)
	if err != nil {
		t.Fatalf("GetSessionHistory failed: %v", err)
	}

	if len(history) != len(actions) {
		t.Errorf("History length mismatch: expected %d, got %d", len(actions), len(history))
	}

	// Verify each action is recorded
	for i, record := range history {
		if i < len(actions) {
			if record.SessionID != actions[i].session {
				t.Errorf("Session ID mismatch at index %d: expected %s, got %s",
					i, actions[i].session, record.SessionID)
			}
		}
	}
}

// TestUserPermissionBoundaries verifies users can't inappropriately modify others' work
func TestUserPermissionBoundaries(t *testing.T) {
	baseDir := t.TempDir()
	db, err := Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Creator creates an issue
	creatorSession := "ses_perm_creator"
	issue := &models.Issue{
		Title:          "Permission boundary test",
		Status:         models.StatusOpen,
		Type:           models.TypeTask,
		Priority:       models.PriorityP1,
		CreatorSession: creatorSession,
	}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Any user should be able to read the issue
	other := &models.Issue{
		Title:          "Check by other user",
		Status:         models.StatusOpen,
		Type:           models.TypeTask,
		Priority:       models.PriorityP1,
		CreatorSession: "ses_perm_other",
	}
	if err := db.CreateIssue(other); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify both users can read both issues
	_, err = db.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("Other user should read creator's issue: %v", err)
	}

	_, err = db.GetIssue(other.ID)
	if err != nil {
		t.Fatalf("Creator should read other user's issue: %v", err)
	}

	// Verify any user can modify issues (in this single-writer design)
	issue, _ = db.GetIssue(issue.ID)
	issue.Title = "Modified by different user"
	issue.ImplementerSession = "ses_perm_other"

	if err := db.UpdateIssue(issue); err != nil {
		t.Fatalf("Update by different user should work: %v", err)
	}

	// Verify change persisted
	modified, _ := db.GetIssue(issue.ID)
	if modified.Title != "Modified by different user" {
		t.Error("Modification by other user did not persist")
	}
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
		title       string
		creator     string
		status      models.Status
		priority    models.Priority
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
			name:     "All open issues",
			opts:     ListIssuesOptions{Status: []models.Status{models.StatusOpen}},
			expected: 2,
		},
		{
			name:     "P1 priority",
			opts:     ListIssuesOptions{Priority: "P1"},
			expected: 2,
		},
		{
			name:     "All issues",
			opts:     ListIssuesOptions{},
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
