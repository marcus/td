package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func newTimelineDB(t *testing.T) (*DB, *models.Issue) {
	t.Helper()
	db, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	issue := &models.Issue{Title: "Timeline test", Type: models.TypeTask, Priority: models.PriorityP2}
	if err := db.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	return db, issue
}

var timelineSeq int

// addActionAt writes an issue action_log row at an exact time.
func addActionAt(t *testing.T, db *DB, issueID, session string, action models.ActionType, prev, next string, at time.Time) {
	t.Helper()
	timelineSeq++
	if _, err := db.conn.Exec(`
		INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, ?, 'issue', ?, ?, ?, ?, 0)`,
		fmt.Sprintf("al-test%04d", timelineSeq), session, action, issueID, prev, next, formatActionLogTimestamp(at.UTC()),
	); err != nil {
		t.Fatalf("insert action: %v", err)
	}
}

// addLogAt writes a log at an exact time, in local time as AddLog does.
func addLogAt(t *testing.T, db *DB, issueID, session, msg string, typ models.LogType, at time.Time) {
	t.Helper()
	timelineSeq++
	if _, err := db.conn.Exec(`
		INSERT INTO logs (id, issue_id, session_id, work_session_id, message, type, timestamp)
		VALUES (?, ?, ?, '', ?, ?, ?)`,
		fmt.Sprintf("lg-test%04d", timelineSeq), issueID, session, msg, typ, at.Local(),
	); err != nil {
		t.Fatalf("insert log: %v", err)
	}
}

func timeline(t *testing.T, db *DB, issueID string, limit int) []models.TimelineEvent {
	t.Helper()
	events, err := db.GetIssueTimeline(issueID, limit)
	if err != nil {
		t.Fatalf("GetIssueTimeline failed: %v", err)
	}
	return events
}

