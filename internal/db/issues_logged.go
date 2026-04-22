package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/reviewpolicy"
)

// reviewInvalidatingDiff computes an IssueMutation describing which review-
// relevant fields changed between prev and next. Used by the logged write
// path to decide whether to supersede any active approval review.
//
// Pure-metadata fields (labels, notes, due_date, defer_*) are intentionally
// excluded so routine bookkeeping updates do not invalidate a pending
// approval. See plan section "Review freshness" for the full list.
func reviewInvalidatingDiff(prev, next *models.Issue, cascadedReparent bool) reviewpolicy.IssueMutation {
	m := reviewpolicy.IssueMutation{}
	if prev == nil || next == nil {
		return m
	}
	m.DescriptionChanged = prev.Description != next.Description
	m.TitleChanged = prev.Title != next.Title
	m.TypeChanged = prev.Type != next.Type
	m.PriorityChanged = prev.Priority != next.Priority
	m.MinorChanged = prev.Minor != next.Minor
	m.ParentIDChanged = prev.ParentID != next.ParentID
	// status transitions: only flag transitions OUT of in_review that are NOT
	// going to closed (the normal close path should not supersede its own
	// approval).
	if prev.Status == models.StatusInReview &&
		next.Status != models.StatusInReview &&
		next.Status != models.StatusClosed {
		m.StatusChangedFromReviewNotClosing = true
	}
	m.ReparentCascade = cascadedReparent
	return m
}

// supersedeIfReviewInvalidating is a helper that runs reviewInvalidatingDiff
// and calls SupersedeActiveReviews if the mutation is review-invalidating.
// Safe to call outside a transaction — SupersedeActiveReviews acquires its
// own write lock.
//
// Dependencies / linked_files / work_session_tags changes arrive through
// separate side-table mutation paths; those call supersedeApprovalIfLinked
// directly (see relations_logged.go and work_sessions.go).
func (db *DB) supersedeIfReviewInvalidating(prev, next *models.Issue) {
	if prev == nil || next == nil {
		return
	}
	m := reviewInvalidatingDiff(prev, next, false)
	if !reviewpolicy.IsReviewInvalidatingMutation(m) {
		return
	}
	// Best-effort supersede and clear reviewer/reviewed_at fields. This
	// helper runs INSIDE withWriteLock from the caller (updateIssueAndLog*),
	// so we must use the lock-free variants to avoid deadlocking on the
	// reentrant flock.
	_ = db.supersedeActiveReviewsLocked(next.ID)
	_, _ = db.conn.Exec(
		`UPDATE issues SET reviewer_session = '', reviewed_at = NULL WHERE id = ?`,
		next.ID,
	)
}

// StaleIssueStatusError indicates the issue status changed after the caller
// loaded the issue but before the logged transition was persisted.
type StaleIssueStatusError struct {
	IssueID  string
	Expected models.Status
	Actual   models.Status
}

func (e *StaleIssueStatusError) Error() string {
	return fmt.Sprintf("issue %s status changed from %s to %s", e.IssueID, e.Expected, e.Actual)
}

// marshalIssue returns a JSON representation of an issue for action_log storage.
func marshalIssue(issue *models.Issue) string {
	data, _ := json.Marshal(issue)
	return string(data)
}

