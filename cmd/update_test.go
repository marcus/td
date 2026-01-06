package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestUpdateIssueTitle tests updating issue title
func TestUpdateIssueTitle(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Original Title"}
	database.CreateIssue(issue)

	issue.Title = "Updated Title"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Title != "Updated Title" {
		t.Errorf("Title not updated: got %q", retrieved.Title)
	}
}

// TestUpdateIssueDescription tests updating issue description
func TestUpdateIssueDescription(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Description: "Original desc"}
	database.CreateIssue(issue)

	issue.Description = "New description"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Description != "New description" {
		t.Errorf("Description not updated: got %q", retrieved.Description)
	}
}

// TestUpdateIssueType tests updating issue type
func TestUpdateIssueType(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Type: models.TypeTask}
	database.CreateIssue(issue)

	issue.Type = models.TypeBug
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Type != models.TypeBug {
		t.Errorf("Type not updated: got %q", retrieved.Type)
	}
}

// TestUpdateIssuePriority tests updating issue priority
func TestUpdateIssuePriority(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Priority: models.PriorityP2}
	database.CreateIssue(issue)

	issue.Priority = models.PriorityP0
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Priority != models.PriorityP0 {
		t.Errorf("Priority not updated: got %q", retrieved.Priority)
	}
}

// TestUpdateIssuePoints tests updating story points
func TestUpdateIssuePoints(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Points: 3}
	database.CreateIssue(issue)

	issue.Points = 8
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Points != 8 {
		t.Errorf("Points not updated: got %d", retrieved.Points)
	}
}

// TestUpdateIssueLabels tests updating labels
func TestUpdateIssueLabels(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Labels: []string{"old"}}
	database.CreateIssue(issue)

	issue.Labels = []string{"new1", "new2"}
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if len(retrieved.Labels) != 2 {
		t.Errorf("Labels not updated: got %v", retrieved.Labels)
	}
}

// TestUpdateIssueClearLabels tests clearing labels
func TestUpdateIssueClearLabels(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Labels: []string{"tag1", "tag2"}}
	database.CreateIssue(issue)

	issue.Labels = nil
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if len(retrieved.Labels) != 0 {
		t.Errorf("Labels not cleared: got %v", retrieved.Labels)
	}
}

// TestUpdateIssueStatus tests status transitions
func TestUpdateIssueStatus(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Status: models.StatusOpen}
	database.CreateIssue(issue)

	// Open -> In Progress
	issue.Status = models.StatusInProgress
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Errorf("Status not updated: got %q", retrieved.Status)
	}

	// In Progress -> In Review
	issue.Status = models.StatusInReview
	database.UpdateIssue(issue)

	retrieved, _ = database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInReview {
		t.Errorf("Status not updated to in_review: got %q", retrieved.Status)
	}
}

// TestUpdateReplaceDependencies tests replacing dependencies
func TestUpdateReplaceDependencies(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issues
	issue := &models.Issue{Title: "Main Issue"}
	dep1 := &models.Issue{Title: "Old Dep"}
	dep2 := &models.Issue{Title: "New Dep"}
	database.CreateIssue(issue)
	database.CreateIssue(dep1)
	database.CreateIssue(dep2)

	// Add original dependency
	database.AddDependency(issue.ID, dep1.ID, "depends_on")

	// Verify original
	deps, _ := database.GetDependencies(issue.ID)
	if len(deps) != 1 || deps[0] != dep1.ID {
		t.Fatalf("Original dependency not set")
	}

	// Replace with new dependency
	database.RemoveDependency(issue.ID, dep1.ID)
	database.AddDependency(issue.ID, dep2.ID, "depends_on")

	// Verify replacement
	deps, _ = database.GetDependencies(issue.ID)
	if len(deps) != 1 || deps[0] != dep2.ID {
		t.Errorf("Dependency not replaced: got %v", deps)
	}
}

