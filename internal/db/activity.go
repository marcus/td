package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// Log Functions
// ============================================================================

// AddLog adds a log entry to an issue
func (db *DB) AddLog(log *models.Log) error {
	return db.withWriteLock(func() error {
		log.Timestamp = time.Now()

		id, err := generateLogID()
		if err != nil {
			return fmt.Errorf("generate ID: %w", err)
		}
		log.ID = id

		_, err = db.conn.Exec(`
			INSERT INTO logs (id, issue_id, session_id, work_session_id, message, type, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, log.ID, log.IssueID, log.SessionID, log.WorkSessionID, log.Message, log.Type, log.Timestamp)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": log.ID, "issue_id": log.IssueID, "session_id": log.SessionID,
			"work_session_id": log.WorkSessionID, "message": log.Message,
			"type": log.Type, "timestamp": log.Timestamp,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, log.SessionID, "create", "logs", log.ID, "", string(newData), time.Now())
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// GetLogs retrieves logs for an issue, including work session logs
func (db *DB) GetLogs(issueID string, limit int) ([]models.Log, error) {
	// Get logs that are either:
	// 1. Directly assigned to this issue (issue_id = ?)
	// 2. Work session logs (issue_id = '') from sessions where this issue is tagged
	query := `SELECT CAST(l.id AS TEXT), l.issue_id, l.session_id, l.work_session_id, l.message, l.type, l.timestamp
	          FROM logs l
	          WHERE l.issue_id = ?
	          OR (l.issue_id = '' AND l.work_session_id IN (
	              SELECT work_session_id FROM work_session_issues WHERE issue_id = ?
	          ))
	          ORDER BY l.timestamp DESC`
	args := []interface{}{issueID, issueID}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	// Reverse to get chronological order
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	return logs, nil
}

// GetLogsByWorkSession retrieves logs for a specific work session
func (db *DB) GetLogsByWorkSession(wsID string) ([]models.Log, error) {
	query := `SELECT CAST(id AS TEXT), issue_id, session_id, work_session_id, message, type, timestamp
	          FROM logs WHERE work_session_id = ? ORDER BY timestamp`

	rows, err := db.conn.Query(query, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetRecentLogsAll returns recent logs across all issues
func (db *DB) GetRecentLogsAll(limit int) ([]models.Log, error) {
	query := `SELECT CAST(id AS TEXT), issue_id, session_id, work_session_id, message, type, timestamp
	          FROM logs ORDER BY timestamp DESC`
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

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetActiveSessions returns distinct session IDs with activity since the given time
func (db *DB) GetActiveSessions(since time.Time) ([]string, error) {
	query := `SELECT session_id FROM logs
	          WHERE session_id != '' AND timestamp > ?
	          GROUP BY session_id
	          ORDER BY MAX(timestamp) DESC`

	rows, err := db.conn.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			continue
		}
		if sessionID != "" {
			sessions = append(sessions, sessionID)
		}
	}

	return sessions, nil
}

// ============================================================================
// Handoff Functions
// ============================================================================

// AddHandoff adds a handoff entry and logs it to action_log for sync/undo.
func (db *DB) AddHandoff(handoff *models.Handoff) error {
	return db.withWriteLock(func() error {
		handoff.Timestamp = time.Now()

		doneJSON, _ := json.Marshal(handoff.Done)
		remainingJSON, _ := json.Marshal(handoff.Remaining)
		decisionsJSON, _ := json.Marshal(handoff.Decisions)
		uncertainJSON, _ := json.Marshal(handoff.Uncertain)

		id, err := generateHandoffID()
		if err != nil {
			return fmt.Errorf("generate ID: %w", err)
		}
		handoff.ID = id

		_, err = db.conn.Exec(`
			INSERT INTO handoffs (id, issue_id, session_id, done, remaining, decisions, uncertain, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, handoff.ID, handoff.IssueID, handoff.SessionID, doneJSON, remainingJSON, decisionsJSON, uncertainJSON, handoff.Timestamp)
		if err != nil {
			return err
		}

		// Log to action_log for sync and undo support
		newData, _ := json.Marshal(map[string]any{
			"id":         handoff.ID,
			"issue_id":   handoff.IssueID,
			"session_id": handoff.SessionID,
			"done":       string(doneJSON),
			"remaining":  string(remainingJSON),
			"decisions":  string(decisionsJSON),
			"uncertain":  string(uncertainJSON),
			"timestamp":  handoff.Timestamp.Format(time.RFC3339),
		})
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		_, err = db.conn.Exec(`
			INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		`, actionID, handoff.SessionID, models.ActionHandoff, "handoff", handoff.ID, string(newData), handoff.Timestamp)
		if err != nil {
			return fmt.Errorf("log handoff action: %w", err)
		}

		return nil
	})
}

// GetLatestHandoff retrieves the latest handoff for an issue
func (db *DB) GetLatestHandoff(issueID string) (*models.Handoff, error) {
	var handoff models.Handoff
	var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string

	err := db.conn.QueryRow(`
		SELECT CAST(id AS TEXT), issue_id, session_id, done, remaining, decisions, uncertain, timestamp
		FROM handoffs WHERE issue_id = ? ORDER BY timestamp DESC LIMIT 1
	`, issueID).Scan(
		&handoff.ID, &handoff.IssueID, &handoff.SessionID,
		&doneJSON, &remainingJSON, &decisionsJSON, &uncertainJSON, &handoff.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(doneJSON), &handoff.Done); err != nil {
		return nil, fmt.Errorf("failed to unmarshal done: %w", err)
	}
	if err := json.Unmarshal([]byte(remainingJSON), &handoff.Remaining); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remaining: %w", err)
	}
	if err := json.Unmarshal([]byte(decisionsJSON), &handoff.Decisions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decisions: %w", err)
	}
	if err := json.Unmarshal([]byte(uncertainJSON), &handoff.Uncertain); err != nil {
		return nil, fmt.Errorf("failed to unmarshal uncertain: %w", err)
	}

	return &handoff, nil
}

