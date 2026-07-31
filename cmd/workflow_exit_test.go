package cmd

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

func setupWorkflowExitTest(t *testing.T) (string, *db.DB, string) {
	t.Helper()
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_workflow_exit_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	setWorkflowExitFlag(t, startCmd, "force", "false")
	setWorkflowExitFlag(t, closeCmd, "self-close-exception", "")
	setWorkflowExitFlag(t, closeCmd, "admin", "")
	setJSONFlag(t, false)
	return dir, database, sess.ID
}

func setWorkflowExitFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("%s: missing --%s flag", cmd.Name(), name)
	}
	original := flag.Value.String()
	originalChanged := flag.Changed
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("%s: set --%s: %v", cmd.Name(), name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Flags().Set(name, original)
		flag.Changed = originalChanged
	})
}

func newWorkflowExitIssue(t *testing.T, database *db.DB, status models.Status, minor bool) *models.Issue {
	t.Helper()
	issue := &models.Issue{
		Title:  "Workflow exit semantics",
		Status: status,
		Minor:  minor,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return issue
}

func requireSilentFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected command failure, got nil")
	}
	if !errors.Is(err, errSilentExit) {
		t.Fatalf("error = %v, want errSilentExit", err)
	}
}

func TestStartExitSemantics(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)

	alreadyStarted := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	if err := startCmd.RunE(startCmd, []string{alreadyStarted.ID}); err != nil {
		t.Fatalf("already-started retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, startCmd.RunE(startCmd, []string{"td-missing-start"}))
	requireSilentFailure(t, startCmd.RunE(startCmd, []string{alreadyStarted.ID, "td-missing-start"}))

	blocked := newWorkflowExitIssue(t, database, models.StatusBlocked, false)
	requireSilentFailure(t, startCmd.RunE(startCmd, []string{blocked.ID}))
	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	requireSilentFailure(t, startCmd.RunE(startCmd, []string{closed.ID}))

	startable := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := startCmd.RunE(startCmd, []string{"td-missing-start", startable.ID}); err != nil {
		t.Fatalf("mixed batch with one successful start must succeed: %v", err)
	}
	got, err := database.GetIssue(startable.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusInProgress {
		t.Fatalf("status = %s, want in_progress", got.Status)
	}
}

func TestRejectExitSemantics(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)

	alreadyOpen := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := rejectCmd.RunE(rejectCmd, []string{alreadyOpen.ID}); err != nil {
		t.Fatalf("already-open retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, rejectCmd.RunE(rejectCmd, []string{"td-missing-reject"}))
	requireSilentFailure(t, rejectCmd.RunE(rejectCmd, []string{alreadyOpen.ID, "td-missing-reject"}))

	inProgress := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	requireSilentFailure(t, rejectCmd.RunE(rejectCmd, []string{inProgress.ID}))

	reviewable := newWorkflowExitIssue(t, database, models.StatusInReview, false)
	if err := rejectCmd.RunE(rejectCmd, []string{"td-missing-reject", reviewable.ID}); err != nil {
		t.Fatalf("mixed batch with one successful reject must succeed: %v", err)
	}
	got, err := database.GetIssue(reviewable.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
}

func TestCloseExitSemantics(t *testing.T) {
	_, database, sessionID := setupWorkflowExitTest(t)

	alreadyClosed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	if err := closeCmd.RunE(closeCmd, []string{alreadyClosed.ID}); err != nil {
		t.Fatalf("already-closed retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, closeCmd.RunE(closeCmd, []string{"td-missing-close"}))
	requireSilentFailure(t, closeCmd.RunE(closeCmd, []string{alreadyClosed.ID, "td-missing-close"}))

	inReview := newWorkflowExitIssue(t, database, models.StatusInReview, false)
	requireSilentFailure(t, closeCmd.RunE(closeCmd, []string{inReview.ID}))

	closeable := newWorkflowExitIssue(t, database, models.StatusOpen, true)
	if err := closeCmd.RunE(closeCmd, []string{"td-missing-close", closeable.ID}); err != nil {
		t.Fatalf("mixed batch with one successful close must succeed: %v", err)
	}
	got, err := database.GetIssue(closeable.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}

	ownWork := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	ownWork.ImplementerSession = sessionID
	if err := database.UpdateIssue(ownWork); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if err := database.RecordSessionAction(ownWork.ID, sessionID, models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction: %v", err)
	}
	requireSilentFailure(t, closeCmd.RunE(closeCmd, []string{ownWork.ID}))
}

func TestWorkflowCommandJSONIdempotentNoops(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)
	setJSONFlag(t, true)

	started := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	reopened := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)

	cases := []struct {
		name       string
		wantAction string
		run        func() error
	}{
		{name: "start", wantAction: "already started", run: func() error {
			return startCmd.RunE(startCmd, []string{started.ID})
		}},
		{name: "reject", wantAction: "already reopened", run: func() error {
			return rejectCmd.RunE(rejectCmd, []string{reopened.ID})
		}},
		{name: "close", wantAction: "already closed", run: func() error {
			return closeCmd.RunE(closeCmd, []string{closed.ID})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() {
				runErr = tc.run()
			})
			if runErr != nil {
				t.Fatalf("idempotent no-op returned error: %v", runErr)
			}
			var env struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("invalid JSON no-op envelope %q: %v", out, err)
			}
			if env.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", env.Action, tc.wantAction)
			}
		})
	}
}

func TestWorkflowCommandJSONFailuresEmitSingleEnvelope(t *testing.T) {
	_, database, sessionID := setupWorkflowExitTest(t)
	setJSONFlag(t, true)

	ownWork := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	ownWork.ImplementerSession = sessionID
	if err := database.UpdateIssue(ownWork); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if err := database.RecordSessionAction(ownWork.ID, sessionID, models.ActionSessionStarted); err != nil {
		t.Fatalf("RecordSessionAction: %v", err)
	}

	cases := []struct {
		name     string
		wantCode string
		run      func() error
	}{
		{name: "start missing", wantCode: "not_found", run: func() error {
			return startCmd.RunE(startCmd, []string{"td-missing-start-json"})
		}},
		{name: "reject missing", wantCode: "not_found", run: func() error {
			return rejectCmd.RunE(rejectCmd, []string{"td-missing-reject-json"})
		}},
		{name: "close missing", wantCode: "not_found", run: func() error {
			return closeCmd.RunE(closeCmd, []string{"td-missing-close-json"})
		}},
		{name: "close policy rejection", wantCode: "cannot_self_approve", run: func() error {
			return closeCmd.RunE(closeCmd, []string{ownWork.ID})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() {
				runErr = tc.run()
			})
			requireSilentFailure(t, runErr)

			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("expected exactly one JSON envelope, got %q: %v", out, err)
			}
			if env.Error.Code != tc.wantCode || env.Error.Message == "" {
				t.Fatalf("unexpected error envelope: %q", out)
			}
		})
	}
}