// TestUpdateClearDependencies tests clearing all dependencies
func TestUpdateClearDependencies(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Main Issue"}
	dep1 := &models.Issue{Title: "Dep 1"}
	dep2 := &models.Issue{Title: "Dep 2"}
	database.CreateIssue(issue)
	database.CreateIssue(dep1)
	database.CreateIssue(dep2)

	database.AddDependency(issue.ID, dep1.ID, "depends_on")
	database.AddDependency(issue.ID, dep2.ID, "depends_on")

	// Clear all dependencies
	deps, _ := database.GetDependencies(issue.ID)
	for _, dep := range deps {
		database.RemoveDependency(issue.ID, dep)
	}

	// Verify cleared
	deps, _ = database.GetDependencies(issue.ID)
	if len(deps) != 0 {
		t.Errorf("Dependencies not cleared: got %v", deps)
	}
}

// TestUpdateReplaceBlocks tests replacing blocked issues
func TestUpdateReplaceBlocks(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	blocker := &models.Issue{Title: "Blocker"}
	blocked1 := &models.Issue{Title: "Blocked 1"}
	blocked2 := &models.Issue{Title: "Blocked 2"}
	database.CreateIssue(blocker)
	database.CreateIssue(blocked1)
	database.CreateIssue(blocked2)

	// blocked1 depends on blocker
	database.AddDependency(blocked1.ID, blocker.ID, "depends_on")

	// Replace: remove blocked1, add blocked2
	database.RemoveDependency(blocked1.ID, blocker.ID)
	database.AddDependency(blocked2.ID, blocker.ID, "depends_on")

	// Verify
	blockedBy, _ := database.GetBlockedBy(blocker.ID)
	if len(blockedBy) != 1 || blockedBy[0] != blocked2.ID {
		t.Errorf("Blocks not replaced: got %v", blockedBy)
	}
}

// TestUpdateBatchMultipleIssues tests updating multiple issues
func TestUpdateBatchMultipleIssues(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "Issue 1", Priority: models.PriorityP3}
	issue2 := &models.Issue{Title: "Issue 2", Priority: models.PriorityP3}
	issue3 := &models.Issue{Title: "Issue 3", Priority: models.PriorityP3}
	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	// Batch update priorities
	for _, issue := range []*models.Issue{issue1, issue2, issue3} {
		issue.Priority = models.PriorityP1
		database.UpdateIssue(issue)
	}

	// Verify all updated
	for _, id := range []string{issue1.ID, issue2.ID, issue3.ID} {
		retrieved, _ := database.GetIssue(id)
		if retrieved.Priority != models.PriorityP1 {
			t.Errorf("Issue %s priority not updated", id)
		}
	}
}

// TestUpdatePartialFields tests that unspecified fields remain unchanged
func TestUpdatePartialFields(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Original",
		Description: "Desc",
		Type:        models.TypeTask,
		Priority:    models.PriorityP2,
		Points:      5,
	}
	database.CreateIssue(issue)

	// Only update title
	issue.Title = "Updated Title"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Title != "Updated Title" {
		t.Error("Title not updated")
	}
	if retrieved.Description != "Desc" {
		t.Error("Description incorrectly changed")
	}
	if retrieved.Type != models.TypeTask {
		t.Error("Type incorrectly changed")
	}
	if retrieved.Priority != models.PriorityP2 {
		t.Error("Priority incorrectly changed")
	}
	if retrieved.Points != 5 {
		t.Error("Points incorrectly changed")
	}
}

// TestUpdateParentID tests updating parent relationship
func TestUpdateParentID(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	parent1 := &models.Issue{Title: "Parent 1", Type: models.TypeEpic}
	parent2 := &models.Issue{Title: "Parent 2", Type: models.TypeEpic}
	child := &models.Issue{Title: "Child", ParentID: ""}
	database.CreateIssue(parent1)
	database.CreateIssue(parent2)
	database.CreateIssue(child)

	// Set initial parent
	child.ParentID = parent1.ID
	database.UpdateIssue(child)

	retrieved, _ := database.GetIssue(child.ID)
	if retrieved.ParentID != parent1.ID {
		t.Errorf("Parent not set: got %q", retrieved.ParentID)
	}

	// Change parent
	child.ParentID = parent2.ID
	database.UpdateIssue(child)

	retrieved, _ = database.GetIssue(child.ID)
	if retrieved.ParentID != parent2.ID {
		t.Errorf("Parent not changed: got %q", retrieved.ParentID)
	}

	// Clear parent
	child.ParentID = ""
	database.UpdateIssue(child)

	retrieved, _ = database.GetIssue(child.ID)
	if retrieved.ParentID != "" {
		t.Errorf("Parent not cleared: got %q", retrieved.ParentID)
	}
}

