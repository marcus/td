package cmd

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestResumeSetsFocus tests that resume command sets focus on an issue
func TestResumeSetsFocus(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Issue to resume",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Set focus via config (simulating resume command)
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Verify focus is set
	focused, err := config.GetFocus(dir)
	if err != nil {
		t.Logf("GetFocus error: %v", err)
	} else if focused != issue.ID {
		t.Errorf("Expected focus %s, got %s", issue.ID, focused)
	}
}

// TestResumeWithInProgressIssue tests resume with in_progress issue
func TestResumeWithInProgressIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "In Progress Work",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusInProgress {
		t.Error("Issue should still be in_progress after resume")
	}

	focused, _ := config.GetFocus(dir)
	if focused != issue.ID {
		t.Error("Focus should be set to resumed issue")
	}
}

// TestResumePreservesIssueState tests that resume doesn't modify issue state
func TestResumePreservesIssueState(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Test Issue",
		Description: "Important work",
		Status:      models.StatusInReview,
		Type:        models.TypeFeature,
		Priority:    models.PriorityP1,
		Points:      8,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	originalStatus := issue.Status

	// Resume (just sets focus)
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Verify issue state is unchanged
	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != originalStatus {
		t.Errorf("Issue status changed: %s -> %s", originalStatus, retrieved.Status)
	}
	if retrieved.Title != issue.Title {
		t.Error("Issue title changed")
	}
	if retrieved.Description != issue.Description {
		t.Error("Issue description changed")
	}
}

// TestResumeMultipleIssuesSequence tests resuming multiple issues in sequence
func TestResumeMultipleIssuesSequence(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "First Issue", Status: models.StatusOpen}
	issue2 := &models.Issue{Title: "Second Issue", Status: models.StatusInProgress}
	issue3 := &models.Issue{Title: "Third Issue", Status: models.StatusInReview}

	if err := database.CreateIssue(issue1); err != nil {
		t.Fatalf("CreateIssue issue1 failed: %v", err)
	}
	if err := database.CreateIssue(issue2); err != nil {
		t.Fatalf("CreateIssue issue2 failed: %v", err)
	}
	if err := database.CreateIssue(issue3); err != nil {
		t.Fatalf("CreateIssue issue3 failed: %v", err)
	}

	// Resume each in sequence
	if err := config.SetFocus(dir, issue1.ID); err != nil {
		t.Fatalf("SetFocus issue1 failed: %v", err)
	}
	focused1, _ := config.GetFocus(dir)
	if focused1 != issue1.ID {
		t.Error("Focus should be issue1")
	}

	if err := config.SetFocus(dir, issue2.ID); err != nil {
		t.Fatalf("SetFocus issue2 failed: %v", err)
	}
	focused2, _ := config.GetFocus(dir)
	if focused2 != issue2.ID {
		t.Error("Focus should be issue2")
	}

	if err := config.SetFocus(dir, issue3.ID); err != nil {
		t.Fatalf("SetFocus issue3 failed: %v", err)
	}
	focused3, _ := config.GetFocus(dir)
	if focused3 != issue3.ID {
		t.Error("Focus should be issue3")
	}
}

// TestResumeAllowsContextInformation tests resume allows getting issue context
func TestResumeAllowsContextInformation(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:       "Complex Feature",
		Description: "Needs thorough testing",
		Status:      models.StatusInProgress,
		Type:        models.TypeFeature,
		Priority:    models.PriorityP0,
		Points:      21,
		Labels:      []string{"backend", "critical"},
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue complex feature failed: %v", err)
	}

	// Resume and retrieve context
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus feature issue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.ID != issue.ID {
		t.Error("Cannot retrieve resumed issue")
	}
	if retrieved.Description != issue.Description {
		t.Error("Context description not available")
	}
	if len(retrieved.Labels) != len(issue.Labels) {
		t.Error("Context labels not available")
	}
}

// TestResumeWithBlockedIssue tests resume with blocked issue
func TestResumeWithBlockedIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Blocked Work",
		Status: models.StatusBlocked,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue blocked issue failed: %v", err)
	}

	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus blocked issue failed: %v", err)
	}

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.Status != models.StatusBlocked {
		t.Error("Blocked status should be preserved")
	}
}

// TestResumeWithClosedIssue tests resume with closed issue
func TestResumeWithClosedIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Completed Work",
		Status: models.StatusClosed,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue closed issue failed: %v", err)
	}

	// Can still resume closed issue for context
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus closed issue failed: %v", err)
	}

	focused, _ := config.GetFocus(dir)
	if focused != issue.ID {
		t.Error("Should be able to resume closed issue for context")
	}
}

// TestResumeNonexistentIssue tests resume with non-existent issue
func TestResumeNonexistentIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Try to get non-existent issue
	_, err = database.GetIssue("td-nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent issue")
	}
}

// TestResumeWithLogs tests resume shows recent activity
func TestResumeWithLogs(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Issue with History",
		Status: models.StatusInProgress,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue log issue failed: %v", err)
	}

	// Add some logs
	for i := 0; i < 3; i++ {
		log := &models.Log{
			IssueID:   issue.ID,
			SessionID: "ses_test",
			Message:   "Progress update",
			Type:      models.LogTypeProgress,
		}
		if err := database.AddLog(log); err != nil {
			t.Fatalf("AddLog failed: %v", err)
		}
	}

	// Resume and verify logs are accessible
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus log issue failed: %v", err)
	}

	logs, _ := database.GetLogs(issue.ID, 10)
	if len(logs) != 3 {
		t.Errorf("Expected 3 logs, got %d", len(logs))
	}
}

// TestResumePreservesParentChild tests resume with parent-child issues
func TestResumePreservesParentChild(t *testing.T) {
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
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child Task",
		ParentID: parent.ID,
		Type:     models.TypeTask,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue child failed: %v", err)
	}

	// Resume child
	if err := config.SetFocus(dir, child.ID); err != nil {
		t.Fatalf("SetFocus child issue failed: %v", err)
	}

	// Verify relationship preserved
	retrieved, _ := database.GetIssue(child.ID)
	if retrieved.ParentID != parent.ID {
		t.Error("Parent-child relationship should be preserved after resume")
	}
}

// TestResumePreserveDependencies tests resume with dependencies
func TestResumePreserveDependencies(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	prerequisite := &models.Issue{Title: "Prerequisite"}
	dependent := &models.Issue{Title: "Dependent"}

	if err := database.CreateIssue(prerequisite); err != nil {
		t.Fatalf("CreateIssue prerequisite failed: %v", err)
	}
	if err := database.CreateIssue(dependent); err != nil {
		t.Fatalf("CreateIssue dependent failed: %v", err)
	}

	// Add dependency
	if err := database.AddDependency(dependent.ID, prerequisite.ID, "depends_on"); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	// Resume dependent
	if err := config.SetFocus(dir, dependent.ID); err != nil {
		t.Fatalf("SetFocus dependent issue failed: %v", err)
	}

	// Verify dependency preserved
	deps, _ := database.GetDependencies(dependent.ID)
	if len(deps) != 1 || deps[0] != prerequisite.ID {
		t.Error("Dependencies should be preserved after resume")
	}
}
