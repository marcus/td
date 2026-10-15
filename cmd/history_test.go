package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestPrintTimeline(t *testing.T) {
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	events := []models.TimelineEvent{
		{Timestamp: at, EventType: models.EventStatusChange, Summary: "Started work", SessionID: "ses_a"},
		{Timestamp: at, EventType: models.EventHandoff, Summary: "Handoff", Detail: "Done: x\n\nRemaining: y", SessionID: "ses_a"},
		{Timestamp: at, EventType: models.EventComment, Summary: "Comment: retry", SessionID: "ses_b", Count: 3},
	}
	stamp := at.Local().Format("2006-01-02 15:04")

	got := captureStdout(t, func() { printTimeline(events) })
	want := "  " + stamp + "  status  Started work  (ses_a)\n" +
		"  " + stamp + "  handoff Handoff\n" +
		historyIndent + "Done: x\n" +
		"\n" +
		historyIndent + "Remaining: y\n" +
		"  " + stamp + "  comment Comment: retry (×3)  (ses_b)\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func setHistoryFlags(t *testing.T, jsonOut bool, limit string) {
	t.Helper()
	for name, val := range map[string]string{"json": fmt.Sprint(jsonOut), "limit": limit} {
		orig := historyCmd.Flags().Lookup(name).Value.String()
		if err := historyCmd.Flags().Set(name, val); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
		t.Cleanup(func() { _ = historyCmd.Flags().Set(name, orig) })
	}
}

func TestHistoryJSON(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "History JSON", models.StatusOpen, "")
	if err := database.DeleteIssue(issue.ID); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	setHistoryFlags(t, true, "0")

	var runErr error
	out := captureStdout(t, func() { runErr = historyCmd.RunE(historyCmd, []string{issue.ID}) })
	if runErr != nil {
		t.Fatalf("history: %v", runErr)
	}
	var got struct {
		IssueID   string                 `json:"issue_id"`
		DeletedAt *time.Time             `json:"deleted_at"`
		Events    []models.TimelineEvent `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.IssueID != issue.ID || got.DeletedAt == nil || len(got.Events) == 0 || got.Events[0].EventType != models.EventCreated {
		t.Fatalf("unexpected payload: %s", out)
	}
}

func TestHistoryJSONErrors(t *testing.T) {
	database, _ := setupReadJSONTest(t)
	issue := newReadJSONIssue(t, database, "History errors", models.StatusOpen, "")

	for _, tc := range []struct {
		name, id, limit, code string
	}{
		{"not found", "td-doesnotexist", "0", "not_found"},
		{"negative limit", issue.ID, "-1", "invalid_input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setHistoryFlags(t, true, tc.limit)
			var runErr error
			out := captureStdout(t, func() { runErr = historyCmd.RunE(historyCmd, []string{tc.id}) })
			requireSilentFailure(t, runErr)
			var env struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil || env.Error.Code != tc.code {
				t.Fatalf("want %s envelope, got %q (%v)", tc.code, out, err)
			}
		})
	}
}
