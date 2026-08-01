package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/models"
)

// This file covers the three list subcommands that advertised the global
// --json flag but printed a human table anyway (td-b97411): `task list`,
// `feature list` and `ws list`. It asserts the same two properties the
// td-0d752e / td-1e090e sweeps pinned for the rest of the family:
//
//  1. --json emits parseable output in the shape the family already uses — a
//     bare array, not a wrapper object.
//  2. A no-results run serializes as [], never null and never a human "no
//     results" line, so an agent can tell "supported, nothing found" from
//     "not supported".

func TestTaskListJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Task list json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := taskListCmd.RunE(taskListCmd, nil); err != nil {
			t.Fatalf("task list RunE: %v", err)
		}
	})

	// Bare issue array, interchangeable with `td list --json`.
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("task list --json = %+v, want the one task %s", issues, issue.ID)
	}
	if issues[0].Title != issue.Title || issues[0].Type != models.TypeTask {
		t.Fatalf("issue fields not carried through: %+v", issues[0])
	}
}

func TestTaskListJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := taskListCmd.RunE(taskListCmd, nil); err != nil {
			t.Fatalf("task list RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// TestTaskListHumanOutputUnchanged pins that the human path still prints the
// table (and its "No tasks found" line), not JSON, when --json is off.
func TestTaskListHumanOutputUnchanged(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Human task", models.StatusOpen, "")
	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := taskListCmd.RunE(taskListCmd, nil); err != nil {
			t.Fatalf("task list RunE: %v", err)
		}
	})
	if !strings.Contains(out, issue.ID) || !strings.Contains(out, "Human task") {
		t.Fatalf("human output missing the task: %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("human mode must not emit JSON: %q", out)
	}
}

func TestFeatureListJSONOutput(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := featureListCmd.RunE(featureListCmd, nil); err != nil {
			t.Fatalf("feature list RunE: %v", err)
		}
	})

	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("expected a JSON array, got: %q", out)
	}
	var entries []featureListEntry
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}

	// Every boolean flag in the registry appears, plus the string-valued
	// review_policy_mode row the human table also carries.
	all := features.ListAll()
	if len(entries) != len(all)+1 {
		t.Fatalf("feature list --json returned %d rows, want %d", len(entries), len(all)+1)
	}

	byName := make(map[string]featureListEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	for _, f := range all {
		e, ok := byName[f.Name]
		if !ok {
			t.Fatalf("feature %s missing from --json output", f.Name)
		}
		if e.Description != f.Description {
			t.Fatalf("feature %s description = %q, want %q", f.Name, e.Description, f.Description)
		}
		if e.Source == "" {
			t.Fatalf("feature %s has no source", f.Name)
		}
		if e.Enabled == nil {
			t.Fatalf("boolean feature %s must carry enabled", f.Name)
		}
		wantState := "off"
		if *e.Enabled {
			wantState = "on"
		}
		if e.State != wantState {
			t.Fatalf("feature %s state = %q, want %q for enabled=%v", f.Name, e.State, wantState, *e.Enabled)
		}
	}

	mode, ok := byName[features.ReviewPolicyMode]
	if !ok {
		t.Fatalf("%s missing from --json output", features.ReviewPolicyMode)
	}
	// The string-valued flag reports its mode as state and deliberately omits
	// enabled rather than reporting a misleading false.
	if mode.State == "" || mode.State == "on" || mode.State == "off" {
		t.Fatalf("%s state = %q, want a policy mode name", features.ReviewPolicyMode, mode.State)
	}
	if mode.Enabled != nil {
		t.Fatalf("%s must omit enabled, got %v", features.ReviewPolicyMode, *mode.Enabled)
	}
}

// TestFeatureListHumanOutputUnchanged pins the table header and a known row.
func TestFeatureListHumanOutputUnchanged(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := featureListCmd.RunE(featureListCmd, nil); err != nil {
			t.Fatalf("feature list RunE: %v", err)
		}
	})
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "STATE") ||
		!strings.Contains(out, "SOURCE") || !strings.Contains(out, "DESCRIPTION") {
		t.Fatalf("human output missing the table header: %q", out)
	}
	if !strings.Contains(out, features.ReviewPolicyMode) {
		t.Fatalf("human output missing %s: %q", features.ReviewPolicyMode, out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Fatalf("human mode must not emit JSON: %q", out)
	}
}

