package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// setTrustedMode pins a test to trusted review policy via env.
func setTrustedMode(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
}

// runShowCapture runs `td show <id>` and returns its stdout.
func runShowCapture(t *testing.T, issueID string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	runErr := showCmd.RunE(showCmd, []string{issueID})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if runErr != nil {
		t.Fatalf("showCmd.RunE: %v", runErr)
	}
	return buf.String()
}

// TestSelfReviewRejectedInNonTrustedModes verifies the flag errors clearly in
// strict, balanced, and delegated modes.
func TestSelfReviewRejectedInNonTrustedModes(t *testing.T) {
	for _, mode := range []string{"strict", "balanced", "delegated"} {
		t.Run(mode, func(t *testing.T) {
			saveAndRestoreGlobals(t)
			t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", mode)

			dir := t.TempDir()
			baseDir := dir
			baseDirOverride = &baseDir

			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatalf("Initialize: %v", err)
			}
			defer database.Close()

			t.Setenv("TD_SESSION_ID", "impl-agent")
			implID := currentSessionID(t, database)
			issue := newInReviewIssueWithImpl(t, database, implID)

			out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
				"self-review": "true",
				"reason":      "I checked it",
			})
			if err == nil {
				t.Fatalf("expected error in %s mode, got success\n%s", mode, out)
			}
			if !strings.Contains(err.Error(), "--self-review requires review_policy_mode=trusted") {
				t.Fatalf("expected trusted-mode guard error, got %v", err)
			}
		})
	}
}

// TestSelfReviewRequiresReason verifies --self-review without --reason fails in
// trusted mode.
func TestSelfReviewRequiresReason(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"self-review": "true",
	})
	if err != nil {
		// RunE itself returns nil for per-issue skips; the error surfaces in
		// output rather than a returned error. Accept either as long as the
		// issue is NOT closed and the message is present.
		_ = err
	}
	if !strings.Contains(out, "--self-review requires --reason") {
		t.Fatalf("expected reason-required message, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status == models.StatusClosed {
		t.Fatal("issue should not be closed without --reason")
	}
}

// TestSelfReviewImplementerApprovesAndCloses is the end-to-end trusted-mode
// path: the implementer self-reviews their own in_review issue with no prior
// recorded approval, approving+closing in one step and stamping self_review.
func TestSelfReviewImplementerApprovesAndCloses(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"self-review": "true",
		"reason":      "reviewed my own diff, tests pass",
	})
	if err != nil {
		t.Fatalf("self-review approve+close: %v\n%s", err, out)
	}
	if !strings.Contains(out, "APPROVED "+issue.ID) || !strings.Contains(out, "self-review") {
		t.Fatalf("expected self-review approval confirmation, got %q", out)
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusClosed {
		t.Fatalf("status=%s want closed", got.Status)
	}
	if got.ReviewerSession != implID {
		t.Fatalf("reviewer_session=%q want %q", got.ReviewerSession, implID)
	}
	if got.ClosedBySession != implID {
		t.Fatalf("closed_by_session=%q want %q", got.ClosedBySession, implID)
	}

	// Audit field round-trip: the recorded review row must have self_review=true.
	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil {
		t.Fatalf("ListIssueReviews: %v", err)
	}
	if len(reviews) == 0 {
		t.Fatal("expected at least one recorded review")
	}
	last := reviews[len(reviews)-1]
	if last.Decision != reviewpolicy.DecisionApproved {
		t.Fatalf("decision=%s want approved", last.Decision)
	}
	if !last.SelfReview {
		t.Fatal("expected self_review=true on recorded review row")
	}

	// td show renders the (self-review) marker.
	showOut := runShowCapture(t, issue.ID)
	if !strings.Contains(showOut, "(self-review)") {
		t.Fatalf("expected td show to render (self-review), got:\n%s", showOut)
	}
}

// TestSelfReviewRejectedWithoutFlagInTrustedMode verifies that in trusted mode
// the implementer is still blocked from approving their own work WITHOUT the
// flag, with the teaching message.
func TestSelfReviewRejectedWithoutFlagInTrustedMode(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	out, _ := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "trying to self-approve without the flag",
	})
	if !strings.Contains(out, "--self-review") {
		t.Fatalf("expected teaching message naming --self-review, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status == models.StatusClosed {
		t.Fatal("implementer should not close own work without --self-review")
	}
}

// TestTrustedNonImplementerApprovesWithoutFlag verifies that an independent
// session in trusted mode approves+closes without the flag and no self_review
// stamp is recorded.
func TestTrustedNonImplementerApprovesWithoutFlag(t *testing.T) {
	saveAndRestoreGlobals(t)
	setTrustedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implID)

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	reviewerID := currentSessionID(t, database)
	if reviewerID == implID {
		t.Fatal("expected distinct sessions")
	}

	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "independent review",
	})
	if err != nil {
		t.Fatalf("independent approve: %v\n%s", err, out)
	}
	if strings.Contains(out, "self-review") {
		t.Fatalf("independent approval should not mention self-review, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("status=%s want closed", got.Status)
	}

	reviews, err := database.ListIssueReviews(issue.ID)
	if err != nil {
		t.Fatalf("ListIssueReviews: %v", err)
	}
	if len(reviews) == 0 {
		t.Fatal("expected a recorded review")
	}
	last := reviews[len(reviews)-1]
	if last.SelfReview {
		t.Fatal("independent approval must NOT stamp self_review=true")
	}
}
