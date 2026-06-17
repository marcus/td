package db

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

// TestBareIssueID_FKWritesSucceed is a regression test for the FK 787 failure
// reported against v0.47.1: handoff / comment / git-snapshot / ws-tag inserts
// persisted the raw user-supplied issue id. When a bare id (no "td-" prefix)
// was passed, the stored issue_id did not match the issues(id) PK and the
// FOREIGN KEY constraint failed once migration 30 turned foreign_keys=ON.
//
// All these write paths must now normalize the id, so a bare id succeeds and
// the persisted issue_id is canonical (prefixed).
func TestBareIssueID_FKWritesSucceed(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Confirm FK enforcement is on, otherwise this test proves nothing.
	var fk int
	if err := database.Conn().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}

	issue := &models.Issue{Title: "Bare id FK regression issue"}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	full := issue.ID // e.g. td-abc123
	bare := strings.TrimPrefix(full, "td-")
	if bare == full {
		t.Fatalf("expected a td- prefixed id, got %q", full)
	}

	// Handoff with bare id.
	if err := database.AddHandoff(&models.Handoff{IssueID: bare, SessionID: "ses1"}); err != nil {
		t.Fatalf("AddHandoff(bare): %v", err)
	}
	// Comment with bare id.
	if err := database.AddComment(&models.Comment{IssueID: bare, SessionID: "ses1", Text: "hi"}); err != nil {
		t.Fatalf("AddComment(bare): %v", err)
	}
	// Git snapshot with bare id.
	if err := database.AddGitSnapshot(&models.GitSnapshot{IssueID: bare, Event: "handoff", CommitSHA: "abc", Branch: "main"}); err != nil {
		t.Fatalf("AddGitSnapshot(bare): %v", err)
	}

	// Work-session tag/untag with bare id.
	ws := &models.WorkSession{Name: "regress", SessionID: "ses1"}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession: %v", err)
	}
	if err := database.TagIssueToWorkSession(ws.ID, bare, "ses1"); err != nil {
		t.Fatalf("TagIssueToWorkSession(bare): %v", err)
	}
	// Untag with the bare id must resolve the same deterministic row id.
	if err := database.UntagIssueFromWorkSession(ws.ID, bare, "ses1"); err != nil {
		t.Fatalf("UntagIssueFromWorkSession(bare): %v", err)
	}
	var nWsi int
	if err := database.Conn().QueryRow(`SELECT COUNT(*) FROM work_session_issues WHERE work_session_id = ?`, ws.ID).Scan(&nWsi); err != nil {
		t.Fatal(err)
	}
	if nWsi != 0 {
		t.Errorf("expected untag to remove the row; got %d remaining", nWsi)
	}

	// Persisted issue_ids must all be canonical (prefixed).
	for _, q := range []string{
		`SELECT issue_id FROM handoffs`,
		`SELECT issue_id FROM comments`,
		`SELECT issue_id FROM git_snapshots`,
	} {
		var got string
		if err := database.Conn().QueryRow(q).Scan(&got); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if got != full {
			t.Errorf("%s persisted %q, want canonical %q", q, got, full)
		}
	}
}
