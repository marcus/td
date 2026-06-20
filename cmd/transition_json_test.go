package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// blockTestIssue creates an issue and forces it to the given status via a direct
// db update so transition tests can exercise reopen/unblock from a known state.
func setIssueStatus(t *testing.T, database *db.DB, id string, status models.Status) {
	t.Helper()
	issue, err := database.GetIssue(id)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	issue.Status = status
	if err := database.UpdateIssue(issue); err != nil {
		t.Fatalf("UpdateIssue failed: %v", err)
	}
}

// TestUnstartJSONOutput verifies `td unstart <id> --json` emits the now-open issue.
func TestUnstartJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Unstart json smoke task")
	setIssueStatus(t, database, id, models.StatusInProgress)

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := unstartCmd.RunE(unstartCmd, []string{id}); err != nil {
			t.Fatalf("unstartCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Action string `json:"action"`
		Issue  struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "unstarted" {
		t.Fatalf("expected action=unstarted, got %q", env.Action)
	}
	if env.ID != id || env.Issue.ID != id {
		t.Fatalf("id mismatch: env.ID=%q issue.ID=%q want %q", env.ID, env.Issue.ID, id)
	}
	if env.Issue.Status != string(models.StatusOpen) {
		t.Fatalf("expected issue status open, got %q", env.Issue.Status)
	}
	if strings.Contains(out, "UNSTARTED ") {
		t.Fatalf("json output should not contain human UNSTARTED line: %q", out)
	}
}

// TestUnstartHumanOutputUnchanged verifies human output without --json.
func TestUnstartHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Unstart human smoke task")
	setIssueStatus(t, database, id, models.StatusInProgress)

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := unstartCmd.RunE(unstartCmd, []string{id}); err != nil {
			t.Fatalf("unstartCmd.RunE failed: %v", err)
		}
	})

	if out != "UNSTARTED "+id+" → open\n" {
		t.Fatalf("expected exactly UNSTARTED line, got %q", out)
	}
}

// TestBlockJSONOutput verifies `td block <id> --reason r --json` emits the
// blocked issue with the reason.
func TestBlockJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Block json smoke task")

	setJSONFlag(t, true)
	if err := blockCmd.Flags().Set("reason", "waiting on dep"); err != nil {
		t.Fatalf("set reason flag: %v", err)
	}
	t.Cleanup(func() { _ = blockCmd.Flags().Set("reason", "") })

	out := captureStdout(t, func() {
		if err := blockCmd.RunE(blockCmd, []string{id}); err != nil {
			t.Fatalf("blockCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Action string `json:"action"`
		Reason string `json:"reason"`
		Issue  struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "blocked" {
		t.Fatalf("expected action=blocked, got %q", env.Action)
	}
	if env.ID != id || env.Issue.Status != string(models.StatusBlocked) {
		t.Fatalf("expected blocked issue %q, got %+v", id, env)
	}
	if env.Reason != "waiting on dep" {
		t.Fatalf("expected reason preserved, got %q", env.Reason)
	}
	if strings.Contains(out, "BLOCKED ") {
		t.Fatalf("json output should not contain human BLOCKED line: %q", out)
	}
}

// TestBlockHumanOutputUnchanged verifies human output without --json.
func TestBlockHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Block human smoke task")

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := blockCmd.RunE(blockCmd, []string{id}); err != nil {
			t.Fatalf("blockCmd.RunE failed: %v", err)
		}
	})

	if out != "BLOCKED "+id+"\n" {
		t.Fatalf("expected exactly %q, got %q", "BLOCKED "+id+"\n", out)
	}
}

