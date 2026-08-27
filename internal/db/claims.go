package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// ClaimRelease names one implementer claim to release.
//
// ExpectedHolder, when non-empty, is a guard: the claim is released only if
// the issue is STILL held by that session at the moment the write lock is
// held. A sweep selects its targets from a snapshot taken before the lock, and
// between the snapshot and the write another process can legitimately unstart
// the issue, hand it off, or start it fresh under a new session. Releasing on
// the strength of a stale snapshot silently revokes that newer claim.
type ClaimRelease struct {
	IssueID string
	// ExpectedHolder is the session the caller believes holds the claim.
	// Empty means "whoever holds it" — used by the named `td unstart <id>`
	// path, where the operator is naming the issue explicitly.
	ExpectedHolder string
	// LogMessage is written to the issue's history as a progress log.
	LogMessage string
}

// ClaimReleaseOutcome reports what happened to one ClaimRelease. Exactly one
// of Released / Skipped / Err is meaningful.
type ClaimReleaseOutcome struct {
	IssueID string
	// Released is true only when this call performed the state change.
	Released bool
	// Skipped is true when the guard did not hold — the issue was not
	// claimed any more, or a different session holds it now. This is not an
	// error: the claim moved on, and the caller must not count it as released.
	Skipped bool
	// Reason explains a Skipped outcome in caller-facing terms.
	Reason string
	// Issue is the post-release state, set only when Released.
	Issue *models.Issue
	// Err is a genuine failure (database error) for this issue.
	Err error
}

// ReleaseClaims releases a batch of implementer claims in ONE write-lock
// acquisition and ONE transaction.
//
// Two properties matter and neither is available by looping over
// UpdateIssueLogged:
//
//  1. Atomic-per-issue guarding. Each issue is re-read inside the lock and the
//     UPDATE carries a WHERE clause on the status and holder that were just
//     read. A row whose claim moved under us reports Skipped, never Released.
//     Nothing is written from the caller's pre-lock snapshot, so a concurrent
//     `td update --title` on the same issue is not reverted.
//
//  2. One lock acquisition for the whole batch. The per-issue path takes and
//     releases the cross-process write lock about four times per issue
//     (session action, issue update, log, focus). Under a large sweep that
//     starves live agents: the lock timeout is 500ms and a real `td start`
//     fails outright when it loses that race. A harness tick whose `td start`
//     fails is a dead tick.
func (db *DB) ReleaseClaims(reqs []ClaimRelease, sessionID string) ([]ClaimReleaseOutcome, error) {
	outcomes := make([]ClaimReleaseOutcome, len(reqs))
	for i, r := range reqs {
		outcomes[i].IssueID = r.IssueID
	}
	if len(reqs) == 0 {
		return outcomes, nil
	}

	err := db.withWriteLock(func() error {
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			for i, req := range reqs {
				out := &outcomes[i]
				prev, err := db.scanIssueRowFrom(tx, req.IssueID)
				if err != nil {
					out.Err = err
					continue
				}

				// The releasable states are: an in_progress issue (the normal
				// case) and an open issue that still carries a claim (which
				// `td reopen` used to leave behind). Anything else means the
				// issue moved on.
				releasable := prev.Status == models.StatusInProgress ||
					(prev.Status == models.StatusOpen && prev.ImplementerSession != "")
				if !releasable {
					out.Skipped = true
					out.Reason = fmt.Sprintf("no longer releasable (status %s, implementer %q)",
						prev.Status, prev.ImplementerSession)
					continue
				}
				if req.ExpectedHolder != "" && prev.ImplementerSession != req.ExpectedHolder {
					out.Skipped = true
					out.Reason = fmt.Sprintf("claim moved: held by %q, expected %q",
						prev.ImplementerSession, req.ExpectedHolder)
					continue
				}

				next := *prev
				next.Status = models.StatusOpen
				next.ImplementerSession = ""
				next.UpdatedAt = time.Now()

				// in_progress -> open is not review-invalidating (only a
				// transition OUT of in_review is), but routing through the
				// shared helper keeps this path from diverging if that
				// policy ever changes.
				if err := db.supersedeIfReviewInvalidating(tx, prev, &next, sessionID); err != nil {
					out.Err = fmt.Errorf("supersede invalidated review: %w", err)
					continue
				}

				res, err := tx.Exec(`
					UPDATE issues SET status = ?, implementer_session = '', updated_at = ?
					WHERE id = ? AND status = ? AND implementer_session = ?
				`, next.Status, next.UpdatedAt, next.ID, prev.Status, prev.ImplementerSession)
				if err != nil {
					out.Err = err
					continue
				}
				affected, err := res.RowsAffected()
				if err != nil {
					out.Err = err
					continue
				}
				if affected == 0 {
					// The guard did not hold. Cannot happen while we hold the
					// write lock and the row was just read in this same
					// transaction, but reporting it is cheaper than assuming.
					out.Skipped = true
					out.Reason = "claim moved before the guarded update applied"
					continue
				}

				if err := db.recordClaimReleaseHistory(tx, prev, &next, sessionID, req.LogMessage); err != nil {
					out.Err = err
					continue
				}

				released := next
				out.Released = true
				out.Issue = &released
			}
			return nil
		})
	})
	if err != nil {
		return outcomes, err
	}
	return outcomes, nil
}

