package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
)

// This file covers the read-shaped commands that advertised the global --json
// flag but printed human text anyway (td-0d752e). Two properties are asserted
// per command:
//
//  1. --json emits output json.Unmarshal accepts, in the shape the rest of the
//     family already uses (bare issue arrays for list shortcuts, matching
//     `td list --json`; a bare object for grouped reads, matching
//     `td status --json`; the mutation envelope for focus/unfocus).
//  2. An empty result serializes as [], never null and never a human "no
//     results" line — an agent must be able to tell "supported, nothing found"
//     from "not supported".

// setupReadJSONTest wires a temp db + fixed session id for the read-command
// --json tests.
func setupReadJSONTest(t *testing.T) (*db.DB, string) {
	t.Helper()
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_read_json_test")

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

// newReadJSONIssue creates an issue in the given status, optionally owned by a
// foreign implementer session.
func newReadJSONIssue(t *testing.T, database *db.DB, title string, status models.Status, implementer string) *models.Issue {
	t.Helper()
	issue := &models.Issue{
		Title:    title,
		Type:     models.TypeTask,
		Status:   status,
		Priority: models.PriorityP1,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if status != models.StatusOpen || implementer != "" {
		issue.Status = status
		issue.ImplementerSession = implementer
		if err := database.UpdateIssue(issue); err != nil {
			t.Fatalf("UpdateIssue failed: %v", err)
		}
	}
	return issue
}

// decodeIssueArray asserts out is a JSON array of issues and returns it. It
// checks the issue field names match models.Issue's tags, which is what makes
// an issue from `ready --json` interchangeable with one from `list --json`.
func decodeIssueArray(t *testing.T, out string) []models.Issue {
	t.Helper()
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("expected a JSON array, got: %q", out)
	}
	var issues []models.Issue
	if err := json.Unmarshal([]byte(trimmed), &issues); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	return issues
}

// requireEmptyJSONArray asserts the no-results case serialized as [], not null.
func requireEmptyJSONArray(t *testing.T, out string) {
	t.Helper()
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty result must serialize as [], got: %q", out)
	}
}

func TestReadyJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Ready json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := readyCmd.RunE(readyCmd, nil); err != nil {
			t.Fatalf("ready RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("ready --json = %+v, want the one open issue %s", issues, issue.ID)
	}
	if issues[0].Title != issue.Title || issues[0].Status != models.StatusOpen {
		t.Fatalf("issue fields not carried through: %+v", issues[0])
	}
}

func TestReadyJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := readyCmd.RunE(readyCmd, nil); err != nil {
			t.Fatalf("ready RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestBlockedJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Blocked json task", models.StatusBlocked, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := blockedListCmd.RunE(blockedListCmd, nil); err != nil {
			t.Fatalf("blocked RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("blocked --json = %+v, want %s", issues, issue.ID)
	}
	if issues[0].Status != models.StatusBlocked {
		t.Fatalf("status = %q, want blocked", issues[0].Status)
	}
}

func TestBlockedJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := blockedListCmd.RunE(blockedListCmd, nil); err != nil {
			t.Fatalf("blocked RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestInReviewJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "In review json task", models.StatusInReview, "ses_other_impl")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := inReviewCmd.RunE(inReviewCmd, nil); err != nil {
			t.Fatalf("in-review RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("in-review --json = %+v, want %s", issues, issue.ID)
	}
	if issues[0].ImplementerSession != "ses_other_impl" {
		t.Fatalf("implementer_session = %q, want ses_other_impl", issues[0].ImplementerSession)
	}
	if strings.Contains(out, "[reviewable]") {
		t.Fatalf("json output must not carry the human reviewable marker: %q", out)
	}
}

func TestInReviewJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := inReviewCmd.RunE(inReviewCmd, nil); err != nil {
			t.Fatalf("in-review RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

// reviewableEnvelope is the grouped-read shape: named buckets whose values are
// plain issue arrays, mirroring `td status --json`'s in_review section.
type reviewableEnvelope struct {
	Awaiting     []models.Issue `json:"awaiting"`
	ReadyToClose []models.Issue `json:"ready_to_close"`
}

func TestReviewableJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Reviewable json task", models.StatusInReview, "ses_other_impl")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := reviewableCmd.RunE(reviewableCmd, nil); err != nil {
			t.Fatalf("reviewable RunE: %v", err)
		}
	})

	var env reviewableEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if len(env.Awaiting) != 1 || env.Awaiting[0].ID != issue.ID {
		t.Fatalf("awaiting = %+v, want the one in_review issue %s", env.Awaiting, issue.ID)
	}
	if strings.Contains(out, "AWAITING YOUR REVIEW") {
		t.Fatalf("json output must not contain the human header: %q", out)
	}
}

func TestReviewableJSONEmptyBucketsAreArrays(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := reviewableCmd.RunE(reviewableCmd, nil); err != nil {
			t.Fatalf("reviewable RunE: %v", err)
		}
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	for _, key := range []string{"awaiting", "ready_to_close"} {
		val, ok := raw[key]
		if !ok {
			t.Fatalf("%s key missing from %q", key, out)
		}
		if strings.TrimSpace(string(val)) != "[]" {
			t.Fatalf("%s = %s, want []", key, val)
		}
	}
}

func TestNextJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Next json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := nextCmd.RunE(nextCmd, nil); err != nil {
			t.Fatalf("next RunE: %v", err)
		}
	})

	var got models.Issue
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if got.ID != issue.ID {
		t.Fatalf("next --json id = %q, want %q", got.ID, issue.ID)
	}
	if strings.Contains(out, "td start") {
		t.Fatalf("json output must not contain the human hint: %q", out)
	}
}

// TestNextJSONNoResultIsNull documents the one deliberate exception to the
// empty-array rule: `next` answers a singular question, so "nothing open" is
// null rather than [].
func TestNextJSONNoResultIsNull(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := nextCmd.RunE(nextCmd, nil); err != nil {
			t.Fatalf("next RunE: %v", err)
		}
	})
	if strings.TrimSpace(out) != "null" {
		t.Fatalf("next --json with no open issues = %q, want null", out)
	}
}

func TestCommentsJSONOutput(t *testing.T) {
	database, sessionID := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Comments json task", models.StatusOpen, "")
	if err := database.AddComment(&models.Comment{
		IssueID:   issue.ID,
		SessionID: sessionID,
		Text:      "first comment",
	}); err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := commentsCmd.RunE(commentsCmd, []string{issue.ID}); err != nil {
			t.Fatalf("comments RunE: %v", err)
		}
	})

	var comments []models.Comment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if len(comments) != 1 {
		t.Fatalf("comments --json = %+v, want one comment", comments)
	}
	if comments[0].Text != "first comment" || comments[0].IssueID != issue.ID {
		t.Fatalf("comment fields not carried through: %+v", comments[0])
	}
	if comments[0].CreatedAt.IsZero() {
		t.Fatalf("created_at must be a real timestamp: %+v", comments[0])
	}
}