// TestUpdateUpdatedAtTimestamp tests that UpdatedAt is updated
func TestUpdateUpdatedAtTimestamp(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test"}
	database.CreateIssue(issue)

	originalUpdatedAt := issue.UpdatedAt

	// Update issue
	issue.Title = "Updated"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if !retrieved.UpdatedAt.After(originalUpdatedAt) && !retrieved.UpdatedAt.Equal(originalUpdatedAt) {
		t.Error("UpdatedAt should be >= original after update")
	}
}

// TestUpdateAcceptanceCriteria tests updating acceptance criteria
func TestUpdateAcceptanceCriteria(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Acceptance: "Original AC"}
	database.CreateIssue(issue)

	issue.Acceptance = "New acceptance criteria"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Acceptance != "New acceptance criteria" {
		t.Errorf("Acceptance not updated: got %q", retrieved.Acceptance)
	}
}

// TestAppendDescription tests appending to existing description
func TestAppendDescription(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Description: "Initial description"}
	database.CreateIssue(issue)

	// Simulate append mode: concat with double newline
	newDesc := "Appended text"
	issue.Description = issue.Description + "\n\n" + newDesc
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	expected := "Initial description\n\nAppended text"
	if retrieved.Description != expected {
		t.Errorf("Description append failed: got %q, want %q", retrieved.Description, expected)
	}
}

// TestAppendToEmptyDescription tests append to empty description sets value directly
func TestAppendToEmptyDescription(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Description: ""}
	database.CreateIssue(issue)

	// With empty existing description, just set the value (no leading separator)
	issue.Description = "New description"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Description != "New description" {
		t.Errorf("Description not set: got %q", retrieved.Description)
	}
}

// TestAppendAcceptance tests appending to existing acceptance criteria
func TestAppendAcceptance(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Acceptance: "Initial criteria"}
	database.CreateIssue(issue)

	// Simulate append mode
	newAC := "Additional criteria"
	issue.Acceptance = issue.Acceptance + "\n\n" + newAC
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	expected := "Initial criteria\n\nAdditional criteria"
	if retrieved.Acceptance != expected {
		t.Errorf("Acceptance append failed: got %q, want %q", retrieved.Acceptance, expected)
	}
}

// TestAppendToEmptyAcceptance tests append to empty acceptance sets value directly
func TestAppendToEmptyAcceptance(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test", Acceptance: ""}
	database.CreateIssue(issue)

	// With empty existing acceptance, just set the value
	issue.Acceptance = "New criteria"
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Acceptance != "New criteria" {
		t.Errorf("Acceptance not set: got %q", retrieved.Acceptance)
	}
}

// TestUpdateCmdHasStatusFlag tests that --status flag exists on update command
func TestUpdateCmdHasStatusFlag(t *testing.T) {
	// Test that --status flag exists
	if updateCmd.Flags().Lookup("status") == nil {
		t.Error("Expected --status flag to be defined on update command")
	}

	// Test that the flag can be set
	if err := updateCmd.Flags().Set("status", "open"); err != nil {
		t.Errorf("Failed to set --status flag: %v", err)
	}

	statusValue, err := updateCmd.Flags().GetString("status")
	if err != nil {
		t.Errorf("Failed to get --status flag value: %v", err)
	}
	if statusValue != "open" {
		t.Errorf("Expected status value 'open', got %s", statusValue)
	}

	// Reset
	updateCmd.Flags().Set("status", "")
}
