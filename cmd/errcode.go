package cmd

import (
	"errors"
	"fmt"

	"github.com/marcus/td/internal/output"
)

// codedError carries an output error code alongside an error so that an error
// returned all the way up to Execute can still be reported with its real
// failure class. Without it, every error reaching the top level was emitted as
// invalid_input, making a database failure, a missing issue, and a genuinely
// bad flag indistinguishable to a JSON caller reading .error.code.
//
// The wrapper is deliberately thin: it changes nothing about the message or
// about errors.Is/As on the wrapped error, so a call site adopts it by wrapping
// the error it already returns.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }

// Unwrap keeps errors.Is/errors.As working through the wrapper, so wrapping a
// sentinel (e.g. errSilentExit) does not change how it is matched.
func (e *codedError) Unwrap() error { return e.err }

// Code returns the output error code this error should be reported with.
func (e *codedError) Code() string { return e.code }

// withErrorCode tags err with an output error code (see internal/output's
// ErrCode* constants). It returns nil for a nil error, so it is safe to apply
// directly to a call's result. An already-coded error keeps its original code:
// the innermost, most specific classification wins.
func withErrorCode(code string, err error) error {
	if err == nil {
		return nil
	}
	var coded *codedError
	if errors.As(err, &coded) {
		return err
	}
	return &codedError{code: code, err: err}
}

// codedErrorf builds a new error carrying an output error code. It is the
// fmt.Errorf of coded errors and supports %w in the same way.
func codedErrorf(code, format string, args ...interface{}) error {
	return &codedError{code: code, err: fmt.Errorf(format, args...)}
}

// silentError marks an error whose message the command has already delivered
// (a JSON envelope, a tailored human message) while keeping the error itself
// intact. Execute recognises it through errSilentExit and prints nothing more,
// so a command that reports its own failure cannot also have it reported a
// second time — which for a --json caller meant two envelopes on stdout and no
// parseable document at all.
//
// It reports both the wrapped error and errSilentExit through Unwrap, so
// errors.Is/As still see the original error (and any code attached to it)
// while errors.Is(err, errSilentExit) is true.
type silentError struct{ err error }

func (e *silentError) Error() string { return e.err.Error() }

func (e *silentError) Unwrap() []error { return []error{e.err, errSilentExit} }

// alreadyReported marks err as fully reported by the command that produced it.
// It returns nil for a nil error and leaves an already-silent error alone.
func alreadyReported(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errSilentExit) {
		return err
	}
	return &silentError{err: err}
}

// errorCode reports the output error code attached to err, if any.
func errorCode(err error) (string, bool) {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.Code(), true
	}
	return "", false
}

// topLevelErrorCode resolves the error code for the JSON envelope Execute emits
// for an error that reached the top level.
//
// A coded error reports its own code. Everything else falls back to
// invalid_input, which is what the uncoded population is dominated by: cobra's
// own parse and argument-validation failures (unknown flag, unknown command,
// "accepts at most N arg(s)", missing required flag) plus the hand-written
// "issue ID required. Usage: ..." validations. Reporting those as anything else
// would trade one wrong code for another; call sites that fail for a different
// reason should say so with withErrorCode instead.
func topLevelErrorCode(err error) string {
	if code, ok := errorCode(err); ok && code != "" {
		return code
	}
	return output.ErrCodeInvalidInput
}

// emitTopLevelJSONError writes the top-level JSON error envelope for err,
// reporting its real code when it carries one. Split out of Execute so the
// mapping is testable without os.Exit.
func emitTopLevelJSONError(err error) {
	output.JSONError(topLevelErrorCode(err), err.Error())
}
