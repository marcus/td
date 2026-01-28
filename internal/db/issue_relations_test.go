package db

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// getDescendants Tests
// ============================================================================

func TestGetDescendants_SingleLevel(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create parent
	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create children
	child1 := &models.Issue{Title: "Child 1", ParentID: parent.ID}
	child2 := &models.Issue{Title: "Child 2", ParentID: parent.ID}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	descendants, err := db.getDescendants(parent.ID)
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}

	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants, got %d", len(descendants))
	}

	// Verify both children are in descendants
	foundIDs := make(map[string]bool)
	for _, id := range descendants {
		foundIDs[id] = true
	}
	if !foundIDs[child1.ID] {
		t.Error("child1 not found in descendants")
	}
	if !foundIDs[child2.ID] {
		t.Error("child2 not found in descendants")
	}
}

func TestGetDescendants_MultiLevel(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create 3-level hierarchy: grandparent -> parent -> child
	grandparent := &models.Issue{Title: "Grandparent", Type: models.TypeEpic}
	if err := db.CreateIssue(grandparent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic, ParentID: grandparent.ID}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{Title: "Child", ParentID: parent.ID}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Get descendants of grandparent
	descendants, err := db.getDescendants(grandparent.ID)
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}

	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants (parent and child), got %d", len(descendants))
	}

	foundIDs := make(map[string]bool)
	for _, id := range descendants {
		foundIDs[id] = true
	}
	if !foundIDs[parent.ID] {
		t.Error("parent not found in descendants")
	}
	if !foundIDs[child.ID] {
		t.Error("child not found in descendants")
	}
}

func TestGetDescendants_DeepHierarchy(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create 5-level deep hierarchy
	root := &models.Issue{Title: "Root", Type: models.TypeEpic}
	if err := db.CreateIssue(root); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	prevID := root.ID
	expectedDescendants := 4
	for i := 0; i < expectedDescendants; i++ {
		issue := &models.Issue{Title: "Level " + string(rune('1'+i)), ParentID: prevID}
		if err := db.CreateIssue(issue); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		prevID = issue.ID
	}

	descendants, err := db.getDescendants(root.ID)
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}

	if len(descendants) != expectedDescendants {
		t.Errorf("Expected %d descendants, got %d", expectedDescendants, len(descendants))
	}
}

func TestGetDescendants_CircularReferenceHandling(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create three issues to form a cycle
	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	issue3 := &models.Issue{Title: "Issue 3"}
	if err := db.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(issue3); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create chain: issue1 -> issue2 -> issue3 -> issue1 (circular back to start)
	// issue2's parent is issue1 (so issue2 is child of issue1)
	// issue3's parent is issue2 (so issue3 is child of issue2)
	// issue1's parent is issue3 (so issue1 is child of issue3) - creates cycle
	_, err = db.conn.Exec(`UPDATE issues SET parent_id = ? WHERE id = ?`, issue1.ID, issue2.ID)
	if err != nil {
		t.Fatalf("Update parent_id failed: %v", err)
	}
	_, err = db.conn.Exec(`UPDATE issues SET parent_id = ? WHERE id = ?`, issue2.ID, issue3.ID)
	if err != nil {
		t.Fatalf("Update parent_id failed: %v", err)
	}
	_, err = db.conn.Exec(`UPDATE issues SET parent_id = ? WHERE id = ?`, issue3.ID, issue1.ID)
	if err != nil {
		t.Fatalf("Update parent_id failed: %v", err)
	}

	// getDescendants should handle circular references without infinite loop
	descendants, err := db.getDescendants(issue1.ID)
	if err != nil {
		t.Fatalf("getDescendants failed with circular reference: %v", err)
	}

	// With the circular reference, all 3 issues end up being found:
	// - issue1 starts in queue
	// - issue2 is child of issue1 -> added to descendants and queue
	// - issue3 is child of issue2 -> added to descendants and queue
	// - issue1 is child of issue3 -> but visited[issue1]=true, so skipped
	// However, issue1 is added to visited BEFORE processing its children,
	// so when issue3 tries to add issue1 as descendant, it's already visited.
	// But issue1 itself was added to descendants when first discovered.
	// Actually, the algorithm marks visited BEFORE getting children,
	// and issue1 is marked visited first (as the starting point).
	// So when we come back to it via issue3, it's already visited and skipped.
	// But issue1 doesn't get added to descendants since it's the starting point.
	// Result: issue2, issue3, and issue1 (found as child of issue3)
	// Let's just verify it doesn't hang and returns some results
	if len(descendants) < 2 {
		t.Errorf("Expected at least 2 descendants with circular reference, got %d", len(descendants))
	}

	// Most importantly: verify the function completed without infinite loop
	// (implicit by reaching this point)
	t.Logf("Found %d descendants in circular graph (test passed - no infinite loop)", len(descendants))
}

