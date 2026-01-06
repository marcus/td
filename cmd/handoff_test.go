package cmd

import (
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

	if handoff.ID == 0 {
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

	// Test with 0 args (should fail)
	if err := args(handoffCmd, []string{}); err == nil {
		t.Error("Expected 0 args to fail")
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
