package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

// TestAlreadyReportedIsSilentButKeepsIdentity pins the contract the fix relies
// on: an already-reported error still says what it said, still carries its
// code, and additionally matches errSilentExit so Execute prints nothing more.
func TestAlreadyReportedIsSilentButKeepsIdentity(t *testing.T) {
	if err := alreadyReported(nil); err != nil {
		t.Fatalf("alreadyReported(nil) = %v, want nil", err)
	}

	base := withErrorCode(output.ErrCodeNotFound, errors.New("issue not found: td-abc1"))
	silent := alreadyReported(base)

	if !errors.Is(silent, errSilentExit) {
		t.Error("already-reported error must match errSilentExit")
	}
	if silent.Error() != base.Error() {
		t.Errorf("message = %q, want %q", silent.Error(), base.Error())
	}
	if got := topLevelErrorCode(silent); got != output.ErrCodeNotFound {
		t.Errorf("code = %q, want %q", got, output.ErrCodeNotFound)
	}
	if again := alreadyReported(silent); again != silent {
		t.Error("marking an already-silent error twice must be a no-op")
	}
}

// TestReportFailureEmitsExactlyOneEnvelope covers the helper directly: one
// envelope on stdout for a JSON caller, and a return value the top level will
// not print a second time.
func TestReportFailureEmitsExactlyOneEnvelope(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		err = reportFailure(true, output.ErrCodeDatabaseError, errors.New("database not found: run 'td init' first"))
	})

	assertSingleJSONDocument(t, out, output.ErrCodeDatabaseError)
	if !errors.Is(err, errSilentExit) {
		t.Fatalf("returned error must be silent, got %v", err)
	}
}

// TestShowMissingIssueJSONIsOneDocument is the regression test for td-d762a5:
// `td show <missing> --json` used to write its own envelope and then return a
// bare error that Execute rendered again, leaving stdout unparseable.
func TestShowMissingIssueJSONIsOneDocument(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDirOverride = &dir
	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.CreateIssue(&models.Issue{Title: "present", Status: models.StatusOpen}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	withJSONFlag(t, showCmd)

	var runErr error
	out := captureStdout(t, func() {
		runErr = showCmd.RunE(showCmd, []string{"td-missing-999"})
	})

	assertSingleJSONDocument(t, out, output.ErrCodeNotFound)

	if runErr == nil {
		t.Fatal("show of a missing issue must fail")
	}
	// The command reported the failure itself, so Execute must add nothing —
	// otherwise stdout carries a second envelope.
	if !errors.Is(runErr, errSilentExit) {
		t.Errorf("returned error must be silent, got %v", runErr)
	}
	if got := topLevelErrorCode(runErr); got != output.ErrCodeNotFound {
		t.Errorf("code = %q, want %q", got, output.ErrCodeNotFound)
	}
}

// TestShowUninitializedJSONIsOneDocument covers the same path when the project
// has no database at all: still one envelope, still silent, and classified as
// a database failure rather than bad input.
func TestShowUninitializedJSONIsOneDocument(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	baseDirOverride = &dir

	withJSONFlag(t, showCmd)

	var runErr error
	out := captureStdout(t, func() {
		runErr = showCmd.RunE(showCmd, []string{"td-anything"})
	})

	assertSingleJSONDocument(t, out, output.ErrCodeDatabaseError)
	if runErr == nil {
		t.Fatal("show against an uninitialized project must fail")
	}
	if !errors.Is(runErr, errSilentExit) {
		t.Errorf("returned error must be silent, got %v", runErr)
	}
}

// withJSONFlag turns --json on for the duration of a test and restores it, so
// the shared command singletons are not left in JSON mode for other tests.
func withJSONFlag(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	_ = cmd.InheritedFlags()
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set --json: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Flags().Set("json", "false") })
}

// assertSingleJSONDocument fails unless out is exactly one JSON error envelope
// carrying wantCode. "Exactly one" is the whole point: two envelopes parse
// individually but make stdout unparseable as a document.
func assertSingleJSONDocument(t *testing.T, out, wantCode string) {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout must be exactly one JSON document, got %d lines: %q", len(lines), out)
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not valid JSON (%v): %q", err, out)
	}
	if dec.More() {
		t.Fatalf("stdout carries more than one JSON document: %q", out)
	}
	if env.Error.Code != wantCode {
		t.Errorf("code = %q, want %q", env.Error.Code, wantCode)
	}
	if env.Error.Message == "" {
		t.Error("envelope must carry a message")
	}
}
