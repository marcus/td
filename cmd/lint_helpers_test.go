package cmd

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/spf13/pflag"
)

func mustSetFocus(t *testing.T, dir, issueID string) {
	t.Helper()
	if err := config.SetFocus(dir, issueID); err != nil {
		t.Fatalf("SetFocus(%q): %v", issueID, err)
	}
}

func mustCreateIssue(t *testing.T, database *db.DB, issue *models.Issue) {
	t.Helper()
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue(%q): %v", issue.Title, err)
	}
}

func mustUpdateIssue(t *testing.T, database *db.DB, issue *models.Issue) {
	t.Helper()
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue(%q): %v", issue.ID, err)
	}
}

func mustDeleteIssue(t *testing.T, database *db.DB, issueID string) {
	t.Helper()
	if err := database.DeleteIssue(issueID); err != nil {
		t.Fatalf("DeleteIssue(%q): %v", issueID, err)
	}
}

func mustAddDependency(t *testing.T, database *db.DB, issueID, dependsOnID, relationType string) {
	t.Helper()
	if err := database.AddDependency(issueID, dependsOnID, relationType); err != nil {
		t.Fatalf("AddDependency(%q, %q): %v", issueID, dependsOnID, err)
	}
}

func mustAddHandoff(t *testing.T, database *db.DB, handoff *models.Handoff) {
	t.Helper()
	if err := database.AddHandoff(handoff); err != nil {
		t.Fatalf("AddHandoff(%q): %v", handoff.IssueID, err)
	}
}

func mustCreateWorkSession(t *testing.T, database *db.DB, ws *models.WorkSession) {
	t.Helper()
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession(%q): %v", ws.Name, err)
	}
}

func mustAddLog(t *testing.T, database *db.DB, log *models.Log) {
	t.Helper()
	if err := database.AddLog(log); err != nil {
		t.Fatalf("AddLog(%q): %v", log.IssueID, err)
	}
}

func mustRemoveDependency(t *testing.T, database *db.DB, issueID, dependsOnID string) {
	t.Helper()
	if err := database.RemoveDependency(issueID, dependsOnID); err != nil {
		t.Fatalf("RemoveDependency(%q, %q): %v", issueID, dependsOnID, err)
	}
}

func mustSetActiveWorkSession(t *testing.T, dir, wsID string) {
	t.Helper()
	if err := config.SetActiveWorkSession(dir, wsID); err != nil {
		t.Fatalf("SetActiveWorkSession(%q): %v", wsID, err)
	}
}

func mustClearActiveWorkSession(t *testing.T, dir string) {
	t.Helper()
	if err := config.ClearActiveWorkSession(dir); err != nil {
		t.Fatalf("ClearActiveWorkSession: %v", err)
	}
}

func mustTagIssueToWorkSession(t *testing.T, database *db.DB, wsID, issueID, sessionID string) {
	t.Helper()
	if err := database.TagIssueToWorkSession(wsID, issueID, sessionID); err != nil {
		t.Fatalf("TagIssueToWorkSession(%q, %q): %v", wsID, issueID, err)
	}
}

func mustUntagIssueFromWorkSession(t *testing.T, database *db.DB, wsID, issueID, sessionID string) {
	t.Helper()
	if err := database.UntagIssueFromWorkSession(wsID, issueID, sessionID); err != nil {
		t.Fatalf("UntagIssueFromWorkSession(%q, %q): %v", wsID, issueID, err)
	}
}

func mustSetFlag(t *testing.T, flags *pflag.FlagSet, name, value string) {
	t.Helper()
	if err := flags.Set(name, value); err != nil {
		t.Fatalf("Set(%q): %v", name, err)
	}
}

func mustSetFlagValue(t *testing.T, flag *pflag.Flag, value string) {
	t.Helper()
	if err := flag.Value.Set(value); err != nil {
		t.Fatalf("Set flag value for %q: %v", flag.Name, err)
	}
}
