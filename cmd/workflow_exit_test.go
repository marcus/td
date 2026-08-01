package cmd

import (
	"encoding/json"
	"errors"
	"strings"
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
	_, database, sessionID := setupWorkflowExitTest(t)

	alreadyStarted := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	alreadyStarted.ImplementerSession = sessionID
	if err := database.UpdateIssue(alreadyStarted); err != nil {
		t.Fatalf("UpdateIssue same-owner start: %v", err)
	}
	if err := startCmd.RunE(startCmd, []string{alreadyStarted.ID}); err != nil {
		t.Fatalf("already-started retry must be an idempotent success: %v", err)
	}

	otherOwner := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	otherOwner.ImplementerSession = "ses_other_implementer"
	if err := database.UpdateIssue(otherOwner); err != nil {
		t.Fatalf("UpdateIssue different-owner start: %v", err)
	}
	var otherOwnerErr error
	otherOwnerOut := captureStdout(t, func() {
		otherOwnerErr = startCmd.RunE(startCmd, []string{otherOwner.ID})
	})
	requireSilentFailure(t, otherOwnerErr)
	if strings.Count(otherOwnerOut, "cannot start "+otherOwner.ID) != 1 {
		t.Fatalf("different-owner failure must be emitted once, got %q", otherOwnerOut)
	}
	if strings.Contains(otherOwnerOut, "Usage:") {
		t.Fatalf("different-owner failure must not include Cobra usage, got %q", otherOwnerOut)
	}
	gotOtherOwner, err := database.GetIssue(otherOwner.ID)
	if err != nil {
		t.Fatalf("GetIssue different-owner start: %v", err)
	}
	if gotOtherOwner.ImplementerSession != "ses_other_implementer" {
		t.Fatalf("implementer = %q, want unchanged owner", gotOtherOwner.ImplementerSession)
	}

	emptyOwner := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	requireSilentFailure(t, startCmd.RunE(startCmd, []string{emptyOwner.ID}))

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
	_, database, sessionID := setupWorkflowExitTest(t)
	setJSONFlag(t, true)

	started := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	started.ImplementerSession = sessionID
	if err := database.UpdateIssue(started); err != nil {
		t.Fatalf("UpdateIssue same-owner start: %v", err)
	}
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
	otherOwner := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	otherOwner.ImplementerSession = "ses_other_implementer"
	if err := database.UpdateIssue(otherOwner); err != nil {
		t.Fatalf("UpdateIssue different-owner start: %v", err)
	}

	cases := []struct {
		name     string
		wantCode string
		run      func() error
	}{
		{name: "start missing", wantCode: "not_found", run: func() error {
			return startCmd.RunE(startCmd, []string{"td-missing-start-json"})
		}},
		{name: "start different owner", wantCode: "invalid_input", run: func() error {
			return startCmd.RunE(startCmd, []string{otherOwner.ID})
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

// TestUnstartExitSemantics covers td-34c833 for unstart: missing and rejected
// targets must exit non-zero, an already-open retry stays exit 0, and a mixed
// batch containing one real unstart stays exit 0.
func TestUnstartExitSemantics(t *testing.T) {
	_, database, sessionID := setupWorkflowExitTest(t)

	alreadyOpen := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := unstartCmd.RunE(unstartCmd, []string{alreadyOpen.ID}); err != nil {
		t.Fatalf("already-open retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, unstartCmd.RunE(unstartCmd, []string{"td-missing-unstart"}))
	requireSilentFailure(t, unstartCmd.RunE(unstartCmd, []string{alreadyOpen.ID, "td-missing-unstart"}))

	inReview := newWorkflowExitIssue(t, database, models.StatusInReview, false)
	requireSilentFailure(t, unstartCmd.RunE(unstartCmd, []string{inReview.ID}))
	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	requireSilentFailure(t, unstartCmd.RunE(unstartCmd, []string{closed.ID}))

	claimed := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	claimed.ImplementerSession = sessionID
	if err := database.UpdateIssue(claimed); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if err := unstartCmd.RunE(unstartCmd, []string{"td-missing-unstart", claimed.ID}); err != nil {
		t.Fatalf("mixed batch with one successful unstart must succeed: %v", err)
	}
	got, err := database.GetIssue(claimed.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusOpen || got.ImplementerSession != "" {
		t.Fatalf("status = %s implementer = %q, want open with no implementer", got.Status, got.ImplementerSession)
	}
}

// TestBlockExitSemantics covers td-34c833 for block.
func TestBlockExitSemantics(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)

	alreadyBlocked := newWorkflowExitIssue(t, database, models.StatusBlocked, false)
	if err := blockCmd.RunE(blockCmd, []string{alreadyBlocked.ID}); err != nil {
		t.Fatalf("already-blocked retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, blockCmd.RunE(blockCmd, []string{"td-missing-block"}))
	requireSilentFailure(t, blockCmd.RunE(blockCmd, []string{alreadyBlocked.ID, "td-missing-block"}))

	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	requireSilentFailure(t, blockCmd.RunE(blockCmd, []string{closed.ID}))

	blockable := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := blockCmd.RunE(blockCmd, []string{"td-missing-block", blockable.ID}); err != nil {
		t.Fatalf("mixed batch with one successful block must succeed: %v", err)
	}
	got, err := database.GetIssue(blockable.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusBlocked {
		t.Fatalf("status = %s, want blocked", got.Status)
	}
}

// TestReopenExitSemantics covers td-34c833 for reopen.
func TestReopenExitSemantics(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)

	alreadyOpen := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := reopenCmd.RunE(reopenCmd, []string{alreadyOpen.ID}); err != nil {
		t.Fatalf("already-open retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, reopenCmd.RunE(reopenCmd, []string{"td-missing-reopen"}))
	requireSilentFailure(t, reopenCmd.RunE(reopenCmd, []string{alreadyOpen.ID, "td-missing-reopen"}))

	inProgress := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	requireSilentFailure(t, reopenCmd.RunE(reopenCmd, []string{inProgress.ID}))

	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	if err := reopenCmd.RunE(reopenCmd, []string{"td-missing-reopen", closed.ID}); err != nil {
		t.Fatalf("mixed batch with one successful reopen must succeed: %v", err)
	}
	got, err := database.GetIssue(closed.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
}

// TestUnblockExitSemantics covers td-34c833 for unblock.
func TestUnblockExitSemantics(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)

	alreadyOpen := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	if err := unblockCmd.RunE(unblockCmd, []string{alreadyOpen.ID}); err != nil {
		t.Fatalf("already-open retry must be an idempotent success: %v", err)
	}

	requireSilentFailure(t, unblockCmd.RunE(unblockCmd, []string{"td-missing-unblock"}))
	requireSilentFailure(t, unblockCmd.RunE(unblockCmd, []string{alreadyOpen.ID, "td-missing-unblock"}))

	inProgress := newWorkflowExitIssue(t, database, models.StatusInProgress, false)
	requireSilentFailure(t, unblockCmd.RunE(unblockCmd, []string{inProgress.ID}))

	blocked := newWorkflowExitIssue(t, database, models.StatusBlocked, false)
	if err := unblockCmd.RunE(unblockCmd, []string{"td-missing-unblock", blocked.ID}); err != nil {
		t.Fatalf("mixed batch with one successful unblock must succeed: %v", err)
	}
	got, err := database.GetIssue(blocked.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != models.StatusOpen {
		t.Fatalf("status = %s, want open", got.Status)
	}
}

// TestTransitionCommandJSONFailuresIdentifyTheTarget asserts that a --json
// caller can tell which target failed and why: exactly one error envelope,
// carrying the id in its message, and a non-zero exit.
func TestTransitionCommandJSONFailuresIdentifyTheTarget(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)
	setJSONFlag(t, true)

	closed := newWorkflowExitIssue(t, database, models.StatusClosed, false)
	inProgress := newWorkflowExitIssue(t, database, models.StatusInProgress, false)

	cases := []struct {
		name     string
		wantCode string
		wantID   string
		run      func() error
	}{
		{name: "unstart missing", wantCode: "not_found", wantID: "td-missing-unstart-json", run: func() error {
			return unstartCmd.RunE(unstartCmd, []string{"td-missing-unstart-json"})
		}},
		{name: "unstart rejected", wantCode: "invalid_input", wantID: closed.ID, run: func() error {
			return unstartCmd.RunE(unstartCmd, []string{closed.ID})
		}},
		{name: "block missing", wantCode: "not_found", wantID: "td-missing-block-json", run: func() error {
			return blockCmd.RunE(blockCmd, []string{"td-missing-block-json"})
		}},
		{name: "block rejected", wantCode: "invalid_input", wantID: closed.ID, run: func() error {
			return blockCmd.RunE(blockCmd, []string{closed.ID})
		}},
		{name: "reopen missing", wantCode: "not_found", wantID: "td-missing-reopen-json", run: func() error {
			return reopenCmd.RunE(reopenCmd, []string{"td-missing-reopen-json"})
		}},
		{name: "reopen rejected", wantCode: "invalid_input", wantID: inProgress.ID, run: func() error {
			return reopenCmd.RunE(reopenCmd, []string{inProgress.ID})
		}},
		{name: "unblock missing", wantCode: "not_found", wantID: "td-missing-unblock-json", run: func() error {
			return unblockCmd.RunE(unblockCmd, []string{"td-missing-unblock-json"})
		}},
		{name: "unblock rejected", wantCode: "invalid_input", wantID: inProgress.ID, run: func() error {
			return unblockCmd.RunE(unblockCmd, []string{inProgress.ID})
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
			if env.Error.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q (%q)", env.Error.Code, tc.wantCode, out)
			}
			if !strings.Contains(env.Error.Message, tc.wantID) {
				t.Fatalf("message %q does not identify target %q", env.Error.Message, tc.wantID)
			}
		})
	}
}

// TestTransitionCommandJSONNoopsStayExitZero asserts idempotent retries emit a
// parseable no-op envelope and keep exit 0.
func TestTransitionCommandJSONNoopsStayExitZero(t *testing.T) {
	_, database, _ := setupWorkflowExitTest(t)
	setJSONFlag(t, true)

	open := newWorkflowExitIssue(t, database, models.StatusOpen, false)
	blocked := newWorkflowExitIssue(t, database, models.StatusBlocked, false)

	cases := []struct {
		name       string
		wantAction string
		run        func() error
	}{
		{name: "unstart", wantAction: "already unstarted", run: func() error {
			return unstartCmd.RunE(unstartCmd, []string{open.ID})
		}},
		{name: "block", wantAction: "already blocked", run: func() error {
			return blockCmd.RunE(blockCmd, []string{blocked.ID})
		}},
		{name: "reopen", wantAction: "already reopened", run: func() error {
			return reopenCmd.RunE(reopenCmd, []string{open.ID})
		}},
		{name: "unblock", wantAction: "already unblocked", run: func() error {
			return unblockCmd.RunE(unblockCmd, []string{open.ID})
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
				ID     string `json:"id"`
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("invalid JSON no-op envelope %q: %v", out, err)
			}
			if env.Action != tc.wantAction || env.ID == "" {
				t.Fatalf("unexpected no-op envelope: %q", out)
			}
		})
	}
}
