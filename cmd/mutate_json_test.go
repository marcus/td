package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// newJSONTestIssue creates an issue directly in the db for use by the mutating
// json tests below and returns its id.
func newJSONTestIssue(t *testing.T, database *db.DB, title string) string {
	t.Helper()
	issue := &models.Issue{Title: title, Type: models.TypeTask}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	return issue.ID
}

// TestUpdateJSONOutput verifies `td update <id> --priority P1 --json` emits the
// updated full issue envelope.
func TestUpdateJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Update json smoke task")

	setJSONFlag(t, true)
	if err := updateCmd.Flags().Set("priority", "P1"); err != nil {
		t.Fatalf("set priority flag: %v", err)
	}
	t.Cleanup(func() { _ = updateCmd.Flags().Set("priority", "") })

	out := captureStdout(t, func() {
		if err := updateCmd.RunE(updateCmd, []string{id}); err != nil {
			t.Fatalf("updateCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Action string `json:"action"`
		Issue  struct {
			ID       string `json:"id"`
			Priority string `json:"priority"`
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "updated" {
		t.Fatalf("expected action=updated, got %q", env.Action)
	}
	if env.ID != id || env.Issue.ID != id {
		t.Fatalf("envelope/issue id mismatch: env.ID=%q issue.ID=%q want %q", env.ID, env.Issue.ID, id)
	}
	if env.Issue.Priority != "P1" {
		t.Fatalf("expected updated priority P1 in issue, got %q", env.Issue.Priority)
	}
	if strings.Contains(out, "UPDATED ") {
		t.Fatalf("json output should not contain human UPDATED line: %q", out)
	}
}

// TestUpdateHumanOutputUnchanged verifies human output without --json.
func TestUpdateHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Update human smoke task")

	setJSONFlag(t, false)
	if err := updateCmd.Flags().Set("priority", "P2"); err != nil {
		t.Fatalf("set priority flag: %v", err)
	}
	t.Cleanup(func() { _ = updateCmd.Flags().Set("priority", "") })

	out := captureStdout(t, func() {
		if err := updateCmd.RunE(updateCmd, []string{id}); err != nil {
			t.Fatalf("updateCmd.RunE failed: %v", err)
		}
	})

	if out != "UPDATED "+id+"\n" {
		t.Fatalf("expected exactly %q, got %q", "UPDATED "+id+"\n", out)
	}
}

// TestStartJSONOutput verifies `td start <id> --json` emits the started issue
// plus a session field.
func TestStartJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Start json smoke task")

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := startCmd.RunE(startCmd, []string{id}); err != nil {
			t.Fatalf("startCmd.RunE failed: %v", err)
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
		} `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "started" {
		t.Fatalf("expected action=started, got %q", env.Action)
	}
	if env.ID != id || env.Issue.ID != id {
		t.Fatalf("id mismatch: env.ID=%q issue.ID=%q want %q", env.ID, env.Issue.ID, id)
	}
	if env.Issue.Status != string(models.StatusInProgress) {
		t.Fatalf("expected issue status in_progress, got %q", env.Issue.Status)
	}
	if env.Session == "" {
		t.Fatalf("expected non-empty session in start envelope, got %q", out)
	}
	if strings.Contains(out, "STARTED ") {
		t.Fatalf("json output should not contain human STARTED line: %q", out)
	}
}

// TestStartBulkJSONOutputNDJSON verifies bulk start emits one JSON object per id
// (NDJSON) and no trailing human summary.
func TestStartBulkJSONOutputNDJSON(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id1 := newJSONTestIssue(t, database, "Bulk start one task")
	id2 := newJSONTestIssue(t, database, "Bulk start two task")

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := startCmd.RunE(startCmd, []string{id1, id2}); err != nil {
			t.Fatalf("startCmd.RunE failed: %v", err)
		}
	})

	dec := json.NewDecoder(strings.NewReader(out))
	var ids []string
	for dec.More() {
		var env struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := dec.Decode(&env); err != nil {
			t.Fatalf("decode NDJSON object failed: %v\noutput: %q", err, out)
		}
		if env.Action != "started" {
			t.Fatalf("expected action=started per object, got %q", env.Action)
		}
		ids = append(ids, env.ID)
	}
	if len(ids) != 2 || ids[0] != id1 || ids[1] != id2 {
		t.Fatalf("expected two NDJSON objects for %q,%q, got %v\noutput: %q", id1, id2, ids, out)
	}
	if strings.Contains(out, "Started ") || strings.Contains(out, "skipped") {
		t.Fatalf("json bulk output should not contain human summary line: %q", out)
	}
}

// TestLogJSONOutput verifies `td log <id> "msg" --json` emits the new log entry.
func TestLogJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Log json smoke task")

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := logCmd.RunE(logCmd, []string{id, "made some progress"}); err != nil {
			t.Fatalf("logCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		Log    struct {
			ID      string `json:"id"`
			IssueID string `json:"issue_id"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"log"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "logged" {
		t.Fatalf("expected action=logged, got %q", env.Action)
	}
	if env.ID != id {
		t.Fatalf("expected id=%q, got %q", id, env.ID)
	}
	if env.Log.ID == "" || env.Log.IssueID != id {
		t.Fatalf("expected full log with id and issue_id=%q, got %+v", id, env.Log)
	}
	if env.Log.Message != "made some progress" {
		t.Fatalf("expected log message preserved, got %q", env.Log.Message)
	}
	if strings.Contains(out, "LOGGED ") {
		t.Fatalf("json output should not contain human LOGGED line: %q", out)
	}
}

// TestLogHumanOutputUnchanged verifies human output without --json.
func TestLogHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Log human smoke task")

	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := logCmd.RunE(logCmd, []string{id, "human log line"}); err != nil {
			t.Fatalf("logCmd.RunE failed: %v", err)
		}
	})

	if out != "LOGGED "+id+"\n" {
		t.Fatalf("expected exactly %q, got %q", "LOGGED "+id+"\n", out)
	}
}

