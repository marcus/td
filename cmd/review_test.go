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
