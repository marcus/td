package db

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionRow represents a session record in the database
type SessionRow struct {
	ID                string
	Name              string
	Branch            string
	AgentType         string
	AgentPID          int
	ContextID         string
	PreviousSessionID string
	StartedAt         time.Time
	LastActivity      time.Time
}

const sessionSelectCols = `id, name, branch, agent_type, agent_pid, context_id,
	previous_session_id, started_at, last_activity`

// UpsertSession inserts or replaces a session in the database
func (db *DB) UpsertSession(sess *SessionRow) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`INSERT OR REPLACE INTO sessions
			(id, name, branch, agent_type, agent_pid, context_id, previous_session_id, started_at, last_activity)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sess.ID, sess.Name, sess.Branch, sess.AgentType, sess.AgentPID,
			sess.ContextID, sess.PreviousSessionID, sess.StartedAt, sess.LastActivity)
		return err
	})
}

// GetSessionByBranchAgent looks up a session by branch + agent type + agent PID.
// Returns nil, nil if not found.
func (db *DB) GetSessionByBranchAgent(branch, agentType string, agentPID int) (*SessionRow, error) {
	row := db.conn.QueryRow(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE branch = ? AND agent_type = ? AND agent_pid = ?
		ORDER BY COALESCE(last_activity, started_at) DESC LIMIT 1`,
		branch, agentType, agentPID)
	return scanSessionRow(row)
}

// GetSessionByID looks up a session by ID. Returns nil, nil if not found.
func (db *DB) GetSessionByID(id string) (*SessionRow, error) {
	row := db.conn.QueryRow(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE id = ?`, id)
	return scanSessionRow(row)
}

// UpdateSessionActivity updates the last_activity timestamp for a session
func (db *DB) UpdateSessionActivity(id string, t time.Time) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE sessions SET last_activity = ? WHERE id = ?`, t, id)
		return err
	})
}

// UpdateSessionName updates the name of a session
func (db *DB) UpdateSessionName(id, name string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
		return err
	})
}

// ListAllSessions returns all sessions ordered by last_activity descending
func (db *DB) ListAllSessions() ([]SessionRow, error) {
	rows, err := db.conn.Query(`SELECT ` + sessionSelectCols + `
		FROM sessions ORDER BY COALESCE(last_activity, started_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionRow
	for rows.Next() {
		s, err := scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *s)
	}
	return sessions, rows.Err()
}

// DeleteStaleSessions removes sessions with last_activity before the given time
func (db *DB) DeleteStaleSessions(before time.Time) (int64, error) {
	var count int64
	err := db.withWriteLock(func() error {
		result, err := db.conn.Exec(
			`DELETE FROM sessions WHERE COALESCE(last_activity, started_at) < ?`, before)
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		return err
	})
	return count, err
}

func scanSessionRow(row *sql.Row) (*SessionRow, error) {
	var s SessionRow
	var lastActivity sql.NullTime
	err := row.Scan(&s.ID, &s.Name, &s.Branch, &s.AgentType, &s.AgentPID,
		&s.ContextID, &s.PreviousSessionID, &s.StartedAt, &lastActivity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastActivity.Valid {
		s.LastActivity = lastActivity.Time
	} else {
		s.LastActivity = s.StartedAt
	}
	return &s, nil
}

// fileSession mirrors the JSON format of filesystem session files
type fileSession struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	Branch            string    `json:"branch,omitempty"`
	AgentType         string    `json:"agent_type,omitempty"`
	AgentPID          int       `json:"agent_pid,omitempty"`
	ContextID         string    `json:"context_id,omitempty"`
	PreviousSessionID string    `json:"previous_session_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	LastActivity      time.Time `json:"last_activity,omitempty"`
}

// parseLegacySession parses a legacy line-based session file
func parseLegacySession(data []byte) (*fileSession, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, os.ErrInvalid
	}
	sess := &fileSession{ID: strings.TrimSpace(lines[0])}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(lines[1])); err == nil {
		sess.StartedAt = t
	}
	if len(lines) >= 3 {
		sess.ContextID = strings.TrimSpace(lines[2])
	}
	if len(lines) >= 4 {
		sess.Name = strings.TrimSpace(lines[3])
	}
	if len(lines) >= 5 {
		sess.PreviousSessionID = strings.TrimSpace(lines[4])
	}
	return sess, nil
}

// MigrateFileSystemSessions migrates sessions from filesystem to DB.
// Safe to call multiple times—no-op if filesystem sessions don't exist.
// Uses INSERT OR IGNORE so concurrent calls are safe.
func (db *DB) MigrateFileSystemSessions(baseDir string) error {
	sessionsPath := filepath.Join(baseDir, ".todos", "sessions")
	legacyPath := filepath.Join(baseDir, ".todos", "session")

	// Quick check: if neither exists, return immediately (O(1) no-op)
	_, sessErr := os.Stat(sessionsPath)
	_, legErr := os.Stat(legacyPath)
	if os.IsNotExist(sessErr) && os.IsNotExist(legErr) {
		return nil
	}

	return db.withWriteLock(func() error {
		// Migrate .todos/sessions/ directory (agent-scoped + branch-scoped files)
		if sessErr == nil {
			filepath.Walk(sessionsPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
					return nil
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil // skip unreadable
				}
				var fs fileSession
				if json.Unmarshal(data, &fs) != nil || fs.ID == "" {
					return nil // skip unparseable
				}
				la := fs.LastActivity
				if la.IsZero() {
					la = fs.StartedAt
				}
				db.conn.Exec(`INSERT OR IGNORE INTO sessions
					(id, name, branch, agent_type, agent_pid, context_id, previous_session_id, started_at, last_activity)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					fs.ID, fs.Name, fs.Branch, fs.AgentType, fs.AgentPID,
					fs.ContextID, fs.PreviousSessionID, fs.StartedAt, la)
				return nil
			})
		}

		// Migrate legacy .todos/session file
		if legErr == nil {
			data, readErr := os.ReadFile(legacyPath)
			if readErr == nil {
				var fs *fileSession
				// Try JSON first
				var jsonSess fileSession
				if json.Unmarshal(data, &jsonSess) == nil && jsonSess.ID != "" {
					fs = &jsonSess
				} else {
					// Try line-based format
					fs, _ = parseLegacySession(data)
				}
				if fs != nil && fs.ID != "" {
					la := fs.LastActivity
					if la.IsZero() {
						la = fs.StartedAt
					}
					db.conn.Exec(`INSERT OR IGNORE INTO sessions
						(id, name, branch, agent_type, agent_pid, context_id, previous_session_id, started_at, last_activity)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						fs.ID, fs.Name, fs.Branch, fs.AgentType, fs.AgentPID,
						fs.ContextID, fs.PreviousSessionID, fs.StartedAt, la)
				}
			}
		}

		// Clean up filesystem after successful migration
		if sessErr == nil {
			os.RemoveAll(sessionsPath) // non-fatal if fails
		}
		if legErr == nil {
			os.Remove(legacyPath) // non-fatal if fails
		}

		return nil
	})
}

func scanSessionRows(rows *sql.Rows) (*SessionRow, error) {
	var s SessionRow
	var lastActivity sql.NullTime
	err := rows.Scan(&s.ID, &s.Name, &s.Branch, &s.AgentType, &s.AgentPID,
		&s.ContextID, &s.PreviousSessionID, &s.StartedAt, &lastActivity)
	if err != nil {
		return nil, err
	}
	if lastActivity.Valid {
		s.LastActivity = lastActivity.Time
	} else {
		s.LastActivity = s.StartedAt
	}
	return &s, nil
}
