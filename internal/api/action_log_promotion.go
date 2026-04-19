// Package api: post-commit promotion of project.db action_log rows into the
// project's per-project events.db.
//
// This file implements Stream 3.1 of the td-watch rich UI plan
// (docs/td-watch-rich-ui-plan.md §7.1). It runs after every mutation handler
// that wrote to project.db via wrapServeHandler and atomically (a) inserts
// the corresponding events into events.db and (b) flips action_log.synced_at
// + .server_seq on the source rows.
//
// Key design points:
//
//  1. Atomicity: SQLite ATTACH DATABASE is used so a single transaction
//     spans both project.db and events.db. On any error we ROLLBACK and
//     return; the action_log rows stay synced_at IS NULL so the *next* call
//     into the same project will retry. This is the recovery valve called
//     out in the plan and we deliberately do NOT add a separate outbox.
//
//  2. Per-row session preservation: we use sync.GetPendingEventsPreserveSession
//     (NOT GetPendingEvents) so each event keeps the session_id stamped on
//     its action_log row by resolveTdWatchSession (e.g. "twu_<user>",
//     "twa_<admin>_as_<target>"). GetPendingEvents would overwrite all
//     events with a single caller-supplied session — wrong for this path.
//
//  3. Device id is the constant TdWatchServerDeviceID = "td_watch_server"
//     (plan §10 Q2). td-watch is not a /sync/pull client, so a single
//     server-originated device id keeps the events dedupe key
//     (device_id, session_id, client_action_id) intact without per-browser
//     device registration.
package api

import (
	"fmt"
	"path/filepath"

	tddb "github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

// promoteActionLog promotes any unsynced action_log rows in projectDB into
// the project's events.db, preserving each row's session_id and stamping
// synced_at + server_seq atomically. Returns the number of events successfully
// inserted.
//
// The events.db file is expected to live at
//
//	{ProjectDataDir}/{projectID}/events.db
//
// (the same path ProjectLivePool.eventsDBPath produces). It is opened by
// SQLite via ATTACH DATABASE under the alias `events_db`, so the events
// table is addressable as `events_db.events`. The events table must already
// exist in events.db (created by ProjectDBPool.openProjectDB → InitServerEventLog
// the first time the project is touched by the existing /v1/sync/push path).
// For projects that have never seen a sync push, this function lazily ensures
// the events table exists in events.db by re-using InitServerEventLog before
// the ATTACH so promotion works for brand-new td-watch-only projects.
//
// On error: rolls back; action_log.synced_at stays NULL → next call retries.
// This is the recovery valve. Callers should log the error but should not
// fail the user-facing response — the write into project.db has already
// succeeded.
func (s *Server) promoteActionLog(projectID string, projectDB *tddb.DB) (int, error) {
	if projectID == "" {
		return 0, fmt.Errorf("promoteActionLog: empty projectID")
	}
	if projectDB == nil {
		return 0, fmt.Errorf("promoteActionLog: nil projectDB")
	}

	eventsPath := s.eventsDBPathFor(projectID)

	// Make sure events.db exists and has the events table. The dbpool's
	// openProjectDB already does this on first /v1/sync/push, but a project
	// that has only ever been written via the REST handlers won't have hit
	// that path. ensureEventsDB is a cheap idempotent open+InitServerEventLog
	// + close — the ATTACH below uses a separate connection on projectDB so
	// we don't fight it for the lock.
	if err := ensureEventsDBSchema(eventsPath); err != nil {
		return 0, fmt.Errorf("ensure events.db schema: %w", err)
	}

	conn := projectDB.Conn()

	// SQLite cannot ATTACH inside a transaction, so do it on the connection
	// before BEGIN. Use a path-quoted ATTACH to tolerate spaces / unusual
	// characters in the data dir.
	attachSQL := fmt.Sprintf("ATTACH DATABASE %s AS events_db", sqliteQuote(eventsPath))
	if _, err := conn.Exec(attachSQL); err != nil {
		return 0, fmt.Errorf("attach events.db: %w", err)
	}
	defer func() {
		// Best-effort detach so the project handle is left in a clean state
		// for the next acquire. Errors here are non-fatal; the next ATTACH
		// would just fail with "already attached" and we'd recover.
		_, _ = conn.Exec("DETACH DATABASE events_db")
	}()

	tx, err := conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin promotion tx: %w", err)
	}

	pending, err := tdsync.GetPendingEventsPreserveSession(tx, TdWatchServerDeviceID)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("get pending action_log: %w", err)
	}
	if len(pending) == 0 {
		_ = tx.Rollback()
		return 0, nil
	}

	// Insert into events_db.events. InsertServerEventsAttached writes against
	// the attached schema in the same tx so the synced_at flip below commits
	// or rolls back together with it.
	pushResult, err := tdsync.InsertServerEventsAttached(tx, "events_db", pending)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("insert events into events.db: %w", err)
	}

	// Stamp action_log.synced_at + server_seq for every accepted event AND
	// for duplicates whose existing server_seq we recovered (idempotent
	// retry path: another goroutine already promoted the row).
	for _, ack := range pushResult.Acks {
		if _, err := tx.Exec(
			`UPDATE main.action_log SET synced_at = CURRENT_TIMESTAMP, server_seq = ? WHERE rowid = ?`,
			ack.ServerSeq, ack.ClientActionID,
		); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("mark action_log synced rowid=%d: %w", ack.ClientActionID, err)
		}
	}
	for _, rej := range pushResult.Rejected {
		// Duplicate rejections come back with the existing server_seq so we
		// can still stamp synced_at. Other rejection reasons (empty fields)
		// should never happen on rows produced by GetPendingEventsPreserveSession
		// — those rows are validated up front — but if they do, we leave
		// synced_at NULL so the row is visible for triage.
		if rej.Reason != "duplicate" || rej.ServerSeq == 0 {
			continue
		}
		if _, err := tx.Exec(
			`UPDATE main.action_log SET synced_at = CURRENT_TIMESTAMP, server_seq = ? WHERE rowid = ?`,
			rej.ServerSeq, rej.ClientActionID,
		); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("mark duplicate action_log synced rowid=%d: %w", rej.ClientActionID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit promotion tx: %w", err)
	}

	return pushResult.Accepted, nil
}

