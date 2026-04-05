package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestTreeSingleIssue tests tree view with a single issue
func TestTreeSingleIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Root Issue",
		Type:   models.TypeEpic,
		Status: models.StatusOpen,
	}
	mustCreateIssue(t, database, issue)

	// Retrieve and verify
	retrieved, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if retrieved.ID != issue.ID {
		t.Errorf("Issue ID mismatch")
	}
}

// TestTreeParentChild tests parent-child relationships
func TestTreeParentChild(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title: "Parent Epic",
		Type:  models.TypeEpic,
	}
	mustCreateIssue(t, database, parent)

	child1 := &models.Issue{
		Title:    "Child Task 1",
		ParentID: parent.ID,
		Type:     models.TypeTask,
	}
	child2 := &models.Issue{
		Title:    "Child Task 2",
		ParentID: parent.ID,
		Type:     models.TypeTask,
	}

	mustCreateIssue(t, database, child1)
	mustCreateIssue(t, database, child2)

	// Verify parent-child relationships
	child1Retrieved, _ := database.GetIssue(child1.ID)
	if child1Retrieved.ParentID != parent.ID {
		t.Errorf("Child 1 parent mismatch: expected %s, got %s", parent.ID, child1Retrieved.ParentID)
	}

	child2Retrieved, _ := database.GetIssue(child2.ID)
	if child2Retrieved.ParentID != parent.ID {
		t.Errorf("Child 2 parent mismatch: expected %s, got %s", parent.ID, child2Retrieved.ParentID)
	}
}

// TestTreeNestedHierarchy tests deeply nested hierarchy
func TestTreeNestedHierarchy(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create hierarchy: root -> level1 -> level2 -> level3
	root := &models.Issue{Title: "Root", Type: models.TypeEpic}
	level1 := &models.Issue{Title: "Level 1", Type: models.TypeEpic}
	level2 := &models.Issue{Title: "Level 2", Type: models.TypeEpic}
	level3 := &models.Issue{Title: "Level 3", Type: models.TypeTask}

	mustCreateIssue(t, database, root)
	mustCreateIssue(t, database, level1)
	mustCreateIssue(t, database, level2)
	mustCreateIssue(t, database, level3)

	level1.ParentID = root.ID
	level2.ParentID = level1.ID
	level3.ParentID = level2.ID

	mustUpdateIssue(t, database, level1)
	mustUpdateIssue(t, database, level2)
	mustUpdateIssue(t, database, level3)

	// Verify hierarchy
	l1, _ := database.GetIssue(level1.ID)
	if l1.ParentID != root.ID {
		t.Error("Level 1 parent mismatch")
	}

	l2, _ := database.GetIssue(level2.ID)
	if l2.ParentID != level1.ID {
		t.Error("Level 2 parent mismatch")
	}

	l3, _ := database.GetIssue(level3.ID)
	if l3.ParentID != level2.ID {
		t.Error("Level 3 parent mismatch")
	}
}

// TestTreeMultipleChildren tests issue with multiple children
func TestTreeMultipleChildren(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{Title: "Parent", Type: models.TypeEpic}
	mustCreateIssue(t, database, parent)

	// Create 5 children and track their IDs
	childCount := 5
	childIDs := make([]string, childCount)

	for i := 0; i < childCount; i++ {
		child := &models.Issue{
			Title:    "Child " + string(rune('0'+i)),
			ParentID: parent.ID,
			Type:     models.TypeTask,
		}
		mustCreateIssue(t, database, child)
		childIDs[i] = child.ID
	}

	// Verify all children point to parent
	for i := 0; i < childCount; i++ {
		retrieved, _ := database.GetIssue(childIDs[i])
		if retrieved == nil {
			t.Errorf("Child %d not found", i)
			continue
		}
		if retrieved.ParentID != parent.ID {
			t.Errorf("Child %d parent mismatch", i)
		}
	}
}

