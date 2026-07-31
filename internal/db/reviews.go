package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// NewReview describes a review row to append. It replaced a positional
// signature that had grown to six arguments and was about to take a seventh —
// at that width a caller transposing SelfReview and an attribution string
// would compile fine and write a false audit record.
type NewReview struct {
	IssueID            string
	ReviewerSession    string // the session recording the row
	Decision           string // reviewpolicy.Decision* constant
	Summary            string
	RequestedBySession string

	// SelfReview marks the row as recorded by an implementation-involved
	// session. Callers must stamp this from the reviewpolicy decision, never
	// from raw user input, or a request could forge the flag in either
	// direction.
	SelfReview bool

	// ReviewedBy names who performed the review when that differs from
	// ReviewerSession. Empty means the recording session reviewed it itself.
	ReviewedBy string
}

// CreateIssueReview inserts a new review row and returns its id. The caller
// is responsible for superseding any prior active review (see
// SupersedeActiveReviews) — this helper only appends history.
func (db *DB) CreateIssueReview(rv NewReview) (string, error) {
	var id string
	err := db.withWriteLock(func() error {
		newID, err := generateTextID(reviewIDPrefix)
		if err != nil {
			return fmt.Errorf("generate review id: %w", err)
		}
		createdAt := time.Now()
		_, err = db.conn.Exec(`
			INSERT INTO issue_reviews (id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, self_review, reviewed_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, newID, NormalizeIssueID(rv.IssueID), rv.ReviewerSession, rv.Decision, rv.Summary,
			rv.RequestedBySession, createdAt, rv.SelfReview, rv.ReviewedBy)
		if err != nil {
			return fmt.Errorf("insert issue_reviews: %w", err)
		}
		created := &models.IssueReview{
			ID:                 newID,
			IssueID:            NormalizeIssueID(rv.IssueID),
			ReviewerSession:    rv.ReviewerSession,
			Decision:           rv.Decision,
			Summary:            rv.Summary,
			RequestedBySession: rv.RequestedBySession,
			CreatedAt:          createdAt,
			SelfReview:         rv.SelfReview,
			ReviewedBy:         rv.ReviewedBy,
		}
		if err := db.logIssueReviewActionLocked(rv.ReviewerSession, models.ActionCreate, nil, created); err != nil {
			_, _ = db.conn.Exec(`DELETE FROM issue_reviews WHERE id = ?`, newID)
			return err
		}
		id = newID
		return nil
	})
	return id, err
}

// logIssueReviewActionLocked writes the sync event for a review mutation.
// Review events are sync-only action_log rows; the user-facing undo operation
// remains the enclosing issue transition carrying ReviewUndoPayload.
func (db *DB) logIssueReviewActionLocked(sessionID string, actionType models.ActionType, previous, next *models.IssueReview) error {
	entityID := ""
	if next != nil {
		entityID = next.ID
	} else if previous != nil {
		entityID = previous.ID
	}
	previousData, _ := json.Marshal(previous)
	newData, _ := json.Marshal(next)
	if previous == nil {
		previousData = nil
	}
	if next == nil {
		newData = nil
	}
	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate review action ID: %w", err)
	}
	_, err = db.conn.Exec(`
		INSERT INTO action_log
			(id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, ?, 'issue_reviews', ?, ?, ?, ?, 0)
	`, actionID, sessionID, string(actionType), entityID, string(previousData), string(newData), actionLogTimestampNow())
	if err != nil {
		return fmt.Errorf("log issue review action: %w", err)
	}
	return nil
}

// GetActiveApprovalReview returns the current non-superseded approval review
// for an issue, or nil if none exists. Only decisions that represent an
// actual approval are considered (approved and approved_by_parent_cascade);
// a non-superseded changes_requested row does not mean the issue has an
// active approval and is therefore skipped.
func (db *DB) GetActiveApprovalReview(issueID string) (*models.IssueReview, error) {
	row := db.conn.QueryRow(`
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, superseded_at, self_review, reviewed_by
		FROM issue_reviews
		WHERE issue_id = ?
		  AND superseded_at IS NULL
		  AND decision IN ('approved', 'approved_by_parent_cascade')
		ORDER BY created_at DESC
		LIMIT 1
	`, NormalizeIssueID(issueID))

	var r models.IssueReview
	var summary, requestedBy, reviewedBy sql.NullString
	var supersededAt sql.NullTime
	if err := row.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary, &requestedBy, &r.CreatedAt, &supersededAt, &r.SelfReview, &reviewedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.Summary = summary.String
	r.RequestedBySession = requestedBy.String
	r.ReviewedBy = reviewedBy.String
	if supersededAt.Valid {
		r.SupersededAt = &supersededAt.Time
	}
	return &r, nil
}

// ListIssueReviews returns all reviews for an issue in chronological order
// (oldest first). Superseded and active reviews are both returned so the
// caller can render full history.
func (db *DB) ListIssueReviews(issueID string) ([]*models.IssueReview, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, superseded_at, self_review, reviewed_by
		FROM issue_reviews
		WHERE issue_id = ?
		ORDER BY created_at ASC
	`, NormalizeIssueID(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []*models.IssueReview
	for rows.Next() {
		var r models.IssueReview
		var summary, requestedBy, reviewedBy sql.NullString
		var supersededAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary, &requestedBy, &r.CreatedAt, &supersededAt, &r.SelfReview, &reviewedBy); err != nil {
			return nil, err
		}
		r.Summary = summary.String
		r.RequestedBySession = requestedBy.String
		r.ReviewedBy = reviewedBy.String
		if supersededAt.Valid {
			r.SupersededAt = &supersededAt.Time
		}
		reviews = append(reviews, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reviews, nil
}

// SupersedeActiveReviews marks all non-superseded reviews for an issue as
// superseded with a NOW() timestamp. Idempotent: a second call is a no-op
// because no active rows remain.
func (db *DB) SupersedeActiveReviews(issueID string) error {
	return db.withWriteLock(func() error {
		return db.supersedeActiveReviewsLocked(issueID)
	})
}

// SupersedeActiveReviewsLogged supersedes active rows and emits one update
// event per row so superseded_at converges across peers.
func (db *DB) SupersedeActiveReviewsLogged(issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		return db.supersedeActiveReviewsLoggedLocked(issueID, sessionID)
	})
}

// supersedeActiveReviewsLocked is the lock-free variant called by helpers
// that already hold withWriteLock (e.g. supersedeIfReviewInvalidating running
// inside updateIssueAndLogFromPrevious).
func (db *DB) supersedeActiveReviewsLocked(issueID string) error {
	_, err := db.conn.Exec(`
		UPDATE issue_reviews
		   SET superseded_at = ?
		 WHERE issue_id = ?
		   AND superseded_at IS NULL
	`, time.Now(), NormalizeIssueID(issueID))
	return err
}

func (db *DB) supersedeActiveReviewsLoggedLocked(issueID, sessionID string) error {
	rows, err := db.ListIssueReviews(NormalizeIssueID(issueID))
	if err != nil {
		return err
	}
	now := time.Now()
	for _, previous := range rows {
		if previous.SupersededAt != nil {
			continue
		}
		if _, err := db.conn.Exec(`UPDATE issue_reviews SET superseded_at = ? WHERE id = ? AND superseded_at IS NULL`, now, previous.ID); err != nil {
			return err
		}
		next := *previous
		next.SupersededAt = &now
		if err := db.logIssueReviewActionLocked(sessionID, models.ActionUpdate, previous, &next); err != nil {
			return err
		}
	}
	return nil
}

// DeleteIssueReview removes a review row by id. Used by the undo path to
// roll back reviews that an action inserted; callers pass the
// CreatedReviewID recorded in the action's ReviewUndoPayload.
func (db *DB) DeleteIssueReview(reviewID string) error {
	if reviewID == "" {
		return nil
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM issue_reviews WHERE id = ?`, reviewID)
		return err
	})
}

// DeleteIssueReviewLogged removes a review as part of undo and emits a hard
// delete event so peers roll back the created approval too.
func (db *DB) DeleteIssueReviewLogged(reviewID, sessionID string) error {
	if reviewID == "" {
		return nil
	}
	return db.withWriteLock(func() error {
		previous, err := db.getIssueReviewByID(reviewID)
		if err != nil || previous == nil {
			return err
		}
		if _, err := db.conn.Exec(`DELETE FROM issue_reviews WHERE id = ?`, reviewID); err != nil {
			return err
		}
		return db.logIssueReviewActionLocked(sessionID, models.ActionReviewDelete, previous, nil)
	})
}

// ClearReviewSupersededAt removes the superseded_at timestamp on a review
// row, re-activating it. Used by undo to restore a prior active approval
// that the undone action superseded.
func (db *DB) ClearReviewSupersededAt(reviewID string) error {
	if reviewID == "" {
		return nil
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(
			`UPDATE issue_reviews SET superseded_at = NULL WHERE id = ?`,
			reviewID,
		)
		return err
	})
}

// ClearReviewSupersededAtLogged reactivates a prior review during undo and
// emits the corresponding update event.
func (db *DB) ClearReviewSupersededAtLogged(reviewID, sessionID string) error {
	if reviewID == "" {
		return nil
	}
	return db.withWriteLock(func() error {
		previous, err := db.getIssueReviewByID(reviewID)
		if err != nil || previous == nil {
			return err
		}
		if _, err := db.conn.Exec(`UPDATE issue_reviews SET superseded_at = NULL WHERE id = ?`, reviewID); err != nil {
			return err
		}
		next := *previous
		next.SupersededAt = nil
		return db.logIssueReviewActionLocked(sessionID, models.ActionUpdate, previous, &next)
	})
}

func (db *DB) getIssueReviewByID(reviewID string) (*models.IssueReview, error) {
	row := db.conn.QueryRow(`
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session,
		       created_at, superseded_at, self_review, reviewed_by
		FROM issue_reviews WHERE id = ?
	`, reviewID)
	var r models.IssueReview
	var summary, requestedBy, reviewedBy sql.NullString
	var supersededAt sql.NullTime
	if err := row.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary,
		&requestedBy, &r.CreatedAt, &supersededAt, &r.SelfReview, &reviewedBy); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.Summary = summary.String
	r.RequestedBySession = requestedBy.String
	r.ReviewedBy = reviewedBy.String
	if supersededAt.Valid {
		r.SupersededAt = &supersededAt.Time
	}
	return &r, nil
}

// supersedeApprovalIfLinked is the side-table counterpart to
// supersedeIfReviewInvalidating. It runs after a linked_files /
// issue_dependencies / work_session_issues mutation on an issue that the
// plan considers review-invalidating (LinkedFilesChanged,
// DependenciesChanged, WorkSessionTagsChanged in reviewpolicy.IssueMutation).
//
// Semantics mirror supersedeIfReviewInvalidating:
//   - only supersede when the issue currently carries an active approval
//     review (no-op otherwise)
//   - clear issues.reviewer_session and reviewed_at so the UI badge stops
//     claiming "reviewed"
//   - best-effort: errors are swallowed because the caller's primary
//     mutation has already succeeded and we do not want to fail the
//     user-facing link op over a stale approval cleanup
//
// No-ops for issues whose status isn't in_review (or is already closed) —
// there's no live approval window to invalidate.
func (db *DB) supersedeApprovalIfLinked(issueID, sessionID string) {
	issueID = NormalizeIssueID(issueID)

	// Cheap pre-check: skip the write when there is no active approval.
	// GetActiveApprovalReview reads-through sql.ErrNoRows as nil/nil, so
	// the common case (issue has no active approval) exits fast.
	rev, err := db.GetActiveApprovalReview(issueID)
	if err != nil || rev == nil {
		return
	}

	if sessionID == "" {
		if err := db.SupersedeActiveReviews(issueID); err != nil {
			return
		}
	} else {
		if err := db.SupersedeActiveReviewsLogged(issueID, sessionID); err != nil {
			return
		}
	}
	_, _ = db.conn.Exec(
		`UPDATE issues SET reviewer_session = '', reviewed_at = NULL WHERE id = ?`,
		issueID,
	)
}
