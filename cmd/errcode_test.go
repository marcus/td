package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

// cobraUnknownFlagError produces a genuine cobra usage error, the population the
// top-level envelope's invalid_input fallback is actually for.
func cobraUnknownFlagError(t *testing.T) error {
	t.Helper()
	c := &cobra.Command{
		Use:           "probe",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          func(*cobra.Command, []string) error { return nil },
	}
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetArgs([]string{"--definitely-not-a-flag"})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected cobra to reject an unknown flag")
	}
	return err
}

// TestTopLevelErrorCodeMapping pins the mapping the top-level JSON envelope
// uses: a coded error reports its own class, and only uncoded errors (cobra
// usage failures and the like) fall back to invalid_input.
func TestTopLevelErrorCodeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "database failure keeps database_error",
			err:  withErrorCode(output.ErrCodeDatabaseError, errors.New("database not found: run 'td init' first")),
			want: output.ErrCodeDatabaseError,
		},
		{
			name: "missing issue keeps not_found",
			err:  codedErrorf(output.ErrCodeNotFound, "issue not found: %s", "td-missing"),
			want: output.ErrCodeNotFound,
		},
		{
			name: "code survives further wrapping",
			err:  fmt.Errorf("close td-abc1: %w", withErrorCode(output.ErrCodeConflict, errors.New("stale write"))),
			want: output.ErrCodeConflict,
		},
		{
			name: "innermost code wins",
			err:  withErrorCode(output.ErrCodeDatabaseError, withErrorCode(output.ErrCodeNotFound, errors.New("nope"))),
			want: output.ErrCodeNotFound,
		},
		{
			name: "uncoded error falls back to invalid_input",
			err:  errors.New("issue ID required. Usage: td show <issue-id>"),
			want: output.ErrCodeInvalidInput,
		},
		{
			name: "cobra usage error falls back to invalid_input",
			err:  cobraUnknownFlagError(t),
			want: output.ErrCodeInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topLevelErrorCode(tt.err); got != tt.want {
				t.Errorf("topLevelErrorCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// TestWithErrorCodePreservesIdentity checks that tagging a code does not change
// how the error is matched or what it says — call sites must be able to adopt
// it without altering errors.Is behaviour (notably for errSilentExit).
func TestWithErrorCodePreservesIdentity(t *testing.T) {
	if err := withErrorCode(output.ErrCodeDatabaseError, nil); err != nil {
		t.Fatalf("withErrorCode(nil) = %v, want nil", err)
	}

	base := fmt.Errorf("no issues started: %w", errSilentExit)
	coded := withErrorCode(output.ErrCodeDatabaseError, base)
	if !errors.Is(coded, errSilentExit) {
		t.Error("coded error must still match errSilentExit")
	}
	if coded.Error() != base.Error() {
		t.Errorf("message = %q, want %q", coded.Error(), base.Error())
	}
	if code, ok := errorCode(coded); !ok || code != output.ErrCodeDatabaseError {
		t.Errorf("errorCode = (%q, %v), want (%q, true)", code, ok, output.ErrCodeDatabaseError)
	}
	if _, ok := errorCode(base); ok {
		t.Error("uncoded error must not report a code")
	}
}

// TestEmitTopLevelJSONErrorEnvelope verifies the envelope Execute writes for a
// JSON caller carries the real code and message.
func TestEmitTopLevelJSONErrorEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"database", withErrorCode(output.ErrCodeDatabaseError, errors.New("database not found: run 'td init' first")), output.ErrCodeDatabaseError},
		{"usage", cobraUnknownFlagError(t), output.ErrCodeInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() { emitTopLevelJSONError(tt.err) })

			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
				t.Fatalf("envelope is not valid JSON (%v): %q", err, out)
			}
			if env.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", env.Error.Code, tt.wantCode)
			}
			if env.Error.Message != tt.err.Error() {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.err.Error())
			}
		})
	}
}

// TestCommandErrorsCarryTheirCode covers the converted call sites end to end: a
// missing database and a missing issue must not reach the top level looking
// like bad input.
func TestCommandErrorsCarryTheirCode(t *testing.T) {
	saveAndRestoreGlobals(t)

	uninitialized := t.TempDir()
	baseDirOverride = &uninitialized

	err := showCmd.RunE(showCmd, []string{"td-anything"})
	if err == nil {
		t.Fatal("show against an uninitialized project must fail")
	}
	if got := topLevelErrorCode(err); got != output.ErrCodeDatabaseError {
		t.Errorf("uninitialized project: code = %q, want %q (err: %v)", got, output.ErrCodeDatabaseError, err)
	}

	initialized := t.TempDir()
	baseDirOverride = &initialized
	database, dbErr := db.Initialize(initialized)
	if dbErr != nil {
		t.Fatalf("Initialize: %v", dbErr)
	}
	t.Cleanup(func() { _ = database.Close() })

	// A real issue exists, so the failure below is genuinely "that id is not
	// here" rather than an empty database.
	if err := database.CreateIssue(&models.Issue{Title: "present", Status: models.StatusOpen}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	err = showCmd.RunE(showCmd, []string{"td-missing-999"})
	if err == nil {
		t.Fatal("show of a missing issue must fail")
	}
	if got := topLevelErrorCode(err); got != output.ErrCodeNotFound {
		t.Errorf("missing issue: code = %q, want %q (err: %v)", got, output.ErrCodeNotFound, err)
	}
}
