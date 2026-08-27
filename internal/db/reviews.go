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

type reviewSyncStore interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (db *DB) withReviewSyncTxLocked(fn func(*sql.Tx) error) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateIssueReview inserts a new review row and returns its id. The caller
// is responsible for superseding any prior active review (see
// SupersedeActiveReviews) — this helper only appends history.
func (db *DB) CreateIssueReview(rv NewReview) (string, error) {
	var id string
	err := db.withWriteLock(func() error {
		err := db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			createdID, err := db.createIssueReview(tx, rv)
			if err != nil {
				return err
			}
			id = createdID
			return nil
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (db *DB) createIssueReview(store reviewSyncStore, rv NewReview) (string, error) {
	newID, err := generateTextID(reviewIDPrefix)
	if err != nil {
		return "", fmt.Errorf("generate review id: %w", err)
	}
	createdAt := time.Now()
	_, err = store.Exec(`
		INSERT INTO issue_reviews (id, issue_id, reviewer_session, decision, summary, requested_by_session, created_at, self_review, reviewed_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, newID, NormalizeIssueID(rv.IssueID), rv.ReviewerSession, rv.Decision, rv.Summary,
		rv.RequestedBySession, createdAt, rv.SelfReview, rv.ReviewedBy)
	if err != nil {
		return "", fmt.Errorf("insert issue_reviews: %w", err)
	}
	created := &models.IssueReview{
		ID: newID, IssueID: NormalizeIssueID(rv.IssueID), ReviewerSession: rv.ReviewerSession,
		Decision: rv.Decision, Summary: rv.Summary, RequestedBySession: rv.RequestedBySession,
		CreatedAt: createdAt, SelfReview: rv.SelfReview, ReviewedBy: rv.ReviewedBy,
	}
	if err := db.logIssueReviewAction(store, rv.ReviewerSession, models.ActionCreate, nil, created); err != nil {
		return "", err
	}
	return newID, nil
}

// CreateIssueReviewAndUpdateIssueLogged persists an entire review lifecycle
// transition in one transaction: prior-review supersession, the new review and
// its sync event, the issue mutation, and the enclosing issue action event.
func (db *DB) CreateIssueReviewAndUpdateIssueLogged(
	rv NewReview, issue *models.Issue, expectedStatus models.Status,
	sessionID string, actionType models.ActionType,
) (string, error) {
	var reviewID string
	err := db.withWriteLock(func() error {
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			prev, err := db.scanIssueRowFrom(tx, issue.ID)
			if err != nil {
				return err
			}
			if prev.Status != expectedStatus {
				return &StaleIssueStatusError{IssueID: issue.ID, Expected: expectedStatus, Actual: prev.Status}
			}
			prior, err := db.getActiveApprovalReview(tx, issue.ID)
			if err != nil {
				return err
			}
			priorID := ""
			if prior != nil {
				priorID = prior.ID
			}
			if err := db.supersedeActiveReviewsLogged(tx, issue.ID, sessionID); err != nil {
				return err
			}
			reviewID, err = db.createIssueReview(tx, rv)
			if err != nil {
				return err
			}
			return db.updateIssueAndLogFromPreviousWithReviewMetaStore(
				tx, issue, prev, sessionID, actionType, reviewID, priorID,
			)
		})
	})
	if err != nil {
		return "", err
	}
	return reviewID, nil
}

// logIssueReviewAction writes the sync event for a review mutation using the
// same transaction/store as the row mutation.
// Review events are sync-only action_log rows; the user-facing undo operation
// remains the enclosing issue transition carrying ReviewUndoPayload.
func (db *DB) logIssueReviewAction(store reviewSyncStore, sessionID string, actionType models.ActionType, previous, next *models.IssueReview) error {
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
	_, err = store.Exec(`
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
	return db.getActiveApprovalReview(db.conn, issueID)
}

func (db *DB) getActiveApprovalReview(store reviewSyncStore, issueID string) (*models.IssueReview, error) {
	row := store.QueryRow(`
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
	defer func() { _ = rows.Close() }()

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
	return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
		return db.supersedeActiveReviewsLogged(tx, issueID, sessionID)
	})
}

func (db *DB) supersedeActiveReviewsLogged(store reviewSyncStore, issueID, sessionID string) error {
	rows, err := db.listIssueReviews(store, NormalizeIssueID(issueID))
	if err != nil {
		return err
	}
	now := time.Now()
	for _, previous := range rows {
		if previous.SupersededAt != nil {
			continue
		}
		if _, err := store.Exec(`UPDATE issue_reviews SET superseded_at = ? WHERE id = ? AND superseded_at IS NULL`, now, previous.ID); err != nil {
			return err
		}
		next := *previous
		next.SupersededAt = &now
		if err := db.logIssueReviewAction(store, sessionID, models.ActionUpdate, previous, &next); err != nil {
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
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			return db.deleteIssueReviewLogged(tx, reviewID, sessionID)
		})
	})
}

func (db *DB) deleteIssueReviewLogged(store reviewSyncStore, reviewID, sessionID string) error {
	previous, err := db.getIssueReviewByID(store, reviewID)
	if err != nil || previous == nil {
		return err
	}
	if _, err := store.Exec(`DELETE FROM issue_reviews WHERE id = ?`, reviewID); err != nil {
		return err
	}
	return db.logIssueReviewAction(store, sessionID, models.ActionReviewDelete, previous, nil)
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
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			return db.clearReviewSupersededAtLogged(tx, reviewID, sessionID)
		})
	})
}

func (db *DB) clearReviewSupersededAtLogged(store reviewSyncStore, reviewID, sessionID string) error {
	previous, err := db.getIssueReviewByID(store, reviewID)
	if err != nil || previous == nil || previous.SupersededAt == nil {
		return err
	}
	if _, err := store.Exec(`UPDATE issue_reviews SET superseded_at = NULL WHERE id = ?`, reviewID); err != nil {
		return err
	}
	next := *previous
	next.SupersededAt = nil
	return db.logIssueReviewAction(store, sessionID, models.ActionUpdate, previous, &next)
}

// UndoIssueReviewEffectsLogged atomically rolls back all issue_reviews
// side-effects carried by a ReviewUndoPayload. If any sync-event insertion
// fails, neither the delete nor the reactivation is committed, so td undo can
// safely return an error and remain retryable.
func (db *DB) UndoIssueReviewEffectsLogged(createdReviewID, priorActiveReviewID, sessionID string) error {
	if createdReviewID == "" && priorActiveReviewID == "" {
		return nil
	}
	return db.withWriteLock(func() error {
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			if createdReviewID != "" {
				if err := db.deleteIssueReviewLogged(tx, createdReviewID, sessionID); err != nil {
					return err
				}
			}
			if priorActiveReviewID != "" {
				if err := db.clearReviewSupersededAtLogged(tx, priorActiveReviewID, sessionID); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// UndoReviewAwareIssueActionLogged rolls back the review effects, restores the
// issue, emits the restoration event, and marks the original action undone in
// one transaction. A failure at any event boundary leaves the action retryable.
func (db *DB) UndoReviewAwareIssueActionLogged(
	originalActionID string, restoredIssue *models.Issue,
	createdReviewID, priorActiveReviewID, sessionID string,
) error {
	return db.withWriteLock(func() error {
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			if createdReviewID != "" {
				if err := db.deleteIssueReviewLogged(tx, createdReviewID, sessionID); err != nil {
					return err
				}
			}
			if priorActiveReviewID != "" {
				if err := db.clearReviewSupersededAtLogged(tx, priorActiveReviewID, sessionID); err != nil {
					return err
				}
			}
			current, err := db.scanIssueRowFrom(tx, restoredIssue.ID)
			if err != nil {
				return err
			}
			if err := db.updateIssueAndLogFromPreviousStore(
				tx, restoredIssue, current, sessionID, models.ActionUpdate,
			); err != nil {
				return err
			}
			result, err := tx.Exec(`UPDATE action_log SET undone = 1 WHERE id = ? AND undone = 0`, originalActionID)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("action %s is missing or already undone", originalActionID)
			}
			return nil
		})
	})
}

func (db *DB) getIssueReviewByID(store reviewSyncStore, reviewID string) (*models.IssueReview, error) {
	row := store.QueryRow(`
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

func (db *DB) listIssueReviews(store reviewSyncStore, issueID string) ([]*models.IssueReview, error) {
	rows, err := store.Query(`
		SELECT id, issue_id, reviewer_session, decision, summary, requested_by_session,
		       created_at, superseded_at, self_review, reviewed_by
		FROM issue_reviews
		WHERE issue_id = ?
		ORDER BY created_at ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var reviews []*models.IssueReview
	for rows.Next() {
		var r models.IssueReview
		var summary, requestedBy, reviewedBy sql.NullString
		var supersededAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.IssueID, &r.ReviewerSession, &r.Decision, &summary,
			&requestedBy, &r.CreatedAt, &supersededAt, &r.SelfReview, &reviewedBy); err != nil {
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
	return reviews, rows.Err()
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
	_ = db.clearIssueReviewStamp(issueID, sessionID)
}

// clearIssueReviewStamp clears issues.reviewer_session / reviewed_at AND
// records the action_log entry that makes the change visible to sync.
//
// The bare `UPDATE issues SET reviewer_session = ”, reviewed_at = NULL` this
// replaces is the update-side form of the td-018ee1 / td-8aa916 defect. The
// sync engine derives its outbound events exclusively from action_log
// (internal/sync/client.go GetPendingEvents), so a column mutated without a
// matching entry stays on the writing client forever: the row itself has a
// create event, so BackfillOrphanEntities sees nothing to rescue, and
// BackfillStaleIssues gives up once last_pulled_server_seq > 0. The reviewing
// peer keeps a reviewer_session this client cleared, permanently — which is
// what TestChaosSync reported as `issues match — common set diverges`.
//
// TestActionLogReconstructsEveryIssue (sync_update_invariant_test.go) is the
// global guard: it replays each issue's action_log the way the receiving side
// applies it and fails if the replay does not reproduce the row.
//
// Callers reach this from outside withWriteLock (the side-table mutation has
// already committed), so the logged write takes the lock itself. Errors are
// returned but callers treat them as best-effort: the primary mutation has
// succeeded and a failed badge cleanup must not fail the user's link op.
func (db *DB) clearIssueReviewStamp(issueID, sessionID string) error {
	return db.withWriteLock(func() error {
		prev, err := db.scanIssueRow(issueID)
		if err != nil || prev == nil {
			return err
		}
		if prev.ReviewerSession == "" && prev.ReviewedAt == nil {
			return nil
		}
		next := *prev
		next.ReviewerSession = ""
		next.ReviewedAt = nil
		return db.updateIssueAndLogFromPrevious(&next, prev, sessionID, models.ActionUpdate)
	})
}
