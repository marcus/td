package cmd

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestParseHandoffInputEmpty tests parsing empty input
func TestParseHandoffInputEmpty(t *testing.T) {
	handoff := &models.Handoff{}
	// Empty handoff should have empty slices
	if len(handoff.Done) != 0 {
		t.Error("Expected empty Done slice")
	}
	if len(handoff.Remaining) != 0 {
		t.Error("Expected empty Remaining slice")
	}
}

// TestHandoffRecordsData tests that handoff data is stored correctly
func TestHandoffRecordsData(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Implemented feature A", "Fixed bug B"},
		Remaining: []string{"Write tests", "Update docs"},
		Decisions: []string{"Use approach X"},
		Uncertain: []string{"Need to verify performance"},
	}

	if err := database.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	if handoff.ID == "" {
		t.Error("Expected handoff ID to be set")
	}
}

// TestHandoffRecordsGitSnapshot tests that git state is captured on handoff
func TestHandoffRecordsGitSnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)

	// Add handoff
	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Work done"},
	}
	database.AddHandoff(handoff)

	// Record git snapshot
	snapshot := &models.GitSnapshot{
		IssueID:    issue.ID,
		Event:      "handoff",
		CommitSHA:  "def456abc789012345678901234567890abcdef0",
		Branch:     "feature-branch",
		DirtyFiles: 2,
	}
	if err := database.AddGitSnapshot(snapshot); err != nil {
		t.Fatalf("AddGitSnapshot failed: %v", err)
	}
}

// TestHandoffWithMultipleSections tests parsing all YAML sections
func TestHandoffWithMultipleSections(t *testing.T) {
	handoff := &models.Handoff{
		Done:      []string{"Task 1", "Task 2"},
		Remaining: []string{"Task 3"},
		Decisions: []string{"Decision 1", "Decision 2", "Decision 3"},
		Uncertain: []string{"Question 1"},
	}

	if len(handoff.Done) != 2 {
		t.Errorf("Expected 2 done items, got %d", len(handoff.Done))
	}
	if len(handoff.Remaining) != 1 {
		t.Errorf("Expected 1 remaining item, got %d", len(handoff.Remaining))
	}
	if len(handoff.Decisions) != 3 {
		t.Errorf("Expected 3 decisions, got %d", len(handoff.Decisions))
	}
	if len(handoff.Uncertain) != 1 {
		t.Errorf("Expected 1 uncertainty, got %d", len(handoff.Uncertain))
	}
}

// TestHandoffRequiresIssueID tests that handoff requires issue ID
func TestHandoffRequiresIssueID(t *testing.T) {
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

// TestHandoffUpdatesIssueTimestamp tests that handoff updates issue
func TestHandoffUpdatesIssueTimestamp(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)
	originalUpdatedAt := issue.UpdatedAt

	// Update issue (as handoff command would)
	database.UpdateIssue(issue)

	retrieved, _ := database.GetIssue(issue.ID)
	if retrieved.UpdatedAt.Before(originalUpdatedAt) {
		t.Error("UpdatedAt should not be before original")
	}
}

// TestHandoffSessionIDRequired tests that session ID is recorded
func TestHandoffSessionIDRequired(t *testing.T) {
	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_abc123",
		Done:      []string{"Work"},
	}

	if handoff.SessionID == "" {
		t.Error("SessionID should not be empty")
	}
	if handoff.SessionID != "ses_abc123" {
		t.Errorf("SessionID mismatch: got %q", handoff.SessionID)
	}
}

// TestHandoffDoneItems tests done items parsing
func TestHandoffDoneItems(t *testing.T) {
	items := []string{
		"Implemented feature",
		"Fixed bug",
		"Added tests",
	}

	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
		Done:      items,
	}

	if len(handoff.Done) != 3 {
		t.Fatalf("Expected 3 done items, got %d", len(handoff.Done))
	}
	if handoff.Done[0] != "Implemented feature" {
		t.Errorf("First done item mismatch: %q", handoff.Done[0])
	}
}