// TestHandoffJSONOutput verifies `td handoff <id> --done a --remaining b --json`
// emits the handoff envelope.
func TestHandoffJSONOutput(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Handoff json smoke task")

	setJSONFlag(t, true)
	if err := handoffCmd.Flags().Set("done", "did a"); err != nil {
		t.Fatalf("set done flag: %v", err)
	}
	if err := handoffCmd.Flags().Set("remaining", "do b"); err != nil {
		t.Fatalf("set remaining flag: %v", err)
	}
	t.Cleanup(func() {
		_ = handoffCmd.Flags().Set("done", "")
		_ = handoffCmd.Flags().Set("remaining", "")
	})

	out := captureStdout(t, func() {
		if err := handoffCmd.RunE(handoffCmd, []string{id}); err != nil {
			t.Fatalf("handoffCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		ID      string `json:"id"`
		Action  string `json:"action"`
		Handoff struct {
			ID        string   `json:"id"`
			IssueID   string   `json:"issue_id"`
			Done      []string `json:"done"`
			Remaining []string `json:"remaining"`
		} `json:"handoff"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "handoff_recorded" {
		t.Fatalf("expected action=handoff_recorded, got %q", env.Action)
	}
	if env.ID != id {
		t.Fatalf("expected id=%q, got %q", id, env.ID)
	}
	if env.Handoff.ID == "" || env.Handoff.IssueID != id {
		t.Fatalf("expected full handoff with id and issue_id=%q, got %+v", id, env.Handoff)
	}
	if len(env.Handoff.Done) != 1 || env.Handoff.Done[0] != "did a" {
		t.Fatalf("expected done=[did a], got %v", env.Handoff.Done)
	}
	if len(env.Handoff.Remaining) != 1 || env.Handoff.Remaining[0] != "do b" {
		t.Fatalf("expected remaining=[do b], got %v", env.Handoff.Remaining)
	}
	if strings.Contains(out, "HANDOFF RECORDED") || strings.Contains(out, "Next:") {
		t.Fatalf("json output should not contain human handoff lines: %q", out)
	}
}

// TestHandoffHumanOutputUnchanged verifies human output begins with the
// HANDOFF RECORDED line and contains no JSON.
func TestHandoffHumanOutputUnchanged(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	id := newJSONTestIssue(t, database, "Handoff human smoke task")

	setJSONFlag(t, false)
	if err := handoffCmd.Flags().Set("done", "x"); err != nil {
		t.Fatalf("set done flag: %v", err)
	}
	t.Cleanup(func() { _ = handoffCmd.Flags().Set("done", "") })

	out := captureStdout(t, func() {
		if err := handoffCmd.RunE(handoffCmd, []string{id}); err != nil {
			t.Fatalf("handoffCmd.RunE failed: %v", err)
		}
	})

	if !strings.HasPrefix(out, "HANDOFF RECORDED "+id) {
		t.Fatalf("expected human output to start with HANDOFF RECORDED %s, got %q", id, out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("human output should not contain JSON, got %q", out)
	}
}