func TestCommentsJSONEmptyIsArray(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Commentless json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := commentsCmd.RunE(commentsCmd, []string{issue.ID}); err != nil {
			t.Fatalf("comments RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestEpicListJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	epic := &models.Issue{Title: "Epic json", Type: models.TypeEpic, Status: models.StatusOpen}
	if err := database.CreateIssue(epic); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := epicListCmd.RunE(epicListCmd, nil); err != nil {
			t.Fatalf("epic list RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != epic.ID {
		t.Fatalf("epic list --json = %+v, want %s", issues, epic.ID)
	}
	if issues[0].Type != models.TypeEpic {
		t.Fatalf("type = %q, want epic", issues[0].Type)
	}
}

func TestEpicListJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := epicListCmd.RunE(epicListCmd, nil); err != nil {
			t.Fatalf("epic list RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestQueryJSONFlagMatchesOutputJSON(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Query json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := queryCmd.RunE(queryCmd, []string{"status = open"}); err != nil {
			t.Fatalf("query RunE: %v", err)
		}
	})
	issues := decodeIssueArray(t, out)
	if len(issues) != 1 || issues[0].ID != issue.ID {
		t.Fatalf("query --json = %+v, want %s", issues, issue.ID)
	}
}

func TestQueryJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := queryCmd.RunE(queryCmd, []string{"status = open"}); err != nil {
			t.Fatalf("query RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestLastJSONOutput(t *testing.T) {
	database, sessionID := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Last json task", models.StatusOpen, "")
	if err := database.LogAction(&models.ActionLog{
		SessionID:  sessionID,
		ActionType: models.ActionStart,
		EntityType: "issue",
		EntityID:   issue.ID,
	}); err != nil {
		t.Fatalf("LogAction failed: %v", err)
	}
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := lastCmd.RunE(lastCmd, nil); err != nil {
			t.Fatalf("last RunE: %v", err)
		}
	})

	var actions []models.ActionLog
	if err := json.Unmarshal([]byte(out), &actions); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if len(actions) != 1 {
		t.Fatalf("last --json = %+v, want one action", actions)
	}
	if actions[0].ActionType != models.ActionStart || actions[0].EntityID != issue.ID {
		t.Fatalf("action fields not carried through: %+v", actions[0])
	}
	if actions[0].Timestamp.IsZero() {
		t.Fatalf("timestamp must be a real time: %+v", actions[0])
	}
}

func TestLastJSONEmptyIsArray(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := lastCmd.RunE(lastCmd, nil); err != nil {
			t.Fatalf("last RunE: %v", err)
		}
	})
	requireEmptyJSONArray(t, out)
}

func TestFocusJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Focus json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := focusCmd.RunE(focusCmd, []string{issue.ID}); err != nil {
			t.Fatalf("focus RunE: %v", err)
		}
	})

	var env struct {
		ID     string        `json:"id"`
		Status string        `json:"status"`
		Action string        `json:"action"`
		Issue  *models.Issue `json:"issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "focused" {
		t.Fatalf("action = %q, want focused", env.Action)
	}
	if env.ID != issue.ID || env.Issue == nil || env.Issue.ID != issue.ID {
		t.Fatalf("focus envelope did not carry the issue: %q", out)
	}
	if strings.Contains(out, "FOCUSED ") {
		t.Fatalf("json output must not contain the human FOCUSED line: %q", out)
	}
}

func TestUnfocusJSONOutput(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "Unfocus json task", models.StatusOpen, "")
	setJSONFlag(t, true)

	captureStdout(t, func() {
		if err := focusCmd.RunE(focusCmd, []string{issue.ID}); err != nil {
			t.Fatalf("focus RunE: %v", err)
		}
	})

	out := captureStdout(t, func() {
		if err := unfocusCmd.RunE(unfocusCmd, nil); err != nil {
			t.Fatalf("unfocus RunE: %v", err)
		}
	})

	var env struct {
		Action        string `json:"action"`
		PreviousIssue string `json:"previous_issue"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Action != "unfocused" {
		t.Fatalf("action = %q, want unfocused", env.Action)
	}
	if env.PreviousIssue != issue.ID {
		t.Fatalf("previous_issue = %q, want %q", env.PreviousIssue, issue.ID)
	}
}

// TestUnfocusJSONWithNothingFocused is the "empty result" case for a command
// with no list to return: the envelope is still emitted, with an empty
// previous_issue rather than a missing key.
func TestUnfocusJSONWithNothingFocused(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := unfocusCmd.RunE(unfocusCmd, nil); err != nil {
			t.Fatalf("unfocus RunE: %v", err)
		}
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if _, ok := raw["previous_issue"]; !ok {
		t.Fatalf("previous_issue key missing from %q", out)
	}
	if string(raw["previous_issue"]) != `""` {
		t.Fatalf("previous_issue = %s, want empty string", raw["previous_issue"])
	}
}

