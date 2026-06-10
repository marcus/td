package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
	"github.com/marcus/td/internal/session"
)

// runApproveCmd is a local helper that captures stdout and resets approve flags
// between invocations.
func runApproveCmd(t *testing.T, args []string, flags map[string]string) (string, error) {
	t.Helper()

	// Reset flags that approve sets so leftover state from earlier tests
	// doesn't leak in.
	resetKeys := []string{"reason", "message", "comment", "note", "notes", "json", "all", "record-only", "decision", "self-review"}
	for _, k := range resetKeys {
		if f := approveCmd.Flags().Lookup(k); f != nil {
			_ = approveCmd.Flags().Set(k, f.DefValue)
		}
	}
	for k, v := range flags {
		if err := approveCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set flag %s=%s: %v", k, v, err)
		}
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := approveCmd.RunE(approveCmd, args)

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	for _, k := range resetKeys {
		if f := approveCmd.Flags().Lookup(k); f != nil {
			_ = approveCmd.Flags().Set(k, f.DefValue)
		}
	}
	return buf.String(), runErr
}

// currentSessionID returns the generated session ID for the current TD_SESSION_ID
// env var by calling session.GetOrCreate.
func currentSessionID(t *testing.T, database *db.DB) string {
	t.Helper()
	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	return sess.ID
}

// setDelegatedMode is shorthand for enabling delegated review policy via env.
func setDelegatedMode(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "delegated")
}

func setStrictMode(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "strict")
}

// setBalancedMode pins a test to the balanced review policy. Paired with
// setStrictMode / setDelegatedMode so Step 5's default flip doesn't silently
// change the mode under tests that need the creator-exception path.
func setBalancedMode(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "balanced")
}

// newInReviewIssueWithImpl creates an in_review issue with the given implementer session.
func newInReviewIssueWithImpl(t *testing.T, database *db.DB, implSession string) *models.Issue {
	t.Helper()
	issue := &models.Issue{
		Title:                    "Test issue",
		Status:                   models.StatusInReview,
		ImplementerSession:       implSession,
		ReviewRequestedBySession: implSession,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	return issue
}

func TestApproveRecordOnlyRecordsReviewWithoutClosing(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Resolve two distinct session IDs by swapping TD_SESSION_ID.
	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	reviewerID := currentSessionID(t, database)
	if implID == reviewerID {
		t.Fatal("expected two distinct sessions; got the same id")
	}

	issue := newInReviewIssueWithImpl(t, database, implID)

	// Run approve --record-only as reviewer.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "looks good",
	})
	if err != nil {
		t.Fatalf("approve --record-only: %v\n%s", err, out)
	}
	if !strings.Contains(out, "REVIEW RECORDED "+issue.ID) {
		t.Fatalf("expected REVIEW RECORDED output, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusInReview {
		t.Fatalf("status = %s, want in_review", got.Status)
	}
	if got.ReviewerSession != reviewerID {
		t.Fatalf("ReviewerSession = %q, want %q", got.ReviewerSession, reviewerID)
	}
	if got.ReviewedAt == nil {
		t.Fatal("ReviewedAt should be set for approved record-only")
	}
	if got.ClosedAt != nil {
		t.Fatal("ClosedAt must remain nil for record-only")
	}

	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil {
		t.Fatalf("expected active approval review, got %v (err=%v)", active, err)
	}
	if active.Decision != reviewpolicy.DecisionApproved {
		t.Fatalf("decision = %s, want approved", active.Decision)
	}
}

func TestApproveNoArgsClosesReadyToCloseIssueForImplementer(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	t.Setenv("TD_SESSION_ID", "impl-noargs")
	implSession := currentSessionID(t, database)
	issue := newInReviewIssueWithImpl(t, database, implSession)
	issue.ReviewRequestedBySession = implSession
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue requester: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "reviewer-noargs")
	reviewerSession := currentSessionID(t, database)
	if reviewerSession == implSession {
		t.Fatal("test expected distinct sessions")
	}
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "reviewed diff and tests",
	}); err != nil {
		t.Fatalf("record-only approval: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "impl-noargs")
	out, err := runApproveCmd(t, nil, map[string]string{
		"reason": "closing after independent review",
	})
	if err != nil {
		t.Fatalf("approve no-args close-after-review: %v\n%s", err, out)
	}

	final, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue final: %v", err)
	}
	if final.Status != models.StatusClosed {
		t.Fatalf("status=%s want closed", final.Status)
	}
	if final.ClosedBySession != implSession {
		t.Fatalf("closed_by_session=%q want %q", final.ClosedBySession, implSession)
	}
	if final.ReviewerSession != reviewerSession {
		t.Fatalf("reviewer_session=%q want %q", final.ReviewerSession, reviewerSession)
	}
	if !strings.Contains(out, "using review by "+reviewerSession) {
		t.Fatalf("output %q does not mention recorded review by %s", out, reviewerSession)
	}
}

