package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/pflag"
	"modernc.org/sqlite"
)

func TestUpdateAtomicLabelsAndComment(t *testing.T) {
	saveAndRestoreGlobals(t)
	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "CLI incremental label update", Labels: []string{"keep", "for-marcus"}}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	saveAndRestoreCommandFlags(t, updateCmd, "comment", "note", "title", "status", "defer", "append")
	for _, name := range []string{"labels", "add-label", "remove-label"} {
		flag := updateCmd.Flags().Lookup(name)
		old, _ := updateCmd.Flags().GetStringArray(name)
		oldChanged := flag.Changed
		value := flag.Value.(pflag.SliceValue)
		if err := value.Replace(nil); err != nil {
			t.Fatal(err)
		}
		flag.Changed = false
		t.Cleanup(func() { _ = value.Replace(old); flag.Changed = oldChanged })
	}
	for _, flag := range []struct{ name, value string }{{"add-label", "ready-for-clara"}, {"add-label", "ready-for-clara"}, {"add-label", "Extra"}, {"remove-label", "for-marcus"}, {"remove-label", "absent"}, {"comment", "Handed back atomically"}} {
		if err := updateCmd.Flags().Set(flag.name, flag.value); err != nil {
			t.Fatal(err)
		}
	}
	captureStdout(t, func() {
		if err := updateCmd.RunE(updateCmd, []string{issue.ID}); err != nil {
			t.Fatal(err)
		}
	})
	got, _ := database.GetIssue(issue.ID)
	if !reflect.DeepEqual(got.Labels, []string{"keep", "ready-for-clara", "Extra"}) {
		t.Fatal(got.Labels)
	}
	comments, _ := database.GetComments(issue.ID)
	if len(comments) != 1 || comments[0].Text != "Handed back atomically" {
		t.Fatal(comments)
	}
	// Validation must happen before any issue or inline-comment write.
	if err := updateCmd.Flags().Set("remove-label", " ready-for-clara "); err != nil {
		t.Fatal(err)
	}
	if err := updateCmd.RunE(updateCmd, []string{issue.ID}); err == nil {
		t.Fatal("expected contradictory label error")
	}
	after, _ := database.GetIssue(issue.ID)
	if !after.UpdatedAt.Equal(got.UpdatedAt) {
		t.Fatal("invalid delta wrote issue")
	}
	comments, _ = database.GetComments(issue.ID)
	if len(comments) != 1 {
		t.Fatal("invalid delta wrote comment")
	}
	actions, err := database.GetRecentActionsAll(10)
	if err != nil {
		t.Fatal(err)
	}
	var update *models.ActionLog
	for i := range actions {
		if actions[i].EntityID == issue.ID && actions[i].ActionType == models.ActionUpdate {
			update = &actions[i]
			break
		}
	}
	if update == nil {
		t.Fatal("missing issue undo record")
	}
	if err := performUndo(database, update, update.SessionID); err != nil {
		t.Fatal(err)
	}
	after, _ = database.GetIssue(issue.ID)
	if !reflect.DeepEqual(after.Labels, issue.Labels) {
		t.Fatalf("undo labels=%v", after.Labels)
	}
	comments, _ = database.GetComments(issue.ID)
	if len(comments) != 1 {
		t.Fatal("issue undo unexpectedly removed comment")
	}
}

// Exercise Cobra and Execute in the real binary: RunE-only assertions cannot
// detect a wrong top-level JSON code or a usage block emitted by Cobra.
func TestUpdateAtomicLabelsCommentFailureCLIContract(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Real CLI atomic error contract", Labels: []string{"original"}}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	before, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec("CREATE TRIGGER fail_comment BEFORE INSERT ON comments BEGIN SELECT RAISE(ABORT, 'forced comment failure'); END"); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(buildTD(t), "update", issue.ID, "--add-label", "ready", "--comment", "Must roll back", "--json")
	child.Dir = dir
	child.Env = append(os.Environ(), "TD_FEATURE_SYNC_AUTOSYNC=false", "TD_FEATURE_SYNC_CLI=false", "HOME="+dir)
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err = child.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("exit=%v, want code 1", err)
	}
	envelope := assertSoleJSONValue(t, "atomic update failure", stdout.String())
	failure, ok := envelope["error"].(map[string]any)
	if !ok || failure["code"] != output.ErrCodeDatabaseError {
		t.Fatalf("error=%v", envelope["error"])
	}
	if message, _ := failure["message"].(string); !strings.Contains(message, "forced comment failure") {
		t.Fatalf("error lost database cause: %v", failure)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr/usage: %s", stderr.String())
	}
	after, err := database.GetIssue(issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("partial issue write: %+v", after)
	}
	comments, err := database.GetComments(issue.ID)
	if err != nil || len(comments) != 0 {
		t.Fatalf("partial comment write: %v %v", comments, err)
	}
	var actions int
	if err := database.Conn().QueryRow("SELECT COUNT(*) FROM action_log WHERE entity_id = ?", issue.ID).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 0 {
		t.Fatalf("partial action records: %d", actions)
	}
}

