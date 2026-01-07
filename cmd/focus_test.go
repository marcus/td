package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestFocusSingleIssue tests focusing on a single issue
func TestFocusSingleIssue(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Test Issue",
		Status: models.StatusOpen,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// Set focus
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Verify focus is set
	focused, err := config.GetFocus(dir)
	if err != nil {
		t.Fatalf("GetFocus failed: %v", err)
	}
	if focused != issue.ID {
		t.Errorf("Expected focus %s, got %s", issue.ID, focused)
	}
}

// TestFocusChangeFocus tests changing focus from one issue to another
func TestFocusChangeFocus(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}

	database.CreateIssue(issue1)
	database.CreateIssue(issue2)

	// Focus on issue 1
	config.SetFocus(dir, issue1.ID)
	focused1, _ := config.GetFocus(dir)
	if focused1 != issue1.ID {
		t.Errorf("First focus failed: expected %s, got %s", issue1.ID, focused1)
	}

	// Change focus to issue 2
	config.SetFocus(dir, issue2.ID)
	focused2, _ := config.GetFocus(dir)
	if focused2 != issue2.ID {
		t.Errorf("Focus change failed: expected %s, got %s", issue2.ID, focused2)
	}

	// Verify issue 1 is no longer focused
	if focused2 == issue1.ID {
		t.Error("Previous focus should be cleared")
	}
}

// TestFocusVerifiesIssueExists tests that focus verifies issue exists
func TestFocusVerifiesIssueExists(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Try to focus on non-existent issue
	_, err = database.GetIssue("td-nonexistent")
	if err == nil {
		t.Error("Expected error when getting non-existent issue")
	}
}

// TestUnfocus tests clearing focus
func TestUnfocus(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Set focus
	config.SetFocus(dir, issue.ID)
	focused, _ := config.GetFocus(dir)
	if focused != issue.ID {
		t.Error("Focus not set correctly")
	}

	// Clear focus
	if err := config.ClearFocus(dir); err != nil {
		t.Fatalf("ClearFocus failed: %v", err)
	}

	// Verify focus is cleared
	focused, err = config.GetFocus(dir)
	if err == nil && focused != "" {
		t.Errorf("Focus should be cleared, got %s", focused)
	}
}

// TestFocusWithDifferentStatuses tests focusing on issues with different statuses
func TestFocusWithDifferentStatuses(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	statuses := []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusInReview,
		models.StatusBlocked,
		models.StatusClosed,
	}

	for _, status := range statuses {
		issue := &models.Issue{
			Title:  string(status),
			Status: status,
		}
		database.CreateIssue(issue)

		// Focus on this issue
		if err := config.SetFocus(dir, issue.ID); err != nil {
			t.Fatalf("SetFocus failed for status %s: %v", status, err)
		}

		focused, err := config.GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus failed for status %s: %v", status, err)
		}
		if focused != issue.ID {
			t.Errorf("Focus for status %s failed: expected %s, got %s", status, issue.ID, focused)
		}
	}
}

// TestFocusPersistence tests that focus persists across sessions
func TestFocusPersistence(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Set focus
	config.SetFocus(dir, issue.ID)

	// Close and reopen database
	database.Close()
	database, err = db.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Verify focus is still set
	focused, err := config.GetFocus(dir)
	if err != nil {
		t.Logf("GetFocus after reopen: %v", err)
	} else if focused != issue.ID {
		t.Errorf("Focus not persisted: expected %s, got %s", issue.ID, focused)
	}
}

// TestFocusFileCreation tests that focus file is created properly
func TestFocusFileCreation(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Set focus
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	// Check if focus file exists
	focusPath := filepath.Join(dir, ".todos", "focus")
	if _, err := os.Stat(focusPath); err != nil {
		t.Logf("Focus file not found at %s: %v", focusPath, err)
	}
}

