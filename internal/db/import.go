package db

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/marcus/td/internal/models"
)

// execer is satisfied by both *sql.DB and *sql.Tx, allowing the same
// SQL helpers to run standalone or inside a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// --- Internal helpers that accept an execer ---

func upsertIssueExec(e execer, issue *models.Issue) error {
	labels := strings.Join(issue.Labels, ",")
	deferUntil := sql.NullString{String: "", Valid: false}
	if issue.DeferUntil != nil {
		deferUntil = sql.NullString{String: *issue.DeferUntil, Valid: true}
	}
	dueDate := sql.NullString{String: "", Valid: false}
	if issue.DueDate != nil {
		dueDate = sql.NullString{String: *issue.DueDate, Valid: true}
	}
	_, err := e.Exec(`
		INSERT OR REPLACE INTO issues (
			id, title, description, status, type, priority, points, labels,
			parent_id, acceptance, sprint, implementer_session, creator_session,
			reviewer_session, created_at, updated_at, closed_at, deleted_at,
			minor, created_branch, defer_until, due_date, defer_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issue.ID, issue.Title, issue.Description, issue.Status, issue.Type,
		issue.Priority, issue.Points, labels, issue.ParentID, issue.Acceptance,
		issue.Sprint, issue.ImplementerSession, issue.CreatorSession,
		issue.ReviewerSession, issue.CreatedAt, issue.UpdatedAt,
		issue.ClosedAt, issue.DeletedAt, issue.Minor, issue.CreatedBranch,
		deferUntil, dueDate, issue.DeferCount)
	return err
}

func insertLogExec(e execer, log *models.Log) error {
	_, err := e.Exec(`
		INSERT OR IGNORE INTO logs (id, issue_id, session_id, work_session_id, message, type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.IssueID, log.SessionID, log.WorkSessionID, log.Message, log.Type, log.Timestamp)
	return err
}

func insertHandoffExec(e execer, handoff *models.Handoff) error {
	doneJSON, _ := json.Marshal(handoff.Done)
	remainingJSON, _ := json.Marshal(handoff.Remaining)
	decisionsJSON, _ := json.Marshal(handoff.Decisions)
	uncertainJSON, _ := json.Marshal(handoff.Uncertain)
	_, err := e.Exec(`
		INSERT OR IGNORE INTO handoffs (id, issue_id, session_id, done, remaining, decisions, uncertain, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, handoff.ID, handoff.IssueID, handoff.SessionID, doneJSON, remainingJSON, decisionsJSON, uncertainJSON, handoff.Timestamp)
	return err
}

func insertIssueFileExec(e execer, file *models.IssueFile) error {
	_, err := e.Exec(`
		INSERT OR IGNORE INTO issue_files (id, issue_id, file_path, role, linked_sha, linked_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, file.ID, file.IssueID, file.FilePath, file.Role, file.LinkedSHA, file.LinkedAt)
	return err
}

// --- Public methods: thin wrappers for standalone use ---

// UpsertIssueRaw inserts or replaces an issue with all fields as-is.
// No ID generation, no status defaulting, no action_log entry.
func (db *DB) UpsertIssueRaw(issue *models.Issue) error {
	return db.withWriteLock(func() error {
		issue.ID = NormalizeIssueID(issue.ID)
		return upsertIssueExec(db.conn, issue)
	})
}

// InsertLogRaw inserts a log entry without generating an ID or writing to action_log.
// Skips duplicates via INSERT OR IGNORE.
func (db *DB) InsertLogRaw(log *models.Log) error {
	return db.withWriteLock(func() error {
		return insertLogExec(db.conn, log)
	})
}

// InsertHandoffRaw inserts a handoff without generating an ID or writing to action_log.
// Skips duplicates via INSERT OR IGNORE.
func (db *DB) InsertHandoffRaw(handoff *models.Handoff) error {
	return db.withWriteLock(func() error {
		return insertHandoffExec(db.conn, handoff)
	})
}

// InsertIssueFileRaw inserts a linked file without writing to action_log.
// Preserves the original linked_at timestamp. Skips duplicates via INSERT OR IGNORE.
func (db *DB) InsertIssueFileRaw(file *models.IssueFile) error {
	return db.withWriteLock(func() error {
		return insertIssueFileExec(db.conn, file)
	})
}

// ReplaceIssueRaw deletes all associated data for an issue and then upserts
// the issue itself atomically. Equivalent to ImportItemRaw with no associated data.
func (db *DB) ReplaceIssueRaw(issue *models.Issue) error {
	return db.ImportItemRaw(issue, nil, nil, nil, nil, true)
}

// --- Composite method for atomic import ---

// ImportItemRaw atomically imports an issue with all associated data in a single
// transaction. When replace is true, existing associated data is deleted first.
// No action_log entries are created.
func (db *DB) ImportItemRaw(issue *models.Issue, logs []models.Log, handoffs []models.Handoff, deps []models.IssueDependency, files []models.IssueFile, replace bool) error {
	return db.withWriteLock(func() error {
		issue.ID = NormalizeIssueID(issue.ID)
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if replace {
			for _, table := range []string{"logs", "handoffs", "issue_files", "issue_dependencies"} {
				if _, err := tx.Exec(`DELETE FROM `+table+` WHERE issue_id = ?`, issue.ID); err != nil {
					return err
				}
			}
		}

		if err := upsertIssueExec(tx, issue); err != nil {
			return err
		}
		for i := range logs {
			if err := insertLogExec(tx, &logs[i]); err != nil {
				return err
			}
		}
		for i := range handoffs {
			if err := insertHandoffExec(tx, &handoffs[i]); err != nil {
				return err
			}
		}
		for _, dep := range deps {
			depID := DependencyID(issue.ID, dep.DependsOnID, dep.RelationType)
			if _, err := tx.Exec(`
				INSERT OR REPLACE INTO issue_dependencies (id, issue_id, depends_on_id, relation_type)
				VALUES (?, ?, ?, ?)
			`, depID, issue.ID, dep.DependsOnID, dep.RelationType); err != nil {
				return err
			}
		}
		for i := range files {
			if err := insertIssueFileExec(tx, &files[i]); err != nil {
				return err
			}
		}

		return tx.Commit()
	})
}
