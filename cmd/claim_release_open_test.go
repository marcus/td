package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// TestUpdateStatusOpenReleasesClaim pins the CLI half of td-cdbbbe:
// `td update <id> --status open` moved an in_progress issue back to open but
// left implementer_session set, producing an issue that is open AND held. The
// release now happens in the shared logged-write path (db.releaseClaimOnOpen),
// so this surface gets it without update.go knowing about claims at all.
func TestUpdateStatusOpenReleasesClaim(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Claimed work to unstart via update", Type: models.TypeTask}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	issue.Status = models.StatusInProgress
	issue.ImplementerSession = "ses_holder"
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}

	if err := updateCmd.Flags().Set("status", "open"); err != nil {
		t.Fatalf("set status flag: %v", err)
	}
	t.Cleanup(func() { _ = updateCmd.Flags().Set("status", "") })

	_ = captureStdout(t, func() {
		if err := updateCmd.RunE(updateCmd, []string{issue.ID}); err != nil {
			t.Fatalf("updateCmd.RunE failed: %v", err)
		}
	})

	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
	if got.ImplementerSession != "" {
		t.Fatalf("implementer = %q, want empty: open issue still holds a claim", got.ImplementerSession)
	}
}