// eventsDBPathFor returns the absolute path to the project's events.db,
// matching ProjectLivePool.eventsDBPath. It exists as a method on Server so
// promoteActionLog doesn't need to reach through the projectLivePool field
// (which is mostly internal to its own concerns).
func (s *Server) eventsDBPathFor(projectID string) string {
	return filepath.Join(s.config.ProjectDataDir, projectID, "events.db")
}

// ensureEventsDBSchema makes sure the events table exists at eventsPath. It
// opens its own short-lived connection so it doesn't contend with the live
// project handle's connection used by the ATTACH path.
func ensureEventsDBSchema(eventsPath string) error {
	conn, err := tddb.OpenSQLite(eventsPath, tddb.OpenOptions{})
	if err != nil {
		return fmt.Errorf("open events.db: %w", err)
	}
	defer conn.Close()
	if err := tdsync.InitServerEventLog(conn); err != nil {
		return fmt.Errorf("init events table: %w", err)
	}
	// Ensure the WAL is checkpointed so the ATTACH path sees consistent
	// schema state immediately. PASSIVE never blocks other writers.
	_, _ = conn.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	return nil
}

// sqliteQuote returns a single-quoted SQLite string literal with embedded
// single quotes doubled, suitable for use in an ATTACH DATABASE path.
// SQLite doesn't accept positional parameters for ATTACH so this is the
// safest way to compose the statement when the path contains characters
// like spaces.
func sqliteQuote(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, c)
	}
	out = append(out, '\'')
	return string(out)
}

// shouldPromote returns true when the request method is one that may have
// produced action_log rows in project.db. We deliberately keep this
// permissive: GET / HEAD / OPTIONS skip; everything else triggers (the
// promotion is a no-op when no rows are pending, so the worst case is one
// extra ATTACH+SELECT round trip).
func shouldPromote(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}

