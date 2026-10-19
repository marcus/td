package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

func TestApproveCmdHasCommentFlag(t *testing.T) {
	f := approveCmd.Flags().Lookup("comment")
	if f == nil {
		t.Fatalf("expected approveCmd to have --comment flag")
	}
}

func TestApprovalReasonPrecedence(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	// Lowest precedence: --comment
	if err := cmd.Flags().Set("comment", "c"); err != nil {
		t.Fatalf("set comment: %v", err)
	}
	if got := approvalReason(cmd); got != "c" {
		t.Fatalf("comment only: got %q, want %q", got, "c")
	}

	// Middle precedence: --message overrides comment
	if err := cmd.Flags().Set("message", "m"); err != nil {
		t.Fatalf("set message: %v", err)
	}
	if got := approvalReason(cmd); got != "m" {
		t.Fatalf("message+comment: got %q, want %q", got, "m")
	}

	// Highest precedence: --reason overrides message
	if err := cmd.Flags().Set("reason", "r"); err != nil {
		t.Fatalf("set reason: %v", err)
	}
	if got := approvalReason(cmd); got != "r" {
		t.Fatalf("reason+message+comment: got %q, want %q", got, "r")
	}
}

func TestApprovalReasonEmpty(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	if got := approvalReason(cmd); got != "" {
		t.Fatalf("empty: got %q, want empty", got)
	}
}

func TestApproveCmdHasNoteFlags(t *testing.T) {
	// Test --note flag exists
	if f := approveCmd.Flags().Lookup("note"); f == nil {
		t.Error("expected approveCmd to have --note flag")
	}

	// Test --notes flag exists
	if f := approveCmd.Flags().Lookup("notes"); f == nil {
		t.Error("expected approveCmd to have --notes flag")
	}
}

func TestApprovalReasonWithNoteFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("reason", "", "")
	cmd.Flags().String("message", "", "")
	cmd.Flags().String("comment", "", "")
	cmd.Flags().String("note", "", "")
	cmd.Flags().String("notes", "", "")

	// Test --note works
	if err := cmd.Flags().Set("note", "my note"); err != nil {
		t.Fatalf("set note: %v", err)
	}
	if got := approvalReason(cmd); got != "my note" {
		t.Fatalf("note only: got %q, want %q", got, "my note")
	}

	// Reset and test --notes
	_ = cmd.Flags().Set("note", "")
	if err := cmd.Flags().Set("notes", "my notes"); err != nil {
		t.Fatalf("set notes: %v", err)
	}
	if got := approvalReason(cmd); got != "my notes" {
		t.Fatalf("notes only: got %q, want %q", got, "my notes")
	}

	// --comment has lower priority than --note
	_ = cmd.Flags().Set("notes", "")
	_ = cmd.Flags().Set("comment", "c")
	_ = cmd.Flags().Set("note", "n")
	if got := approvalReason(cmd); got != "n" {
		t.Fatalf("note vs comment: got %q, want %q", got, "n")
	}
}