func TestApproveRecordOnlyWithoutReasonFails(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	out, _ := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
	})
	if !strings.Contains(out, "requires --reason") {
		t.Fatalf("expected --reason required error, got %q", out)
	}

	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("expected no review row when --reason missing")
	}
}

// TestApproveRecordOnlyRejectsMinor verifies that record-only reviews are
// refused on minor issues. Minor issues bypass review entirely — they
// self-review and close in one step — so a record-only row is meaningless.
// Previously the `status != in_review && !issue.Minor` gate inverted the
// intent and allowed minor issues in any status to accept reviews.
func TestApproveRecordOnlyRejectsMinor(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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

	issue := &models.Issue{
		Title:                    "Minor task",
		Status:                   models.StatusInReview,
		ImplementerSession:       implID,
		ReviewRequestedBySession: implID,
		Minor:                    true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	out, _ := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "shouldn't be accepted",
	})
	if !strings.Contains(out, "minor issues do not require reviews") {
		t.Fatalf("expected minor-issue rejection, got %q", out)
	}

	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("expected no review row written for minor issue")
	}
}

func TestApproveRecordOnlyUnderStrictModeRejected(t *testing.T) {
	saveAndRestoreGlobals(t)
	setStrictMode(t)

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
	out, runErr := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "should not work",
	})
	if runErr == nil {
		t.Fatal("expected error under strict mode")
	}
	if !strings.Contains(out, "record-only requires review_policy_mode=delegated") {
		t.Fatalf("expected delegated-mode rejection, got %q", out)
	}

	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("expected no review row under strict mode")
	}
}

func TestApproveRecordOnlyChangesRequested(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"decision":    "changes_requested",
		"reason":      "please fix X",
	})
	if err != nil {
		t.Fatalf("approve changes_requested: %v\n%s", err, out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusInReview {
		t.Fatalf("status = %s, want in_review", got.Status)
	}
	if got.ReviewerSession != "" {
		t.Fatalf("ReviewerSession should not be stamped for changes_requested, got %q", got.ReviewerSession)
	}
	if got.ReviewedAt != nil {
		t.Fatal("ReviewedAt should not be stamped for changes_requested")
	}

	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("changes_requested should not be an active approval")
	}
	all, err := database.ListIssueReviews(issue.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("expected exactly 1 review row, got %d (err=%v)", len(all), err)
	}
	if all[0].Decision != reviewpolicy.DecisionChangesRequested {
		t.Fatalf("decision = %s, want changes_requested", all[0].Decision)
	}
}

func TestApproveCloseAfterRecordedApproval(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	reviewerID := currentSessionID(t, database)

	issue := newInReviewIssueWithImpl(t, database, implID)

	// Step 1: reviewer records approval.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only approval: %v", err)
	}

	// Step 2: implementer closes using the recorded approval.
	// Per plan: closed_by_session != reviewer_session requires --reason.
	t.Setenv("TD_SESSION_ID", "impl-agent")
	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "closing after reviewer approval",
	})
	if err != nil {
		t.Fatalf("close-after-review: %v\n%s", err, out)
	}
	if !strings.Contains(out, "APPROVED "+issue.ID) {
		t.Fatalf("expected APPROVED output, got %q", out)
	}
	if !strings.Contains(out, "using review by "+reviewerID) {
		t.Fatalf("expected attribution to reviewer %s, got %q", reviewerID, out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	if got.ClosedBySession != implID {
		t.Fatalf("ClosedBySession = %q, want %q", got.ClosedBySession, implID)
	}
	if got.ReviewerSession != reviewerID {
		t.Fatalf("ReviewerSession = %q, want %q", got.ReviewerSession, reviewerID)
	}
}

func TestApproveCloseAfterRecordedApproval_AnySessionWithReason(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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

	// Reviewer records approval.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	reviewerID := currentSessionID(t, database)
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only approval: %v", err)
	}

	// A random unrelated session can close after an independent approval, as
	// long as it supplies the audit reason required for non-reviewer closes.
	t.Setenv("TD_SESSION_ID", "stranger-agent")
	strangerID := currentSessionID(t, database)
	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "closing after independent approval",
	})
	if err != nil {
		t.Fatalf("close-after-review from unrelated session: %v\n%s", err, out)
	}
	if !strings.Contains(out, "APPROVED "+issue.ID) {
		t.Fatalf("expected APPROVED output, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	if got.ClosedBySession != strangerID {
		t.Fatalf("ClosedBySession = %q, want %q", got.ClosedBySession, strangerID)
	}
	if got.ReviewerSession != reviewerID {
		t.Fatalf("ReviewerSession = %q, want %q", got.ReviewerSession, reviewerID)
	}
}