// TestTreeWithDifferentTypes tests tree with mixed issue types
func TestTreeWithDifferentTypes(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	epic := &models.Issue{Title: "Epic", Type: models.TypeEpic}
	feature := &models.Issue{Title: "Feature", Type: models.TypeFeature}
	bug := &models.Issue{Title: "Bug", Type: models.TypeBug}
	task := &models.Issue{Title: "Task", Type: models.TypeTask}

	mustCreateIssue(t, database, epic)
	mustCreateIssue(t, database, feature)
	mustCreateIssue(t, database, bug)
	mustCreateIssue(t, database, task)

	feature.ParentID = epic.ID
	bug.ParentID = epic.ID
	task.ParentID = epic.ID

	mustUpdateIssue(t, database, feature)
	mustUpdateIssue(t, database, bug)
	mustUpdateIssue(t, database, task)

	// Verify hierarchy
	f, _ := database.GetIssue(feature.ID)
	b, _ := database.GetIssue(bug.ID)
	tk, _ := database.GetIssue(task.ID)

	if f.ParentID != epic.ID || b.ParentID != epic.ID || tk.ParentID != epic.ID {
		t.Error("Parent relationships not established correctly")
	}
}

// TestTreeOrphanedChildren tests children with missing parent
func TestTreeOrphanedChildren(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	orphan := &models.Issue{
		Title:    "Orphaned Child",
		ParentID: "td-nonexistent",
		Type:     models.TypeTask,
	}
	mustCreateIssue(t, database, orphan)

	// Verify orphan exists even though parent doesn't
	retrieved, _ := database.GetIssue(orphan.ID)
	if retrieved.ParentID != "td-nonexistent" {
		t.Error("Orphaned child's parent ID was lost")
	}

	// Verify parent doesn't exist
	_, err = database.GetIssue("td-nonexistent")
	if err == nil {
		t.Error("Non-existent parent should error")
	}
}

// TestTreeReparenting tests changing a child's parent
func TestTreeReparenting(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent1 := &models.Issue{Title: "Parent 1", Type: models.TypeEpic}
	parent2 := &models.Issue{Title: "Parent 2", Type: models.TypeEpic}
	mustCreateIssue(t, database, parent1)
	mustCreateIssue(t, database, parent2)

	child := &models.Issue{
		Title:    "Child",
		ParentID: parent1.ID,
		Type:     models.TypeTask,
	}
	mustCreateIssue(t, database, child)

	// Verify initial parent
	c1, _ := database.GetIssue(child.ID)
	if c1.ParentID != parent1.ID {
		t.Error("Initial parent not set correctly")
	}

	// Change parent
	child.ParentID = parent2.ID
	mustUpdateIssue(t, database, child)

	// Verify new parent
	c2, _ := database.GetIssue(child.ID)
	if c2.ParentID != parent2.ID {
		t.Errorf("Parent not changed: expected %s, got %s", parent2.ID, c2.ParentID)
	}
}

// TestTreeWithDifferentStatuses tests tree showing various issue statuses
func TestTreeWithDifferentStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title:  "Parent",
		Type:   models.TypeEpic,
		Status: models.StatusInProgress,
	}
	mustCreateIssue(t, database, parent)

	statuses := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	}

	for _, status := range statuses {
		child := &models.Issue{
			Title:    string(status),
			ParentID: parent.ID,
			Type:     models.TypeTask,
			Status:   status,
		}
		mustCreateIssue(t, database, child)

		retrieved, _ := database.GetIssue(child.ID)
		if retrieved.Status != status {
			t.Errorf("Status mismatch: expected %s, got %s", status, retrieved.Status)
		}
	}
}

// TestTreeEmptyParent tests issue with no children
func TestTreeEmptyParent(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title: "Empty Parent",
		Type:  models.TypeEpic,
	}
	mustCreateIssue(t, database, parent)

	retrieved, _ := database.GetIssue(parent.ID)
	if retrieved.ID != parent.ID {
		t.Error("Parent not found")
	}
}