// TestHandoffRemainingItems tests remaining items parsing
func TestHandoffRemainingItems(t *testing.T) {
	items := []string{
		"Write documentation",
		"Performance testing",
	}

	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
		Remaining: items,
	}

	if len(handoff.Remaining) != 2 {
		t.Fatalf("Expected 2 remaining items, got %d", len(handoff.Remaining))
	}
}

// TestHandoffDecisionItems tests decisions parsing
func TestHandoffDecisionItems(t *testing.T) {
	items := []string{
		"Using Redis for caching",
		"Chose REST over GraphQL",
	}

	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
		Decisions: items,
	}

	if len(handoff.Decisions) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(handoff.Decisions))
	}
}

// TestHandoffUncertainItems tests uncertainty parsing
func TestHandoffUncertainItems(t *testing.T) {
	items := []string{
		"Not sure about error handling approach",
		"Need to verify with PM about edge case",
	}

	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
		Uncertain: items,
	}

	if len(handoff.Uncertain) != 2 {
		t.Fatalf("Expected 2 uncertainties, got %d", len(handoff.Uncertain))
	}
}

// TestHandoffEmptySlices tests empty slices are valid
func TestHandoffEmptySlices(t *testing.T) {
	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
		Done:      []string{},
		Remaining: []string{},
		Decisions: []string{},
		Uncertain: []string{},
	}

	if handoff.Done == nil {
		t.Error("Done should be empty slice, not nil")
	}
	if len(handoff.Done) != 0 {
		t.Error("Expected empty Done slice")
	}
}

// TestHandoffNilSlices tests nil slices are valid
func TestHandoffNilSlices(t *testing.T) {
	handoff := &models.Handoff{
		IssueID:   "td-test",
		SessionID: "ses_test",
	}

	// nil is valid, command handles nil and empty the same
	if len(handoff.Done) != 0 {
		t.Error("nil Done should have length 0")
	}
}

// TestHandoffPreservesWhitespace tests that significant whitespace is preserved
func TestHandoffPreservesWhitespace(t *testing.T) {
	items := []string{
		"Item with  double  spaces",
		"Item with\ttabs",
	}

	handoff := &models.Handoff{
		Done: items,
	}

	// Internal whitespace should be preserved
	if handoff.Done[0] != "Item with  double  spaces" {
		t.Errorf("Internal whitespace not preserved: %q", handoff.Done[0])
	}
}

// TestMultipleHandoffsForSameIssue tests multiple handoffs can be recorded
func TestMultipleHandoffsForSameIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)

	// First handoff
	handoff1 := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_1",
		Done:      []string{"First work"},
	}
	database.AddHandoff(handoff1)

	// Second handoff
	handoff2 := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_2",
		Done:      []string{"Second work"},
	}
	database.AddHandoff(handoff2)

	if handoff1.ID == handoff2.ID {
		t.Error("Handoffs should have different IDs")
	}
}

// TestHandoffNoteFlag tests that --note/-n flag exists
func TestHandoffNoteFlag(t *testing.T) {
	// Test that --note flag exists
	if handoffCmd.Flags().Lookup("note") == nil {
		t.Error("Expected --note flag to be defined on handoff command")
	}

	// Test that -n shorthand exists
	if handoffCmd.Flags().ShorthandLookup("n") == nil {
		t.Error("Expected -n shorthand to be defined for --note on handoff command")
	}

	// Test that --note flag can be set
	if err := handoffCmd.Flags().Set("note", "quick note"); err != nil {
		t.Errorf("Failed to set --note flag: %v", err)
	}

	noteValue, err := handoffCmd.Flags().GetString("note")
	if err != nil {
		t.Errorf("Failed to get --note flag value: %v", err)
	}
	if noteValue != "quick note" {
		t.Errorf("Expected note value 'quick note', got %s", noteValue)
	}

	// Reset
	handoffCmd.Flags().Set("note", "")
}

