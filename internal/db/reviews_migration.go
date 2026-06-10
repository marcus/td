package db

import "fmt"

// migrateReviewAttestations is migration 31. It adds review-attestation
// columns to the issues table, creates the issue_reviews history table, and
// backfills closed_by_session for historical closed rows.
//
// The migration is idempotent: if columns or the table already exist the
// corresponding steps are skipped so partial runs or cross-version databases
// re-open cleanly.
func (db *DB) migrateReviewAttestations() error {
	// 1. Add nullable reviewed_at column to issues.
	hasReviewedAt, err := db.columnExists("issues", "reviewed_at")
	if err != nil {
		return fmt.Errorf("check issues.reviewed_at: %w", err)
	}
	if !hasReviewedAt {
		if _, err := db.conn.Exec(`ALTER TABLE issues ADD COLUMN reviewed_at DATETIME`); err != nil {
			return fmt.Errorf("add issues.reviewed_at: %w", err)
		}
	}

	// 2. Add review_requested_by_session column to issues.
	hasRequestedBy, err := db.columnExists("issues", "review_requested_by_session")
	if err != nil {
		return fmt.Errorf("check issues.review_requested_by_session: %w", err)
	}
	if !hasRequestedBy {
		if _, err := db.conn.Exec(`ALTER TABLE issues ADD COLUMN review_requested_by_session TEXT DEFAULT ''`); err != nil {
			return fmt.Errorf("add issues.review_requested_by_session: %w", err)
		}
	}

	// 3. Add closed_by_session column to issues.
	hasClosedBy, err := db.columnExists("issues", "closed_by_session")
	if err != nil {
		return fmt.Errorf("check issues.closed_by_session: %w", err)
	}
	if !hasClosedBy {
		if _, err := db.conn.Exec(`ALTER TABLE issues ADD COLUMN closed_by_session TEXT DEFAULT ''`); err != nil {
			return fmt.Errorf("add issues.closed_by_session: %w", err)
		}
	}

	// 4. Create issue_reviews table + indexes (idempotent).
	// TODO (Step 2): the issue_id FK has no ON DELETE CASCADE. Adding ON DELETE
	// to an existing FK in SQLite requires recreating the table with PRAGMA
	// foreign_keys=OFF, copying rows, dropping the old table, renaming. That
	// carries real downgrade/corruption risk for Batch 1b (no behavior change)
	// and is deferred to Step 2 where the schema already gets a broader
	// reshape. For now an explicit delete of an issue with reviews will leave
	// orphan issue_reviews rows; Step 2 should either add a cascade migration
	// or surface an application-level cleanup path in DeleteIssue.
	if _, err := db.conn.Exec(`
CREATE TABLE IF NOT EXISTS issue_reviews (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    reviewer_session TEXT NOT NULL,
    decision TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    requested_by_session TEXT DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    superseded_at DATETIME,
    FOREIGN KEY (issue_id) REFERENCES issues(id)
);
CREATE INDEX IF NOT EXISTS idx_issue_reviews_issue_id ON issue_reviews(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_reviews_active ON issue_reviews(issue_id) WHERE superseded_at IS NULL;
`); err != nil {
		return fmt.Errorf("create issue_reviews: %w", err)
	}

	// 5. Backfill closed_by_session for historical closed rows. reviewed_at is
	// intentionally left NULL on historical rows since we cannot know the true
	// review timing; closed_at would misrepresent it.
	if _, err := db.conn.Exec(`
UPDATE issues
   SET closed_by_session = reviewer_session
 WHERE status = 'closed'
   AND reviewer_session != ''
   AND (closed_by_session IS NULL OR closed_by_session = '')
`); err != nil {
		return fmt.Errorf("backfill closed_by_session: %w", err)
	}

	return nil
}

// migrateSelfReviewColumn is migration 32. It adds the self_review audit
// column to the issue_reviews table (Stage 2 of trusted-review-mode).
//
// Idempotent: the columnExists guard skips the ALTER when the column already
// exists, so partial runs or cross-version databases re-open cleanly. Mirrors
// the v31 columnExists precedent rather than a raw ALTER.
func (db *DB) migrateSelfReviewColumn() error {
	hasSelfReview, err := db.columnExists("issue_reviews", "self_review")
	if err != nil {
		return fmt.Errorf("check issue_reviews.self_review: %w", err)
	}
	if !hasSelfReview {
		if _, err := db.conn.Exec(`ALTER TABLE issue_reviews ADD COLUMN self_review INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add issue_reviews.self_review: %w", err)
		}
	}
	return nil
}
