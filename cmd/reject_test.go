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
)

func runRejectCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()

	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_reject_cmd")

	baseDir := dir
	baseDirOverride = &baseDir

	_ = rejectCmd.Flags().Set("json", "false")
	_ = rejectCmd.Flags().Set("reason", "")
	_ = rejectCmd.Flags().Set("comment", "")
	_ = rejectCmd.Flags().Set("message", "")
	_ = rejectCmd.Flags().Set("note", "")
	_ = rejectCmd.Flags().Set("notes", "")

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := rejectCmd.RunE(rejectCmd, args)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	if runErr != nil {
		t.Fatalf("rejectCmd.RunE returned error: %v", runErr)
	}

	return output.String()
}

func TestRejectOpenIssueIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:              "Already reopened review target",
		Status:             models.StatusOpen,
		ImplementerSession: "ses_impl",
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected active session")
	}

	output := runRejectCommand(t, dir, issue.ID)
	if !strings.Contains(output, "already reopened") {
		t.Fatalf("expected idempotent reject output, got %s", output)
	}

	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updated.Status != models.StatusOpen {
		t.Fatalf("status = %s, want %s", updated.Status, models.StatusOpen)
	}
	if updated.ImplementerSession != "ses_impl" {
		t.Fatalf("implementer session = %q, want %q", updated.ImplementerSession, "ses_impl")
	}
}
