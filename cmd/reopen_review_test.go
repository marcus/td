package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestReopenSupersedesActiveApproval is the CLI journey: approve+close, then
// td reopen. The leftover approval must not survive into the next cycle.
func TestReopenSupersedesActiveApproval(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_reopen_review")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "closed with leftover approval",
		Type:   models.TypeTask,
		Status: models.StatusClosed,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issue.ID,
		ReviewerSession: "ses-reviewer",
		Decision:        "approved",
		Summary:         "lgtm",
	}); err != nil {
		t.Fatal(err)
	}
	active, _ := database.GetActiveApprovalReview(issue.ID)
	if active == nil {
		t.Fatal("expected an active approval before reopen")
	}

	setWorkflowExitFlag(t, reopenCmd, "reason", "")
	if err := reopenCmd.RunE(reopenCmd, []string{issue.ID}); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
	active, err = database.GetActiveApprovalReview(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("reopen left approval %s active", active.ID)
	}
}