// TestUnblockJSONOutput verifies `td unblock <id> --json` emits the reopened-open
// issue.
func TestUnblockJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Unblock json smoke task")
	setIssueStatus(t, database, id, models.StatusBlocked)

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := unblockCmd.RunE(unblockCmd, []string{id}); err != nil {
			t.Fatalf("unblockCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Issue  struct {
			Status string `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "unblocked" {
		t.Fatalf("expected action=unblocked, got %q", env.Action)
	}
	if env.ID != id || env.Issue.Status != string(models.StatusOpen) {
		t.Fatalf("expected unblocked open issue %q, got %+v", id, env)
	}
	if strings.Contains(out, "UNBLOCKED ") {
		t.Fatalf("json output should not contain human UNBLOCKED line: %q", out)
	}
}

// TestReopenJSONOutput verifies `td reopen <id> --json` emits the reopened issue.
func TestReopenJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Reopen json smoke task")
	setIssueStatus(t, database, id, models.StatusClosed)

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := reopenCmd.RunE(reopenCmd, []string{id}); err != nil {
			t.Fatalf("reopenCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Issue  struct {
			Status string `json:"status"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "reopened" {
		t.Fatalf("expected action=reopened, got %q", env.Action)
	}
	if env.ID != id || env.Issue.Status != string(models.StatusOpen) {
		t.Fatalf("expected reopened open issue %q, got %+v", id, env)
	}
	if strings.Contains(out, "REOPENED ") {
		t.Fatalf("json output should not contain human REOPENED line: %q", out)
	}
}

// TestDeferJSONOutput verifies `td defer <id> <date> --json` emits the deferred
// issue plus a defer_until field.
func TestDeferJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Defer json smoke task")

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := deferCmd.RunE(deferCmd, []string{id, "2026-12-31"}); err != nil {
			t.Fatalf("deferCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID         string `json:"id"`
		Action     string `json:"action"`
		DeferUntil string `json:"defer_until"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "deferred" {
		t.Fatalf("expected action=deferred, got %q", env.Action)
	}
	if env.ID != id {
		t.Fatalf("expected id=%q, got %q", id, env.ID)
	}
	if env.DeferUntil != "2026-12-31" {
		t.Fatalf("expected defer_until=2026-12-31, got %q", env.DeferUntil)
	}
	if strings.Contains(out, "DEFERRED ") {
		t.Fatalf("json output should not contain human DEFERRED line: %q", out)
	}
}

// TestDeferClearJSONOutput verifies `td defer <id> --clear --json` emits the
// deferral_cleared envelope.
func TestDeferClearJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Defer clear json smoke task")

	setJSONFlag(t, true)
	if err := deferCmd.Flags().Set("clear", "true"); err != nil {
		t.Fatalf("set clear flag: %v", err)
	}
	t.Cleanup(func() { _ = deferCmd.Flags().Set("clear", "false") })

	out := captureStdout(t, func() {
		if err := deferCmd.RunE(deferCmd, []string{id}); err != nil {
			t.Fatalf("deferCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "deferral_cleared" {
		t.Fatalf("expected action=deferral_cleared, got %q", env.Action)
	}
	if env.ID != id {
		t.Fatalf("expected id=%q, got %q", id, env.ID)
	}
	if strings.Contains(out, "DEFERRAL CLEARED") {
		t.Fatalf("json output should not contain human DEFERRAL CLEARED line: %q", out)
	}
}

// TestLinkDependsOnJSONOutput verifies `td link <id> --depends-on <id2> --json`
// emits a dependency_added relationship envelope.
func TestLinkDependsOnJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	from := newJSONTestIssue(t, database, "Link from json task")
	to := newJSONTestIssue(t, database, "Link to json task")

	setJSONFlag(t, true)
	if err := linkCmd.Flags().Set("depends-on", to); err != nil {
		t.Fatalf("set depends-on flag: %v", err)
	}
	t.Cleanup(func() { _ = linkCmd.Flags().Set("depends-on", "") })

	out := captureStdout(t, func() {
		if err := linkCmd.RunE(linkCmd, []string{from}); err != nil {
			t.Fatalf("linkCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		Action string `json:"action"`
		From   string `json:"from"`
		To     string `json:"to"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "dependency_added" {
		t.Fatalf("expected action=dependency_added, got %q", env.Action)
	}
	if env.From != from || env.To != to {
		t.Fatalf("expected from=%q to=%q, got from=%q to=%q", from, to, env.From, env.To)
	}
	if env.Type != "depends_on" {
		t.Fatalf("expected type=depends_on, got %q", env.Type)
	}
	if strings.Contains(out, "ADDED:") {
		t.Fatalf("json output should not contain human ADDED line: %q", out)
	}
}

// TestNoteAddJSONOutput verifies `td note add <title> --content c --json` emits a
// note_created envelope with the full note.
func TestNoteAddJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)
	if err := noteAddCmd.Flags().Set("content", "json note body"); err != nil {
		t.Fatalf("set content flag: %v", err)
	}
	t.Cleanup(func() { _ = noteAddCmd.Flags().Set("content", "") })

	out := captureStdout(t, func() {
		if err := noteAddCmd.RunE(noteAddCmd, []string{"JSON note title"}); err != nil {
			t.Fatalf("noteAddCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Note   struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Content string `json:"content"`
		} `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "note_created" {
		t.Fatalf("expected action=note_created, got %q", env.Action)
	}
	if env.ID == "" || env.Note.ID != env.ID {
		t.Fatalf("expected matching note id, got env.ID=%q note.ID=%q", env.ID, env.Note.ID)
	}
	if env.Note.Title != "JSON note title" || env.Note.Content != "json note body" {
		t.Fatalf("expected full note title/content, got %+v", env.Note)
	}
	if strings.Contains(out, "CREATED ") {
		t.Fatalf("json output should not contain human CREATED line: %q", out)
	}
}

// TestNoteEditJSONOutput verifies `td note edit <id> --title t --json` emits a
// note_updated envelope.
func TestNoteEditJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	note, err := database.CreateNote("Original title", "body")
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	setJSONFlag(t, true)
	if err := noteEditCmd.Flags().Set("title", "Edited title"); err != nil {
		t.Fatalf("set title flag: %v", err)
	}
	t.Cleanup(func() { _ = noteEditCmd.Flags().Set("title", "") })

	out := captureStdout(t, func() {
		if err := noteEditCmd.RunE(noteEditCmd, []string{note.ID}); err != nil {
			t.Fatalf("noteEditCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Note   struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "note_updated" {
		t.Fatalf("expected action=note_updated, got %q", env.Action)
	}
	if env.ID != note.ID || env.Note.Title != "Edited title" {
		t.Fatalf("expected updated note title, got %+v", env)
	}
	if strings.Contains(out, "UPDATED ") {
		t.Fatalf("json output should not contain human UPDATED line: %q", out)
	}
}

// TestNoteDeleteJSONOutput verifies `td note delete <id> --json` emits a
// note_deleted envelope.
func TestNoteDeleteJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	note, err := database.CreateNote("Delete me", "body")
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := noteDeleteCmd.RunE(noteDeleteCmd, []string{note.ID}); err != nil {
			t.Fatalf("noteDeleteCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "note_deleted" {
		t.Fatalf("expected action=note_deleted, got %q", env.Action)
	}
	if env.ID != note.ID {
		t.Fatalf("expected id=%q, got %q", note.ID, env.ID)
	}
	if strings.Contains(out, "DELETED ") {
		t.Fatalf("json output should not contain human DELETED line: %q", out)
	}
}

// TestNoteDeleteHumanOutputUnchanged verifies human output without --json.
func TestNoteDeleteHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	note, err := database.CreateNote("Delete me human", "body")
	if err != nil {
		t.Fatalf("CreateNote failed: %v", err)
	}

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := noteDeleteCmd.RunE(noteDeleteCmd, []string{note.ID}); err != nil {
			t.Fatalf("noteDeleteCmd.RunE failed: %v", err)
		}
	})

	if out != "DELETED "+note.ID+"\n" {
		t.Fatalf("expected exactly %q, got %q", "DELETED "+note.ID+"\n", out)
	}
}