// DeleteHandoff removes a handoff by ID (for undo support)
func (db *DB) DeleteHandoff(handoffID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM handoffs WHERE id = ?`, handoffID)
		return err
	})
}

// GetRecentHandoffs retrieves recent handoffs across all issues
func (db *DB) GetRecentHandoffs(limit int, since time.Time) ([]models.Handoff, error) {
	var handoffs []models.Handoff

	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), issue_id, session_id, done, remaining, decisions, uncertain, timestamp
		FROM handoffs WHERE timestamp > ? ORDER BY timestamp DESC LIMIT ?
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var h models.Handoff
		var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string
		err := rows.Scan(&h.ID, &h.IssueID, &h.SessionID,
			&doneJSON, &remainingJSON, &decisionsJSON, &uncertainJSON, &h.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan handoff row: %w", err)
		}
		if err := json.Unmarshal([]byte(doneJSON), &h.Done); err != nil {
			return nil, fmt.Errorf("failed to unmarshal done: %w", err)
		}
		if err := json.Unmarshal([]byte(remainingJSON), &h.Remaining); err != nil {
			return nil, fmt.Errorf("failed to unmarshal remaining: %w", err)
		}
		if err := json.Unmarshal([]byte(decisionsJSON), &h.Decisions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal decisions: %w", err)
		}
		if err := json.Unmarshal([]byte(uncertainJSON), &h.Uncertain); err != nil {
			return nil, fmt.Errorf("failed to unmarshal uncertain: %w", err)
		}
		handoffs = append(handoffs, h)
	}

	return handoffs, nil
}

// ============================================================================
// Comment Functions
// ============================================================================

// AddComment adds a comment to an issue
func (db *DB) AddComment(comment *models.Comment) error {
	return db.withWriteLock(func() error {
		comment.CreatedAt = time.Now()

		id, err := generateCommentID()
		if err != nil {
			return fmt.Errorf("generate ID: %w", err)
		}
		comment.ID = id

		_, err = db.conn.Exec(`
			INSERT INTO comments (id, issue_id, session_id, text, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, comment.ID, comment.IssueID, comment.SessionID, comment.Text, comment.CreatedAt)
		if err != nil {
			return err
		}

		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData, _ := json.Marshal(map[string]interface{}{
			"id": comment.ID, "issue_id": comment.IssueID, "session_id": comment.SessionID,
			"text": comment.Text, "created_at": comment.CreatedAt,
		})
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, comment.SessionID, "create", "comments", comment.ID, "", string(newData), time.Now())
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// GetComments retrieves comments for an issue
func (db *DB) GetComments(issueID string) ([]models.Comment, error) {
	rows, err := db.conn.Query(`
		SELECT CAST(id AS TEXT), issue_id, session_id, text, created_at
		FROM comments WHERE issue_id = ? ORDER BY created_at
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.SessionID, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// GetRecentCommentsAll returns recent comments across all issues
func (db *DB) GetRecentCommentsAll(limit int) ([]models.Comment, error) {
	query := `SELECT CAST(id AS TEXT), issue_id, session_id, text, created_at
	          FROM comments ORDER BY created_at DESC`
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

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.SessionID, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// ============================================================================
// Action Log Functions (Undo Support)
// ============================================================================

// LogAction records an action for undo support
func (db *DB) LogAction(action *models.ActionLog) error {
	return db.withWriteLock(func() error {
		action.Timestamp = time.Now()

		id, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate ID: %w", err)
		}
		action.ID = id

		_, err = db.conn.Exec(`
			INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)
		`, action.ID, action.SessionID, action.ActionType, action.EntityType, action.EntityID, action.PreviousData, action.NewData, action.Timestamp)
		if err != nil {
			return err
		}

		return nil
	})
}

// GetLastAction returns the most recent undoable action for a session
func (db *DB) GetLastAction(sessionID string) (*models.ActionLog, error) {
	var action models.ActionLog
	var undone int

	err := db.conn.QueryRow(`
		SELECT CAST(id AS TEXT), session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		WHERE session_id = ? AND undone = 0 AND entity_type NOT IN ('logs', 'comments', 'work_sessions')
		ORDER BY timestamp DESC LIMIT 1
	`, sessionID).Scan(
		&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
		&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	action.Undone = undone == 1
	return &action, nil
}

// MarkActionUndone marks an action as undone
func (db *DB) MarkActionUndone(actionID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE action_log SET undone = 1 WHERE id = ?`, actionID)
		return err
	})
}