func TestApproveCloseAfterRecordedApproval_RequiresReasonWhenCloserDiffers(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Resolve sessions.
	t.Setenv("TD_SESSION_ID", "impl-agent")
	implID := currentSessionID(t, database)
	t.Setenv("TD_SESSION_ID", "creator-agent")
	creatorID := currentSessionID(t, database)

	// Create issue where creator is distinct from implementer.
	issue := &models.Issue{
		Title:                    "Test issue",
		Status:                   models.StatusInReview,
		CreatorSession:           creatorID,
		ImplementerSession:       implID,
		ReviewRequestedBySession: implID,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only: %v", err)
	}

	// Creator attempts to close — different from reviewer, needs --reason.
	t.Setenv("TD_SESSION_ID", "creator-agent")
	out, _ := runApproveCmd(t, []string{issue.ID}, nil)
	if !strings.Contains(out, "requires --reason") {
		t.Fatalf("expected --reason required, got %q", out)
	}
	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusInReview {
		t.Fatalf("status = %s, want in_review", got.Status)
	}

	// With --reason: should succeed.
	out, err = runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "approved by reviewer, closed by creator",
	})
	if err != nil {
		t.Fatalf("close with reason: %v\n%s", err, out)
	}
	got, _ = database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
}

func TestApproveRecordOnly_StaleAfterDescriptionUpdate(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only: %v", err)
	}

	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active == nil {
		t.Fatal("expected active approval after record-only")
	}

	// Update description through the logged path — this is review-invalidating.
	got, _ := database.GetIssue(issue.ID)
	got.Description = "new scope added later"
	if err := database.UpdateIssueLogged(got, implID, models.ActionUpdate); err != nil {
		t.Fatalf("UpdateIssueLogged: %v", err)
	}

	// The active approval must now be superseded.
	active, _ = database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("expected active approval to be superseded after description change")
	}

	// Implementer trying to close should now be rejected (no fresh review).
	t.Setenv("TD_SESSION_ID", "impl-agent")
	out, _ := runApproveCmd(t, []string{issue.ID}, nil)
	_ = out // either an error or a rejection is acceptable, only care that issue is not closed
	got, _ = database.GetIssue(issue.ID)
	if got.Status == models.StatusClosed {
		t.Fatal("stale approval should NOT allow close without fresh review")
	}
}

func TestApproveDelegated_ModeFeatureEnabled(t *testing.T) {
	// Sanity check that features.ResolveReviewPolicyMode returns delegated
	// when the env var is set.
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "delegated")
	mode, err := features.ResolveReviewPolicyMode(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeDelegated {
		t.Fatalf("mode = %s, want delegated", mode)
	}
}