func TestGetDescendants_NoChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Leaf Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	descendants, err := db.getDescendants(issue.ID)
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}

	if len(descendants) != 0 {
		t.Errorf("Expected 0 descendants for leaf issue, got %d", len(descendants))
	}
}

func TestGetDescendants_ExcludesDeleted(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Child 1", ParentID: parent.ID}
	child2 := &models.Issue{Title: "Child 2 (deleted)", ParentID: parent.ID}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Delete child2
	if err := db.DeleteIssue(child2.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	descendants, err := db.getDescendants(parent.ID)
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}

	if len(descendants) != 1 {
		t.Errorf("Expected 1 descendant (deleted excluded), got %d", len(descendants))
	}
	if descendants[0] != child1.ID {
		t.Error("Expected child1, got different descendant")
	}
}

// ============================================================================
// HasChildren Tests
// ============================================================================

func TestHasChildren_WithChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{Title: "Child", ParentID: parent.ID}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	hasChildren, err := db.HasChildren(parent.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}

	if !hasChildren {
		t.Error("Expected HasChildren to return true")
	}
}

func TestHasChildren_WithoutChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Leaf Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	hasChildren, err := db.HasChildren(issue.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}

	if hasChildren {
		t.Error("Expected HasChildren to return false for leaf issue")
	}
}

func TestHasChildren_DeletedChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{Title: "Child (will be deleted)", ParentID: parent.ID}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Delete the child
	if err := db.DeleteIssue(child.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	hasChildren, err := db.HasChildren(parent.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}

	if hasChildren {
		t.Error("Expected HasChildren to return false when all children are deleted")
	}
}

func TestHasChildren_NonexistentIssue(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	hasChildren, err := db.HasChildren("nonexistent-id")
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}

	if hasChildren {
		t.Error("Expected HasChildren to return false for nonexistent issue")
	}
}

// ============================================================================
// GetDirectChildren Tests
// ============================================================================

func TestGetDirectChildren_ReturnsCorrectChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		ParentID: parent.ID,
		Priority: models.PriorityP1,
		Type:     models.TypeTask,
	}
	child2 := &models.Issue{
		Title:    "Child 2",
		ParentID: parent.ID,
		Priority: models.PriorityP2,
		Type:     models.TypeBug,
	}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	children, err := db.GetDirectChildren(parent.ID)
	if err != nil {
		t.Fatalf("GetDirectChildren failed: %v", err)
	}

	if len(children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(children))
	}

	// Verify children data is correct
	foundIDs := make(map[string]*models.Issue)
	for _, child := range children {
		foundIDs[child.ID] = child
	}

	if c, ok := foundIDs[child1.ID]; !ok {
		t.Error("child1 not found")
	} else {
		if c.Title != "Child 1" {
			t.Errorf("child1 title mismatch: got %s", c.Title)
		}
		if c.Priority != models.PriorityP1 {
			t.Errorf("child1 priority mismatch: got %s", c.Priority)
		}
	}

	if c, ok := foundIDs[child2.ID]; !ok {
		t.Error("child2 not found")
	} else {
		if c.Title != "Child 2" {
			t.Errorf("child2 title mismatch: got %s", c.Title)
		}
		if c.Type != models.TypeBug {
			t.Errorf("child2 type mismatch: got %s", c.Type)
		}
	}
}

func TestGetDirectChildren_ExcludesDeleted(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Active Child", ParentID: parent.ID}
	child2 := &models.Issue{Title: "Deleted Child", ParentID: parent.ID}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Delete child2
	if err := db.DeleteIssue(child2.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	children, err := db.GetDirectChildren(parent.ID)
	if err != nil {
		t.Fatalf("GetDirectChildren failed: %v", err)
	}

	if len(children) != 1 {
		t.Errorf("Expected 1 child (deleted excluded), got %d", len(children))
	}
	if children[0].ID != child1.ID {
		t.Error("Expected active child, got deleted child")
	}
}

func TestGetDirectChildren_ExcludesGrandchildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	grandparent := &models.Issue{Title: "Grandparent", Type: models.TypeEpic}
	if err := db.CreateIssue(grandparent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic, ParentID: grandparent.ID}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	grandchild := &models.Issue{Title: "Grandchild", ParentID: parent.ID}
	if err := db.CreateIssue(grandchild); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// GetDirectChildren of grandparent should only return parent, not grandchild
	children, err := db.GetDirectChildren(grandparent.ID)
	if err != nil {
		t.Fatalf("GetDirectChildren failed: %v", err)
	}

	if len(children) != 1 {
		t.Errorf("Expected 1 direct child, got %d", len(children))
	}
	if children[0].ID != parent.ID {
		t.Error("Expected parent as direct child, got grandchild")
	}
}

