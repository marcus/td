package cmd

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestClearFocusIfNeeded(t *testing.T) {
	dir := t.TempDir()

	// Initialize database to create .todos directory
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	database.Close()

	// Set focus on an issue
	targetID := "td-test123"
	if err := config.SetFocus(dir, targetID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Verify focus is set
	focused, _ := config.GetFocus(dir)
	if focused != targetID {
		t.Fatalf("Focus not set: got %q, want %q", focused, targetID)
	}

	// Clear focus with matching ID
	clearFocusIfNeeded(dir, targetID)

	// Verify focus is cleared
	focused, _ = config.GetFocus(dir)
	if focused != "" {
		t.Errorf("Focus not cleared: got %q, want empty", focused)
	}
}

func TestClearFocusIfNeededNonMatching(t *testing.T) {
	dir := t.TempDir()

	// Initialize database to create .todos directory
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	database.Close()

	// Set focus on an issue
	focusedID := "td-focused"
	if err := config.SetFocus(dir, focusedID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Try to clear with different ID
	clearFocusIfNeeded(dir, "td-different")

	// Focus should still be set
	focused, _ := config.GetFocus(dir)
	if focused != focusedID {
		t.Errorf("Focus was incorrectly cleared: got %q, want %q", focused, focusedID)
	}
}

func TestClearFocusIfNeededNoFocus(t *testing.T) {
	dir := t.TempDir()

	// Initialize database to create .todos directory
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	database.Close()

	// Don't set any focus, just try to clear
	clearFocusIfNeeded(dir, "td-any")

	// Should not panic or error
	focused, _ := config.GetFocus(dir)
	if focused != "" {
		t.Errorf("Unexpected focus found: %q", focused)
	}
}

func TestReviewRequiresHandoff(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify no handoff exists
	handoff, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if handoff != nil {
		t.Error("Expected no handoff, but found one")
	}

	// Note: Full command testing would require setting up session and executing command
	// This test verifies the handoff check logic by checking database state
}

func TestApproveRequiresDifferentSession(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_impl123"

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Update to set implementer (CreateIssue doesn't persist implementer_session)
	issue.ImplementerSession = sessionID
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify the issue has the implementer set
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.ImplementerSession != sessionID {
		t.Errorf("Implementer not set: got %q, want %q", retrieved.ImplementerSession, sessionID)
	}

	// Note: The actual "cannot approve own implementation" check is in the command
	// This test verifies the data model supports tracking implementer sessions
}

func TestRejectResetsToInProgress(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue in review
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Update to in_progress (simulating reject)
	issue.Status = models.StatusInProgress
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify status changed
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Errorf("Status not updated: got %q, want %q", retrieved.Status, models.StatusInProgress)
	}
}

func TestCloseSetsClosedAt(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify ClosedAt is nil initially
	if issue.ClosedAt != nil {
		t.Error("ClosedAt should be nil for new issue")
	}

	// Update to closed with ClosedAt
	issue.Status = models.StatusClosed
	now := issue.UpdatedAt
	issue.ClosedAt = &now
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify ClosedAt is set
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.ClosedAt == nil {
		t.Error("ClosedAt should be set after closing")
	}
	if retrieved.Status != models.StatusClosed {
		t.Errorf("Status not updated: got %q, want %q", retrieved.Status, models.StatusClosed)
	}
}

func TestApproveAddsReviewerSession(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	implSession := "ses_impl123"
	reviewSession := "ses_review456"

	// Create an issue with implementer
	issue := &models.Issue{
		Title:              "Test Issue",
		Status:             models.StatusInReview,
		ImplementerSession: implSession,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Update with reviewer (simulating approve)
	issue.Status = models.StatusClosed
	issue.ReviewerSession = reviewSession
	now := issue.UpdatedAt
	issue.ClosedAt = &now
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify reviewer is set
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.ReviewerSession != reviewSession {
		t.Errorf("ReviewerSession not set: got %q, want %q", retrieved.ReviewerSession, reviewSession)
	}
	if retrieved.ImplementerSession != implSession {
		t.Errorf("ImplementerSession changed: got %q, want %q", retrieved.ImplementerSession, implSession)
	}
}

func TestReviewAddsLogEntry(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Add a log entry (simulating what review command does)
	log := &models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Message:   "Submitted for review",
		Type:      models.LogTypeProgress,
	}
	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	// Verify log was added
	logs, err := database.GetLogs(issue.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("Expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != "Submitted for review" {
		t.Errorf("Wrong log message: got %q", logs[0].Message)
	}
}

func TestHasChildren(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Initially has no children
	hasChildren, err := database.HasChildren(epic.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}
	if hasChildren {
		t.Error("Epic should have no children initially")
	}

	// Create child
	child := &models.Issue{
		Title:    "Child",
		Status:   models.StatusOpen,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Now has children
	hasChildren, err = database.HasChildren(epic.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}
	if !hasChildren {
		t.Error("Epic should have children after adding child")
	}
}

func TestGetDescendantIssues(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic -> sub-epic -> task hierarchy
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	subEpic := &models.Issue{
		Title:    "Sub-Epic",
		Type:     models.TypeEpic,
		Status:   models.StatusInProgress,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(subEpic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	task := &models.Issue{
		Title:    "Task",
		Status:   models.StatusOpen,
		ParentID: subEpic.ID,
	}
	if err := database.CreateIssue(task); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	closedTask := &models.Issue{
		Title:    "Closed Task",
		Status:   models.StatusClosed,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(closedTask); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Get all descendants
	all, err := database.GetDescendantIssues(epic.ID, nil)
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Expected 3 descendants, got %d", len(all))
	}

	// Get only open/in_progress descendants
	active, err := database.GetDescendantIssues(epic.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("Expected 2 active descendants, got %d", len(active))
	}

	// Verify closed task was filtered out
	for _, issue := range active {
		if issue.Status == models.StatusClosed {
			t.Error("Should not include closed issues when filtering")
		}
	}
}

func TestCascadeReviewMarksDescendants(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic with children
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusOpen,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusInProgress,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	closedChild := &models.Issue{
		Title:    "Closed Child",
		Status:   models.StatusClosed,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(closedChild); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Simulate cascade review logic
	sessionID := "ses_test"
	descendants, err := database.GetDescendantIssues(epic.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	for _, child := range descendants {
		child.Status = models.StatusInReview
		if child.ImplementerSession == "" {
			child.ImplementerSession = sessionID
		}
		if err := database.UpdateIssue(child); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Verify child1 and child2 are now in_review
	c1, _ := database.GetIssue(child1.ID)
	if c1.Status != models.StatusInReview {
		t.Errorf("child1 status: got %q, want %q", c1.Status, models.StatusInReview)
	}
	if c1.ImplementerSession != sessionID {
		t.Errorf("child1 implementer: got %q, want %q", c1.ImplementerSession, sessionID)
	}

	c2, _ := database.GetIssue(child2.ID)
	if c2.Status != models.StatusInReview {
		t.Errorf("child2 status: got %q, want %q", c2.Status, models.StatusInReview)
	}

	// Verify closedChild is unchanged
	cc, _ := database.GetIssue(closedChild.ID)
	if cc.Status != models.StatusClosed {
		t.Errorf("closedChild status should remain closed: got %q", cc.Status)
	}
}

func TestCascadeReviewNestedEpics(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic -> sub-epic -> task
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	subEpic := &models.Issue{
		Title:    "Sub-Epic",
		Type:     models.TypeEpic,
		Status:   models.StatusInProgress,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(subEpic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	task := &models.Issue{
		Title:    "Deeply Nested Task",
		Status:   models.StatusOpen,
		ParentID: subEpic.ID,
	}
	if err := database.CreateIssue(task); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Get descendants from top-level epic
	descendants, err := database.GetDescendantIssues(epic.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	// Should include both sub-epic and deeply nested task
	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants (sub-epic + task), got %d", len(descendants))
	}

	// Mark all for review
	for _, d := range descendants {
		d.Status = models.StatusInReview
		database.UpdateIssue(d)
	}

	// Verify all are in_review
	se, _ := database.GetIssue(subEpic.ID)
	if se.Status != models.StatusInReview {
		t.Errorf("sub-epic status: got %q, want %q", se.Status, models.StatusInReview)
	}

	tk, _ := database.GetIssue(task.ID)
	if tk.Status != models.StatusInReview {
		t.Errorf("task status: got %q, want %q", tk.Status, models.StatusInReview)
	}
}

// Tests for cascade up behavior

func TestCascadeUpToReviewAllChildrenReview(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic with two children
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusInReview,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusInProgress,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// First, cascade up should NOT update epic (child2 still in_progress)
	database.CascadeUpParentStatus(child1.ID, models.StatusInReview, sessionID)

	e, _ := database.GetIssue(epic.ID)
	if e.Status != models.StatusOpen {
		t.Errorf("Epic should remain open: got %q", e.Status)
	}

	// Now mark child2 as in_review
	child2.Status = models.StatusInReview
	database.UpdateIssue(child2)

	// Cascade up should now update epic
	cascaded, _ := database.CascadeUpParentStatus( child2.ID, models.StatusInReview, sessionID)

	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded, got %d", cascaded)
	}

	e, _ = database.GetIssue(epic.ID)
	if e.Status != models.StatusInReview {
		t.Errorf("Epic should be in_review: got %q", e.Status)
	}
}

func TestCascadeUpToClosedAllChildrenClosed(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic with two children
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusClosed,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusClosed,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// All children closed, cascade up should update epic
	cascaded, _ := database.CascadeUpParentStatus( child2.ID, models.StatusClosed, sessionID)

	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded, got %d", cascaded)
	}

	e, _ := database.GetIssue(epic.ID)
	if e.Status != models.StatusClosed {
		t.Errorf("Epic should be closed: got %q", e.Status)
	}
	if e.ClosedAt == nil {
		t.Error("Epic ClosedAt should be set")
	}
}

func TestCascadeUpRecursive(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create grandparent -> parent -> child hierarchy (all epics)
	grandparent := &models.Issue{
		Title:  "Grandparent Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(grandparent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	parent := &models.Issue{
		Title:    "Parent Epic",
		Type:     models.TypeEpic,
		Status:   models.StatusOpen,
		ParentID: grandparent.ID,
	}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child Task",
		Status:   models.StatusInReview,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// Child is only child of parent, parent is only child of grandparent
	// Cascade up from child should update both parent and grandparent
	cascaded, _ := database.CascadeUpParentStatus( child.ID, models.StatusInReview, sessionID)

	if cascaded != 2 {
		t.Errorf("Expected 2 cascaded (parent + grandparent), got %d", cascaded)
	}

	p, _ := database.GetIssue(parent.ID)
	if p.Status != models.StatusInReview {
		t.Errorf("Parent should be in_review: got %q", p.Status)
	}

	gp, _ := database.GetIssue(grandparent.ID)
	if gp.Status != models.StatusInReview {
		t.Errorf("Grandparent should be in_review: got %q", gp.Status)
	}
}

func TestCascadeUpNoActionNonEpicParent(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create a task parent (not an epic)
	parent := &models.Issue{
		Title:  "Parent Task",
		Type:   models.TypeTask,
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child Task",
		Status:   models.StatusInReview,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// Should NOT cascade up to non-epic parent
	cascaded, _ := database.CascadeUpParentStatus( child.ID, models.StatusInReview, sessionID)

	if cascaded != 0 {
		t.Errorf("Expected 0 cascaded (parent not epic), got %d", cascaded)
	}

	p, _ := database.GetIssue(parent.ID)
	if p.Status != models.StatusInProgress {
		t.Errorf("Parent status should be unchanged: got %q", p.Status)
	}
}

func TestCascadeUpNoActionNotAllChildrenReady(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic with two children, only one in_review
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusInReview,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusOpen, // Not in_review yet
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// Should NOT cascade up because child2 is still open
	cascaded, _ := database.CascadeUpParentStatus( child1.ID, models.StatusInReview, sessionID)

	if cascaded != 0 {
		t.Errorf("Expected 0 cascaded (not all children ready), got %d", cascaded)
	}

	e, _ := database.GetIssue(epic.ID)
	if e.Status != models.StatusOpen {
		t.Errorf("Epic status should be unchanged: got %q", e.Status)
	}
}

func TestCascadeUpReviewAllowsClosedSiblings(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create epic with two children: one in_review, one closed
	epic := &models.Issue{
		Title:  "Epic",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusInReview,
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusClosed, // Already closed
		ParentID: epic.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// For in_review target, closed siblings should count as "ready"
	cascaded, _ := database.CascadeUpParentStatus( child1.ID, models.StatusInReview, sessionID)

	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded, got %d", cascaded)
	}

	e, _ := database.GetIssue(epic.ID)
	if e.Status != models.StatusInReview {
		t.Errorf("Epic should be in_review: got %q", e.Status)
	}
}

// Tests for new flags added to workflow commands

func TestReviewMinorFlag(t *testing.T) {
	// Test that --minor flag exists
	if reviewCmd.Flags().Lookup("minor") == nil {
		t.Error("Expected --minor flag to be defined on review command")
	}

	// Test that --minor flag can be set
	if err := reviewCmd.Flags().Set("minor", "true"); err != nil {
		t.Errorf("Failed to set --minor flag: %v", err)
	}

	minorValue, err := reviewCmd.Flags().GetBool("minor")
	if err != nil {
		t.Errorf("Failed to get --minor flag value: %v", err)
	}
	if !minorValue {
		t.Error("Expected minor flag to be true")
	}

	// Reset
	reviewCmd.Flags().Set("minor", "false")
}

func TestReviewReasonShorthand(t *testing.T) {
	// Test that -m shorthand exists for --reason
	if reviewCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --reason on review command")
	}

	// Test that -m flag can be set
	if err := reviewCmd.Flags().Set("reason", "test message"); err != nil {
		t.Errorf("Failed to set --reason flag: %v", err)
	}

	reasonValue, err := reviewCmd.Flags().GetString("reason")
	if err != nil {
		t.Errorf("Failed to get --reason flag value: %v", err)
	}
	if reasonValue != "test message" {
		t.Errorf("Expected reason value 'test message', got %s", reasonValue)
	}

	// Reset
	reviewCmd.Flags().Set("reason", "")
}

func TestApproveReasonShorthand(t *testing.T) {
	// Test that -m shorthand exists for --reason on approve
	if approveCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --reason on approve command")
	}
}

func TestRejectReasonShorthand(t *testing.T) {
	// Test that -m shorthand exists for --reason on reject
	if rejectCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --reason on reject command")
	}
}

func TestCloseReasonShorthand(t *testing.T) {
	// Test that -m shorthand exists for --reason on close
	if closeCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --reason on close command")
	}
}

func TestCloseSelfCloseExceptionFlagExists(t *testing.T) {
	// Test that --self-close-exception flag exists on close command
	if closeCmd.Flags().Lookup("self-close-exception") == nil {
		t.Error("Expected --self-close-exception flag to be defined on close command")
	}
}

func TestCloseSelfCloseExceptionRequiresValue(t *testing.T) {
	// Test that the flag can accept a value
	flag := closeCmd.Flags().Lookup("self-close-exception")
	if flag == nil {
		t.Fatal("--self-close-exception flag not found")
	}

	// Reset flag to default before test
	flag.Value.Set("")

	// Set a test value
	if err := flag.Value.Set("test reason"); err != nil {
		t.Errorf("Failed to set --self-close-exception value: %v", err)
	}

	val := flag.Value.String()
	if val != "test reason" {
		t.Errorf("Expected 'test reason', got %q", val)
	}

	// Reset for other tests
	flag.Value.Set("")
}

func TestCloseSelfCloseScenarios(t *testing.T) {
	// Test the data model behavior for self-close detection scenarios

	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_impl123"
	otherSessionID := "ses_other456"

	// Scenario 1: Issue with ImplementerSession set (would trigger self-close check)
	issueWithImpl := &models.Issue{
		Title:              "Implemented Issue",
		Status:             models.StatusInProgress,
		ImplementerSession: sessionID,
	}
	if err := database.CreateIssue(issueWithImpl); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	database.UpdateIssue(issueWithImpl)

	retrieved, _ := database.GetIssue(issueWithImpl.ID)
	if retrieved.ImplementerSession != sessionID {
		t.Errorf("ImplementerSession not saved: got %q, want %q", retrieved.ImplementerSession, sessionID)
	}

	// Check self-closing detection logic
	isSelfClosing := retrieved.ImplementerSession != "" && retrieved.ImplementerSession == sessionID
	if !isSelfClosing {
		t.Error("Expected isSelfClosing to be true for same session")
	}

	// Check not self-closing for different session
	isSelfClosingOther := retrieved.ImplementerSession != "" && retrieved.ImplementerSession == otherSessionID
	if isSelfClosingOther {
		t.Error("Expected isSelfClosing to be false for different session")
	}

	// Scenario 2: Issue with no ImplementerSession (never worked on, should bypass check)
	issueNoImpl := &models.Issue{
		Title:  "Never Started Issue",
		Status: models.StatusOpen,
		// ImplementerSession is empty
	}
	if err := database.CreateIssue(issueNoImpl); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	retrievedNoImpl, _ := database.GetIssue(issueNoImpl.ID)
	if retrievedNoImpl.ImplementerSession != "" {
		t.Errorf("ImplementerSession should be empty: got %q", retrievedNoImpl.ImplementerSession)
	}

	// Check that self-closing is false when no implementer
	isSelfClosingNoImpl := retrievedNoImpl.ImplementerSession != "" && retrievedNoImpl.ImplementerSession == sessionID
	if isSelfClosingNoImpl {
		t.Error("Expected isSelfClosing to be false when no ImplementerSession")
	}
}

func TestCloseSelfCloseExceptionLogMessage(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_impl123"

	// Create issue with implementer
	issue := &models.Issue{
		Title:              "Self Close Test",
		Status:             models.StatusInProgress,
		ImplementerSession: sessionID,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	database.UpdateIssue(issue)

	// Simulate closing with exception - manually add the log entry
	exceptionReason := "trivial typo fix"
	logMsg := "[test-agent] Closed (SELF-CLOSE EXCEPTION: " + exceptionReason + ")"

	database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: sessionID,
		Message:   logMsg,
		Type:      models.LogTypeSecurity,
	})

	// Verify log contains exception
	logs, _ := database.GetLogs(issue.ID, 0)
	if len(logs) == 0 {
		t.Fatal("Expected log entry")
	}
	if logs[0].Message != "[test-agent] Closed (SELF-CLOSE EXCEPTION: trivial typo fix)" {
		t.Errorf("Log message wrong: got %q", logs[0].Message)
	}
	if logs[0].Type != models.LogTypeSecurity {
		t.Errorf("Log type wrong: got %q, want %q", logs[0].Type, models.LogTypeSecurity)
	}
}

func TestCascadeUpNoActionNoParent(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create task with no parent
	task := &models.Issue{
		Title:  "Orphan Task",
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(task); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	sessionID := "ses_test"

	// Should return 0 since no parent
	cascaded, _ := database.CascadeUpParentStatus( task.ID, models.StatusInReview, sessionID)

	if cascaded != 0 {
		t.Errorf("Expected 0 cascaded (no parent), got %d", cascaded)
	}
}

func TestReviewSubmitAlias(t *testing.T) {
	// Test that "submit" is an alias for "review"
	found := false
	for _, alias := range reviewCmd.Aliases {
		if alias == "submit" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected 'submit' to be an alias for review command")
	}
}

// Tests for auto-created handoff behavior

func TestReviewAutoCreatesHandoffWhenMissing(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_test123"

	// Create an issue in_progress
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify no handoff exists
	handoff, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if handoff != nil {
		t.Fatal("Expected no handoff initially, but found one")
	}

	// Simulate the auto-create logic from review command
	autoHandoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: sessionID,
		Done:      []string{"Auto-generated for review submission"},
		Remaining: []string{},
		Decisions: []string{},
		Uncertain: []string{},
	}
	if err := database.AddHandoff(autoHandoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Verify handoff was created
	created, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed after auto-create: %v", err)
	}
	if created == nil {
		t.Fatal("Expected handoff to exist after auto-create")
	}
	if len(created.Done) != 1 || created.Done[0] != "Auto-generated for review submission" {
		t.Errorf("Handoff Done field wrong: got %v", created.Done)
	}
	if created.SessionID != sessionID {
		t.Errorf("Handoff SessionID wrong: got %q, want %q", created.SessionID, sessionID)
	}
}

func TestReviewPreservesExistingHandoff(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_test123"

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create an explicit handoff with custom content
	explicitHandoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: sessionID,
		Done:      []string{"Implemented feature X", "Added tests"},
		Remaining: []string{"Documentation"},
		Decisions: []string{"Used approach A"},
		Uncertain: []string{"Performance implications"},
	}
	if err := database.AddHandoff(explicitHandoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Simulate review command checking for handoff
	existing, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}

	// Handoff exists, so review should NOT auto-create
	if existing == nil {
		t.Fatal("Expected existing handoff to be found")
	}

	// Verify existing handoff is unchanged
	if len(existing.Done) != 2 {
		t.Errorf("Expected 2 Done items, got %d", len(existing.Done))
	}
	if existing.Done[0] != "Implemented feature X" {
		t.Errorf("Done[0] wrong: got %q", existing.Done[0])
	}
	if len(existing.Remaining) != 1 || existing.Remaining[0] != "Documentation" {
		t.Errorf("Remaining wrong: got %v", existing.Remaining)
	}
	if len(existing.Decisions) != 1 || existing.Decisions[0] != "Used approach A" {
		t.Errorf("Decisions wrong: got %v", existing.Decisions)
	}
	if len(existing.Uncertain) != 1 || existing.Uncertain[0] != "Performance implications" {
		t.Errorf("Uncertain wrong: got %v", existing.Uncertain)
	}
}

func TestReviewWithWorkSessionTaggedIssue(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	sessionID := "ses_test123"

	// Create a work session
	ws := &models.WorkSession{
		ID:        "ws-test123",
		Name:      "Test Work Session",
		SessionID: sessionID,
	}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}

	// Create an issue
	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Tag issue to work session
	if err := database.TagIssueToWorkSession(ws.ID, issue.ID, "test-session"); err != nil {
		t.Fatalf("TagIssueToWorkSession failed: %v", err)
	}

	// Verify issue is tagged to work session
	tagged, err := database.GetWorkSessionIssues(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSessionIssues failed: %v", err)
	}
	if len(tagged) != 1 || tagged[0] != issue.ID {
		t.Errorf("Expected issue to be tagged: got %v", tagged)
	}

	// Simulate auto-create handoff (as review command would do)
	autoHandoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: sessionID,
		Done:      []string{"Auto-generated for review submission"},
		Remaining: []string{},
		Decisions: []string{},
		Uncertain: []string{},
	}
	if err := database.AddHandoff(autoHandoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Verify handoff was created
	handoff, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if handoff == nil {
		t.Fatal("Expected handoff to exist")
	}

	// Verify issue is STILL tagged to work session (not untagged by handoff)
	stillTagged, err := database.GetWorkSessionIssues(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSessionIssues failed after handoff: %v", err)
	}
	if len(stillTagged) != 1 || stillTagged[0] != issue.ID {
		t.Errorf("Issue should still be tagged to work session: got %v", stillTagged)
	}

	// Verify work session is still active (not ended)
	retrievedWS, err := database.GetWorkSession(ws.ID)
	if err != nil {
		t.Fatalf("GetWorkSession failed: %v", err)
	}
	if retrievedWS == nil {
		t.Fatal("Work session should still exist")
	}
	if retrievedWS.EndedAt != nil {
		t.Error("Work session should NOT be ended by individual handoff")
	}
}

// ============================================================================
// Auto-Unblock Integration Tests
// ============================================================================

func TestApproveAutoUnblocksDependents(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create blocker (in_review, ready to be approved)
	blocker := &models.Issue{
		Title:              "Blocker",
		Status:             models.StatusInReview,
		ImplementerSession: "ses_impl",
	}
	database.CreateIssue(blocker)

	// Create dependent (blocked, depends on blocker)
	dependent := &models.Issue{
		Title:  "Dependent",
		Status: models.StatusBlocked,
	}
	database.CreateIssue(dependent)
	database.AddDependency(dependent.ID, blocker.ID, "depends_on")

	// Simulate approve: close the blocker then cascade unblock
	blocker.Status = models.StatusClosed
	database.UpdateIssue(blocker)
	database.CascadeUnblockDependents(blocker.ID, "ses_reviewer")

	// Verify dependent is now open
	updated, _ := database.GetIssue(dependent.ID)
	if updated.Status != models.StatusOpen {
		t.Errorf("dependent should be open after blocker approved, got %s", updated.Status)
	}
}

func TestCloseAutoUnblocksDependents(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	blocker := &models.Issue{
		Title:  "Blocker",
		Status: models.StatusOpen,
	}
	database.CreateIssue(blocker)

	dependent := &models.Issue{
		Title:  "Dependent",
		Status: models.StatusBlocked,
	}
	database.CreateIssue(dependent)
	database.AddDependency(dependent.ID, blocker.ID, "depends_on")

	// Simulate close: set closed then cascade unblock
	blocker.Status = models.StatusClosed
	database.UpdateIssue(blocker)
	database.CascadeUnblockDependents(blocker.ID, "ses_closer")

	updated, _ := database.GetIssue(dependent.ID)
	if updated.Status != models.StatusOpen {
		t.Errorf("dependent should be open after blocker closed, got %s", updated.Status)
	}
}

func TestApproveAutoUnblockPartialDeps(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	a1 := &models.Issue{
		Title:              "A1",
		Status:             models.StatusInReview,
		ImplementerSession: "ses_impl",
	}
	a2 := &models.Issue{
		Title:  "A2",
		Status: models.StatusOpen,
	}
	dependent := &models.Issue{
		Title:  "Dependent",
		Status: models.StatusBlocked,
	}
	database.CreateIssue(a1)
	database.CreateIssue(a2)
	database.CreateIssue(dependent)
	database.AddDependency(dependent.ID, a1.ID, "depends_on")
	database.AddDependency(dependent.ID, a2.ID, "depends_on")

	// Approve only A1
	a1.Status = models.StatusClosed
	database.UpdateIssue(a1)
	database.CascadeUnblockDependents(a1.ID, "ses_reviewer")

	// Dependent should still be blocked (A2 not closed)
	updated, _ := database.GetIssue(dependent.ID)
	if updated.Status != models.StatusBlocked {
		t.Errorf("dependent should remain blocked (A2 still open), got %s", updated.Status)
	}
}
