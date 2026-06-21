package db

import (
	"database/sql"
	"fmt"
)

// SessionStateScope identifies one local session/worktree state row.
// WorktreeID may be empty for legacy callers and non-git contexts.
type SessionStateScope struct {
	SessionID  string
	WorktreeID string

	// Legacy fallback hooks are read-only compatibility shims for the release
	// that moves current state out of config.json. They are optional so db does
	// not import internal/config.
	ConfigBaseDir              string
	LegacyGetFocus             func(baseDir string) (string, error)
	LegacyGetActiveWorkSession func(baseDir string) (string, error)
}

func (s SessionStateScope) validate() error {
	if s.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	return nil
}

// SetFocus stores the focused issue for the scope in the local DB.
func (db *DB) SetFocus(scope SessionStateScope, issueID string) error {
	if err := scope.validate(); err != nil {
		return err
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
INSERT INTO session_state (session_id, worktree_id, focused_issue_id, updated_at)
VALUES (?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
ON CONFLICT(session_id, worktree_id) DO UPDATE SET
    focused_issue_id = excluded.focused_issue_id,
    updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
`, scope.SessionID, scope.WorktreeID, issueID)
		return err
	})
}

// GetFocus returns the scoped focused issue. When no scoped DB row exists yet,
// it optionally falls back to config.json through the provided read-only hook.
func (db *DB) GetFocus(scope SessionStateScope) (string, error) {
	if err := scope.validate(); err != nil {
		return "", err
	}
	value, ok, err := db.getSessionStateValue(scope, "focused_issue_id")
	if err != nil {
		return "", err
	}
	if ok {
		return value, nil
	}
	if scope.LegacyGetFocus != nil {
		return scope.LegacyGetFocus(scope.ConfigBaseDir)
	}
	return "", nil
}

// ClearFocus clears the scoped focused issue in the local DB.
func (db *DB) ClearFocus(scope SessionStateScope) error {
	return db.SetFocus(scope, "")
}

// SetActiveWorkSession stores the active work session for the scope in the local DB.
func (db *DB) SetActiveWorkSession(scope SessionStateScope, wsID string) error {
	if err := scope.validate(); err != nil {
		return err
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
INSERT INTO session_state (session_id, worktree_id, active_work_session_id, updated_at)
VALUES (?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
ON CONFLICT(session_id, worktree_id) DO UPDATE SET
    active_work_session_id = excluded.active_work_session_id,
    updated_at = strftime('%Y-%m-%d %H:%M:%f', 'now')
`, scope.SessionID, scope.WorktreeID, wsID)
		return err
	})
}

// GetActiveWorkSession returns the scoped active work session. When no scoped
// DB row exists yet, it optionally falls back to config.json through the
// provided read-only hook.
func (db *DB) GetActiveWorkSession(scope SessionStateScope) (string, error) {
	if err := scope.validate(); err != nil {
		return "", err
	}
	value, ok, err := db.getSessionStateValue(scope, "active_work_session_id")
	if err != nil {
		return "", err
	}
	if ok {
		return value, nil
	}
	if scope.LegacyGetActiveWorkSession != nil {
		return scope.LegacyGetActiveWorkSession(scope.ConfigBaseDir)
	}
	return "", nil
}

// ClearActiveWorkSession clears the scoped active work session in the local DB.
func (db *DB) ClearActiveWorkSession(scope SessionStateScope) error {
	return db.SetActiveWorkSession(scope, "")
}

func (db *DB) getSessionStateValue(scope SessionStateScope, column string) (string, bool, error) {
	var value string
	err := db.conn.QueryRow(
		fmt.Sprintf(`SELECT %s FROM session_state WHERE session_id = ? AND worktree_id = ?`, column),
		scope.SessionID, scope.WorktreeID,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}
