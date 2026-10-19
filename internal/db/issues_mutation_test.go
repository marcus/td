package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestMutateIssueLoggedConcurrentLabelEdits(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Current field survives", Labels: []string{"unrelated", "remove"}}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := database.MutateIssueLogged(issue.ID, "ses_labels", nil, models.LabelChange{Add: []string{fmt.Sprintf("label-%d", i)}, Remove: []string{"remove"}}, "", nil)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != issue.Title || len(got.Labels) != writers+1 || got.Labels[0] != "unrelated" {
		t.Fatalf("current labels/fields lost: %+v", got)
	}
	var count int
	if err := database.conn.QueryRow("SELECT COUNT(*) FROM action_log WHERE entity_id = ? AND action_type = 'update'", issue.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != writers {
		t.Fatalf("action count=%d", count)
	}
}

func TestMutateIssueLoggedCommentAndRevision(t *testing.T) {
	database, err := Initialize(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Label hand-back", Labels: []string{"for-marcus", "keep"}}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	before, _ := database.GetIssue(issue.ID)
	got, err := database.MutateIssueLogged(issue.ID, "ses_labels", before, models.LabelChange{Add: []string{"ready-for-clara"}, Remove: []string{"for-marcus"}}, "Handed back", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Labels, []string{"keep", "ready-for-clara"}) {
		t.Fatal(got.Labels)
	}
	comments, err := database.GetComments(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Text != "Handed back" {
		t.Fatal(comments)
	}
	_, err = database.MutateIssueLogged(issue.ID, "ses_labels", before, models.LabelChange{Add: []string{"stale"}}, "stale comment", nil)
	if !errors.Is(err, ErrIssueChanged) {
		t.Fatalf("stale err=%v", err)
	}
	var previousData string
	if err := database.conn.QueryRow("SELECT previous_data FROM action_log WHERE entity_id = ? AND action_type = 'update'", issue.ID).Scan(&previousData); err != nil {
		t.Fatal(err)
	}
	var previous models.Issue
	if err := json.Unmarshal([]byte(previousData), &previous); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(previous.Labels, before.Labels) {
		t.Fatalf("undo snapshot=%v", previous.Labels)
	}
}

func TestMutateIssueLoggedRollsBackCommentFailure(t *testing.T) {
	for _, failure := range []string{"comment insert", "comment log"} {
		t.Run(failure, func(t *testing.T) {
			database, err := Initialize(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close() }()
			issue := &models.Issue{Title: "Atomic rollback", Labels: []string{"original"}}
			if err := database.CreateIssue(issue); err != nil {
				t.Fatal(err)
			}
			before, _ := database.GetIssue(issue.ID)
			trigger := "CREATE TRIGGER fail_comment BEFORE INSERT ON comments BEGIN SELECT RAISE(ABORT, 'forced comment failure'); END"
			if failure == "comment log" {
				trigger = "CREATE TRIGGER fail_comment BEFORE INSERT ON action_log WHEN NEW.entity_type = 'comments' BEGIN SELECT RAISE(ABORT, 'forced comment log failure'); END"
			}
			if _, err := database.conn.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			_, err = database.MutateIssueLogged(issue.ID, "ses_labels", nil, models.LabelChange{Add: []string{"new"}}, "Must roll back", func(current *models.Issue) error { current.Title = "Should not stick"; return nil })
			if err == nil {
				t.Fatal("expected failure")
			}
			after, _ := database.GetIssue(issue.ID)
			if marshalIssue(after) != marshalIssue(before) {
				t.Fatalf("partial issue update: %+v", after)
			}
			comments, _ := database.GetComments(issue.ID)
			if len(comments) != 0 {
				t.Fatal(comments)
			}
			var count int
			if err := database.conn.QueryRow("SELECT COUNT(*) FROM action_log WHERE session_id = 'ses_labels'").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("partial action log=%d", count)
			}
		})
	}
}
