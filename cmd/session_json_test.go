package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// setupSessionJSONTest wires a temp db + fixed session id so the session-family
// --json tests are deterministic.
func setupSessionJSONTest(t *testing.T) (*db.DB, string) {
	t.Helper()
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_session_json_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	return database, sess.ID
}

// TestWhoamiJSONOutput asserts the whoami envelope carries the session id, an
// ISO-8601 start time, and the issues this session touched.
func TestWhoamiJSONOutput(t *testing.T) {
	database, sessionID := setupSessionJSONTest(t)

	id := newJSONTestIssue(t, database, "Whoami json smoke task")
	if err := database.AddLog(&models.Log{
		IssueID:   id,
		SessionID: sessionID,
		Message:   "Started work",
		Type:      models.LogTypeProgress,
	}); err != nil {
		t.Fatalf("AddLog failed: %v", err)
	}

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})

	var env struct {
		Action        string   `json:"action"`
		Session       string   `json:"session"`
		Started       string   `json:"started"`
		IssuesTouched []string `json:"issues_touched"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "whoami" {
		t.Fatalf("action = %q, want whoami", env.Action)
	}
	if env.Session != sessionID {
		t.Fatalf("session = %q, want %q", env.Session, sessionID)
	}
	if _, err := time.Parse(time.RFC3339, env.Started); err != nil {
		t.Fatalf("started %q is not RFC3339: %v", env.Started, err)
	}
	if len(env.IssuesTouched) != 1 || env.IssuesTouched[0] != id {
		t.Fatalf("issues_touched = %v, want [%s]", env.IssuesTouched, id)
	}
	if strings.Contains(out, "SESSION:") {
		t.Fatalf("json output must not contain the human SESSION line: %q", out)
	}
}

// TestWhoamiJSONEmptyIssuesIsArray guards the "no results" case: an untouched
// session must still emit issues_touched as [], never null or an omitted key.
func TestWhoamiJSONEmptyIssuesIsArray(t *testing.T) {
	setupSessionJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})

	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	raw, ok := env["issues_touched"]
	if !ok {
		t.Fatalf("issues_touched key missing from %q", out)
	}
	if strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("issues_touched = %s, want []", raw)
	}
}

// TestSessionListJSONOutput asserts the session list envelope shape, including
// an ISO-8601 last_activity (not the human "17m0s") and a numeric age_seconds.
func TestSessionListJSONOutput(t *testing.T) {
	database, sessionID := setupSessionJSONTest(t)

	idle := time.Now().Add(-90 * time.Minute)
	if err := database.UpdateSessionActivity(sessionID, idle); err != nil {
		t.Fatalf("UpdateSessionActivity failed: %v", err)
	}

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := sessionListCmd.RunE(sessionListCmd, nil); err != nil {
			t.Fatalf("sessionListCmd.RunE failed: %v", err)
		}
	})

	var rows []struct {
		Branch       string `json:"branch"`
		Agent        string `json:"agent"`
		Session      string `json:"session"`
		LastActivity string `json:"last_activity"`
		AgeSeconds   int64  `json:"age_seconds"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not a valid JSON array: %v\noutput: %q", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 session row, got %d: %q", len(rows), out)
	}
	row := rows[0]
	if row.Session != sessionID {
		t.Fatalf("session = %q, want %q", row.Session, sessionID)
	}
	parsed, err := time.Parse(time.RFC3339, row.LastActivity)
	if err != nil {
		t.Fatalf("last_activity %q is not RFC3339: %v", row.LastActivity, err)
	}
	if diff := parsed.Sub(idle); diff > time.Second || diff < -time.Second {
		t.Fatalf("last_activity = %v, want ~%v", parsed, idle)
	}
	if row.AgeSeconds < 5300 || row.AgeSeconds > 5700 {
		t.Fatalf("age_seconds = %d, want ~5400", row.AgeSeconds)
	}
	if strings.Contains(out, "BRANCH") {
		t.Fatalf("json output must not contain the human table header: %q", out)
	}
}

