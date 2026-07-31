package cmd

import (
	"bytes"
	"encoding/json"
	"github.com/marcus/td/internal/reviewpolicy"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestShowFormatFlagParsing tests that --format flag is defined and works
func TestShowFormatFlagParsing(t *testing.T) {
	// Test that --format flag exists
	if showCmd.Flags().Lookup("format") == nil {
		t.Error("Expected --format flag to be defined")
	}

	// Test that -f shorthand exists
	if showCmd.Flags().ShorthandLookup("f") == nil {
		t.Error("Expected -f shorthand to be defined for --format")
	}

	// Test that --format flag can be set
	if err := showCmd.Flags().Set("format", "json"); err != nil {
		t.Errorf("Failed to set --format flag: %v", err)
	}

	formatValue, err := showCmd.Flags().GetString("format")
	if err != nil {
		t.Errorf("Failed to get --format flag value: %v", err)
	}
	if formatValue != "json" {
		t.Errorf("Expected format value 'json', got %s", formatValue)
	}

	// Reset
	showCmd.Flags().Set("format", "")
}

// TestShowAcceptsZeroArgs tests that show can be called with no arguments
func TestShowAcceptsZeroArgs(t *testing.T) {
	// Test that show command accepts 0 arguments
	args := showCmd.Args
	if args == nil {
		t.Fatal("Expected Args validator to be set")
	}

	// Test with 0 args (should be valid - will try to find current work)
	if err := args(showCmd, []string{}); err != nil {
		t.Errorf("Expected 0 args to be valid: %v", err)
	}

	// Test with 1 arg (should be valid)
	if err := args(showCmd, []string{"td-test123"}); err != nil {
		t.Errorf("Expected 1 arg to be valid: %v", err)
	}
}

// TestShowJSONFlagStillWorks tests that --json flag is still available
func TestShowJSONFlagStillWorks(t *testing.T) {
	// Test that --json flag exists
	if showCmd.Flags().Lookup("json") == nil {
		t.Error("Expected --json flag to still be defined")
	}

	// Test that --json flag can be set
	if err := showCmd.Flags().Set("json", "true"); err != nil {
		t.Errorf("Failed to set --json flag: %v", err)
	}

	jsonValue, err := showCmd.Flags().GetBool("json")
	if err != nil {
		t.Errorf("Failed to get --json flag value: %v", err)
	}
	if !jsonValue {
		t.Error("Expected json flag to be true")
	}

	// Reset
	showCmd.Flags().Set("json", "false")
}

// TestShowRenderMarkdownFlagExists tests that --render-markdown flag is defined
func TestShowRenderMarkdownFlagExists(t *testing.T) {
	if showCmd.Flags().Lookup("render-markdown") == nil {
		t.Error("Expected --render-markdown flag to be defined")
	}
	if showCmd.Flags().ShorthandLookup("m") == nil {
		t.Error("Expected -m shorthand to be defined for --render-markdown")
	}
}

func TestShowNoArgsUsesSingleInReviewIssue(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Reviewable single issue",
		Status: models.StatusInReview,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := showCmd.RunE(showCmd, []string{})

	w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	if runErr != nil {
		t.Fatalf("showCmd.RunE returned error: %v", runErr)
	}

	got := output.String()
	if !strings.Contains(got, issue.ID) {
		t.Fatalf("expected output to contain issue ID %q, got %s", issue.ID, got)
	}
	if !strings.Contains(got, issue.Title) {
		t.Fatalf("expected output to contain issue title %q, got %s", issue.Title, got)
	}
}

// runShowCmd captures stdout for a `td show <id>` invocation.
func runShowCmd(t *testing.T, args []string, jsonOut bool) string {
	t.Helper()
	_ = showCmd.Flags().Set("json", "false")
	if jsonOut {
		_ = showCmd.Flags().Set("json", "true")
	}
	defer func() { _ = showCmd.Flags().Set("json", "false") }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	runErr := showCmd.RunE(showCmd, args)
	w.Close()
	os.Stdout = oldStdout

	var out bytes.Buffer
	_, _ = io.Copy(&out, r)
	if runErr != nil {
		t.Fatalf("showCmd.RunE: %v (output %s)", runErr, out.String())
	}
	return out.String()
}

// TestShowRendersReviewAttribution covers the two display acceptance criteria
// of the attribution work, which two review rounds found had no coverage:
// an attributed review must render the reviewer's NAME, and the JSON must
// carry reviewed_by.
//
// It matters that the attributed case does not render "(self-review)". Both
// facts are true of the row — an involved session recorded it AND someone else
// reviewed it — but showing only the first misreports the record in exactly
// the direction this feature exists to correct.
func TestShowRendersReviewAttribution(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	attributed := &models.Issue{Title: "Attributed", Status: models.StatusInReview}
	if err := database.CreateIssue(attributed); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         attributed.ID,
		ReviewerSession: "ses-orchestrator",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "sub-agent reviewed it",
		SelfReview:      true,
		ReviewedBy:      "code-reviewer sub-agent",
	}); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}

	human := runShowCmd(t, []string{attributed.ID}, false)
	if !strings.Contains(human, "reviewed by code-reviewer sub-agent") {
		t.Errorf("human output must name the reviewer, got:\n%s", human)
	}
	if strings.Contains(human, "(self-review)") {
		t.Errorf("an attributed review must not render as a self-review, got:\n%s", human)
	}

	raw := runShowCmd(t, []string{attributed.ID}, true)
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal show --json: %v (raw %s)", err, raw)
	}
	history, _ := payload["review_history"].([]any)
	if len(history) != 1 {
		t.Fatalf("expected one review_history entry, got %v", payload["review_history"])
	}
	entry, _ := history[0].(map[string]any)
	if entry["reviewed_by"] != "code-reviewer sub-agent" {
		t.Errorf("review_history.reviewed_by = %v, want the attribution", entry["reviewed_by"])
	}
	if entry["self_review"] != true {
		t.Errorf("review_history.self_review = %v, want true", entry["self_review"])
	}

	// An unattributed self-review still renders as one — the two cases must
	// stay distinguishable in the output, not just in the database.
	selfReviewed := &models.Issue{Title: "Self-reviewed", Status: models.StatusInReview}
	if err := database.CreateIssue(selfReviewed); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         selfReviewed.ID,
		ReviewerSession: "ses-impl",
		Decision:        reviewpolicy.DecisionApproved,
		Summary:         "reviewed my own diff",
		SelfReview:      true,
	}); err != nil {
		t.Fatalf("CreateIssueReview: %v", err)
	}
	human = runShowCmd(t, []string{selfReviewed.ID}, false)
	if !strings.Contains(human, "(self-review)") {
		t.Errorf("an unattributed self-review must still render as one, got:\n%s", human)
	}
}