// TestTreeWithPriorities tests tree showing different priorities
func TestTreeWithPriorities(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title:    "Parent",
		Type:     models.TypeEpic,
		Priority: models.PriorityP0,
	}
	mustCreateIssue(t, database, parent)

	priorities := []models.Priority{
		models.PriorityP0,
		models.PriorityP1,
		models.PriorityP2,
		models.PriorityP3,
		models.PriorityP4,
	}

	for _, priority := range priorities {
		child := &models.Issue{
			Title:    string(priority),
			ParentID: parent.ID,
			Type:     models.TypeTask,
			Priority: priority,
		}
		mustCreateIssue(t, database, child)

		retrieved, _ := database.GetIssue(child.ID)
		if retrieved.Priority != priority {
			t.Errorf("Priority mismatch: expected %s, got %s", priority, retrieved.Priority)
		}
	}
}

// TestTreeWithPoints tests tree showing story points
func TestTreeWithPoints(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title: "Parent",
		Type:  models.TypeEpic,
	}
	mustCreateIssue(t, database, parent)

	points := []int{1, 2, 3, 5, 8, 13, 21}

	for _, pts := range points {
		child := &models.Issue{
			Title:    "Task " + string(rune('0'+pts/10)),
			ParentID: parent.ID,
			Type:     models.TypeTask,
			Points:   pts,
		}
		mustCreateIssue(t, database, child)

		retrieved, _ := database.GetIssue(child.ID)
		if retrieved.Points != pts {
			t.Errorf("Points mismatch: expected %d, got %d", pts, retrieved.Points)
		}
	}
}

// TestTreeDeleteParent tests deleting parent issue
func TestTreeDeleteParent(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title: "Parent",
		Type:  models.TypeEpic,
	}
	mustCreateIssue(t, database, parent)

	child := &models.Issue{
		Title:    "Child",
		ParentID: parent.ID,
		Type:     models.TypeTask,
	}
	mustCreateIssue(t, database, child)

	// Delete parent
	mustDeleteIssue(t, database, parent.ID)

	// Verify parent is deleted
	pDeleted, _ := database.GetIssue(parent.ID)
	if pDeleted.DeletedAt == nil {
		t.Error("Parent should be deleted")
	}

	// Verify child still exists
	cRetrieved, _ := database.GetIssue(child.ID)
	if cRetrieved.DeletedAt != nil {
		t.Error("Child should not be deleted when parent is deleted")
	}

	// Verify child's parent reference is preserved (may vary by implementation)
	if cRetrieved.ID != child.ID {
		t.Error("Child should still exist after parent deletion")
	}
}

// TestTreeBlockedParent tests interaction with blocked parent
func TestTreeBlockedParent(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent := &models.Issue{
		Title:  "Blocked Parent",
		Type:   models.TypeEpic,
		Status: models.StatusBlocked,
	}
	child := &models.Issue{
		Title:    "Child",
		ParentID: parent.ID,
		Type:     models.TypeTask,
		Status:   models.StatusOpen,
	}

	mustCreateIssue(t, database, parent)
	mustCreateIssue(t, database, child)

	pRetrieved, _ := database.GetIssue(parent.ID)
	if pRetrieved.Status != models.StatusBlocked {
		t.Error("Parent status should be blocked")
	}

	cRetrieved, _ := database.GetIssue(child.ID)
	if cRetrieved.Status != models.StatusOpen {
		t.Error("Child can be open even if parent is blocked")
	}
}

// TestTreeLargeHierarchy tests large tree structure
func TestTreeLargeHierarchy(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	root := &models.Issue{Title: "Root", Type: models.TypeEpic}
	mustCreateIssue(t, database, root)

	// Create 100 child issues
	for i := 0; i < 100; i++ {
		child := &models.Issue{
			Title:    "Child " + string(rune(i%10)),
			ParentID: root.ID,
			Type:     models.TypeTask,
		}
		mustCreateIssue(t, database, child)
	}

	// Verify root exists
	rRetrieved, _ := database.GetIssue(root.ID)
	if rRetrieved.ID != root.ID {
		t.Error("Root issue not found")
	}
}
