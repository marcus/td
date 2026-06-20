package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file holds the consolidated, cross-command JSON contract regression
// tests that tie the --json epic together. Per-command shape assertions live in
// create_json_test.go / mutate_json_test.go / transition_json_test.go /
// close_json_test.go; here we assert two epic-wide invariants:
//
//  1. Every key mutating command, in --json mode, emits output that
//     json.Unmarshal parses successfully and that carries an "action" (and,
//     where applicable, an "id").
//  2. The JSON error path: invoking a mutating command on a missing id with
//     --json yields a valid JSON object with an "error" key.

// TestJSONContractParseableAcrossCommands runs each key mutating command in
// --json mode against a fresh issue and asserts the output parses as JSON and
// carries the expected "action"/"id" contract keys. It deliberately re-parses
// rather than asserting exact shapes (those are covered per-command) so that the
// cross-command parse-ability contract is enforced in one place.
func TestJSONContractParseableAcrossCommands(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_json_contract_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)

	// setFlags applies temporary flag values on cmd and registers cleanup that
	// fully resets them, so commands sharing a package-level flag set don't leak
	// state into other tests. stringArray flags accumulate on Set (and Set("")
	// appends an empty element rather than clearing), so cleanup uses the
	// SliceValue.Replace path to truly empty them.
	setFlags := func(cmd *cobra.Command, kv map[string]string) {
		for k, v := range kv {
			if err := cmd.Flags().Set(k, v); err != nil {
				t.Fatalf("set %s flag on %s: %v", k, cmd.Name(), err)
			}
			k, cmd := k, cmd
			t.Cleanup(func() {
				f := cmd.Flags().Lookup(k)
				if f == nil {
					return
				}
				if sv, ok := f.Value.(pflag.SliceValue); ok {
					_ = sv.Replace([]string{})
					f.Changed = false
					return
				}
				_ = cmd.Flags().Set(k, "")
				f.Changed = false
			})
		}
	}

	cases := []struct {
		name       string
		wantAction string
		wantID     bool // envelope should carry a non-empty "id"
		run        func(t *testing.T) string
	}{
		{
			name:       "create",
			wantAction: "created",
			wantID:     true,
			run: func(t *testing.T) string {
				return captureStdout(t, func() {
					if err := createCmd.RunE(createCmd, []string{"Contract create task one"}); err != nil {
						t.Fatalf("create RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "update",
			wantAction: "updated",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract update task")
				setFlags(updateCmd, map[string]string{"priority": "P1"})
				return captureStdout(t, func() {
					if err := updateCmd.RunE(updateCmd, []string{id}); err != nil {
						t.Fatalf("update RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "start",
			wantAction: "started",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract start task")
				return captureStdout(t, func() {
					if err := startCmd.RunE(startCmd, []string{id}); err != nil {
						t.Fatalf("start RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "log",
			wantAction: "logged",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract log task")
				return captureStdout(t, func() {
					if err := logCmd.RunE(logCmd, []string{id, "made some progress"}); err != nil {
						t.Fatalf("log RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "handoff",
			wantAction: "handoff_recorded",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract handoff task")
				setFlags(handoffCmd, map[string]string{"done": "did x", "remaining": "do y"})
				return captureStdout(t, func() {
					if err := handoffCmd.RunE(handoffCmd, []string{id}); err != nil {
						t.Fatalf("handoff RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "defer",
			wantAction: "deferred",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract defer task")
				return captureStdout(t, func() {
					if err := deferCmd.RunE(deferCmd, []string{id, "2026-12-31"}); err != nil {
						t.Fatalf("defer RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "block",
			wantAction: "blocked",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract block task")
				setFlags(blockCmd, map[string]string{"reason": "waiting"})
				return captureStdout(t, func() {
					if err := blockCmd.RunE(blockCmd, []string{id}); err != nil {
						t.Fatalf("block RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "unblock",
			wantAction: "unblocked",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract unblock task")
				setIssueStatus(t, database, id, models.StatusBlocked)
				return captureStdout(t, func() {
					if err := unblockCmd.RunE(unblockCmd, []string{id}); err != nil {
						t.Fatalf("unblock RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "unstart",
			wantAction: "unstarted",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract unstart task")
				setIssueStatus(t, database, id, models.StatusInProgress)
				return captureStdout(t, func() {
					if err := unstartCmd.RunE(unstartCmd, []string{id}); err != nil {
						t.Fatalf("unstart RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "reopen",
			wantAction: "reopened",
			wantID:     true,
			run: func(t *testing.T) string {
				id := newJSONTestIssue(t, database, "Contract reopen task")
				setIssueStatus(t, database, id, models.StatusClosed)
				return captureStdout(t, func() {
					if err := reopenCmd.RunE(reopenCmd, []string{id}); err != nil {
						t.Fatalf("reopen RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "link",
			wantAction: "dependency_added",
			wantID:     false, // link emits from/to, not a top-level id
			run: func(t *testing.T) string {
				from := newJSONTestIssue(t, database, "Contract link from task")
				to := newJSONTestIssue(t, database, "Contract link to task")
				setFlags(linkCmd, map[string]string{"depends-on": to})
				return captureStdout(t, func() {
					if err := linkCmd.RunE(linkCmd, []string{from}); err != nil {
						t.Fatalf("link RunE: %v", err)
					}
				})
			},
		},
		{
			name:       "note_add",
			wantAction: "note_created",
			wantID:     true,
			run: func(t *testing.T) string {
				setFlags(noteAddCmd, map[string]string{"content": "contract note body"})
				return captureStdout(t, func() {
					if err := noteAddCmd.RunE(noteAddCmd, []string{"Contract note title"}); err != nil {
						t.Fatalf("note add RunE: %v", err)
					}
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.run(t)

			// Cross-command invariant #1: the output must parse as a single JSON
			// object.
			var env map[string]any
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("%s --json output is not valid JSON: %v\noutput: %q", tc.name, err, out)
			}

			action, ok := env["action"].(string)
			if !ok || action == "" {
				t.Fatalf("%s --json envelope missing string \"action\": %q", tc.name, out)
			}
			if action != tc.wantAction {
				t.Fatalf("%s --json action = %q, want %q", tc.name, action, tc.wantAction)
			}

			if tc.wantID {
				id, ok := env["id"].(string)
				if !ok || id == "" {
					t.Fatalf("%s --json envelope missing non-empty \"id\": %q", tc.name, out)
				}
			}
		})
	}
}

// TestJSONContractErrorEnvelopeShape asserts the JSON error envelope shape that
// commands emit at the command level. `close` (and the review family) emit a
// per-id JSON error line on a missing id, so we can capture and parse it here
// without going through os.Exit. The envelope must be a valid JSON object with
// an "error" key carrying a non-empty code and message.
func TestJSONContractErrorEnvelopeShape(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_json_contract_err_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)

	out := captureStdout(t, func() {
		// A missing id is a per-issue skip for close, so RunE returns nil but
		// emits a JSON error line for the id (no os.Exit involved).
		_ = closeCmd.RunE(closeCmd, []string{"td-doesnotexist"})
	})

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		t.Fatalf("close --json on missing id produced no error envelope")
	}
	if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
		t.Fatalf("close --json error output is not valid JSON: %v\noutput: %q", err, out)
	}
	if env.Error.Code == "" {
		t.Fatalf("error envelope missing non-empty error.code: %q", out)
	}
	if env.Error.Message == "" {
		t.Fatalf("error envelope missing non-empty error.message: %q", out)
	}
}

// TestJSONContractMutationsErrorOnMissingID asserts the other half of the error
// contract: mutating commands that resolve to the top-level JSON error handler
// (rather than emitting at the command level) still surface the failure as a
// RunE error when given a non-existent id. Together with
// TestJSONContractErrorEnvelopeShape this covers both the envelope shape and the
// RunE-returns-error behavior across the mutating surface.
func TestJSONContractMutationsErrorOnMissingID(t *testing.T) {
	saveAndRestoreGlobals(t)
	t.Setenv("TD_SESSION_ID", "ses_json_contract_err2_test")

	dir := t.TempDir()
	baseDir := dir
	baseDirOverride = &baseDir

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	setJSONFlag(t, true)

	const missing = "td-doesnotexist"

	cases := []struct {
		name string
		run  func() error
	}{
		{
			name: "log",
			run:  func() error { return logCmd.RunE(logCmd, []string{missing, "a message that is long enough"}) },
		},
		{
			name: "handoff",
			run: func() error {
				if err := handoffCmd.Flags().Set("done", "x"); err != nil {
					t.Fatalf("set done flag: %v", err)
				}
				t.Cleanup(func() {
					f := handoffCmd.Flags().Lookup("done")
					if sv, ok := f.Value.(pflag.SliceValue); ok {
						_ = sv.Replace([]string{})
						f.Changed = false
					}
				})
				return handoffCmd.RunE(handoffCmd, []string{missing})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatalf("%s on missing id: expected RunE to return an error, got nil", tc.name)
			}
		})
	}
}
