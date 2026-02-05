package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// marshalIssue returns a JSON representation of an issue for action_log storage.
func marshalIssue(issue *models.Issue) string {
	data, _ := json.Marshal(issue)
	return string(data)
}

// scanIssueRow reads a full issue row from the DB within a withWriteLock closure.
// Returns the issue and any error. Uses the same column set as GetIssue.
func (db *DB) scanIssueRow(id string) (*models.Issue, error) {
	var issue models.Issue
	var labels string
	var closedAt, deletedAt sql.NullTime
	var parentID, acceptance, sprint sql.NullString
	var implSession, creatorSession, reviewerSession sql.NullString
	var createdBranch sql.NullString
	var pointsNull sql.NullInt64

	err := db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
		&pointsNull, &labels, &parentID, &acceptance, &sprint,
		&implSession, &creatorSession, &reviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	if labels != "" {
		issue.Labels = strings.Split(labels, ",")
	}
	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if deletedAt.Valid {
		issue.DeletedAt = &deletedAt.Time
	}
	issue.Points = int(pointsNull.Int64)
	issue.ParentID = parentID.String
	issue.Acceptance = acceptance.String
	issue.Sprint = sprint.String
	issue.ImplementerSession = implSession.String
	issue.CreatorSession = creatorSession.String
	issue.ReviewerSession = reviewerSession.String
	issue.CreatedBranch = createdBranch.String

	return &issue, nil
}

// CreateIssueLogged creates an issue and logs the action atomically within a single withWriteLock call.
func (db *DB) CreateIssueLogged(issue *models.Issue, sessionID string) error {
	return db.withWriteLock(func() error {
		if issue.Status == "" {
			issue.Status = models.StatusOpen
		}
		if issue.Type == "" {
			issue.Type = models.TypeTask
		}
		if issue.Priority == "" {
			issue.Priority = models.PriorityP2
		}

		now := time.Now()
		issue.CreatedAt = now
		issue.UpdatedAt = now

		labels := strings.Join(issue.Labels, ",")

		const maxRetries = 3
		for attempt := range maxRetries {
			id, err := generateID()
			if err != nil {
				return err
			}
			issue.ID = id

			_, err = db.conn.Exec(`
				INSERT INTO issues (id, title, description, status, type, priority, points, labels, parent_id, acceptance, created_at, updated_at, minor, created_branch, creator_session)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, issue.ID, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority, issue.Points, labels, issue.ParentID, issue.Acceptance, issue.CreatedAt, issue.UpdatedAt, issue.Minor, issue.CreatedBranch, issue.CreatorSession)

			if err == nil {
				break
			}
			if !strings.Contains(err.Error(), "UNIQUE constraint") {
				return err
			}
			if attempt == maxRetries-1 {
				return fmt.Errorf("failed to generate unique issue ID after %d attempts", maxRetries)
			}
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData := marshalIssue(issue)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionCreate), "issue", issue.ID, "", newData, now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// updateIssueAndLog updates an issue and logs the action WITHOUT acquiring withWriteLock.
// Caller MUST already hold the write lock. This is the inner logic shared by
// UpdateIssueLogged and the cascade helpers.
func (db *DB) updateIssueAndLog(issue *models.Issue, sessionID string, actionType models.ActionType) error {
	// Read current state for PreviousData
	prev, err := db.scanIssueRow(issue.ID)
	if err != nil {
		return err
	}
	previousData := marshalIssue(prev)

	// Apply update
	issue.UpdatedAt = time.Now()
	labels := strings.Join(issue.Labels, ",")

	_, err = db.conn.Exec(`
		UPDATE issues SET title = ?, description = ?, status = ?, type = ?, priority = ?,
		                  points = ?, labels = ?, parent_id = ?, acceptance = ?, sprint = ?,
		                  implementer_session = ?, reviewer_session = ?, updated_at = ?,
		                  closed_at = ?, deleted_at = ?
		WHERE id = ?
	`, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority,
		issue.Points, labels, issue.ParentID, issue.Acceptance, issue.Sprint,
		issue.ImplementerSession, issue.ReviewerSession, issue.UpdatedAt,
		issue.ClosedAt, issue.DeletedAt, issue.ID)
	if err != nil {
		return err
	}

	// Log the action
	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate action ID: %w", err)
	}
	newData := marshalIssue(issue)
	_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		actionID, sessionID, string(actionType), "issue", issue.ID, previousData, newData, issue.UpdatedAt)
	if err != nil {
		return fmt.Errorf("log action: %w", err)
	}

	return nil
}

// addLogEntry inserts a progress log entry WITHOUT acquiring withWriteLock.
// Caller MUST already hold the write lock.
func (db *DB) addLogEntry(issueID, sessionID, message string, logType models.LogType) error {
	id, err := generateLogID()
	if err != nil {
		return fmt.Errorf("generate log ID: %w", err)
	}
	now := time.Now()
	_, err = db.conn.Exec(`
		INSERT INTO logs (id, issue_id, session_id, work_session_id, message, type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, issueID, sessionID, "", message, logType, now)
	return err
}

// UpdateIssueLogged updates an issue and logs the action atomically within a single withWriteLock call.
// It reads the current DB state for PreviousData before applying the update.
func (db *DB) UpdateIssueLogged(issue *models.Issue, sessionID string, actionType models.ActionType) error {
	return db.withWriteLock(func() error {
		return db.updateIssueAndLog(issue, sessionID, actionType)
	})
}

// DeleteIssueLogged soft-deletes an issue and logs the action atomically within a single withWriteLock call.
func (db *DB) DeleteIssueLogged(issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanIssueRow(issueID)
		if err != nil {
			return err
		}
		previousData := marshalIssue(prev)

		// Soft delete
		now := time.Now()
		_, err = db.conn.Exec(`UPDATE issues SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, issueID)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionDelete), "issue", issueID, previousData, "", now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// RestoreIssueLogged restores a soft-deleted issue and logs the action atomically.
func (db *DB) RestoreIssueLogged(issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanIssueRow(issueID)
		if err != nil {
			return err
		}
		previousData := marshalIssue(prev)

		// Restore (clear deleted_at)
		now := time.Now()
		_, err = db.conn.Exec(`UPDATE issues SET deleted_at = NULL, updated_at = ? WHERE id = ?`, now, issueID)
		if err != nil {
			return err
		}

		// Read new state for NewData
		restored, err := db.scanIssueRow(issueID)
		if err != nil {
			return err
		}
		newData := marshalIssue(restored)

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionRestore), "issue", issueID, previousData, newData, now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}