// GetRecentActions returns recent actions for a session
func (db *DB) GetRecentActions(sessionID string, limit int) ([]models.ActionLog, error) {
	query := `
		SELECT CAST(id AS TEXT), session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		WHERE session_id = ? AND entity_type NOT IN ('logs', 'comments', 'work_sessions')
		ORDER BY timestamp DESC`
	args := []interface{}{sessionID}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.ActionLog
	for rows.Next() {
		var action models.ActionLog
		var undone int
		err := rows.Scan(
			&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
			&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
		)
		if err != nil {
			return nil, err
		}
		action.Undone = undone == 1
		actions = append(actions, action)
	}

	return actions, nil
}

// GetRecentActionsAll returns recent action_log entries across all sessions
func (db *DB) GetRecentActionsAll(limit int) ([]models.ActionLog, error) {
	query := `
		SELECT CAST(id AS TEXT), session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		ORDER BY timestamp DESC`
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

	var actions []models.ActionLog
	for rows.Next() {
		var action models.ActionLog
		var undone int
		err := rows.Scan(
			&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
			&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
		)
		if err != nil {
			return nil, err
		}
		action.Undone = undone == 1
		actions = append(actions, action)
	}

	return actions, nil
}

// GetRejectedInProgressIssueIDs returns IDs of in_progress issues that have a
// recent ActionReject without a subsequent ActionReview (needs rework)
func (db *DB) GetRejectedInProgressIssueIDs() (map[string]bool, error) {
	query := `
		SELECT DISTINCT i.id FROM issues i
		WHERE i.status = 'in_progress' AND i.deleted_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM action_log al
			WHERE al.entity_id = i.id AND al.action_type = 'reject' AND al.undone = 0
			  AND NOT EXISTS (
				SELECT 1 FROM action_log al2
				WHERE al2.entity_id = i.id AND al2.action_type = 'review'
				  AND al2.timestamp > al.timestamp
			  )
		  )
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}

	return result, nil
}

// ============================================================================
// Git Snapshot Functions
// ============================================================================

// AddGitSnapshot records a git state snapshot
func (db *DB) AddGitSnapshot(snapshot *models.GitSnapshot) error {
	return db.withWriteLock(func() error {
		snapshot.Timestamp = time.Now()

		id, err := generateSnapshotID()
		if err != nil {
			return fmt.Errorf("generate ID: %w", err)
		}
		snapshot.ID = id

		_, err = db.conn.Exec(`
			INSERT INTO git_snapshots (id, issue_id, event, commit_sha, branch, dirty_files, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, snapshot.ID, snapshot.IssueID, snapshot.Event, snapshot.CommitSHA, snapshot.Branch, snapshot.DirtyFiles, snapshot.Timestamp)
		if err != nil {
			return err
		}

		return nil
	})
}

// GetStartSnapshot returns the start snapshot for an issue
func (db *DB) GetStartSnapshot(issueID string) (*models.GitSnapshot, error) {
	var snapshot models.GitSnapshot

	err := db.conn.QueryRow(`
		SELECT CAST(id AS TEXT), issue_id, event, commit_sha, branch, dirty_files, timestamp
		FROM git_snapshots WHERE issue_id = ? AND event = 'start' ORDER BY timestamp DESC LIMIT 1
	`, issueID).Scan(
		&snapshot.ID, &snapshot.IssueID, &snapshot.Event,
		&snapshot.CommitSHA, &snapshot.Branch, &snapshot.DirtyFiles, &snapshot.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}