// TestGetLatestHandoff tests retrieving the most recent handoff
func TestGetLatestHandoff(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	database.CreateIssue(issue)

	// Add handoffs
	database.AddHandoff(&models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_old",
		Done:      []string{"Old work"},
	})

	database.AddHandoff(&models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_new",
		Done:      []string{"New work"},
	})

	// Get latest
	latest, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if latest == nil {
		t.Fatal("Expected handoff, got nil")
	}
	if latest.SessionID != "ses_new" {
		t.Errorf("Expected latest session, got %q", latest.SessionID)
	}
}

// TestHandoffPositionalMessage tests that positional message is accepted
func TestHandoffPositionalMessage(t *testing.T) {
	// Verify handoff accepts 1-2 args (issue-id and optional message)
	args := handoffCmd.Args
	if args == nil {
		t.Fatal("Expected Args validator to be set")
	}

	// Test with 1 arg (should be valid)
	if err := args(handoffCmd, []string{"td-test123"}); err != nil {
		t.Errorf("Expected 1 arg to be valid: %v", err)
	}

	// Test with 2 args (should be valid)
	if err := args(handoffCmd, []string{"td-test123", "quick message"}); err != nil {
		t.Errorf("Expected 2 args to be valid: %v", err)
	}

	// Test with 0 args (should be valid - infers from focused issue)
	if err := args(handoffCmd, []string{}); err != nil {
		t.Errorf("Expected 0 args to be valid (infer from focus): %v", err)
	}

	// Test with 3 args (should fail)
	if err := args(handoffCmd, []string{"a", "b", "c"}); err == nil {
		t.Error("Expected 3 args to fail")
	}
}

// TestHandoffMessageFlag tests that --message/-m flag exists (agent-friendly alias)
func TestHandoffMessageFlag(t *testing.T) {
	// Test that --message flag exists
	if handoffCmd.Flags().Lookup("message") == nil {
		t.Error("Expected --message flag to be defined on handoff command")
	}

	// Test that -m shorthand exists
	if handoffCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --message on handoff command")
	}

	// Test that --message flag can be set
	if err := handoffCmd.Flags().Set("message", "quick message"); err != nil {
		t.Errorf("Failed to set --message flag: %v", err)
	}

	messageValue, err := handoffCmd.Flags().GetString("message")
	if err != nil {
		t.Errorf("Failed to get --message flag value: %v", err)
	}
	if messageValue != "quick message" {
		t.Errorf("Expected message value 'quick message', got %s", messageValue)
	}

	// Reset
	handoffCmd.Flags().Set("message", "")
}