// decodeWSListEntries asserts out is a JSON array of work-session rows.
func decodeWSListEntries(t *testing.T, out string) []wsListEntry {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("expected a JSON array, got: %q", out)
	}
	var entries []wsListEntry
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	return entries
}

func TestWSListJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Tagged issue", models.StatusInProgress, "")

	ws := &models.WorkSession{Name: "json ws", SessionID: "ses_read_json_test"}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}
	if err := database.TagIssueToWorkSession(ws.ID, issue.ID, "ses_read_json_test"); err != nil {
		t.Fatalf("TagIssueToWorkSession failed: %v", err)
	}
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := wsListCmd.RunE(wsListCmd, nil); err != nil {
			t.Fatalf("ws list RunE: %v", err)
		}
	})

	entries := decodeWSListEntries(t, out)
	if len(entries) != 1 {
		t.Fatalf("ws list --json = %+v, want one session", entries)
	}
	got := entries[0]
	// The embedded models.WorkSession fields come through unchanged, so a
	// session here is field-for-field the one `ws current --json` returns.
	if got.ID != ws.ID || got.Name != "json ws" || got.SessionID != "ses_read_json_test" {
		t.Fatalf("work session fields not carried through: %+v", got)
	}
	if got.StartedAt.IsZero() {
		t.Fatalf("started_at not carried through: %+v", got)
	}
	if len(got.Issues) != 1 || got.Issues[0] != issue.ID {
		t.Fatalf("issues = %v, want [%s]", got.Issues, issue.ID)
	}
	// Not the active session for this scope, and not ended.
	if got.Status != "abandoned" {
		t.Fatalf("status = %q, want abandoned", got.Status)
	}
}

// TestWSListJSONEmptyIssuesIsArray pins that a session with no tagged issues
// reports "issues": [], not null.
func TestWSListJSONEmptyIssuesIsArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	ws := &models.WorkSession{Name: "empty ws", SessionID: "ses_read_json_test"}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := wsListCmd.RunE(wsListCmd, nil); err != nil {
			t.Fatalf("ws list RunE: %v", err)
		}
	})
	if strings.Contains(out, "null") {
		t.Fatalf("ws list --json must not emit null: %q", out)
	}
	entries := decodeWSListEntries(t, out)
	if len(entries) != 1 || entries[0].Issues == nil || len(entries[0].Issues) != 0 {
		t.Fatalf("issues must serialize as [], got: %q", out)
	}
}

func TestWSListJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := wsListCmd.RunE(wsListCmd, nil); err != nil {
			t.Fatalf("ws list RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// TestWSListHumanOutputUnchanged pins the human line format, including the
// bracketed status marker, and the empty "No work sessions" line.
func TestWSListHumanOutputUnchanged(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	ws := &models.WorkSession{Name: "human ws", SessionID: "ses_read_json_test"}
	if err := database.CreateWorkSession(ws); err != nil {
		t.Fatalf("CreateWorkSession failed: %v", err)
	}
	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := wsListCmd.RunE(wsListCmd, nil); err != nil {
			t.Fatalf("ws list RunE: %v", err)
		}
	})
	if !strings.Contains(out, ws.ID) || !strings.Contains(out, `"human ws"`) {
		t.Fatalf("human output missing the session: %q", out)
	}
	if !strings.Contains(out, "[abandoned]") {
		t.Fatalf("human output missing the bracketed status: %q", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[{") {
		t.Fatalf("human mode must not emit JSON: %q", out)
	}
}

func TestWSListHumanEmptyUnchanged(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, false)

	out := captureStdout(t, func() {
		if err := wsListCmd.RunE(wsListCmd, nil); err != nil {
			t.Fatalf("ws list RunE: %v", err)
		}
	})
	if !strings.Contains(out, "No work sessions") {
		t.Fatalf("expected the human empty line, got: %q", out)
	}
}