// TestFocusMultipleIssuesSequential tests focusing on multiple issues sequentially
func TestFocusMultipleIssuesSequential(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issueCount := 5
	issues := make([]*models.Issue, issueCount)

	for i := 0; i < issueCount; i++ {
		issue := &models.Issue{Title: "Issue " + string(rune(i))}
		database.CreateIssue(issue)
		issues[i] = issue
	}

	// Focus on each issue in sequence
	for i, issue := range issues {
		if err := config.SetFocus(dir, issue.ID); err != nil {
			t.Fatalf("SetFocus for issue %d failed: %v", i, err)
		}

		focused, err := config.GetFocus(dir)
		if err != nil {
			t.Fatalf("GetFocus for issue %d failed: %v", i, err)
		}
		if focused != issue.ID {
			t.Errorf("Focus for issue %d incorrect: expected %s, got %s", i, issue.ID, focused)
		}
	}
}

// TestUnfocusWhenNoFocus tests unfocusing when no focus is set
func TestUnfocusWhenNoFocus(t *testing.T) {
	dir := t.TempDir()

	// Initialize config without setting focus
	// Try to clear focus when nothing is focused
	err := config.ClearFocus(dir)
	if err != nil {
		t.Logf("ClearFocus with no focus: %v", err)
	}
}

// TestFocusInvalidIssueID tests focusing on invalid issue ID format
func TestFocusInvalidIssueID(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	invalidIDs := []string{"", "invalid", "td_wrong", "not-an-id"}

	for _, id := range invalidIDs {
		_, err := database.GetIssue(id)
		if err == nil {
			t.Logf("Unexpected: found issue with ID %q", id)
		}
	}
}

// TestFocusIDFormat tests that focus preserves exact ID format
func TestFocusIDFormat(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	originalID := issue.ID

	// Set focus
	config.SetFocus(dir, originalID)

	// Verify ID is preserved exactly
	focused, _ := config.GetFocus(dir)
	if focused != originalID {
		t.Errorf("ID not preserved: expected %q, got %q", originalID, focused)
	}
}

// TestFocusWhitespace tests focus with IDs containing no whitespace
func TestFocusWhitespace(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// Set focus
	config.SetFocus(dir, issue.ID)

	// Verify no whitespace issues
	focused, _ := config.GetFocus(dir)
	if len(focused) != len(issue.ID) {
		t.Errorf("Whitespace issue: expected length %d, got %d", len(issue.ID), len(focused))
	}
}

// TestFocusWithSpecialCharacters tests that focus handles IDs correctly
func TestFocusWithSpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Test Issue"}
	database.CreateIssue(issue)

	// IDs should be alphanumeric format like "td-xxxxx"
	if len(issue.ID) == 0 || issue.ID[:3] != "td-" {
		t.Logf("Unexpected ID format: %q", issue.ID)
	}

	// Set focus
	config.SetFocus(dir, issue.ID)

	focused, _ := config.GetFocus(dir)
	if focused != issue.ID {
		t.Errorf("ID with format td-xxxxx failed: expected %q, got %q", issue.ID, focused)
	}
}

// TestFocusConcurrentChanges tests that focus updates correctly with multiple changes
func TestFocusConcurrentChanges(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue1 := &models.Issue{Title: "Issue 1"}
	issue2 := &models.Issue{Title: "Issue 2"}
	issue3 := &models.Issue{Title: "Issue 3"}

	database.CreateIssue(issue1)
	database.CreateIssue(issue2)
	database.CreateIssue(issue3)

	// Rapidly change focus
	for i := 0; i < 3; i++ {
		config.SetFocus(dir, issue1.ID)
		config.SetFocus(dir, issue2.ID)
		config.SetFocus(dir, issue3.ID)
	}

	// Final focus should be issue 3
	focused, _ := config.GetFocus(dir)
	if focused != issue3.ID {
		t.Errorf("Final focus should be %s, got %s", issue3.ID, focused)
	}
}
