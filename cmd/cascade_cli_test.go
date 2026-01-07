package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestCascadeCLIBasic tests basic parent-child cascade behavior
func TestCascadeCLIBasic(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	epic := &models.Issue{Title: "Epic: Feature X", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	child := &models.Issue{Title: "Task: Implement", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child)

	sessionID := "ses_test_cascade"

	// Close child
	child.Status = models.StatusClosed
	now := time.Now()
	child.ClosedAt = &now
	database.UpdateIssue(child)

	cascaded, cascadedIDs := database.CascadeUpParentStatus(child.ID, models.StatusClosed, sessionID)

	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded parent, got %d", cascaded)
	}
	if len(cascadedIDs) != 1 || cascadedIDs[0] != epic.ID {
		t.Errorf("Expected cascaded ID %s, got %v", epic.ID, cascadedIDs)
	}

	retrievedEpic, _ := database.GetIssue(epic.ID)
	if retrievedEpic.Status != models.StatusClosed {
		t.Errorf("Epic should be closed, got %q", retrievedEpic.Status)
	}
}

// TestCascadeCLIMultipleChildren tests cascade with multiple children
func TestCascadeCLIMultipleChildren(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	sessionID := "ses_multi_children"
	epic := &models.Issue{Title: "Epic: Big Feature", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	children := make([]*models.Issue, 3)
	for i := 0; i < 3; i++ {
		children[i] = &models.Issue{
			Title: fmt.Sprintf("Child %d", i+1), Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID,
		}
		database.CreateIssue(children[i])
	}

	// Close first two
	now := time.Now()
	for i := 0; i < 2; i++ {
		children[i].Status = models.StatusClosed
		children[i].ClosedAt = &now
		database.UpdateIssue(children[i])
	}

	// Should NOT cascade yet
	cascaded, _ := database.CascadeUpParentStatus(children[0].ID, models.StatusClosed, sessionID)
	if cascaded != 0 {
		t.Errorf("Epic should not cascade yet, got %d cascaded", cascaded)
	}

	// Close last child
	children[2].Status = models.StatusClosed
	children[2].ClosedAt = &now
	database.UpdateIssue(children[2])

	// Now cascade
	cascaded, _ = database.CascadeUpParentStatus(children[2].ID, models.StatusClosed, sessionID)
	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded, got %d", cascaded)
	}

	epic, _ = database.GetIssue(epic.ID)
	if epic.Status != models.StatusClosed {
		t.Errorf("Epic should be closed, got %q", epic.Status)
	}
}