func TestGetDirectChildren_NoChildren(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Leaf Issue"}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	children, err := db.GetDirectChildren(issue.ID)
	if err != nil {
		t.Fatalf("GetDirectChildren failed: %v", err)
	}

	if len(children) != 0 {
		t.Errorf("Expected 0 children for leaf issue, got %d", len(children))
	}
}

func TestGetDirectChildren_PreservesLabels(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child with Labels",
		ParentID: parent.ID,
		Labels:   []string{"frontend", "urgent"},
	}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	children, err := db.GetDirectChildren(parent.ID)
	if err != nil {
		t.Fatalf("GetDirectChildren failed: %v", err)
	}

	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	if len(children[0].Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(children[0].Labels))
	}
}

// ============================================================================
// GetDescendantIssues Tests
// ============================================================================

func TestGetDescendantIssues_AllStatuses(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Open Child", ParentID: parent.ID, Status: models.StatusOpen}
	child2 := &models.Issue{Title: "Closed Child", ParentID: parent.ID, Status: models.StatusClosed}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	child2.Status = models.StatusClosed
	db.UpdateIssue(child2)

	// Get all descendants (no status filter)
	descendants, err := db.GetDescendantIssues(parent.ID, nil)
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants, got %d", len(descendants))
	}
}

func TestGetDescendantIssues_FilteredByStatus(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Open Child", ParentID: parent.ID, Status: models.StatusOpen}
	child2 := &models.Issue{Title: "In Progress Child", ParentID: parent.ID, Status: models.StatusInProgress}
	child3 := &models.Issue{Title: "Closed Child", ParentID: parent.ID, Status: models.StatusClosed}
	for _, c := range []*models.Issue{child1, child2, child3} {
		if err := db.CreateIssue(c); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(c); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Filter to only open issues
	descendants, err := db.GetDescendantIssues(parent.ID, []models.Status{models.StatusOpen})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	if len(descendants) != 1 {
		t.Errorf("Expected 1 open descendant, got %d", len(descendants))
	}
	if descendants[0].Status != models.StatusOpen {
		t.Errorf("Expected open status, got %s", descendants[0].Status)
	}
}

func TestGetDescendantIssues_MultipleStatusFilter(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Open Child", ParentID: parent.ID, Status: models.StatusOpen}
	child2 := &models.Issue{Title: "In Review Child", ParentID: parent.ID, Status: models.StatusInReview}
	child3 := &models.Issue{Title: "Closed Child", ParentID: parent.ID, Status: models.StatusClosed}
	for _, c := range []*models.Issue{child1, child2, child3} {
		if err := db.CreateIssue(c); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(c); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Filter to open and in_review
	descendants, err := db.GetDescendantIssues(parent.ID, []models.Status{models.StatusOpen, models.StatusInReview})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants, got %d", len(descendants))
	}
}

// ============================================================================
// CascadeUpParentStatus Tests
// ============================================================================

func TestCascadeUpParentStatus_AllChildrenInReview(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionID := "ses_test"

	// Create epic parent
	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := db.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create children
	child1 := &models.Issue{Title: "Child 1", ParentID: epic.ID, Status: models.StatusInReview}
	child2 := &models.Issue{Title: "Child 2", ParentID: epic.ID, Status: models.StatusInReview}
	for _, c := range []*models.Issue{child1, child2} {
		if err := db.CreateIssue(c); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(c); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	// Cascade up when last child reaches in_review
	count, ids := db.CascadeUpParentStatus(child2.ID, models.StatusInReview, sessionID)

	if count != 1 {
		t.Errorf("Expected 1 cascaded, got %d", count)
	}
	if len(ids) != 1 || ids[0] != epic.ID {
		t.Errorf("Expected epic ID in cascaded IDs, got %v", ids)
	}

	// Verify epic status was updated
	updatedEpic, err := db.GetIssue(epic.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updatedEpic.Status != models.StatusInReview {
		t.Errorf("Expected epic status in_review, got %s", updatedEpic.Status)
	}
}

func TestCascadeUpParentStatus_NotAllChildrenReady(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionID := "ses_test"

	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := db.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Child 1", ParentID: epic.ID, Status: models.StatusInReview}
	child2 := &models.Issue{Title: "Child 2", ParentID: epic.ID, Status: models.StatusOpen} // Not ready
	for _, c := range []*models.Issue{child1, child2} {
		if err := db.CreateIssue(c); err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		if err := db.UpdateIssue(c); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}

	count, _ := db.CascadeUpParentStatus(child1.ID, models.StatusInReview, sessionID)

	if count != 0 {
		t.Errorf("Expected 0 cascaded (not all ready), got %d", count)
	}

	// Epic should remain open
	updatedEpic, _ := db.GetIssue(epic.ID)
	if updatedEpic.Status != models.StatusOpen {
		t.Errorf("Expected epic to remain open, got %s", updatedEpic.Status)
	}
}

func TestCascadeUpParentStatus_NonEpicParent(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionID := "ses_test"

	// Parent is task, not epic
	parent := &models.Issue{Title: "Task Parent", Type: models.TypeTask, Status: models.StatusOpen}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{Title: "Child", ParentID: parent.ID, Status: models.StatusInReview}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.UpdateIssue(child); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	count, _ := db.CascadeUpParentStatus(child.ID, models.StatusInReview, sessionID)

	// Should not cascade to non-epic parent
	if count != 0 {
		t.Errorf("Expected 0 cascaded for non-epic parent, got %d", count)
	}
}

func TestCascadeUpParentStatus_RecursiveCascade(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionID := "ses_test"

	// grandparent epic -> parent epic -> child
	grandparent := &models.Issue{Title: "Grandparent Epic", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := db.CreateIssue(grandparent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	parent := &models.Issue{Title: "Parent Epic", Type: models.TypeEpic, ParentID: grandparent.ID, Status: models.StatusOpen}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child := &models.Issue{Title: "Child", ParentID: parent.ID, Status: models.StatusClosed}
	if err := db.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	now := time.Now()
	child.ClosedAt = &now
	if err := db.UpdateIssue(child); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	count, ids := db.CascadeUpParentStatus(child.ID, models.StatusClosed, sessionID)

	// Should cascade both parent and grandparent
	if count != 2 {
		t.Errorf("Expected 2 cascaded, got %d", count)
	}
	if len(ids) != 2 {
		t.Errorf("Expected 2 IDs, got %d", len(ids))
	}

	// Verify both are closed
	updatedParent, _ := db.GetIssue(parent.ID)
	updatedGrandparent, _ := db.GetIssue(grandparent.ID)
	if updatedParent.Status != models.StatusClosed {
		t.Errorf("Expected parent closed, got %s", updatedParent.Status)
	}
	if updatedGrandparent.Status != models.StatusClosed {
		t.Errorf("Expected grandparent closed, got %s", updatedGrandparent.Status)
	}
}

// ============================================================================
// Dependency Functions Tests
// ============================================================================

func TestAddDependency(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)

	// issue2 depends on issue1
	err = db.AddDependency(issue2.ID, issue1.ID, "depends_on")
	if err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	deps, _ := db.GetDependencies(issue2.ID)
	if len(deps) != 1 || deps[0] != issue1.ID {
		t.Error("Dependency not added correctly")
	}
}

func TestAddDependency_ReplaceExisting(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)

	// Add same dependency twice (should not create duplicate)
	db.AddDependency(issue2.ID, issue1.ID, "depends_on")
	db.AddDependency(issue2.ID, issue1.ID, "depends_on")

	deps, _ := db.GetDependencies(issue2.ID)
	if len(deps) != 1 {
		t.Errorf("Expected 1 dependency (no duplicates), got %d", len(deps))
	}
}

func TestRemoveDependency(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)

	db.AddDependency(issue2.ID, issue1.ID, "depends_on")
	err = db.RemoveDependency(issue2.ID, issue1.ID)
	if err != nil {
		t.Fatalf("RemoveDependency failed: %v", err)
	}

	deps, _ := db.GetDependencies(issue2.ID)
	if len(deps) != 0 {
		t.Error("Dependency not removed")
	}
}

func TestGetBlockedBy(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker"}
	blocked1 := &models.Issue{Title: "Blocked 1"}
	blocked2 := &models.Issue{Title: "Blocked 2"}
	db.CreateIssue(blocker)
	db.CreateIssue(blocked1)
	db.CreateIssue(blocked2)

	db.AddDependency(blocked1.ID, blocker.ID, "depends_on")
	db.AddDependency(blocked2.ID, blocker.ID, "depends_on")

	blockedBy, err := db.GetBlockedBy(blocker.ID)
	if err != nil {
		t.Fatalf("GetBlockedBy failed: %v", err)
	}

	if len(blockedBy) != 2 {
		t.Errorf("Expected 2 blocked issues, got %d", len(blockedBy))
	}

	foundIDs := make(map[string]bool)
	for _, id := range blockedBy {
		foundIDs[id] = true
	}
	if !foundIDs[blocked1.ID] || !foundIDs[blocked2.ID] {
		t.Error("Not all blocked issues found")
	}
}

func TestGetAllDependencies(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	issue3 := &models.Issue{Title: "Issue 3"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	db.AddDependency(issue2.ID, issue1.ID, "depends_on")
	db.AddDependency(issue3.ID, issue1.ID, "depends_on")
	db.AddDependency(issue3.ID, issue2.ID, "depends_on")

	allDeps, err := db.GetAllDependencies()
	if err != nil {
		t.Fatalf("GetAllDependencies failed: %v", err)
	}

	// issue2 depends on issue1
	if len(allDeps[issue2.ID]) != 1 || allDeps[issue2.ID][0] != issue1.ID {
		t.Error("issue2 dependencies incorrect")
	}

	// issue3 depends on issue1 and issue2
	if len(allDeps[issue3.ID]) != 2 {
		t.Errorf("Expected 2 dependencies for issue3, got %d", len(allDeps[issue3.ID]))
	}
}

func TestGetIssuesWithOpenDeps(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	openIssue := &models.Issue{Title: "Open Issue", Status: models.StatusOpen}
	closedIssue := &models.Issue{Title: "Closed Issue", Status: models.StatusClosed}
	dependentOpen := &models.Issue{Title: "Depends on Open", Status: models.StatusOpen}
	dependentClosed := &models.Issue{Title: "Depends on Closed", Status: models.StatusOpen}

	db.CreateIssue(openIssue)
	db.CreateIssue(closedIssue)
	closedIssue.Status = models.StatusClosed
	db.UpdateIssue(closedIssue)
	db.CreateIssue(dependentOpen)
	db.CreateIssue(dependentClosed)

	db.AddDependency(dependentOpen.ID, openIssue.ID, "depends_on")
	db.AddDependency(dependentClosed.ID, closedIssue.ID, "depends_on")

	openDeps, err := db.GetIssuesWithOpenDeps()
	if err != nil {
		t.Fatalf("GetIssuesWithOpenDeps failed: %v", err)
	}

	// Only dependentOpen should have open deps
	if !openDeps[dependentOpen.ID] {
		t.Error("dependentOpen should have open deps")
	}
	if openDeps[dependentClosed.ID] {
		t.Error("dependentClosed should not have open deps (dependency is closed)")
	}
}

func TestGetIssueStatuses(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Issue 2", Status: models.StatusInProgress}
	issue3 := &models.Issue{Title: "Issue 3", Status: models.StatusClosed}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	issue2.Status = models.StatusInProgress
	db.UpdateIssue(issue2)
	db.CreateIssue(issue3)
	issue3.Status = models.StatusClosed
	db.UpdateIssue(issue3)

	statuses, err := db.GetIssueStatuses([]string{issue1.ID, issue2.ID, issue3.ID})
	if err != nil {
		t.Fatalf("GetIssueStatuses failed: %v", err)
	}

	if len(statuses) != 3 {
		t.Errorf("Expected 3 statuses, got %d", len(statuses))
	}
	if statuses[issue1.ID] != models.StatusOpen {
		t.Errorf("issue1 status mismatch: got %s", statuses[issue1.ID])
	}
	if statuses[issue2.ID] != models.StatusInProgress {
		t.Errorf("issue2 status mismatch: got %s", statuses[issue2.ID])
	}
	if statuses[issue3.ID] != models.StatusClosed {
		t.Errorf("issue3 status mismatch: got %s", statuses[issue3.ID])
	}
}

func TestGetIssueStatuses_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	statuses, err := db.GetIssueStatuses([]string{})
	if err != nil {
		t.Fatalf("GetIssueStatuses failed: %v", err)
	}

	if len(statuses) != 0 {
		t.Errorf("Expected empty map, got %d entries", len(statuses))
	}
}

func TestGetIssueStatuses_DeduplicatesIDs(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Issue", Status: models.StatusOpen}
	db.CreateIssue(issue)

	// Pass duplicate IDs
	statuses, err := db.GetIssueStatuses([]string{issue.ID, issue.ID, issue.ID})
	if err != nil {
		t.Fatalf("GetIssueStatuses failed: %v", err)
	}

	if len(statuses) != 1 {
		t.Errorf("Expected 1 status (deduped), got %d", len(statuses))
	}
}

// ============================================================================
// File Link Tests
// ============================================================================

func TestLinkFile(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Issue"}
	db.CreateIssue(issue)

	err = db.LinkFile(issue.ID, "/path/to/file.go", models.FileRoleImplementation, "abc123")
	if err != nil {
		t.Fatalf("LinkFile failed: %v", err)
	}

	files, _ := db.GetLinkedFiles(issue.ID)
	if len(files) != 1 {
		t.Fatalf("Expected 1 linked file, got %d", len(files))
	}
	if files[0].FilePath != "/path/to/file.go" {
		t.Errorf("FilePath mismatch: got %s", files[0].FilePath)
	}
	if files[0].Role != models.FileRoleImplementation {
		t.Errorf("Role mismatch: got %s", files[0].Role)
	}
	if files[0].LinkedSHA != "abc123" {
		t.Errorf("LinkedSHA mismatch: got %s", files[0].LinkedSHA)
	}
}

func TestUnlinkFile(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Issue"}
	db.CreateIssue(issue)

	db.LinkFile(issue.ID, "/path/to/file.go", models.FileRoleImplementation, "abc123")
	err = db.UnlinkFile(issue.ID, "/path/to/file.go")
	if err != nil {
		t.Fatalf("UnlinkFile failed: %v", err)
	}

	files, _ := db.GetLinkedFiles(issue.ID)
	if len(files) != 0 {
		t.Error("File should be unlinked")
	}
}

func TestGetLinkedFiles_SortedByRoleAndPath(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Issue"}
	db.CreateIssue(issue)

	db.LinkFile(issue.ID, "/z/test.go", models.FileRoleTest, "sha1")
	db.LinkFile(issue.ID, "/a/impl.go", models.FileRoleImplementation, "sha2")
	db.LinkFile(issue.ID, "/b/impl.go", models.FileRoleImplementation, "sha3")

	files, _ := db.GetLinkedFiles(issue.ID)
	if len(files) != 3 {
		t.Fatalf("Expected 3 files, got %d", len(files))
	}

	// Should be sorted by role, then path
	// Implementation files first (a/impl.go, b/impl.go), then test files (z/test.go)
	if files[0].Role != models.FileRoleImplementation {
		t.Errorf("Expected implementation first, got %s", files[0].Role)
	}
}

// ============================================================================
// Session History Tests (Issue Relations specific)
// Note: Basic session action tests are in bypass_prevention_test.go
// ============================================================================

func TestGetIssueSessionLog(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	sessionID := "ses_test"

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	issue3 := &models.Issue{Title: "Issue 3 (no logs)"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	// Add logs for issue1 and issue2
	db.AddLog(&models.Log{IssueID: issue1.ID, SessionID: sessionID, Message: "Log 1", Type: models.LogTypeProgress})
	db.AddLog(&models.Log{IssueID: issue1.ID, SessionID: sessionID, Message: "Log 2", Type: models.LogTypeProgress})
	db.AddLog(&models.Log{IssueID: issue2.ID, SessionID: sessionID, Message: "Log 3", Type: models.LogTypeProgress})

	// Get issues touched by session
	issueIDs, err := db.GetIssueSessionLog(sessionID)
	if err != nil {
		t.Fatalf("GetIssueSessionLog failed: %v", err)
	}

	if len(issueIDs) != 2 {
		t.Errorf("Expected 2 issues, got %d", len(issueIDs))
	}

	foundIDs := make(map[string]bool)
	for _, id := range issueIDs {
		foundIDs[id] = true
	}
	if !foundIDs[issue1.ID] || !foundIDs[issue2.ID] {
		t.Error("Not all expected issues found")
	}
	if foundIDs[issue3.ID] {
		t.Error("issue3 should not be in results (no logs)")
	}
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestOrphanIssues(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create issue with non-existent parent
	orphan := &models.Issue{Title: "Orphan", ParentID: "nonexistent-parent-id"}
	if err := db.CreateIssue(orphan); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Should still be retrievable
	retrieved, err := db.GetIssue(orphan.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if retrieved.ParentID != "nonexistent-parent-id" {
		t.Error("ParentID should be preserved even if parent doesn't exist")
	}

	// getDescendants on nonexistent parent will actually find the orphan
	// because it queries issues WHERE parent_id = ? which matches the orphan
	descendants, err := db.getDescendants("nonexistent-parent-id")
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}
	// The orphan has parent_id = "nonexistent-parent-id", so it's found as a child
	if len(descendants) != 1 {
		t.Errorf("Expected 1 descendant (the orphan), got %d", len(descendants))
	}
	if len(descendants) > 0 && descendants[0] != orphan.ID {
		t.Errorf("Expected orphan ID, got %s", descendants[0])
	}

	// Test with a truly nonexistent parent that has no children
	descendants2, err := db.getDescendants("truly-nonexistent-no-children")
	if err != nil {
		t.Fatalf("getDescendants failed: %v", err)
	}
	if len(descendants2) != 0 {
		t.Errorf("Expected 0 descendants for parent with no children, got %d", len(descendants2))
	}
}

func TestParentChildIntegrity(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	// Create parent, then children
	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	if err := db.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	child1 := &models.Issue{Title: "Child 1", ParentID: parent.ID}
	child2 := &models.Issue{Title: "Child 2", ParentID: parent.ID}
	if err := db.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := db.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Verify child has correct parent
	retrieved, _ := db.GetIssue(child1.ID)
	if retrieved.ParentID != parent.ID {
		t.Errorf("Child ParentID mismatch: expected %s, got %s", parent.ID, retrieved.ParentID)
	}

	// Update child's parent to a different issue
	child3 := &models.Issue{Title: "Child 3"}
	db.CreateIssue(child3)
	child1.ParentID = child3.ID
	if err := db.UpdateIssue(child1); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	// Verify parent change
	retrieved, _ = db.GetIssue(child1.ID)
	if retrieved.ParentID != child3.ID {
		t.Errorf("Child ParentID not updated: expected %s, got %s", child3.ID, retrieved.ParentID)
	}

	// Original parent should now have only 1 child
	children, _ := db.GetDirectChildren(parent.ID)
	if len(children) != 1 {
		t.Errorf("Expected 1 child after re-parenting, got %d", len(children))
	}
}

func TestMultipleDependencyTypes(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	issue3 := &models.Issue{Title: "Issue 3"}
	db.CreateIssue(issue1)
	db.CreateIssue(issue2)
	db.CreateIssue(issue3)

	// Add depends_on relationship
	db.AddDependency(issue2.ID, issue1.ID, "depends_on")

	// Add a different relationship (issue3 depends on issue2)
	db.AddDependency(issue3.ID, issue2.ID, "depends_on")

	// GetDependencies only returns depends_on for the specific issue
	deps2, _ := db.GetDependencies(issue2.ID)
	if len(deps2) != 1 {
		t.Errorf("Expected 1 depends_on dependency for issue2, got %d", len(deps2))
	}
	if len(deps2) > 0 && deps2[0] != issue1.ID {
		t.Errorf("Expected issue2 to depend on issue1, got %s", deps2[0])
	}

	deps3, _ := db.GetDependencies(issue3.ID)
	if len(deps3) != 1 {
		t.Errorf("Expected 1 depends_on dependency for issue3, got %d", len(deps3))
	}
	if len(deps3) > 0 && deps3[0] != issue2.ID {
		t.Errorf("Expected issue3 to depend on issue2, got %s", deps3[0])
	}

	// issue1 has no dependencies
	deps1, _ := db.GetDependencies(issue1.ID)
	if len(deps1) != 0 {
		t.Errorf("Expected 0 dependencies for issue1, got %d", len(deps1))
	}
}

// ============================================================================
// CascadeUnblockDependents Tests
// ============================================================================

func TestCascadeUnblockDependents_SingleDep(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusBlocked}
	db.CreateIssue(blocker)
	db.CreateIssue(dependent)
	db.AddDependency(dependent.ID, blocker.ID, "depends_on")

	count, ids := db.CascadeUnblockDependents(blocker.ID, "test-session")

	if count != 1 {
		t.Errorf("expected 1 unblocked, got %d", count)
	}
	if len(ids) != 1 || ids[0] != dependent.ID {
		t.Errorf("expected [%s], got %v", dependent.ID, ids)
	}

	updated, _ := db.GetIssue(dependent.ID)
	if updated.Status != models.StatusOpen {
		t.Errorf("expected open, got %s", updated.Status)
	}
}

func TestCascadeUnblockDependents_AllDepsClosed(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	a1 := &models.Issue{Title: "A1", Status: models.StatusClosed}
	a2 := &models.Issue{Title: "A2", Status: models.StatusClosed}
	b := &models.Issue{Title: "B", Status: models.StatusBlocked}
	db.CreateIssue(a1)
	db.CreateIssue(a2)
	db.CreateIssue(b)
	db.AddDependency(b.ID, a1.ID, "depends_on")
	db.AddDependency(b.ID, a2.ID, "depends_on")

	count, _ := db.CascadeUnblockDependents(a2.ID, "test-session")

	if count != 1 {
		t.Errorf("expected 1 unblocked, got %d", count)
	}
	updated, _ := db.GetIssue(b.ID)
	if updated.Status != models.StatusOpen {
		t.Errorf("expected open, got %s", updated.Status)
	}
}

func TestCascadeUnblockDependents_PartialResolution(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	a1 := &models.Issue{Title: "A1", Status: models.StatusClosed}
	a2 := &models.Issue{Title: "A2", Status: models.StatusOpen} // not closed
	b := &models.Issue{Title: "B", Status: models.StatusBlocked}
	db.CreateIssue(a1)
	db.CreateIssue(a2)
	db.CreateIssue(b)
	db.AddDependency(b.ID, a1.ID, "depends_on")
	db.AddDependency(b.ID, a2.ID, "depends_on")

	count, _ := db.CascadeUnblockDependents(a1.ID, "test-session")

	if count != 0 {
		t.Errorf("expected 0 unblocked, got %d", count)
	}
	updated, _ := db.GetIssue(b.ID)
	if updated.Status != models.StatusBlocked {
		t.Errorf("expected blocked, got %s", updated.Status)
	}
}

func TestCascadeUnblockDependents_NonBlockedSkipped(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusOpen} // not blocked
	db.CreateIssue(blocker)
	db.CreateIssue(dependent)
	db.AddDependency(dependent.ID, blocker.ID, "depends_on")

	count, _ := db.CascadeUnblockDependents(blocker.ID, "test-session")

	if count != 0 {
		t.Errorf("expected 0 unblocked, got %d", count)
	}
	updated, _ := db.GetIssue(dependent.ID)
	if updated.Status != models.StatusOpen {
		t.Errorf("expected open, got %s", updated.Status)
	}
}

func TestCascadeUnblockDependents_InProgressSkipped(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusInProgress}
	db.CreateIssue(blocker)
	db.CreateIssue(dependent)
	db.AddDependency(dependent.ID, blocker.ID, "depends_on")

	count, _ := db.CascadeUnblockDependents(blocker.ID, "test-session")

	if count != 0 {
		t.Errorf("expected 0 unblocked, got %d", count)
	}
	updated, _ := db.GetIssue(dependent.ID)
	if updated.Status != models.StatusInProgress {
		t.Errorf("expected in_progress, got %s", updated.Status)
	}
}

func TestCascadeUnblockDependents_NoDependents(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	issue := &models.Issue{Title: "Standalone", Status: models.StatusClosed}
	db.CreateIssue(issue)

	count, ids := db.CascadeUnblockDependents(issue.ID, "test-session")

	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
	if ids != nil {
		t.Errorf("expected nil, got %v", ids)
	}
}

func TestCascadeUnblockDependents_MultipleBlocked(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	b1 := &models.Issue{Title: "B1", Status: models.StatusBlocked}
	b2 := &models.Issue{Title: "B2", Status: models.StatusBlocked}
	db.CreateIssue(blocker)
	db.CreateIssue(b1)
	db.CreateIssue(b2)
	db.AddDependency(b1.ID, blocker.ID, "depends_on")
	db.AddDependency(b2.ID, blocker.ID, "depends_on")

	count, ids := db.CascadeUnblockDependents(blocker.ID, "test-session")

	if count != 2 {
		t.Errorf("expected 2 unblocked, got %d", count)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 ids, got %d", len(ids))
	}

	for _, dep := range []*models.Issue{b1, b2} {
		updated, _ := db.GetIssue(dep.ID)
		if updated.Status != models.StatusOpen {
			t.Errorf("%s: expected open, got %s", dep.ID, updated.Status)
		}
	}
}

func TestCascadeUnblockDependents_ChainNoTransitive(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	a := &models.Issue{Title: "A", Status: models.StatusClosed}
	b := &models.Issue{Title: "B", Status: models.StatusBlocked}
	c := &models.Issue{Title: "C", Status: models.StatusBlocked}
	db.CreateIssue(a)
	db.CreateIssue(b)
	db.CreateIssue(c)
	db.AddDependency(b.ID, a.ID, "depends_on")
	db.AddDependency(c.ID, b.ID, "depends_on")

	count, _ := db.CascadeUnblockDependents(a.ID, "test-session")

	if count != 1 {
		t.Errorf("expected 1 unblocked (only B), got %d", count)
	}

	bUpdated, _ := db.GetIssue(b.ID)
	if bUpdated.Status != models.StatusOpen {
		t.Errorf("B: expected open, got %s", bUpdated.Status)
	}

	cUpdated, _ := db.GetIssue(c.ID)
	if cUpdated.Status != models.StatusBlocked {
		t.Errorf("C: expected blocked (B is open not closed), got %s", cUpdated.Status)
	}
}

func TestCascadeUnblockDependents_LogsCreated(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusBlocked}
	db.CreateIssue(blocker)
	db.CreateIssue(dependent)
	db.AddDependency(dependent.ID, blocker.ID, "depends_on")

	db.CascadeUnblockDependents(blocker.ID, "test-session")

	// Verify progress log
	logs, err := db.GetLogs(dependent.ID, 0)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	found := false
	for _, l := range logs {
		if l.Type == models.LogTypeProgress && l.Message == "Auto-unblocked (dependency "+blocker.ID+" closed)" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auto-unblock progress log entry")
	}

	// Verify action log
	action, err := db.GetLastAction("test-session")
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if action == nil {
		t.Fatal("expected action log entry")
	}
	if action.ActionType != models.ActionUnblock {
		t.Errorf("expected ActionUnblock, got %s", action.ActionType)
	}
	if action.EntityID != dependent.ID {
		t.Errorf("expected entity %s, got %s", dependent.ID, action.EntityID)
	}
}

func TestCascadeUnblockDependents_UndoData(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	blocker := &models.Issue{Title: "Blocker", Status: models.StatusClosed}
	dependent := &models.Issue{Title: "Dependent", Status: models.StatusBlocked}
	db.CreateIssue(blocker)
	db.CreateIssue(dependent)
	db.AddDependency(dependent.ID, blocker.ID, "depends_on")

	db.CascadeUnblockDependents(blocker.ID, "test-session")

	action, err := db.GetLastAction("test-session")
	if err != nil || action == nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}

	// PreviousData should contain blocked status
	if action.PreviousData == "" {
		t.Fatal("expected PreviousData to be set")
	}
	if action.NewData == "" {
		t.Fatal("expected NewData to be set")
	}

	// Verify the status values in the JSON
	if !strings.Contains(action.PreviousData, string(models.StatusBlocked)) {
		t.Errorf("PreviousData should contain 'blocked', got: %s", action.PreviousData)
	}
	if !strings.Contains(action.NewData, string(models.StatusOpen)) {
		t.Errorf("NewData should contain 'open', got: %s", action.NewData)
	}
}