// TestSessionCleanupJSONPreviewAndForce asserts cleanup reports what would be
// deleted in preview mode and what was deleted under --force, and that preview
// deletes nothing.
func TestSessionCleanupJSONPreviewAndForce(t *testing.T) {
	database, sessionID := setupSessionJSONTest(t)

	stale := &db.SessionRow{
		ID:           "ses_stale_cleanup",
		Branch:       "main",
		AgentType:    "claude-code",
		StartedAt:    time.Now().Add(-30 * 24 * time.Hour),
		LastActivity: time.Now().Add(-30 * 24 * time.Hour),
	}
	if err := database.UpsertSession(stale); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	setJSONFlag(t, true)
	setWorkflowExitFlag(t, sessionCleanupCmd, "older-than", "7d")
	setWorkflowExitFlag(t, sessionCleanupCmd, "force", "false")

	type envelope struct {
		Action    string `json:"action"`
		OlderThan string `json:"older_than"`
		Forced    bool   `json:"forced"`
		Count     int    `json:"count"`
		Sessions  []struct {
			Session      string `json:"session"`
			Branch       string `json:"branch"`
			LastActivity string `json:"last_activity"`
			AgeSeconds   int64  `json:"age_seconds"`
		} `json:"sessions"`
	}

	previewOut := captureStdout(t, func() {
		if err := sessionCleanupCmd.RunE(sessionCleanupCmd, nil); err != nil {
			t.Fatalf("preview failed: %v", err)
		}
	})
	var preview envelope
	if err := json.Unmarshal([]byte(previewOut), &preview); err != nil {
		t.Fatalf("preview output is not valid JSON: %v\noutput: %q", err, previewOut)
	}
	if preview.Action != "would_cleanup_sessions" || preview.Forced {
		t.Fatalf("unexpected preview envelope: %q", previewOut)
	}
	if preview.Count != 1 || len(preview.Sessions) != 1 || preview.Sessions[0].Session != stale.ID {
		t.Fatalf("preview should name the one stale session, got %q", previewOut)
	}
	if _, err := time.Parse(time.RFC3339, preview.Sessions[0].LastActivity); err != nil {
		t.Fatalf("last_activity %q is not RFC3339: %v", preview.Sessions[0].LastActivity, err)
	}
	if sessions, err := session.ListSessions(database); err != nil || len(sessions) != 2 {
		t.Fatalf("preview must not delete anything: sessions=%d err=%v", len(sessions), err)
	}

	setWorkflowExitFlag(t, sessionCleanupCmd, "force", "true")
	forceOut := captureStdout(t, func() {
		if err := sessionCleanupCmd.RunE(sessionCleanupCmd, nil); err != nil {
			t.Fatalf("force failed: %v", err)
		}
	})
	var forced envelope
	if err := json.Unmarshal([]byte(forceOut), &forced); err != nil {
		t.Fatalf("force output is not valid JSON: %v\noutput: %q", err, forceOut)
	}
	if forced.Action != "cleaned_up_sessions" || !forced.Forced || forced.Count != 1 {
		t.Fatalf("unexpected force envelope: %q", forceOut)
	}

	remaining, err := session.ListSessions(database)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != sessionID {
		t.Fatalf("only the live session should remain, got %+v", remaining)
	}
}