// scanIssueRow reads a full issue row from the DB within a withWriteLock closure.
// Returns the issue and any error. Uses the same column set as GetIssue.
func (db *DB) scanIssueRow(id string) (*models.Issue, error) {
	var issue models.Issue
	// NullString for every TEXT DEFAULT '' column: old rows or incoming sync
	// payloads may have written NULL (see internal/sync/events.go). Scanning
	// NULL into plain string crashes `td monitor` and many CLI commands.
	var description, labels sql.NullString
	var closedAt, deletedAt, reviewedAt sql.NullTime
	var parentID, acceptance, sprint sql.NullString
	var implSession, creatorSession, reviewerSession sql.NullString
	var reviewRequestedBy, closedBy sql.NullString
	var createdBranch sql.NullString
	var pointsNull sql.NullInt64
	var deferUntil, dueDate sql.NullString

	err := db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, review_requested_by_session, closed_by_session,
		       created_at, updated_at, reviewed_at, closed_at, deleted_at, minor, created_branch,
		       defer_until, due_date, defer_count
		FROM issues WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &description, &issue.Status, &issue.Type, &issue.Priority,
		&pointsNull, &labels, &parentID, &acceptance, &sprint,
		&implSession, &creatorSession, &reviewerSession, &reviewRequestedBy, &closedBy,
		&issue.CreatedAt, &issue.UpdatedAt, &reviewedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
		&deferUntil, &dueDate, &issue.DeferCount,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	issue.Description = description.String
	if labels.Valid && labels.String != "" {
		issue.Labels = strings.Split(labels.String, ",")
	}
	if reviewedAt.Valid {
		issue.ReviewedAt = &reviewedAt.Time
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
	issue.ReviewRequestedBySession = reviewRequestedBy.String
	issue.ClosedBySession = closedBy.String
	issue.CreatedBranch = createdBranch.String
	if deferUntil.Valid {
		issue.DeferUntil = &deferUntil.String
	}
	if dueDate.Valid {
		issue.DueDate = &dueDate.String
	}

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

			deferUntil := sql.NullString{String: "", Valid: false}
			if issue.DeferUntil != nil {
				deferUntil = sql.NullString{String: *issue.DeferUntil, Valid: true}
			}
			dueDate := sql.NullString{String: "", Valid: false}
			if issue.DueDate != nil {
				dueDate = sql.NullString{String: *issue.DueDate, Valid: true}
			}

			_, err = db.conn.Exec(`
				INSERT INTO issues (id, title, description, status, type, priority, points, labels, parent_id, acceptance, created_at, updated_at, minor, created_branch, creator_session, defer_until, due_date, defer_count)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, issue.ID, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority, issue.Points, labels, issue.ParentID, issue.Acceptance, issue.CreatedAt, issue.UpdatedAt, issue.Minor, issue.CreatedBranch, issue.CreatorSession, deferUntil, dueDate, issue.DeferCount)

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
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionCreate), "issue", issue.ID, "", newData, actionTS)
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
	return db.updateIssueAndLogFromPrevious(issue, prev, sessionID, actionType)
}

func (db *DB) updateIssueAndLogFromPrevious(issue, prev *models.Issue, sessionID string, actionType models.ActionType) error {
	previousData := marshalIssue(prev)

	// Apply update
	issue.UpdatedAt = time.Now()
	labels := strings.Join(issue.Labels, ",")

	deferUntil := sql.NullString{String: "", Valid: false}
	if issue.DeferUntil != nil {
		deferUntil = sql.NullString{String: *issue.DeferUntil, Valid: true}
	}
	dueDate := sql.NullString{String: "", Valid: false}
	if issue.DueDate != nil {
		dueDate = sql.NullString{String: *issue.DueDate, Valid: true}
	}

	_, err := db.conn.Exec(`
		UPDATE issues SET title = ?, description = ?, status = ?, type = ?, priority = ?,
		                  points = ?, labels = ?, parent_id = ?, acceptance = ?, sprint = ?,
		                  implementer_session = ?, reviewer_session = ?,
		                  review_requested_by_session = ?, closed_by_session = ?,
		                  updated_at = ?, reviewed_at = ?,
		                  closed_at = ?, deleted_at = ?,
		                  defer_until = ?, due_date = ?, defer_count = ?,
		                  creator_session = ?, minor = ?, created_branch = ?
		WHERE id = ?
	`, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority,
		issue.Points, labels, issue.ParentID, issue.Acceptance, issue.Sprint,
		issue.ImplementerSession, issue.ReviewerSession,
		issue.ReviewRequestedBySession, issue.ClosedBySession,
		issue.UpdatedAt, issue.ReviewedAt,
		issue.ClosedAt, issue.DeletedAt,
		deferUntil, dueDate, issue.DeferCount,
		issue.CreatorSession, issue.Minor, issue.CreatedBranch, issue.ID)
	if err != nil {
		return err
	}

	// Log the action
	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate action ID: %w", err)
	}
	newData := marshalIssue(issue)
	actionTS := formatActionLogTimestamp(issue.UpdatedAt)
	_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		actionID, sessionID, string(actionType), "issue", issue.ID, previousData, newData, actionTS)
	if err != nil {
		return fmt.Errorf("log action: %w", err)
	}

	// Supersede any active approval review if the change is review-
	// invalidating. Approve/close paths are NOT invalidating (status went
	// in_review -> closed), so this no-ops for the normal reviewer-close
	// flow.
	db.supersedeIfReviewInvalidating(prev, issue)

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

// UpdateIssueLoggedWithReviewMeta performs the same atomic issue update + log
// as UpdateIssueLoggedIfStatus but records review-undo metadata in the
// action_log NewData column so cmd/undo.go can roll back issue_reviews
// side-effects. Used by the delegated-review flow (Step 2) for:
//   - ActionApprove (direct reviewer close: inserted one review row)
//   - ActionReviewApprove / ActionReviewChangesRequested (record-only)
//   - ActionCloseAfterReview (no review row inserted, but may want audit of
//     prior reviewer/closed-by fields; the Issue JSON in PreviousData already
//     carries those)
//
// createdReviewID and priorActiveReviewID are optional; empty strings are
// acceptable and produce a bare-Issue NewData (backward compatible).
func (db *DB) UpdateIssueLoggedWithReviewMeta(
	issue *models.Issue, expectedStatus models.Status, sessionID string,
	actionType models.ActionType,
	createdReviewID, priorActiveReviewID string,
) error {
	return db.withWriteLock(func() error {
		prev, err := db.scanIssueRow(issue.ID)
		if err != nil {
			return err
		}
		if prev.Status != expectedStatus {
			return &StaleIssueStatusError{
				IssueID:  issue.ID,
				Expected: expectedStatus,
				Actual:   prev.Status,
			}
		}
		return db.updateIssueAndLogFromPreviousWithReviewMeta(
			issue, prev, sessionID, actionType,
			createdReviewID, priorActiveReviewID,
		)
	})
}

// updateIssueAndLogFromPreviousWithReviewMeta mirrors
// updateIssueAndLogFromPrevious but serializes a ReviewUndoPayload into the
// action_log NewData column. Caller MUST already hold the write lock.
func (db *DB) updateIssueAndLogFromPreviousWithReviewMeta(
	issue, prev *models.Issue, sessionID string, actionType models.ActionType,
	createdReviewID, priorActiveReviewID string,
) error {
	previousData := marshalIssue(prev)

	issue.UpdatedAt = time.Now()
	labels := strings.Join(issue.Labels, ",")

	deferUntil := sql.NullString{String: "", Valid: false}
	if issue.DeferUntil != nil {
		deferUntil = sql.NullString{String: *issue.DeferUntil, Valid: true}
	}
	dueDate := sql.NullString{String: "", Valid: false}
	if issue.DueDate != nil {
		dueDate = sql.NullString{String: *issue.DueDate, Valid: true}
	}

	_, err := db.conn.Exec(`
		UPDATE issues SET title = ?, description = ?, status = ?, type = ?, priority = ?,
		                  points = ?, labels = ?, parent_id = ?, acceptance = ?, sprint = ?,
		                  implementer_session = ?, reviewer_session = ?,
		                  review_requested_by_session = ?, closed_by_session = ?,
		                  updated_at = ?, reviewed_at = ?,
		                  closed_at = ?, deleted_at = ?,
		                  defer_until = ?, due_date = ?, defer_count = ?,
		                  creator_session = ?, minor = ?, created_branch = ?
		WHERE id = ?
	`, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority,
		issue.Points, labels, issue.ParentID, issue.Acceptance, issue.Sprint,
		issue.ImplementerSession, issue.ReviewerSession,
		issue.ReviewRequestedBySession, issue.ClosedBySession,
		issue.UpdatedAt, issue.ReviewedAt,
		issue.ClosedAt, issue.DeletedAt,
		deferUntil, dueDate, issue.DeferCount,
		issue.CreatorSession, issue.Minor, issue.CreatedBranch, issue.ID)
	if err != nil {
		return err
	}

	// Serialize review metadata as an extended ReviewUndoPayload into NewData.
	// Older undo code expects NewData to be bare Issue JSON; since most undo
	// code paths only read PreviousData, NewData is a safe place to stash
	// review metadata. The undo code explicitly parses NewData as
	// ReviewUndoPayload when handling review-aware actions.
	payload := models.ReviewUndoPayload{
		Issue:               issue,
		CreatedReviewID:     createdReviewID,
		PriorActiveReviewID: priorActiveReviewID,
	}
	newData, _ := json.Marshal(payload)

	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate action ID: %w", err)
	}
	actionTS := formatActionLogTimestamp(issue.UpdatedAt)
	_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		actionID, sessionID, string(actionType), "issue", issue.ID, previousData, string(newData), actionTS)
	if err != nil {
		return fmt.Errorf("log action: %w", err)
	}

	// Approve / close-after-review are NOT review-invalidating: status goes
	// in_review -> closed, and the approve path intentionally creates the
	// review row it wants to keep active. Skip supersedeIfReviewInvalidating
	// for these actions so we don't immediately supersede our own just-created
	// approval row.
	if actionType != models.ActionApprove &&
		actionType != models.ActionReviewApprove &&
		actionType != models.ActionReviewChangesRequested &&
		actionType != models.ActionCloseAfterReview {
		db.supersedeIfReviewInvalidating(prev, issue)
	}

	return nil
}

// UpdateIssueLoggedIfStatus updates an issue and logs the action atomically,
// but only if the current persisted status still matches expectedStatus.
func (db *DB) UpdateIssueLoggedIfStatus(issue *models.Issue, expectedStatus models.Status, sessionID string, actionType models.ActionType) error {
	return db.withWriteLock(func() error {
		prev, err := db.scanIssueRow(issue.ID)
		if err != nil {
			return err
		}
		if prev.Status != expectedStatus {
			return &StaleIssueStatusError{
				IssueID:  issue.ID,
				Expected: expectedStatus,
				Actual:   prev.Status,
			}
		}
		return db.updateIssueAndLogFromPrevious(issue, prev, sessionID, actionType)
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
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionDelete), "issue", issueID, previousData, "", actionTS)
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
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionRestore), "issue", issueID, previousData, newData, actionTS)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}