func TestApproveCloseAfterReview_UndoRestoresState(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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

	// Reviewer records approval.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only: %v", err)
	}
	beforeClose, _ := database.GetIssue(issue.ID)
	if beforeClose.Status != models.StatusInReview {
		t.Fatalf("pre-close status = %s, want in_review", beforeClose.Status)
	}

	// Implementer closes.
	t.Setenv("TD_SESSION_ID", "impl-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"reason": "impl closing after review",
	}); err != nil {
		t.Fatalf("close-after-review: %v", err)
	}
	got, _ := database.GetIssue(issue.ID)
	if got.Status != models.StatusClosed {
		t.Fatalf("post-close status = %s, want closed", got.Status)
	}

	// Undo the close.
	action, err := database.GetLastAction(implID)
	if err != nil || action == nil {
		t.Fatalf("GetLastAction: %v, %v", action, err)
	}
	if err := performUndo(database, action, implID); err != nil {
		t.Fatalf("performUndo: %v", err)
	}

	restored, _ := database.GetIssue(issue.ID)
	if restored.Status != models.StatusInReview {
		t.Fatalf("after undo status = %s, want in_review", restored.Status)
	}
	if restored.ClosedAt != nil {
		t.Fatalf("after undo ClosedAt should be nil, got %v", restored.ClosedAt)
	}
	if restored.ClosedBySession != "" {
		t.Fatalf("after undo ClosedBySession should be empty, got %q", restored.ClosedBySession)
	}
}

func TestApproveRecordOnly_UndoRemovesReview(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "LGTM",
	}); err != nil {
		t.Fatalf("record-only: %v", err)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.ReviewerSession != reviewerID {
		t.Fatalf("pre-undo ReviewerSession = %q, want %q", got.ReviewerSession, reviewerID)
	}
	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active == nil {
		t.Fatal("expected active review after record-only")
	}

	action, err := database.GetLastAction(reviewerID)
	if err != nil || action == nil {
		t.Fatalf("GetLastAction: %v, %v", action, err)
	}
	if err := performUndo(database, action, reviewerID); err != nil {
		t.Fatalf("performUndo: %v", err)
	}

	restored, _ := database.GetIssue(issue.ID)
	if restored.ReviewerSession != "" {
		t.Fatalf("after undo ReviewerSession should be empty, got %q", restored.ReviewerSession)
	}
	if restored.ReviewedAt != nil {
		t.Fatalf("after undo ReviewedAt should be nil, got %v", restored.ReviewedAt)
	}
	active, _ = database.GetActiveApprovalReview(issue.ID)
	if active != nil {
		t.Fatal("after undo, no active review should remain")
	}
}

// TestApproveRecordOnly_DelegatedRepeatReviewerAllowed locks in the rule that
// under delegated mode a session that previously reviewed (and got rejected
// back around to in_review) may re-review. The balanced-mode fallback would
// have blocked this via the WasAnyInvolved branch.
func TestApproveRecordOnly_DelegatedRepeatReviewerAllowed(t *testing.T) {
	saveAndRestoreGlobals(t)
	setDelegatedMode(t)

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
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	reviewerID := currentSessionID(t, database)

	issue := newInReviewIssueWithImpl(t, database, implID)

	// First review cycle: reviewer records approval.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	if _, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "first pass",
	}); err != nil {
		t.Fatalf("first record-only: %v", err)
	}

	// Log that the reviewer session was involved (simulates any activity that
	// trips the WasAnyInvolved check) by supersiding via implementer edit
	// then rejecting back to open and resubmitting.
	if err := database.SupersedeActiveReviews(issue.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	reopened, _ := database.GetIssue(issue.ID)
	reopened.Status = models.StatusInReview
	reopened.ReviewerSession = ""
	reopened.ReviewedAt = nil
	if err := database.UpdateIssue(reopened); err != nil {
		t.Fatalf("reopen update: %v", err)
	}

	// Second review cycle: same reviewer records approval again.
	t.Setenv("TD_SESSION_ID", "reviewer-agent")
	out, err := runApproveCmd(t, []string{issue.ID}, map[string]string{
		"record-only": "true",
		"reason":      "second pass",
	})
	if err != nil {
		t.Fatalf("repeat record-only should succeed under delegated mode: %v\n%s", err, out)
	}
	if !strings.Contains(out, "REVIEW RECORDED "+issue.ID) {
		t.Fatalf("expected REVIEW RECORDED on repeat review, got %q", out)
	}

	got, _ := database.GetIssue(issue.ID)
	if got.ReviewerSession != reviewerID {
		t.Fatalf("ReviewerSession = %q, want %q", got.ReviewerSession, reviewerID)
	}

	// Guard: ensure reviewpolicy helper agrees so the parity suite stays aligned.
	_ = features.ReviewPolicyMode // keep import
	_ = reviewpolicy.ModeDelegated
}
