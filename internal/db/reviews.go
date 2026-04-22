package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// CreateIssueReview inserts a new review row and returns its id. The caller
// is responsible for superseding any prior active review (see
// SupersedeActiveReviews) — this helper only appends history.
func (db *DB) CreateIssueReview(issueID, reviewerSession, decision, summary, requestedBySession string) (string, error) {
	var id string
	err := db.withWriteLock(func() error {
		newID, err := generateTextID(reviewIDPrefix)
		if err != nil {
			return fmt.Errorf("generate review id: %w", err)
		}
		_, err = db.conn.Exec(`
			INSERT INTO issue_reviews (id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, newID, NormalizeIssueID(issueID), reviewerSession, decision, summary, requestedBySession, time.Now())
		if err != nil {
			return fmt.Errorf("insert issue_reviews: %w", err)
		}
		id = newID
		return nil
	})
	return id, err
}

// GetActiveApprovalReview returns the current non-superseded approval review
// for an issue, or nil if none exists. Only decisions that represent an
// actual approval are considered (approved and approved_by_parent_cascade);
// a non-superseded changes_requested row does not mean the issue has an
// active approval and is therefore skipped.
func (db *DB) GetActiveApprovalReview(issueID string) (*models.IssueReview, error) {
	row := db.conn.QueryRow(`
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, superseded_at
		FROM issue_reviews
		WHERE issue_id = ?
		  AND superseded_at IS NULL
		  AND decision IN ('approved', 'approved_by_parent_cascade')
		ORDER BY created_at DESC
		LIMIT 1
	`, NormalizeIssueID(issueID))

	var r models.IssueReview
	var summary, requestedBy sql.NullString
	var supersededAt sql.NullTime
	if err := row.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary, &requestedBy, &r.CreatedAt, &supersededAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.Summary = summary.String
	r.RequestedBySession = requestedBy.String
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
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, superseded_at
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
		var summary, requestedBy sql.NullString
		var supersededAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary, &requestedBy, &r.CreatedAt, &supersededAt); err != nil {
			return nil, err
		}
		r.Summary = summary.String
		r.RequestedBySession = requestedBy.String
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
func (db *DB) supersedeApprovalIfLinked(issueID string) {
	issueID = NormalizeIssueID(issueID)

	// Cheap pre-check: skip the write when there is no active approval.
	// GetActiveApprovalReview reads-through sql.ErrNoRows as nil/nil, so
	// the common case (issue has no active approval) exits fast.
	rev, err := db.GetActiveApprovalReview(issueID)
	if err != nil || rev == nil {
		return
	}

	if err := db.SupersedeActiveReviews(issueID); err != nil {
		return
	}
	_, _ = db.conn.Exec(
		`UPDATE issues SET reviewer_session = '', reviewed_at = NULL WHERE id = ?`,
		issueID,
	)
}
