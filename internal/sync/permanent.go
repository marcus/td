package sync

import (
	"errors"
	"strings"
)

// IsPermanentApplyError reports whether a per-event apply failure is guaranteed
// to recur on every retry, and so must be quarantined rather than retried.
//
// # The rule
//
// An apply failure is PERMANENT when it is a pure function of the event itself
// and of local state that no later pull can change:
//
//   - the payload cannot be decoded, or carries no usable fields;
//   - the entity type or action type is one this client does not implement;
//   - the entity ID is empty;
//   - the statement violated a schema constraint (FOREIGN KEY, UNIQUE, NOT
//     NULL, CHECK).
//
// Replaying such an event reproduces the identical failure forever, because
// events are delivered in server_seq order: nothing that has not already
// arrived can arrive earlier and make the event valid.
//
// An apply failure is TRANSIENT — and therefore retried with the cursor
// preserved, exactly as before — when it comes from the environment rather than
// the event: SQLITE_BUSY / database is locked, I/O errors, disk full, a closed
// or cancelled connection, timeouts. Those succeed on a later attempt and must
// never be quarantined, because quarantining one would skip an event that was
// perfectly valid.
//
// The default is TRANSIENT. An error this function does not recognise is
// retried, which preserves the old behaviour: a peer stalls loudly rather than
// silently dropping an event whose nature we could not establish. Quarantine is
// opt-in per error class, never a catch-all.
//
// Matching is on error text rather than driver error types on purpose: td links
// two SQLite drivers (mattn/go-sqlite3 and modernc.org/sqlite) with different
// error types, and the store interface is meant to stay driver-agnostic.
func IsPermanentApplyError(err error) bool {
	if err == nil {
		return false
	}

	// Structured cases first — no string matching needed.
	var orphan *OrphanedParentError
	if errors.As(err, &orphan) {
		return true
	}

	msg := strings.ToLower(err.Error())

	// Transient wins over permanent: a constraint check aborted by a lock
	// timeout can mention both, and retrying is always the safe reading.
	for _, t := range transientMarkers {
		if strings.Contains(msg, t) {
			return false
		}
	}
	for _, p := range permanentMarkers {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// transientMarkers are environment failures that a retry can clear.
var transientMarkers = []string{
	"database is locked",
	"database table is locked",
	"sqlite_busy",
	"sqlite_locked",
	"disk i/o error",
	"database or disk is full",
	"unable to open database",
	"no such file or directory",
	"connection is already closed",
	"bad connection",
	"context deadline exceeded",
	"context canceled",
	"i/o timeout",
	"interrupted",
}

// permanentMarkers are defects in the event or in schema compatibility. Each
// recurs identically on every replay.
var permanentMarkers = []string{
	"constraint failed",
	"constraint violation",
	"invalid entity type",
	"unknown action type",
	"empty entity id",
	"payload has no fields",
	"unmarshal payload",
	"nil payload",
	"invalid table name",
	"no such table",
	"no such column",
	"datatype mismatch",
}
