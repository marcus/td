package cmd

import (
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
