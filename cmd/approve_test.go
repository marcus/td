package cmd

import (
	"bytes"
	"io"
	"os"
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
	defer database.Close()

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

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := approveCmd.RunE(approveCmd, []string{})

	w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	if runErr != nil {
		t.Fatalf("approveCmd.RunE returned error: %v", runErr)
	}

	got := output.String()
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