func summaries(events []models.TimelineEvent) string {
	var b strings.Builder
	for _, ev := range events {
		fmt.Fprintf(&b, "%s: %s", ev.EventType, ev.Summary)
		if ev.Detail != "" {
			fmt.Fprintf(&b, " / %s", ev.Detail)
		}
		if ev.Count > 0 {
			fmt.Fprintf(&b, " x%d", ev.Count)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestGetIssueTimeline_UnknownIssue(t *testing.T) {
	db, _ := newTimelineDB(t)
	if _, err := db.GetIssueTimeline("td-nope00", 0); err == nil {
		t.Fatal("expected an error for an unknown issue")
	}
}

func TestGetIssueTimeline_SynthesizesCreated(t *testing.T) {
	// CreateIssue writes no action_log row; the timeline still starts with
	// the issue's creation.
	db, issue := newTimelineDB(t)
	events := timeline(t, db, issue.ID, 0)
	if len(events) != 1 || events[0].EventType != models.EventCreated || events[0].Summary != "Created task (P2)" {
		t.Fatalf("got:\n%s", summaries(events))
	}
}

func TestGetIssueTimeline_ChronologicalAndUTC(t *testing.T) {
	db, issue := newTimelineDB(t)
	base := issue.CreatedAt.Add(time.Hour)

	// Logs are stored in local time and actions in UTC; the timeline must
	// order them by instant and report all of them in UTC.
	addLogAt(t, db, issue.ID, "ses_a", "second", models.LogTypeProgress, base.Add(2*time.Minute))
	addActionAt(t, db, issue.ID, "ses_b", models.ActionBlock, "", "", base.Add(time.Minute))
	addLogAt(t, db, issue.ID, "ses_a", "fourth", models.LogTypeProgress, base.Add(4*time.Minute))
	addActionAt(t, db, issue.ID, "ses_b", models.ActionUnblock, "", "", base.Add(3*time.Minute))

	events := timeline(t, db, issue.ID, 0)
	want := "created: Created task (P2)\nstatus_change: Blocked\nlog: second\nstatus_change: Unblocked\nlog: fourth\n"
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, ev := range events {
		if ev.Timestamp.Location() != time.UTC {
			t.Errorf("%s timestamp %v is not UTC", ev.Summary, ev.Timestamp)
		}
	}
}

func TestGetIssueTimeline_FoldsLifecycleLogs(t *testing.T) {
	db, issue := newTimelineDB(t)
	base := issue.CreatedAt.Add(time.Hour)

	// start writes "Started work" a few seconds after its action: it only
	// repeats the summary, so it disappears.
	addActionAt(t, db, issue.ID, "ses_a", models.ActionStart, "", "", base)
	addLogAt(t, db, issue.ID, "ses_a", "Started work", models.LogTypeProgress, base.Add(4*time.Second))
	// Another session's log at the same moment is not folded.
	addLogAt(t, db, issue.ID, "ses_other", "unrelated note", models.LogTypeProgress, base.Add(5*time.Second))
	// The same session's log well outside the window is not folded either.
	addLogAt(t, db, issue.ID, "ses_a", "later note", models.LogTypeProgress, base.Add(time.Minute))

	// review then approve one second apart: each log goes to its own action.
	r := base.Add(10 * time.Minute)
	addActionAt(t, db, issue.ID, "ses_r", models.ActionReview, "", "", r)
	addActionAt(t, db, issue.ID, "ses_r", models.ActionApprove, "", "", r.Add(time.Second))
	addLogAt(t, db, issue.ID, "ses_r", "Submitted for review", models.LogTypeProgress, r.Add(100*time.Millisecond))
	addLogAt(t, db, issue.ID, "ses_r", "Approved (self-review): tests pass", models.LogTypeSecurity, r.Add(1100*time.Millisecond))

	events := timeline(t, db, issue.ID, 0)
	want := strings.Join([]string{
		"created: Created task (P2)",
		"status_change: Started work",
		"log: unrelated note",
		"log: later note",
		"status_change: Submitted for review",
		"status_change: Approved and closed / Approved (self-review): tests pass",
		"",
	}, "\n")
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetIssueTimeline_UpdateDoesNotSwallowNextLog(t *testing.T) {
	// `td update ... && td log ...` runs within milliseconds; the note is the
	// agent's own and must stay a log, not become the update's detail.
	db, issue := newTimelineDB(t)
	base := issue.CreatedAt.Add(time.Hour)
	addActionAt(t, db, issue.ID, "ses_a", models.ActionUpdate, `{"priority":"P2"}`, `{"priority":"P1"}`, base)
	addLogAt(t, db, issue.ID, "ses_a", "bumped because of the outage", models.LogTypeProgress, base.Add(50*time.Millisecond))

	events := timeline(t, db, issue.ID, 0)
	want := "created: Created task (P2)\nupdate: Updated priority: P2 → P1\nlog: bumped because of the outage\n"
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetIssueTimeline_Updates(t *testing.T) {
	db, issue := newTimelineDB(t)
	base := issue.CreatedAt.Add(time.Hour)

	addActionAt(t, db, issue.ID, "ses_a", models.ActionUpdate,
		`{"priority":"P2","updated_at":"a"}`, `{"priority":"P2","updated_at":"b"}`, base)
	addActionAt(t, db, issue.ID, "ses_a", models.ActionUpdate,
		`{"priority":"P2","description":"x","labels":""}`,
		`{"priority":"P1","description":"`+strings.Repeat("long ", 20)+`","labels":"ui"}`, base.Add(time.Minute))
	addActionAt(t, db, issue.ID, "ses_a", models.ActionUpdate,
		`{"status":"open"}`, `{"status":"blocked"}`, base.Add(2*time.Minute))
	addActionAt(t, db, issue.ID, "ses_a", models.ActionUpdate, `not json`, `{}`, base.Add(3*time.Minute))

	events := timeline(t, db, issue.ID, 0)
	want := strings.Join([]string{
		"created: Created task (P2)",
		"update: Updated description, labels: (none) → ui, priority: P2 → P1",
		"status_change: Updated status: open → blocked",
		"update: Updated",
		"",
	}, "\n")
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetIssueTimeline_SkipsUndoneActions(t *testing.T) {
	db, issue := newTimelineDB(t)
	addActionAt(t, db, issue.ID, "ses_a", models.ActionClose, "", "", issue.CreatedAt.Add(time.Hour))
	if _, err := db.conn.Exec(`UPDATE action_log SET undone = 1 WHERE entity_id = ?`, issue.ID); err != nil {
		t.Fatalf("mark undone: %v", err)
	}
	if events := timeline(t, db, issue.ID, 0); len(events) != 1 {
		t.Fatalf("undone close still shown:\n%s", summaries(events))
	}
}

func TestGetIssueTimeline_CommentsAndRepeats(t *testing.T) {
	db, issue := newTimelineDB(t)
	long := strings.Repeat("é", 100) // multi-byte: must not be cut mid-rune
	for _, text := range []string{"looks good", long, "line one\nline two", "retry failed", "retry failed", "retry failed"} {
		if err := db.AddComment(&models.Comment{IssueID: issue.ID, SessionID: "ses_c", Text: text}); err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	events := timeline(t, db, issue.ID, 0)
	want := strings.Join([]string{
		"created: Created task (P2)",
		"comment: Comment: looks good",
		"comment: Comment / " + long,
		"comment: Comment / line one\nline two",
		"comment: Comment: retry failed x3",
		"",
	}, "\n")
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetIssueTimeline_HandoffsSnapshotsFiles(t *testing.T) {
	db, issue := newTimelineDB(t)
	if err := db.AddGitSnapshot(&models.GitSnapshot{
		IssueID: issue.ID, Event: "start", CommitSHA: "abc1234567890", Branch: "main", DirtyFiles: 2,
	}); err != nil {
		t.Fatalf("AddGitSnapshot: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := db.AddHandoff(&models.Handoff{
		IssueID: issue.ID, SessionID: "ses_a", Done: []string{"a", "b"}, Uncertain: []string{"c"},
	}); err != nil {
		t.Fatalf("AddHandoff: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := db.LinkFile(issue.ID, "cmd/history.go", models.FileRoleImplementation, "deadbeef"); err != nil {
		t.Fatalf("LinkFile: %v", err)
	}

	events := timeline(t, db, issue.ID, 0)
	want := strings.Join([]string{
		"created: Created task (P2)",
		"git_snapshot: Git start: abc1234 on main (2 dirty)",
		"handoff: Handoff / Done: a; b\nUncertain: c",
		"file_link: Linked file: cmd/history.go (implementation)",
		"",
	}, "\n")
	if got := summaries(events); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetIssueTimeline_Limit(t *testing.T) {
	db, issue := newTimelineDB(t)
	base := issue.CreatedAt.Add(time.Hour)
	for i := 0; i < 5; i++ {
		addLogAt(t, db, issue.ID, "ses_a", fmt.Sprintf("log %d", i), models.LogTypeProgress, base.Add(time.Duration(i)*time.Minute))
	}

	events := timeline(t, db, issue.ID, 2)
	if got, want := summaries(events), "log: log 3\nlog: log 4\n"; got != want {
		t.Fatalf("limit keeps the most recent events; got:\n%s", got)
	}
}
