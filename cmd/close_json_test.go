package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// TestCloseJSONOutputEmitsClosedIssue verifies that `td close <id> --json`
// prints a JSON envelope with action=closed, the closed id/status, and the
// full issue object, and that the human "CLOSED" line is suppressed.
func TestCloseJSONOutputEmitsClosedIssue(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_close_json_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	if _, err := session.GetOrCreate(database); err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// An open, minor issue with no implementation history is closeable by any
	// session via the admin close path.
	issue := &models.Issue{
		Title:  "JSON close smoke task",
		Status: models.StatusOpen,
		Minor:  true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := closeCmd.RunE(closeCmd, []string{issue.ID}); err != nil {
			t.Fatalf("closeCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Action  string `json:"action"`
		Session string `json:"session"`
		Issue   struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Title  string `json:"title"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}

	if env.Action != "closed" {
		t.Fatalf("expected action=closed, got %q (output: %q)", env.Action, out)
	}
	if env.ID != issue.ID {
		t.Fatalf("expected id=%q, got %q", issue.ID, env.ID)
	}
	if env.Status != string(models.StatusClosed) {
		t.Fatalf("expected status=closed, got %q", env.Status)
	}
	if env.Issue.ID != issue.ID {
		t.Fatalf("envelope id %q does not match issue.id %q", env.ID, env.Issue.ID)
	}
	if env.Issue.Status != string(models.StatusClosed) {
		t.Fatalf("expected issue.status=closed, got %q", env.Issue.Status)
	}
	if env.Issue.Title != "JSON close smoke task" {
		t.Fatalf("expected full issue with title, got %q", env.Issue.Title)
	}

	// Human output must NOT appear in json mode.
	if strings.Contains(out, "CLOSED ") {
		t.Fatalf("json output should not contain human CLOSED line: %q", out)
	}

	// The issue must actually be closed in the database.
	updated, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if updated.Status != models.StatusClosed {
		t.Fatalf("expected issue to be closed in db, got %q", updated.Status)
	}
}

// TestCloseHumanOutputUnchanged verifies that without --json the close command
// still prints exactly "CLOSED td-..." and no JSON.
func TestCloseHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_close_human_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	if _, err := session.GetOrCreate(database); err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	issue := &models.Issue{
		Title:  "Human close smoke task",
		Status: models.StatusOpen,
		Minor:  true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := closeCmd.RunE(closeCmd, []string{issue.ID}); err != nil {
			t.Fatalf("closeCmd.RunE failed: %v", err)
		}
	})

	if !strings.Contains(out, "CLOSED "+issue.ID) {
		t.Fatalf("expected human output to contain 'CLOSED %s', got %q", issue.ID, out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("human output should not contain JSON, got %q", out)
	}
}

// TestCloseJSONErrorOnNotFound verifies that close --json emits a structured
// JSON error (rather than the human warning) when the issue does not exist.
func TestCloseJSONErrorOnNotFound(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_close_json_err_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	if _, err := session.GetOrCreate(database); err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		// A missing id is a per-issue skip, so RunE returns nil but emits a
		// JSON error line for the id.
		_ = closeCmd.RunE(closeCmd, []string{"td-doesnotexist"})
	})

	// Output must be a single JSON error envelope, not the human
	// "WARNING: issue not found" line.
	var errEnv struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &errEnv); err != nil {
		t.Fatalf("expected JSON error envelope, got %q (%v)", out, err)
	}
	if errEnv.Error.Code != "not_found" {
		t.Fatalf("expected not_found code, got %q", out)
	}
}

// TestRejectJSONShapeUnchanged is a regression guard: the reject --json output
// must keep its historical {id, status, action, session} keys byte-for-byte.
// This task adds close --json without touching the existing review-family shape.
func TestRejectJSONShapeUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_reject_json_shape_test")

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
		Title:  "Reject json shape task",
		Status: models.StatusInReview,
		Minor:  true,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	// reject reads its own local --json flag directly (it predates jsonMode),
	// so set the local flag rather than the inherited persistent one.
	if err := rejectCmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set reject json flag: %v", err)
	}
	t.Cleanup(func() { _ = rejectCmd.Flags().Set("json", "false") })

	out := captureStdout(t, func() {
		if err := rejectCmd.RunE(rejectCmd, []string{issue.ID}); err != nil {
			t.Fatalf("rejectCmd.RunE failed: %v", err)
		}
	})

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("reject output is not valid JSON: %v\noutput: %q", err, out)
	}

	// Exact key set: {id, status, action, session}. No accidental additions.
	wantKeys := map[string]bool{"id": true, "status": true, "action": true, "session": true}
	for k := range m {
		if !wantKeys[k] {
			t.Fatalf("unexpected key %q in reject json (shape must be unchanged): %q", k, out)
		}
	}
	for k := range wantKeys {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing expected key %q in reject json: %q", k, out)
		}
	}
	if m["action"] != "rejected" {
		t.Fatalf("expected action=rejected, got %v", m["action"])
	}
	if m["status"] != "open" {
		t.Fatalf("expected status=open, got %v", m["status"])
	}
	if m["session"] != sess.ID {
		t.Fatalf("expected session=%q, got %v", sess.ID, m["session"])
	}
}