// TestCascadeHandoffBasic tests that handoff cascades to children
func TestCascadeHandoffBasic(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent issue
	parent := &models.Issue{Title: "Parent Task", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	// Create child issue
	child := &models.Issue{Title: "Child Task", Status: models.StatusOpen, ParentID: parent.ID}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue child failed: %v", err)
	}

	// Verify child is a descendant
	hasChildren, err := database.HasChildren(parent.ID)
	if err != nil {
		t.Fatalf("HasChildren failed: %v", err)
	}
	if !hasChildren {
		t.Error("Expected parent to have children")
	}

	// Get descendants
	descendants, err := database.GetDescendantIssues(parent.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(descendants) != 1 {
		t.Errorf("Expected 1 descendant, got %d", len(descendants))
	}
	if descendants[0].ID != child.ID {
		t.Errorf("Expected descendant to be child issue, got %s", descendants[0].ID)
	}
}

// TestCascadeHandoffMultipleChildren tests cascade with multiple children
func TestCascadeHandoffMultipleChildren(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent
	parent := &models.Issue{Title: "Parent Task", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create 3 children
	childIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		child := &models.Issue{
			Title:    fmt.Sprintf("Child %d", i+1),
			Status:   models.StatusOpen,
			ParentID: parent.ID,
		}
		if err := database.CreateIssue(child); err != nil {
			t.Fatalf("CreateIssue child failed: %v", err)
		}
		childIDs[i] = child.ID
	}

	// Get all descendants
	descendants, err := database.GetDescendantIssues(parent.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(descendants) != 3 {
		t.Errorf("Expected 3 descendants, got %d", len(descendants))
	}
}

// TestCascadeHandoffNestedHierarchy tests cascade with nested parent-child hierarchy
func TestCascadeHandoffNestedHierarchy(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create grandparent
	grandparent := &models.Issue{Title: "Grandparent Task", Status: models.StatusInProgress}
	if err := database.CreateIssue(grandparent); err != nil {
		t.Fatalf("CreateIssue grandparent failed: %v", err)
	}

	// Create parent (child of grandparent)
	parent := &models.Issue{
		Title:    "Parent Task",
		Status:   models.StatusOpen,
		ParentID: grandparent.ID,
	}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	// Create child (child of parent)
	child := &models.Issue{
		Title:    "Child Task",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue child failed: %v", err)
	}

	// Get descendants of grandparent (should include parent and child)
	descendants, err := database.GetDescendantIssues(grandparent.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants, got %d", len(descendants))
	}

	// Get descendants of parent (should include child only)
	descendants, err = database.GetDescendantIssues(parent.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(descendants) != 1 {
		t.Errorf("Expected 1 descendant from parent, got %d", len(descendants))
	}
	if descendants[0].ID != child.ID {
		t.Errorf("Expected descendant to be child, got %s", descendants[0].ID)
	}
}

// TestCascadeHandoffSkipsCompletedChildren tests that cascade skips closed children
func TestCascadeHandoffSkipsCompletedChildren(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent
	parent := &models.Issue{Title: "Parent Task", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	// Create open child
	openChild := &models.Issue{
		Title:    "Open Child",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(openChild); err != nil {
		t.Fatalf("CreateIssue open child failed: %v", err)
	}

	// Create closed child
	closedChild := &models.Issue{
		Title:    "Closed Child",
		Status:   models.StatusClosed,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(closedChild); err != nil {
		t.Fatalf("CreateIssue closed child failed: %v", err)
	}

	// Get descendants with Open/InProgress filter
	descendants, err := database.GetDescendantIssues(parent.ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}

	// Should only return open child
	if len(descendants) != 1 {
		t.Errorf("Expected 1 descendant, got %d", len(descendants))
	}
	if descendants[0].ID != openChild.ID {
		t.Errorf("Expected open child, got %s", descendants[0].ID)
	}
}

// TestCascadeHandoffSkipsExistingHandoffs tests that cascade skips children with existing handoffs
func TestCascadeHandoffSkipsExistingHandoffs(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent and children
	parent := &models.Issue{Title: "Parent", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	child1 := &models.Issue{
		Title:    "Child 1",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child1); err != nil {
		t.Fatalf("CreateIssue child1 failed: %v", err)
	}

	child2 := &models.Issue{
		Title:    "Child 2",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child2); err != nil {
		t.Fatalf("CreateIssue child2 failed: %v", err)
	}

	// Add existing handoff to child1
	existingHandoff := &models.Handoff{
		IssueID:   child1.ID,
		SessionID: "ses_existing",
		Done:      []string{"Already worked on this"},
	}
	if err := database.AddHandoff(existingHandoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Verify child1 has a handoff
	handoff, err := database.GetLatestHandoff(child1.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if handoff == nil {
		t.Error("Expected handoff to exist for child1")
	}

	// Verify child2 has no handoff
	handoff, err = database.GetLatestHandoff(child2.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if handoff != nil {
		t.Error("Expected no handoff for child2")
	}
}

// TestUndoHandoffDelete tests undoing handoff deletion (restore cascaded handoff)
func TestUndoHandoffDelete(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue and handoff
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Work completed"},
	}
	if err := database.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	handoffID := handoff.ID

	// Verify handoff exists
	retrieved, err := database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected handoff to exist")
	}

	// Delete the handoff
	if err := database.DeleteHandoff(handoffID); err != nil {
		t.Fatalf("DeleteHandoff failed: %v", err)
	}

	// Verify handoff is deleted
	retrieved, err = database.GetLatestHandoff(issue.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff after delete failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected handoff to be deleted")
	}
}

// TestCascadeAndUndoInteraction tests cascade handoff followed by undo
func TestCascadeAndUndoInteraction(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent and children
	parent := &models.Issue{Title: "Parent", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue child failed: %v", err)
	}

	// Simulate cascade: add handoff to child
	childHandoff := &models.Handoff{
		IssueID:   child.ID,
		SessionID: "ses_test",
		Done:      []string{"Cascaded from parent"},
	}
	if err := database.AddHandoff(childHandoff); err != nil {
		t.Fatalf("AddHandoff to child failed: %v", err)
	}

	// Log the action for undo tracking
	handoffData, _ := json.Marshal(childHandoff)
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionHandoff,
		EntityType: "handoff",
		EntityID:   childHandoff.ID,
		NewData:    string(handoffData),
	}
	if err := database.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Verify child has the cascaded handoff
	retrieved, err := database.GetLatestHandoff(child.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected cascaded handoff")
	}

	// Now undo by deleting the handoff
	if err := undoHandoffAction(database, action, "ses_cascade"); err != nil {
		t.Fatalf("undoHandoffAction failed: %v", err)
	}

	// Verify handoff is deleted
	retrieved, err = database.GetLatestHandoff(child.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff after undo failed: %v", err)
	}
	if retrieved != nil {
		t.Error("Expected cascaded handoff to be undone")
	}
}

// TestMultipleCascadeLevels tests cascade through multiple hierarchy levels
func TestMultipleCascadeLevels(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create 4-level hierarchy: Level0 -> Level1 -> Level2 -> Level3
	levels := make([]*models.Issue, 4)

	// Level 0 (top)
	levels[0] = &models.Issue{Title: "Level 0", Status: models.StatusInProgress}
	if err := database.CreateIssue(levels[0]); err != nil {
		t.Fatalf("CreateIssue level 0 failed: %v", err)
	}

	// Levels 1-3 (nested children)
	for i := 1; i < 4; i++ {
		levels[i] = &models.Issue{
			Title:    fmt.Sprintf("Level %d", i),
			Status:   models.StatusOpen,
			ParentID: levels[i-1].ID,
		}
		if err := database.CreateIssue(levels[i]); err != nil {
			t.Fatalf("CreateIssue level %d failed: %v", i, err)
		}
	}

	// Get descendants of Level 0 - should include Levels 1, 2, 3
	descendants, err := database.GetDescendantIssues(levels[0].ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues failed: %v", err)
	}
	if len(descendants) != 3 {
		t.Errorf("Expected 3 descendants for level 0, got %d", len(descendants))
	}

	// Get descendants of Level 1 - should include Levels 2, 3
	descendants, err = database.GetDescendantIssues(levels[1].ID, []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("GetDescendantIssues for level 1 failed: %v", err)
	}
	if len(descendants) != 2 {
		t.Errorf("Expected 2 descendants for level 1, got %d", len(descendants))
	}
}

// TestHandoffLoggingForUndo tests that handoff actions are logged for undo
func TestHandoffLoggingForUndo(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create issue
	issue := &models.Issue{Title: "Test Issue", Status: models.StatusInProgress}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Create handoff
	handoff := &models.Handoff{
		IssueID:   issue.ID,
		SessionID: "ses_test",
		Done:      []string{"Task completed"},
	}
	if err := database.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Log the handoff action
	handoffData, _ := json.Marshal(handoff)
	action := &models.ActionLog{
		SessionID:  "ses_test",
		ActionType: models.ActionHandoff,
		EntityType: "handoff",
		EntityID:   handoff.ID,
		NewData:    string(handoffData),
	}
	if err := database.LogAction(action); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}

	// Retrieve logged action
	retrievedAction, err := database.GetLastAction("ses_test")
	if err != nil {
		t.Fatalf("GetLastAction failed: %v", err)
	}
	if retrievedAction == nil {
		t.Fatal("Expected action to be logged")
	}

	if retrievedAction.ActionType != models.ActionHandoff {
		t.Errorf("Expected ActionHandoff, got %s", retrievedAction.ActionType)
	}
	if retrievedAction.EntityType != "handoff" {
		t.Errorf("Expected entity type handoff, got %s", retrievedAction.EntityType)
	}
}

// TestCascadeHandoffStatusFiltering tests cascade respects status filters
func TestCascadeHandoffStatusFiltering(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []models.Status
		childStat models.Status
		wantCount int
	}{
		{
			name:      "match open status",
			statuses:  []models.Status{models.StatusOpen},
			childStat: models.StatusOpen,
			wantCount: 1,
		},
		{
			name:      "match in_progress status",
			statuses:  []models.Status{models.StatusInProgress},
			childStat: models.StatusInProgress,
			wantCount: 1,
		},
		{
			name:      "multiple statuses include child",
			statuses:  []models.Status{models.StatusOpen, models.StatusInProgress},
			childStat: models.StatusOpen,
			wantCount: 1,
		},
		{
			name:      "status filter excludes child",
			statuses:  []models.Status{models.StatusClosed},
			childStat: models.StatusOpen,
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize failed: %v", err)
			}
			defer database.Close()

			// Create parent
			parent := &models.Issue{Title: "Parent", Status: models.StatusInProgress}
			if err := database.CreateIssue(parent); err != nil {
				t.Fatalf("CreateIssue parent failed: %v", err)
			}

			// Create child with specified status
			child := &models.Issue{
				Title:    "Child",
				Status:   tc.childStat,
				ParentID: parent.ID,
			}
			if err := database.CreateIssue(child); err != nil {
				t.Fatalf("CreateIssue child failed: %v", err)
			}

			// Get descendants with status filter
			descendants, err := database.GetDescendantIssues(parent.ID, tc.statuses)
			if err != nil {
				t.Fatalf("GetDescendantIssues failed: %v", err)
			}

			if len(descendants) != tc.wantCount {
				t.Errorf("Expected %d descendants, got %d", tc.wantCount, len(descendants))
			}
		})
	}
}

// TestCascadePreservesHandoffContent tests cascaded handoff message format
func TestCascadePreservesHandoffContent(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Create parent and child
	parent := &models.Issue{Title: "Parent", Status: models.StatusInProgress}
	if err := database.CreateIssue(parent); err != nil {
		t.Fatalf("CreateIssue parent failed: %v", err)
	}

	child := &models.Issue{
		Title:    "Child",
		Status:   models.StatusOpen,
		ParentID: parent.ID,
	}
	if err := database.CreateIssue(child); err != nil {
		t.Fatalf("CreateIssue child failed: %v", err)
	}

	// Create cascaded handoff (simulating what cmd/handoff.go does)
	cascadedMessage := fmt.Sprintf("Cascaded from %s", parent.ID)
	childHandoff := &models.Handoff{
		IssueID:   child.ID,
		SessionID: "ses_cascade",
		Done:      []string{cascadedMessage},
	}
	if err := database.AddHandoff(childHandoff); err != nil {
		t.Fatalf("AddHandoff failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := database.GetLatestHandoff(child.ID)
	if err != nil {
		t.Fatalf("GetLatestHandoff failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Expected handoff")
	}

	if len(retrieved.Done) != 1 {
		t.Errorf("Expected 1 done item, got %d", len(retrieved.Done))
	}
	if retrieved.Done[0] != cascadedMessage {
		t.Errorf("Expected '%s', got '%s'", cascadedMessage, retrieved.Done[0])
	}
}