func TestWorkflowJSONOutput(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		if err := workflowCmd.RunE(workflowCmd, nil); err != nil {
			t.Fatalf("workflow RunE: %v", err)
		}
	})

	var env struct {
		Statuses    []string `json:"statuses"`
		Transitions []struct {
			From   string   `json:"from"`
			To     string   `json:"to"`
			Name   string   `json:"name"`
			Guards []string `json:"guards"`
		} `json:"transitions"`
		Modes []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"modes"`
		Guards []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"guards"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if len(env.Statuses) == 0 {
		t.Fatalf("statuses must not be empty: %q", out)
	}
	if len(env.Transitions) == 0 {
		t.Fatalf("transitions must not be empty: %q", out)
	}
	var sawStart bool
	for _, tr := range env.Transitions {
		if tr.Guards == nil {
			t.Fatalf("transition %s->%s has null guards, want []", tr.From, tr.To)
		}
		if tr.From == string(models.StatusOpen) && tr.To == string(models.StatusInProgress) {
			sawStart = true
			if tr.Name != "start" {
				t.Fatalf("open->in_progress name = %q, want start", tr.Name)
			}
		}
	}
	if !sawStart {
		t.Fatalf("open -> in_progress transition missing from %q", out)
	}
	if len(env.Modes) == 0 || len(env.Guards) == 0 {
		t.Fatalf("modes/guards must be present: %q", out)
	}
	if strings.Contains(out, "ISSUE STATUS WORKFLOW") {
		t.Fatalf("json output must not contain the human header: %q", out)
	}
}

// TestWorkflowJSONCarriesDiagram asserts --json does not silently discard
// --mermaid / --dot: the diagram text comes back as a string field.
func TestWorkflowJSONCarriesDiagram(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)
	setWorkflowExitFlag(t, workflowCmd, "mermaid", "true")

	out := captureStdout(t, func() {
		if err := workflowCmd.RunE(workflowCmd, nil); err != nil {
			t.Fatalf("workflow RunE: %v", err)
		}
	})

	var env struct {
		Format  string `json:"format"`
		Diagram string `json:"diagram"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Format != "mermaid" {
		t.Fatalf("format = %q, want mermaid", env.Format)
	}
	if !strings.Contains(env.Diagram, "stateDiagram-v2") {
		t.Fatalf("diagram does not look like mermaid source: %q", env.Diagram)
	}
}

func TestVersionJSONOutput(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)
	// Keep the test hermetic: --check=false skips the network/cache lookup.
	setWorkflowExitFlag(t, versionCmd, "check", "false")

	out := captureStdout(t, func() {
		versionCmd.Run(versionCmd, nil)
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, out)
	}
	for _, key := range []string{"version", "development", "checked", "update_available", "latest_version", "update_command"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("%s key missing from %q", key, out)
		}
	}
	if string(raw["update_available"]) != "false" {
		t.Fatalf("update_available = %s, want false when no check ran", raw["update_available"])
	}
	if string(raw["checked"]) != "false" {
		t.Fatalf("checked = %s, want false when --check=false", raw["checked"])
	}
	if strings.Contains(out, "td version ") {
		t.Fatalf("json output must not contain the human version line: %q", out)
	}
}

// TestVersionJSONIgnoresShort asserts --json wins over --short so the envelope
// stays parseable rather than degrading to a bare version string.
func TestVersionJSONIgnoresShort(t *testing.T) {
	setupReadJSONTest(t)
	setJSONFlag(t, true)
	setWorkflowExitFlag(t, versionCmd, "check", "false")
	setWorkflowExitFlag(t, versionCmd, "short", "true")

	out := captureStdout(t, func() {
		versionCmd.Run(versionCmd, nil)
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("--json with --short is not valid JSON: %v\noutput: %q", err, out)
	}
	if _, ok := raw["version"]; !ok {
		t.Fatalf("version key missing from %q", out)
	}
}
