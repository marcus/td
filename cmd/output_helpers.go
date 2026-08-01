package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

// failTransition reports a single per-issue mutation failure exactly once: a
// JSON error envelope for --json callers, a human warning otherwise. The
// caller counts the failure and decides the batch's exit status; this only
// emits.
func failTransition(isJSON bool, code, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	if isJSON {
		output.JSONError(code, message)
		return
	}
	output.Warning("%s", message)
}

// reportFailure reports a command-fatal failure exactly once — a JSON error
// envelope for --json callers, the human ERROR line otherwise — and returns an
// error the top level will not report again.
//
// The returned error keeps err's message, carries code so anything that does
// inspect it classifies it correctly, and is marked already-reported so
// Execute only sets the exit status. Returning the bare error instead is what
// produced two JSON envelopes on stdout (and a duplicated human message).
func reportFailure(isJSON bool, code string, err error) error {
	if err == nil {
		return nil
	}
	if isJSON {
		output.JSONError(code, err.Error())
	} else {
		output.Error("%v", err)
	}
	return alreadyReported(withErrorCode(code, err))
}

// noopTransition reports an idempotent retry — the requested end state already
// holds — exactly once. This is a success, not a failure: the batch keeps
// exit 0. action reads as a phrase ("already blocked") to match the envelope
// the start/reject/close family already emits.
func noopTransition(isJSON bool, issueID string, status models.Status, action string) {
	message := fmt.Sprintf("%s %s", action, issueID)
	if !isJSON {
		output.Warning("%s", message)
		return
	}
	if err := output.JSON(map[string]interface{}{
		"id":      issueID,
		"status":  string(status),
		"action":  action,
		"message": message,
	}); err != nil {
		output.JSONError(output.ErrCodeDatabaseError, err.Error())
	}
}

// jsonMode reports whether --json was requested, checking the command's own
// flag first (for commands that still define a local --json) then the
// inherited persistent flag. This avoids cobra's local-shadows-persistent
// gotcha during the migration window: cmd.Flags() resolves both a locally
// registered flag and the inherited persistent flag, preferring the local one.
//
// It is intentionally robust: if the "json" flag does not exist on the command
// (e.g. in a test that builds a bare command), GetBool returns an error and we
// treat that as "not json mode".
func jsonMode(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	// Trigger cobra's lazy merge of inherited persistent flags into the
	// command's own flag set. cmd.Flags() alone does NOT merge parent
	// persistent flags; InheritedFlags() calls mergePersistentFlags(), which
	// adds them into cmd.Flags(). At normal runtime parsing has already merged
	// them, but calling this makes jsonMode correct even when invoked outside
	// the parse path (and in tests that build commands directly).
	_ = cmd.InheritedFlags()
	v, err := cmd.Flags().GetBool("json")
	if err != nil {
		return false
	}
	return v
}

// jsonList normalizes a possibly-nil slice for --json output.
//
// A nil slice marshals to `null`, which forces every agent caller to special
// case "no results" before it can range over the answer. Every list-shaped
// --json surface must emit `[]` instead, so read paths funnel their slice
// through here on the way to output.JSON.
func jsonList[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