// TestSessionCleanupJSONNoMatches asserts an empty result is still a parseable
// envelope with count 0 rather than a human "No sessions older than" line.
func TestSessionCleanupJSONNoMatches(t *testing.T) {
	setupSessionJSONTest(t)

	setJSONFlag(t, true)
	setWorkflowExitFlag(t, sessionCleanupCmd, "older-than", "7d")
	setWorkflowExitFlag(t, sessionCleanupCmd, "force", "false")

	out := captureStdout(t, func() {
		if err := sessionCleanupCmd.RunE(sessionCleanupCmd, nil); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}
	})

	var env struct {
		Action   string           `json:"action"`
		Count    int              `json:"count"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "would_cleanup_sessions" || env.Count != 0 || len(env.Sessions) != 0 {
		t.Fatalf("unexpected empty envelope: %q", out)
	}
	if strings.Contains(out, "No sessions older than") {
		t.Fatalf("json output must not contain the human empty line: %q", out)
	}
}

// TestSessionFamilyHumanOutputUnchanged guards the non-json path.
func TestSessionFamilyHumanOutputUnchanged(t *testing.T) {
	setupSessionJSONTest(t)
	setJSONFlag(t, false)

	whoamiOut := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})
	if !strings.HasPrefix(whoamiOut, "SESSION: ") || !strings.Contains(whoamiOut, "STARTED: ") {
		t.Fatalf("unexpected human whoami output: %q", whoamiOut)
	}

	listOut := captureStdout(t, func() {
		if err := sessionListCmd.RunE(sessionListCmd, nil); err != nil {
			t.Fatalf("sessionListCmd.RunE failed: %v", err)
		}
	})
	if !strings.Contains(listOut, "BRANCH") || !strings.Contains(listOut, "LAST ACTIVITY") {
		t.Fatalf("unexpected human session list output: %q", listOut)
	}
}

// TestSessionListMarksTheCurrentSession: `current` was computed by comparing
// the stored agent_type against the bare agent TYPE, but createSession stores
// the full fingerprint ("claude-code_94806"). The result was inverted — false
// for the real current session, and true for any row that happened to store a
// bare type. The `*` marker in human output had the same bug.
func TestSessionListMarksTheCurrentSession(t *testing.T) {
	database, sessionID := setupSessionJSONTest(t)

	// A fabricated row that the old bare-type comparison would have matched.
	impostor := &db.SessionRow{
		ID:           "ses_impostor",
		Branch:       session.GetCurrentBranch(),
		AgentType:    "explicit",
		StartedAt:    time.Now().Add(-time.Hour),
		LastActivity: time.Now().Add(-time.Hour),
	}
	if err := database.UpsertSession(impostor); err != nil {
		t.Fatalf("UpsertSession failed: %v", err)
	}

	setJSONFlag(t, true)
	out := captureStdout(t, func() {
		if err := sessionListCmd.RunE(sessionListCmd, nil); err != nil {
			t.Fatalf("sessionListCmd.RunE failed: %v", err)
		}
	})
	var rows []struct {
		Session string `json:"session"`
		Current bool   `json:"current"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not a valid JSON array: %v\noutput: %q", err, out)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Session] = row.Current
	}
	if !seen[sessionID] {
		t.Fatalf("the real current session %s is not marked current: %q", sessionID, out)
	}
	if seen["ses_impostor"] {
		t.Fatalf("an unrelated row was marked current: %q", out)
	}

	setJSONFlag(t, false)
	human := captureStdout(t, func() {
		if err := sessionListCmd.RunE(sessionListCmd, nil); err != nil {
			t.Fatalf("sessionListCmd.RunE failed: %v", err)
		}
	})
	for _, line := range strings.Split(human, "\n") {
		if strings.Contains(line, sessionID) && !strings.HasPrefix(line, "*") {
			t.Fatalf("current session line is not marked with *: %q", line)
		}
		if strings.Contains(line, "ses_impostor") && strings.HasPrefix(line, "*") {
			t.Fatalf("impostor line is marked current: %q", line)
		}
	}
}

// TestWhoamiCarriesBranchAndAgent: without them a JSON caller cannot correlate
// itself against a row of `td session list` at all.
func TestWhoamiCarriesBranchAndAgent(t *testing.T) {
	setupSessionJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})
	var env struct {
		Branch string `json:"branch"`
		Agent  string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Branch == "" || env.Agent == "" {
		t.Fatalf("whoami must report branch and agent: %q", out)
	}

	listOut := captureStdout(t, func() {
		if err := sessionListCmd.RunE(sessionListCmd, nil); err != nil {
			t.Fatalf("sessionListCmd.RunE failed: %v", err)
		}
	})
	var rows []struct {
		Branch  string `json:"branch"`
		Agent   string `json:"agent"`
		Current bool   `json:"current"`
	}
	if err := json.Unmarshal([]byte(listOut), &rows); err != nil {
		t.Fatalf("session list output is not valid JSON: %v", err)
	}
	var matched bool
	for _, row := range rows {
		if row.Current && row.Branch == env.Branch && row.Agent == env.Agent {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("whoami's branch/agent do not correlate with session list: %q vs %q", out, listOut)
	}
}

// TestWhoamiHumanStartedIsUTC: the human line formatted LOCAL time with a
// hardcoded literal Z, so it read as UTC and was off by the local offset —
// sitting next to a correct JSON value in the same command.
func TestWhoamiHumanStartedIsUTC(t *testing.T) {
	setupSessionJSONTest(t)

	setJSONFlag(t, true)
	jsonOut := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})
	var env struct {
		Started string `json:"started"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	setJSONFlag(t, false)
	human := captureStdout(t, func() {
		if err := whoamiCmd.RunE(whoamiCmd, nil); err != nil {
			t.Fatalf("whoamiCmd.RunE failed: %v", err)
		}
	})
	var started string
	for _, line := range strings.Split(human, "\n") {
		if strings.HasPrefix(line, "STARTED: ") {
			started = strings.TrimSpace(strings.TrimPrefix(line, "STARTED: "))
		}
	}
	if started != env.Started {
		t.Fatalf("human STARTED %q disagrees with JSON started %q", started, env.Started)
	}
	if _, err := time.Parse(time.RFC3339, started); err != nil {
		t.Fatalf("STARTED %q is not RFC3339: %v", started, err)
	}
}