// recordClaimReleaseHistory writes the audit trail for one released claim:
// the action_log entry, the session-history row (recorded against the
// pre-release state so bypass prevention still sees the touch), and the
// progress log naming the reason.
func (db *DB) recordClaimReleaseHistory(tx *sql.Tx, prev, next *models.Issue, sessionID, logMessage string) error {
	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate action ID: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		actionID, sessionID, string(models.ActionReopen), "issue", next.ID,
		marshalIssue(prev), marshalIssue(next), formatActionLogTimestamp(next.UpdatedAt)); err != nil {
		return fmt.Errorf("log action: %w", err)
	}

	histID, err := generateID()
	if err != nil {
		return fmt.Errorf("generate history ID: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO issue_session_history (id, issue_id, session_id, action, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		histID, NormalizeIssueID(next.ID), sessionID, models.ActionSessionUnstarted, next.UpdatedAt); err != nil {
		return fmt.Errorf("record session history: %w", err)
	}

	if logMessage == "" {
		return nil
	}
	logID, err := generateLogID()
	if err != nil {
		return fmt.Errorf("generate log ID: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO logs (id, issue_id, session_id, work_session_id, message, type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		logID, next.ID, sessionID, "", logMessage, models.LogTypeProgress, next.UpdatedAt); err != nil {
		return fmt.Errorf("write progress log: %w", err)
	}

	// The sync engine derives its events from action_log, so a logs row written
	// without a matching action_log entry is invisible to every other client:
	// the release is recorded here and nowhere else, permanently. Backfill does
	// not rescue it either — it only runs before a client's first pull.
	logActionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate log action ID: %w", err)
	}
	logData, err := json.Marshal(map[string]any{
		"id": logID, "issue_id": next.ID, "session_id": sessionID,
		"work_session_id": "", "message": logMessage,
		"type": models.LogTypeProgress, "timestamp": next.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal progress log: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, 'create', 'logs', ?, '', ?, ?, 0)`,
		logActionID, sessionID, logID, string(logData),
		formatActionLogTimestamp(next.UpdatedAt)); err != nil {
		return fmt.Errorf("log progress log action: %w", err)
	}
	return nil
}

// LatestIssueActivity returns, per issue id, the most recent timestamp of any
// recorded work on that issue: its own updated_at, its newest progress log,
// and its newest action_log entry — by ANY session.
//
// Claim liveness cannot be read off the holder session row alone. Session
// identity is keyed by branch + agent fingerprint + context + worktree, so an
// agent that changes branch or worktree mid-task mints a NEW session row and
// stops heartbeating the row named in implementer_session. The old row then
// looks dead while the agent is alive and working — and the evidence that it
// is alive is exactly the writes it keeps making against the issue.
//
// Missing ids are simply absent from the map. Timestamps that cannot be parsed
// are skipped rather than treated as the zero time, which would read as
// "ancient" and push a claim toward release.
func (db *DB) LatestIssueActivity(issueIDs []string) (map[string]time.Time, error) {
	latest := make(map[string]time.Time, len(issueIDs))
	if len(issueIDs) == 0 {
		return latest, nil
	}
	wanted := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		wanted[NormalizeIssueID(id)] = true
	}

	note := func(id string, t time.Time) {
		if t.IsZero() {
			return
		}
		id = NormalizeIssueID(id)
		if !wanted[id] {
			return
		}
		if cur, ok := latest[id]; !ok || t.After(cur) {
			latest[id] = t
		}
	}

	// Timestamps in both tables have been written by several td generations in
	// several layouts, so they are read as text and parsed leniently.
	queries := []string{
		`SELECT issue_id, MAX(timestamp) FROM logs WHERE issue_id != '' GROUP BY issue_id`,
		`SELECT entity_id, MAX(timestamp) FROM action_log WHERE entity_type = 'issue' GROUP BY entity_id`,
		`SELECT id, updated_at FROM issues`,
	}
	for _, q := range queries {
		rows, err := db.conn.Query(q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var ts sql.NullString
			if err := rows.Scan(&id, &ts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !ts.Valid {
				continue
			}
			if t, ok := ParseLenientTime(ts.String); ok {
				note(id, t)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return latest, nil
}

// DeleteSessionsByID removes exactly the named session rows.
//
// `td session cleanup` previews in Go and used to delete in SQL with a
// re-evaluated predicate, so the two could disagree — and the rows it must NOT
// delete (holders of live claims) are not expressible as a time predicate at
// all. Deleting the list that was previewed keeps the preview honest.
func (db *DB) DeleteSessionsByID(ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var count int64
	err := db.withWriteLock(func() error {
		for _, id := range ids {
			res, err := db.conn.Exec(`DELETE FROM sessions WHERE id = ?`, id)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			count += n
		}
		return nil
	})
	return count, err
}

// SessionsHoldingClaims returns, per session id, how many issues name that
// session as implementer. `td session cleanup` uses it to avoid deleting the
// very rows that make a leaked claim reclaimable.
//
// The selection is the same set ReleaseClaims calls releasable: in_progress,
// plus open-while-still-naming-a-holder. That second case is the leak this
// exists to protect — older builds of `td reopen` and `td unblock` left the
// implementer set on an open issue, and counting only in_progress meant
// cleanup deleted that holder's session row while the claim was still on the
// issue, leaving nothing any surface could reclaim.
//
// Deliberately NOT widened past that. A closed issue keeps its implementer as
// the record of who did the work, and protecting those holders forever would
// mean cleanup could never delete a session that ever finished anything. A
// blocked issue keeps its holder on purpose and is reachable through
// `td list --status blocked`, so it is not invisible the way open+held is.
func (db *DB) SessionsHoldingClaims() (map[string]int, error) {
	rows, err := db.conn.Query(`
		SELECT implementer_session, COUNT(*) FROM issues
		WHERE status IN (?, ?) AND implementer_session != '' AND deleted_at IS NULL
		GROUP BY implementer_session`,
		string(models.StatusInProgress), string(models.StatusOpen))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	held := make(map[string]int)
	for rows.Next() {
		var sessionID string
		var count int
		if err := rows.Scan(&sessionID, &count); err != nil {
			return nil, err
		}
		held[sessionID] = count
	}
	return held, rows.Err()
}
