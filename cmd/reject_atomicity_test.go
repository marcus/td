package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func TestReject_LaterIssueEventFailureDoesNotRejectIssueAndCanRetry(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_reject_atomicity_test")

	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title:  "Reject must be atomic",
		Status: models.StatusInReview,
		Minor:  true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	reviewID, err := database.CreateIssueReview(db.NewReview{
		IssueID:         issue.ID,
		ReviewerSession: "ses-independent-reviewer",
		Decision:        "approved",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec(`
		CREATE TRIGGER fail_issue_action
		BEFORE INSERT ON action_log
		WHEN NEW.entity_type = 'issue' AND NEW.action_type = 'reject'
		BEGIN
			SELECT RAISE(ABORT, 'injected issue_reviews action failure');
		END;
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	if err := rejectCmd.RunE(rejectCmd, []string{issue.ID}); err == nil {
		t.Fatal("reject succeeded despite injected review event failure")
	}
	unchanged, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != models.StatusInReview {
		t.Fatalf("issue status = %s, want in_review", unchanged.Status)
	}
	active, err := database.GetActiveApprovalReview(issue.ID)
	if err != nil || active == nil || active.ID != reviewID {
		t.Fatalf("active review changed: active=%+v err=%v", active, err)
	}
	if _, err := database.Conn().Exec(`DROP TRIGGER fail_issue_action`); err != nil {
		t.Fatal(err)
	}
	if err := rejectCmd.RunE(rejectCmd, []string{issue.ID}); err != nil {
		t.Fatalf("retry reject: %v", err)
	}
	rejected, _ := database.GetIssue(issue.ID)
	if rejected.Status != models.StatusOpen {
		t.Fatalf("retry status = %s, want open", rejected.Status)
	}
}