func TestUpdateLabelReplacementCLICompatibility(t *testing.T) {
	for _, test := range []struct {
		name    string
		flags   []string
		want    []string
		invalid bool
	}{
		{name: "long replacement", flags: []string{"--labels", "foo,bar"}, want: []string{"foo", "bar"}},
		{name: "short replacement", flags: []string{"-l", "foo,bar"}, want: []string{"foo", "bar"}},
		{name: "repeated replacement", flags: []string{"--labels", "foo", "--labels", "bar,foo"}, want: []string{"foo", "bar"}},
		{name: "long clear", flags: []string{"--labels", ""}},
		{name: "short clear", flags: []string{"-l", ""}},
		{name: "replacement with add", flags: []string{"--labels", "foo", "--add-label", "ready"}, invalid: true},
		{name: "replacement with remove", flags: []string{"-l", "foo", "--remove-label", "original"}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Initialize(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close() }()
			issue := &models.Issue{Title: "Fresh CLI label replacement", Labels: []string{"original", "keep"}}
			if err := database.CreateIssue(issue); err != nil {
				t.Fatal(err)
			}
			before, err := database.GetIssue(issue.ID)
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"update", issue.ID}, test.flags...)
			args = append(args, "--comment", "Replacement comment", "--json")
			child := exec.Command(buildTD(t), args...)
			child.Dir = dir
			child.Env = append(os.Environ(), "TD_FEATURE_SYNC_AUTOSYNC=false", "TD_FEATURE_SYNC_CLI=false", "TD_CONTEXT_ID=test-label-replacement", "HOME="+dir)
			var stdout, stderr bytes.Buffer
			child.Stdout, child.Stderr = &stdout, &stderr
			err = child.Run()
			envelope := assertSoleJSONValue(t, test.name, stdout.String())
			after, readErr := database.GetIssue(issue.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			comments, readErr := database.GetComments(issue.ID)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if test.invalid {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("expected exit 1; err=%v", err)
				}
				failure, ok := envelope["error"].(map[string]any)
				if !ok || !strings.Contains(fmt.Sprint(failure["message"]), "cannot combine label replacement") {
					t.Fatalf("wrong conflict: %v", envelope)
				}
				if !reflect.DeepEqual(after, before) || len(comments) != 0 {
					t.Fatalf("conflict partially wrote issue/comment: %+v %v", after, comments)
				}
				return
			}
			if err != nil {
				t.Fatalf("replacement failed: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
			}
			if envelope["action"] != "updated" {
				t.Fatalf("not an update envelope: %v", envelope)
			}
			if !reflect.DeepEqual(after.Labels, test.want) {
				t.Fatalf("labels=%v want=%v", after.Labels, test.want)
			}
			if len(comments) != 1 || comments[0].Text != "Replacement comment" {
				t.Fatalf("comments=%v", comments)
			}
		})
	}
}

func TestUpdateAtomicLabelsCommentFailureReturnsError(t *testing.T) {
	saveAndRestoreGlobals(t)
	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	issue := &models.Issue{Title: "Comment failure returns nonzero", Labels: []string{"original"}}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	saveAndRestoreCommandFlags(t, updateCmd, "comment", "note", "title", "status", "defer", "append")
	for _, name := range []string{"labels", "add-label", "remove-label"} {
		flag := updateCmd.Flags().Lookup(name)
		old, _ := updateCmd.Flags().GetStringArray(name)
		oldChanged := flag.Changed
		value := flag.Value.(pflag.SliceValue)
		if err := value.Replace(nil); err != nil {
			t.Fatal(err)
		}
		flag.Changed = false
		t.Cleanup(func() { _ = value.Replace(old); flag.Changed = oldChanged })
	}
	if err := updateCmd.Flags().Set("add-label", "should-rollback"); err != nil {
		t.Fatal(err)
	}
	if err := updateCmd.Flags().Set("comment", "Must fail together"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Conn().Exec("CREATE TRIGGER fail_comment BEFORE INSERT ON comments BEGIN SELECT RAISE(ABORT, 'forced comment failure'); END"); err != nil {
		t.Fatal(err)
	}
	setJSONFlag(t, true)
	var runErr error
	captureStdout(t, func() { runErr = updateCmd.RunE(updateCmd, []string{issue.ID}) })
	var cause *sqlite.Error
	if !errors.As(runErr, &cause) {
		t.Fatalf("database cause lost: %v", runErr)
	}
	if topLevelErrorCode(runErr) != output.ErrCodeDatabaseError {
		t.Fatalf("wrong error code: %v", runErr)
	}
	after, _ := database.GetIssue(issue.ID)
	if !reflect.DeepEqual(after.Labels, issue.Labels) {
		t.Fatalf("partial label write: %v", after.Labels)
	}
}