// TestCascadeCLINestedHierarchy tests cascade through multiple levels
func TestCascadeCLINestedHierarchy(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	sessionID := "ses_nested"

	grandparent := &models.Issue{Title: "Epic: L1", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(grandparent)

	parent := &models.Issue{Title: "Epic: L2", Type: models.TypeEpic, Status: models.StatusOpen, ParentID: grandparent.ID}
	database.CreateIssue(parent)

	child := &models.Issue{Title: "Task: L3", Type: models.TypeTask, Status: models.StatusOpen, ParentID: parent.ID}
	database.CreateIssue(child)

	now := time.Now()
	child.Status = models.StatusClosed
	child.ClosedAt = &now
	database.UpdateIssue(child)

	cascaded, _ := database.CascadeUpParentStatus(child.ID, models.StatusClosed, sessionID)

	if cascaded != 2 {
		t.Errorf("Expected 2 cascaded levels, got %d", cascaded)
	}

	retrievedParent, _ := database.GetIssue(parent.ID)
	if retrievedParent.Status != models.StatusClosed {
		t.Errorf("Parent should be closed, got %q", retrievedParent.Status)
	}

	retrievedGrandparent, _ := database.GetIssue(grandparent.ID)
	if retrievedGrandparent.Status != models.StatusClosed {
		t.Errorf("Grandparent should be closed, got %q", retrievedGrandparent.Status)
	}
}

// TestCascadeCLIStatusRules tests cascade respects status transitions
func TestCascadeCLIStatusRules(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	sessionID := "ses_rules"
	epic := &models.Issue{Title: "Epic for review", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	child := &models.Issue{Title: "Child for review", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child)

	child.Status = models.StatusInReview
	database.UpdateIssue(child)

	cascaded, _ := database.CascadeUpParentStatus(child.ID, models.StatusInReview, sessionID)

	if cascaded != 1 {
		t.Errorf("Expected cascade to in_review, got %d", cascaded)
	}

	epic, _ = database.GetIssue(epic.ID)
	if epic.Status != models.StatusInReview {
		t.Errorf("Epic should be in_review, got %q", epic.Status)
	}
}

// TestCascadeCLINoParent tests cascade handles orphan issues
func TestCascadeCLINoParent(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	task := &models.Issue{Title: "Orphan Task", Type: models.TypeTask, Status: models.StatusOpen}
	database.CreateIssue(task)

	now := time.Now()
	task.Status = models.StatusClosed
	task.ClosedAt = &now
	database.UpdateIssue(task)

	cascaded, cascadedIDs := database.CascadeUpParentStatus(task.ID, models.StatusClosed, "ses_orphan")

	if cascaded != 0 {
		t.Errorf("Expected 0 cascaded for orphan, got %d", cascaded)
	}
	if len(cascadedIDs) != 0 {
		t.Errorf("Expected no cascaded IDs, got %v", cascadedIDs)
	}
}

// TestCascadeCLIUndoable tests cascade operations are logged
func TestCascadeCLIUndoable(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	sessionID := "ses_undo_test"
	epic := &models.Issue{Title: "Epic for undo", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	child := &models.Issue{Title: "Child for undo", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child)

	now := time.Now()
	child.Status = models.StatusClosed
	child.ClosedAt = &now
	database.UpdateIssue(child)

	database.CascadeUpParentStatus(child.ID, models.StatusClosed, sessionID)

	actions, _ := database.GetRecentActions(sessionID, 10)
	foundCascadeAction := false
	for _, action := range actions {
		if action.EntityID == epic.ID && action.ActionType == models.ActionClose {
			foundCascadeAction = true
			if action.PreviousData == "" || action.NewData == "" {
				t.Error("Cascade action should have previous and new data for undo")
			}
			break
		}
	}

	if !foundCascadeAction {
		t.Error("Expected cascade action to be logged")
	}
}

// TestCascadeCLINonEpicParent tests cascade doesn't cascade through non-epic parents
func TestCascadeCLINonEpicParent(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	parentTask := &models.Issue{Title: "Parent Task", Type: models.TypeTask, Status: models.StatusOpen}
	database.CreateIssue(parentTask)

	childTask := &models.Issue{Title: "Child Task", Type: models.TypeTask, Status: models.StatusOpen, ParentID: parentTask.ID}
	database.CreateIssue(childTask)

	now := time.Now()
	childTask.Status = models.StatusClosed
	childTask.ClosedAt = &now
	database.UpdateIssue(childTask)

	cascaded, _ := database.CascadeUpParentStatus(childTask.ID, models.StatusClosed, "ses_non_epic")

	if cascaded != 0 {
		t.Errorf("Should not cascade through non-epic parent, got %d", cascaded)
	}

	parentTask, _ = database.GetIssue(parentTask.ID)
	if parentTask.Status != models.StatusOpen {
		t.Errorf("Non-epic parent should not be affected, got %q", parentTask.Status)
	}
}

// TestCascadeCLITableDriven is a table-driven test for cascade scenarios
func TestCascadeCLITableDriven(t *testing.T) {
	tests := []struct {
		name                string
		parentType          models.Type
		numChildren         int
		childrenToClose     int
		targetStatus        models.Status
		expectedCascaded    int
		shouldCascadeParent bool
	}{
		{"Epic 1 child closed", models.TypeEpic, 1, 1, models.StatusClosed, 1, true},
		{"Epic 3 children 2 closed", models.TypeEpic, 3, 2, models.StatusClosed, 0, false},
		{"Epic 3 children all closed", models.TypeEpic, 3, 3, models.StatusClosed, 1, true},
		{"Task parent no cascade", models.TypeTask, 1, 1, models.StatusClosed, 0, false},
		{"Epic to in_review all children", models.TypeEpic, 2, 2, models.StatusInReview, 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			database, _ := db.Initialize(dir)
			defer database.Close()

			sessionID := fmt.Sprintf("ses_%s", tt.name)
			parent := &models.Issue{Title: fmt.Sprintf("Parent: %s", tt.name), Type: tt.parentType, Status: models.StatusOpen}
			database.CreateIssue(parent)

			children := make([]*models.Issue, tt.numChildren)
			for i := 0; i < tt.numChildren; i++ {
				children[i] = &models.Issue{
					Title: fmt.Sprintf("Child %d", i+1), Type: models.TypeTask, Status: models.StatusOpen, ParentID: parent.ID,
				}
				database.CreateIssue(children[i])
			}

			now := time.Now()
			for i := 0; i < tt.childrenToClose; i++ {
				children[i].Status = tt.targetStatus
				if tt.targetStatus == models.StatusClosed {
					children[i].ClosedAt = &now
				}
				database.UpdateIssue(children[i])
			}

			cascaded, _ := database.CascadeUpParentStatus(children[tt.childrenToClose-1].ID, tt.targetStatus, sessionID)

			if cascaded != tt.expectedCascaded {
				t.Errorf("Expected %d cascaded, got %d", tt.expectedCascaded, cascaded)
			}

			retrievedParent, _ := database.GetIssue(parent.ID)
			if tt.shouldCascadeParent && retrievedParent.Status != tt.targetStatus {
				t.Errorf("Parent should be %q, got %q", tt.targetStatus, retrievedParent.Status)
			}
			if !tt.shouldCascadeParent && retrievedParent.Status != models.StatusOpen {
				t.Errorf("Parent should remain open, got %q", retrievedParent.Status)
			}
		})
	}
}

// TestCascadeCLIInReviewStatus tests cascade with in_review
func TestCascadeCLIInReviewStatus(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	epic := &models.Issue{Title: "Epic in review", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	child1 := &models.Issue{Title: "Child 1", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child1)

	child2 := &models.Issue{Title: "Child 2", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child2)

	child1.Status = models.StatusInReview
	database.UpdateIssue(child1)

	now := time.Now()
	child2.Status = models.StatusClosed
	child2.ClosedAt = &now
	database.UpdateIssue(child2)

	cascaded, _ := database.CascadeUpParentStatus(child1.ID, models.StatusInReview, "ses_review")

	if cascaded != 1 {
		t.Errorf("Expected 1 cascaded, got %d", cascaded)
	}

	epic, _ = database.GetIssue(epic.ID)
	if epic.Status != models.StatusInReview {
		t.Errorf("Epic should be in_review, got %q", epic.Status)
	}
}

// TestCascadeCLIMixedStatusChildren tests with mixed child statuses
func TestCascadeCLIMixedStatusChildren(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	epic := &models.Issue{Title: "Mixed epic", Type: models.TypeEpic, Status: models.StatusOpen}
	database.CreateIssue(epic)

	child1 := &models.Issue{Title: "Open", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child1)

	child2 := &models.Issue{Title: "InProgress", Type: models.TypeTask, Status: models.StatusInProgress, ParentID: epic.ID}
	database.CreateIssue(child2)

	child3 := &models.Issue{Title: "Closed", Type: models.TypeTask, Status: models.StatusClosed, ParentID: epic.ID}
	now := time.Now()
	child3.ClosedAt = &now
	database.CreateIssue(child3)

	cascaded, _ := database.CascadeUpParentStatus(child3.ID, models.StatusClosed, "ses_mixed")

	if cascaded != 0 {
		t.Errorf("Should not cascade with mixed statuses, got %d", cascaded)
	}

	epic, _ = database.GetIssue(epic.ID)
	if epic.Status != models.StatusOpen {
		t.Errorf("Epic should remain open, got %q", epic.Status)
	}
}

// TestCascadeCLIAlreadyClosed tests cascade with already-closed parent
func TestCascadeCLIAlreadyClosed(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.Initialize(dir)
	defer database.Close()

	now := time.Now()
	epic := &models.Issue{Title: "Closed epic", Type: models.TypeEpic, Status: models.StatusClosed, ClosedAt: &now}
	database.CreateIssue(epic)

	child := &models.Issue{Title: "Orphan child", Type: models.TypeTask, Status: models.StatusOpen, ParentID: epic.ID}
	database.CreateIssue(child)

	child.Status = models.StatusClosed
	child.ClosedAt = &now
	database.UpdateIssue(child)

	cascaded, _ := database.CascadeUpParentStatus(child.ID, models.StatusClosed, "ses_already_closed")

	if cascaded != 0 {
		t.Errorf("Should not cascade to already-closed parent, got %d", cascaded)
	}
}
