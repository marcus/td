package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// CreateWorkSession creates a new work session
func (db *DB) CreateWorkSession(ws *models.WorkSession) error {
	return db.withWriteLock(func() error {
		id, err := generateWSID()
		if err != nil {
			return err
		}
		ws.ID = id
		ws.StartedAt = time.Now()

		_, err = db.conn.Exec(`
			INSERT INTO work_sessions (id, name, session_id, started_at, start_sha)
			VALUES (?, ?, ?, ?, ?)
		`, ws.ID, ws.Name, ws.SessionID, ws.StartedAt, ws.StartSHA)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": ws.ID, "name": ws.Name, "session_id": ws.SessionID,
			"started_at": ws.StartedAt, "start_sha": ws.StartSHA,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, ws.SessionID, "create", "work_sessions", ws.ID, "", string(newData), time.Now())
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// GetWorkSession retrieves a work session
func (db *DB) GetWorkSession(id string) (*models.WorkSession, error) {
	var ws models.WorkSession
	var endedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, session_id, started_at, ended_at, start_sha, end_sha
		FROM work_sessions WHERE id = ?
	`, id).Scan(&ws.ID, &ws.Name, &ws.SessionID, &ws.StartedAt, &endedAt, &ws.StartSHA, &ws.EndSHA)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("work session not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	if endedAt.Valid {
		ws.EndedAt = &endedAt.Time
	}

	return &ws, nil
}

// UpdateWorkSession updates a work session
func (db *DB) UpdateWorkSession(ws *models.WorkSession) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			UPDATE work_sessions SET name = ?, ended_at = ?, end_sha = ?
			WHERE id = ?
		`, ws.Name, ws.EndedAt, ws.EndSHA, ws.ID)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": ws.ID, "name": ws.Name, "session_id": ws.SessionID,
			"started_at": ws.StartedAt, "ended_at": ws.EndedAt,
			"start_sha": ws.StartSHA, "end_sha": ws.EndSHA,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, ws.SessionID, "update", "work_sessions", ws.ID, "", string(newData), time.Now())
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// TagIssueToWorkSession links an issue to a work session
func (db *DB) TagIssueToWorkSession(wsID, issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		id := WsiID(wsID, issueID)
		now := time.Now()
		_, err := db.conn.Exec(`
			INSERT OR IGNORE INTO work_session_issues (id, work_session_id, issue_id, tagged_at)
			VALUES (?, ?, ?, ?)
		`, id, wsID, issueID, now)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": id, "work_session_id": wsID, "issue_id": issueID, "tagged_at": now,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionWorkSessionTag), "work_session_issues", id, "", string(newData), now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// UntagIssueFromWorkSession removes an issue from a work session
func (db *DB) UntagIssueFromWorkSession(wsID, issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		id := WsiID(wsID, issueID)
		_, err := db.conn.Exec(`DELETE FROM work_session_issues WHERE id = ?`, id)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": id, "work_session_id": wsID, "issue_id": issueID,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionWorkSessionUntag), "work_session_issues", id, "", string(newData), time.Now())
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// GetWorkSessionIssues returns issues tagged to a work session
func (db *DB) GetWorkSessionIssues(wsID string) ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT issue_id FROM work_session_issues WHERE work_session_id = ? ORDER BY tagged_at
	`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ListWorkSessions returns recent work sessions
func (db *DB) ListWorkSessions(limit int) ([]models.WorkSession, error) {
	query := `SELECT id, name, session_id, started_at, ended_at, start_sha, end_sha
	          FROM work_sessions ORDER BY started_at DESC`
	args := []interface{}{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.WorkSession
	for rows.Next() {
		var ws models.WorkSession
		var endedAt sql.NullTime

		if err := rows.Scan(&ws.ID, &ws.Name, &ws.SessionID, &ws.StartedAt, &endedAt, &ws.StartSHA, &ws.EndSHA); err != nil {
			return nil, err
		}

		if endedAt.Valid {
			ws.EndedAt = &endedAt.Time
		}

		sessions = append(sessions, ws)
	}

	return sessions, nil
}