func TestApproveNoArgsUsesSingleReviewableIssue(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_cmd_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	issue := &models.Issue{
		Title:              "Single reviewable issue",
		Status:             models.StatusInReview,
		Minor:              true,
		ImplementerSession: sess.ID,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	var runErr error
	got := captureOutput(t, func() {
		runErr = approveCmd.RunE(approveCmd, []string{})
	})

	if runErr != nil {
		t.Fatalf("approveCmd.RunE returned error: %v", runErr)
	}

	if !strings.Contains(got, "APPROVED "+issue.ID) {
		t.Fatalf("expected approval output for %q, got %s", issue.ID, got)
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updated.Status != models.StatusClosed {
		t.Fatalf("status = %s, want %s", updated.Status, models.StatusClosed)
	}
	if updated.ReviewerSession != sess.ID {
		t.Fatalf("reviewer session = %q, want %q", updated.ReviewerSession, sess.ID)
	}
}

func TestApproveClosedIssueIsIdempotent(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_cmd_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{
		Title:              "Already closed review target",
		Status:             models.StatusClosed,
		ReviewerSession:    "ses_original_reviewer",
		ImplementerSession: "ses_impl",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	var runErr error
	got := captureOutput(t, func() {
		runErr = approveCmd.RunE(approveCmd, []string{issue.ID})
	})

	if runErr != nil {
		t.Fatalf("approveCmd.RunE returned error: %v", runErr)
	}

	if !strings.Contains(got, "already approved/closed") {
		t.Fatalf("expected idempotent approval output, got %s", got)
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updated.Status != models.StatusClosed {
		t.Fatalf("status = %s, want %s", updated.Status, models.StatusClosed)
	}
	if updated.ReviewerSession != "ses_original_reviewer" {
		t.Fatalf("reviewer session = %q, want %q", updated.ReviewerSession, "ses_original_reviewer")
	}
}

func TestApproveClosedIssueUsesLatestApprovalReasonContext(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_cmd_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer func() { _ = database.Close() }()

	issue := &models.Issue{
		Title:              "Recently approved issue",
		Status:             models.StatusInReview,
		ImplementerSession: "ses_impl",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
	if err := database.AddLog(&models.Log{
		IssueID:   issue.ID,
		SessionID: "ses_impl",
		Message:   "Submitted for review",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	if err := approveCmd.Flags().Set("reason", "ship it"); err != nil {
		t.Fatalf("set reason: %v", err)
	}

	var runErr error
	_ = captureOutput(t, func() {
		runErr = approveCmd.RunE(approveCmd, []string{issue.ID})
	})
	if runErr != nil {
		t.Fatalf("first approveCmd.RunE returned error: %v", runErr)
	}

	if err := approveCmd.Flags().Set("reason", ""); err != nil {
		t.Fatalf("clear reason: %v", err)
	}

	got := captureOutput(t, func() {
		runErr = approveCmd.RunE(approveCmd, []string{issue.ID})
	})
	if runErr != nil {
		t.Fatalf("second approveCmd.RunE returned error: %v", runErr)
	}

	if !strings.Contains(got, "Recent transition: approved") {
		t.Fatalf("expected latest approval transition in %s", got)
	}
	if strings.Contains(got, "Recent transition: submitted for review") {
		t.Fatalf("expected stale submitted-for-review context to be skipped in %s", got)
	}
}

func TestApproveInitialWrongStatusReportsWorkflowRefusal(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_CONTEXT_ID", "approve-initial-status-regression")
	for _, status := range []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked} {
		for _, jsonOut := range []bool{false, true} {
			dir := t.TempDir()
			baseDirOverride = &dir
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatal(err)
			}
			issue := &models.Issue{Title: "Not submitted for review", Status: status}
			if err := database.CreateIssue(issue); err != nil {
				t.Fatal(err)
			}
			flags := map[string]string{"self-review": "true", "reason": "reviewed diff"}
			if jsonOut {
				flags["json"] = "true"
			}
			out, runErr := runApproveCmd(t, []string{issue.ID}, flags)
			if runErr == nil {
				t.Fatalf("approve of %s succeeded: %s", status, out)
			}
			message := out
			if jsonOut {
				var envelope struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(out), &envelope); err != nil {
					t.Fatalf("invalid JSON %s: %v", out, err)
				}
				message = envelope.Error.Message
			}
			if !strings.Contains(message, "status is "+string(status)+", not in_review") || !strings.Contains(message, "td review "+issue.ID) {
				t.Fatalf("missing actual status or guidance: %s", out)
			}
			if strings.Contains(message, "another session") {
				t.Fatalf("invented concurrent writer: %s", out)
			}
			got, err := database.GetIssue(issue.ID)
			if err != nil || got.Status != status {
				t.Fatalf("changed refused issue: %v, %v", got, err)
			}
			reviews, err := database.ListIssueReviews(issue.ID)
			if err != nil || len(reviews) != 0 {
				t.Fatalf("review written for refused issue: %v, %v", reviews, err)
			}
			_ = database.Close()
		}
	}
}

func TestApproveWrongStatusDoesNotPreventOtherIDs(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_CONTEXT_ID", "approve-mixed-status-regression")
	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	wrong := &models.Issue{Title: "Still implementing", Status: models.StatusInProgress}
	ready := &models.Issue{Title: "Ready for approval", Status: models.StatusInReview}
	for _, issue := range []*models.Issue{wrong, ready} {
		if err := database.CreateIssue(issue); err != nil {
			t.Fatal(err)
		}
	}
	out, runErr := runApproveCmd(t, []string{wrong.ID, ready.ID}, nil)
	if runErr != nil {
		t.Fatalf("mixed batch with a successful approval should retain its success exit: %v", runErr)
	}
	if !strings.Contains(out, "status is in_progress, not in_review") || !strings.Contains(out, "APPROVED "+ready.ID) {
		t.Fatalf("mixed results missing: %s", out)
	}
	if got, err := database.GetIssue(ready.ID); err != nil || got.Status != models.StatusClosed {
		t.Fatalf("later ready issue was not approved: %v, %v", got, err)
	}
}
