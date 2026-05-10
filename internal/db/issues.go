package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// ListIssuesOptions contains filter options for listing issues
type ListIssuesOptions struct {
	Status               []models.Status
	Type                 []models.Type
	Priority             string
	Labels               []string
	IncludeDeleted       bool
	OnlyDeleted          bool
	Search               string
	Implementer          string
	Reviewer             string
	ReviewableBy         string // Issues that this session can review
	BalancedReviewPolicy bool   // Allow creator-only approvals/reviews when externally implemented
	// ReviewPolicyMode overrides the mode used by ReviewableBy/ReadyToCloseBy
	// filter composition. When empty, falls back to strict (or balanced when
	// BalancedReviewPolicy is true). Step 2 flips delegated-mode callers.
	ReviewPolicyMode string
	// ReadyToCloseBy returns issues where an active approval review exists
	// and the current mode allows close-after-review. In delegated mode any
	// session may close after independent approval; the session value is kept
	// for API symmetry but is not part of the SQL predicate.
	// Empty under strict/balanced; populated under delegated. Safe to set
	// regardless of mode; the SQL composer short-circuits to `0=1` when not
	// applicable.
	ReadyToCloseBy     string
	ParentID           string
	EpicID             string // Filter by epic (parent_id matches epic, recursively)
	PointsMin          int
	PointsMax          int
	CreatedAfter       time.Time
	CreatedBefore      time.Time
	UpdatedAfter       time.Time
	UpdatedBefore      time.Time
	ClosedAfter        time.Time
	ClosedBefore       time.Time
	SortBy             string
	SortDesc           bool
	Limit              int
	IDs                []string
	ExcludeDeferred    bool // Hide issues where defer_until > today
	DeferredOnly       bool // Show ONLY deferred issues (defer_until > today)
	OverdueOnly        bool // Show ONLY overdue issues (due_date < today, not closed)
	SurfacingOnly      bool // Show ONLY surfacing issues (defer_until <= today, defer_count > 0)
	DueSoonDays        int  // Show issues due within N days (0 = disabled)
	ExcludeHasOpenDeps bool // Hide issues that have unresolved (non-closed) dependencies
}

// CreateIssue creates a new issue WITHOUT logging to action_log.
// For local mutations, use CreateIssueLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) CreateIssue(issue *models.Issue) error {
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

		// Retry loop for rare ID collisions (6 hex chars = 16.7M keyspace).
		// 3 retries is sufficient: P(collision) per attempt ≈ N/16.7M where N is
		// existing issues. Even at 10K issues, P(3 consecutive collisions) < 10^-9.
		// We detect collisions via string-based UNIQUE constraint check on the error
		// message because database/sql doesn't expose SQLite error codes directly.
		const maxRetries = 3
		for attempt := 0; attempt < maxRetries; attempt++ {
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
				return nil
			}
			// Only retry on UNIQUE constraint violation (ID collision)
			if !strings.Contains(err.Error(), "UNIQUE constraint") {
				return err
			}
		}
		return fmt.Errorf("failed to generate unique issue ID after %d attempts", maxRetries)
	})
}

// GetIssue retrieves an issue by ID
// Accepts bare IDs without the td- prefix (e.g., "abc123" becomes "td-abc123")
func (db *DB) GetIssue(id string) (*models.Issue, error) {
	id = NormalizeIssueID(id)
	var issue models.Issue
	// NullString for every TEXT DEFAULT '' column: defense against rows
	// with NULL (old data, or sync payloads that pre-dated the fix in
	// internal/sync/events.go).
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
	issue.Points = int(pointsNull.Int64)
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

// GetIssuesByIDs fetches multiple issues in a single query
func (db *DB) GetIssuesByIDs(ids []string) ([]models.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Normalize and dedupe IDs
	seen := make(map[string]bool)
	normalizedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		nid := NormalizeIssueID(id)
		if !seen[nid] {
			seen[nid] = true
			normalizedIDs = append(normalizedIDs, nid)
		}
	}

	placeholders := make([]string, len(normalizedIDs))
	args := make([]interface{}, len(normalizedIDs))
	for i, id := range normalizedIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, review_requested_by_session, closed_by_session,
		       created_at, updated_at, reviewed_at, closed_at, deleted_at, minor, created_branch,
		       defer_until, due_date, defer_count
		FROM issues WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []models.Issue
	for rows.Next() {
		var issue models.Issue
		// NullString for every TEXT DEFAULT '' column — see GetIssue.
		var description, labels sql.NullString
		var closedAt, deletedAt, reviewedAt sql.NullTime
		var parentID, acceptance, sprint sql.NullString
		var implSession, creatorSession, reviewerSession sql.NullString
		var reviewRequestedBy, closedBy sql.NullString
		var createdBranch sql.NullString
		var pointsNull sql.NullInt64
		var deferUntil, dueDate sql.NullString
		if err := rows.Scan(
			&issue.ID, &issue.Title, &description, &issue.Status, &issue.Type, &issue.Priority,
			&pointsNull, &labels, &parentID, &acceptance, &sprint,
			&implSession, &creatorSession, &reviewerSession, &reviewRequestedBy, &closedBy,
			&issue.CreatedAt, &issue.UpdatedAt, &reviewedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
			&deferUntil, &dueDate, &issue.DeferCount,
		); err != nil {
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
		issues = append(issues, issue)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}

// GetIssueTitles fetches titles for multiple issues in a single query
func (db *DB) GetIssueTitles(ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return make(map[string]string), nil
	}

	// Normalize and dedupe IDs
	seen := make(map[string]bool)
	normalizedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		nid := NormalizeIssueID(id)
		if !seen[nid] {
			seen[nid] = true
			normalizedIDs = append(normalizedIDs, nid)
		}
	}

	// Build query with placeholders
	placeholders := make([]string, len(normalizedIDs))
	args := make([]interface{}, len(normalizedIDs))
	for i, id := range normalizedIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, title FROM issues WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	titles := make(map[string]string)
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return titles, nil
}

// UpdateIssue updates an issue WITHOUT logging to action_log.
// For local mutations, use UpdateIssueLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) UpdateIssue(issue *models.Issue) error {
	return db.withWriteLock(func() error {
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

		return err
	})
}

// DeleteIssue soft-deletes an issue WITHOUT logging to action_log.
// For local mutations, use DeleteIssueLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) DeleteIssue(id string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		_, err := db.conn.Exec(`UPDATE issues SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
		return err
	})
}

// RestoreIssue restores a soft-deleted issue
func (db *DB) RestoreIssue(id string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE issues SET deleted_at = NULL, updated_at = ? WHERE id = ?`, time.Now(), id)
		return err
	})
}

// ReviewableByFilter returns the SQL fragment and args for the ReviewableBy filter.
// It is exported so that other packages (e.g. internal/api) can reuse the same policy logic.
//
// Mode mapping (Batch 1c):
//   - balanced=false -> strict SQL
//   - balanced=true  -> balanced SQL
//
// Delegated mode is driven by ReviewableByFilterForMode. The boolean signature
// is kept for backward compatibility; ListIssuesOptions still passes a bool so
// existing callers do not have to be rewritten. A delegated-mode caller (Step 2)
// uses ReviewableByFilterForMode directly.
func ReviewableByFilter(sessionID string, balanced bool) (string, []interface{}) {
	if balanced {
		return reviewableByFilterBalanced(sessionID)
	}
	return reviewableByFilterStrict(sessionID)
}

// ReviewableByFilterForMode composes the reviewable-by SQL fragment for the
// supplied mode string. Exported so other surfaces (monitor list helpers,
// snapshot query source, Step-2 CLI callers) can route through the same
// policy-aware composer as the primary list path.
//
// For "delegated" the filter is based only on implementation independence:
// a session may review when it is not the current implementer and it has no
// started/unstarted history. Prior review/log/history involvement is not a
// review disqualifier in delegated mode.
func ReviewableByFilterForMode(sessionID, mode string) (string, []interface{}) {
	switch mode {
	case "balanced":
		return reviewableByFilterBalanced(sessionID)
	case "delegated":
		return reviewableByFilterDelegated(sessionID)
	default:
		return reviewableByFilterStrict(sessionID)
	}
}

func reviewableByFilterStrict(sessionID string) (string, []interface{}) {
	sql := ` AND status = ? AND implementer_session != '' AND (
		minor = 1 OR (
			implementer_session != ?
			AND (creator_session = '' OR creator_session != ?)
			AND NOT EXISTS (
				SELECT 1 FROM issue_session_history
				WHERE issue_id = issues.id AND session_id = ?
			)
		)
	)`
	return sql, []interface{}{models.StatusInReview, sessionID, sessionID, sessionID}
}

func reviewableByFilterBalanced(sessionID string) (string, []interface{}) {
	sql := ` AND status = ? AND implementer_session != '' AND (
		minor = 1 OR (
			implementer_session != ?
			AND (
				(
					(creator_session = '' OR creator_session != ?)
					AND NOT EXISTS (
						SELECT 1 FROM issue_session_history
						WHERE issue_id = issues.id AND session_id = ?
					)
				)
				OR
				(
					creator_session = ?
					AND implementer_session != ?
					AND NOT EXISTS (
						SELECT 1 FROM issue_session_history
						WHERE issue_id = issues.id
						  AND session_id = ?
						  AND action IN ('started', 'unstarted')
					)
				)
			)
		)
	)`
	return sql, []interface{}{
		models.StatusInReview,
		sessionID, sessionID, sessionID,
		sessionID, sessionID, sessionID,
	}
}

func reviewableByFilterDelegated(sessionID string) (string, []interface{}) {
	sql := ` AND status = ? AND implementer_session != '' AND (
		minor = 1 OR (
			implementer_session != ?
			AND NOT EXISTS (
				SELECT 1 FROM issue_session_history
				WHERE issue_id = issues.id
				  AND session_id = ?
				  AND action IN ('started', 'unstarted')
			)
		)
	)`
	return sql, []interface{}{models.StatusInReview, sessionID, sessionID}
}

// ReadyToCloseByFilter returns the SQL fragment and args for issues that are
// ready to close because an active approval review already exists. Under
// strict and balanced modes there is no close-after-recorded-review path, so
// the filter returns an always-false clause. Under delegated mode it matches
// in_review issues with a non-superseded approval in issue_reviews; the
// closing session is recorded for audit but does not gate the close.
//
// Step 2 wires the CLI / monitor / snapshot-query-source callers; Batch 1c
// only ships the composer so it is ready.
func ReadyToCloseByFilter(sessionID, mode string) (string, []interface{}) {
	if mode != "delegated" {
		// Empty category under strict/balanced: no close-after-review flow.
		return " AND 0=1", nil
	}
	sql := ` AND status = ? AND EXISTS (
		SELECT 1 FROM issue_reviews
		WHERE issue_reviews.issue_id = issues.id
		  AND issue_reviews.superseded_at IS NULL
		  AND issue_reviews.decision IN ('approved', 'approved_by_parent_cascade')
	)`
	return sql, []interface{}{models.StatusInReview}
}

// ListIssues returns issues matching the filter
func (db *DB) ListIssues(opts ListIssuesOptions) ([]models.Issue, error) {
	if opts.ParentID != "" {
		opts.ParentID = NormalizeIssueID(strings.TrimSpace(opts.ParentID))
	}
	if opts.EpicID != "" {
		opts.EpicID = NormalizeIssueID(strings.TrimSpace(opts.EpicID))
	}
	if len(opts.IDs) > 0 {
		seen := make(map[string]bool, len(opts.IDs))
		normalizedIDs := make([]string, 0, len(opts.IDs))
		for _, id := range opts.IDs {
			normalizedID := NormalizeIssueID(strings.TrimSpace(id))
			if normalizedID == "" || seen[normalizedID] {
				continue
			}
			seen[normalizedID] = true
			normalizedIDs = append(normalizedIDs, normalizedID)
		}
		opts.IDs = normalizedIDs
	}

	query := `SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
                 implementer_session, creator_session, reviewer_session, review_requested_by_session, closed_by_session,
                 created_at, updated_at, reviewed_at, closed_at, deleted_at, minor, created_branch,
                 defer_until, due_date, defer_count
          FROM issues WHERE 1=1`
	var args []interface{}

	// Handle deleted filter
	if opts.OnlyDeleted {
		query += " AND deleted_at IS NOT NULL"
	} else if !opts.IncludeDeleted {
		query += " AND deleted_at IS NULL"
	}

	// Status filter
	if len(opts.Status) > 0 {
		placeholders := make([]string, len(opts.Status))
		for i, s := range opts.Status {
			placeholders[i] = "?"
			args = append(args, s)
		}
		query += fmt.Sprintf(" AND status IN (%s)", strings.Join(placeholders, ","))
	}

	// Type filter
	if len(opts.Type) > 0 {
		placeholders := make([]string, len(opts.Type))
		for i, t := range opts.Type {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query += fmt.Sprintf(" AND type IN (%s)", strings.Join(placeholders, ","))
	}

	// ID filter
	if len(opts.IDs) > 0 {
		placeholders := make([]string, len(opts.IDs))
		for i, id := range opts.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += fmt.Sprintf(" AND id IN (%s)", strings.Join(placeholders, ","))
	}

	// Priority filter
	if opts.Priority != "" {
		if strings.HasPrefix(opts.Priority, "<=") {
			prio := strings.TrimPrefix(opts.Priority, "<=")
			query += " AND priority <= ?"
			args = append(args, prio)
		} else if strings.HasPrefix(opts.Priority, ">=") {
			prio := strings.TrimPrefix(opts.Priority, ">=")
			query += " AND priority >= ?"
			args = append(args, prio)
		} else {
			query += " AND priority = ?"
			args = append(args, opts.Priority)
		}
	}

	// Labels filter
	if len(opts.Labels) > 0 {
		for _, label := range opts.Labels {
			query += " AND (labels LIKE ? OR labels LIKE ? OR labels LIKE ? OR labels = ?)"
			args = append(args, label+",%", "%,"+label+",%", "%,"+label, label)
		}
	}

	// Search filter
	if opts.Search != "" {
		query += " AND (id LIKE ? OR title LIKE ? OR description LIKE ?)"
		searchPattern := "%" + opts.Search + "%"
		args = append(args, searchPattern, searchPattern, searchPattern)
	}

	// Implementer filter
	if opts.Implementer != "" {
		query += " AND implementer_session = ?"
		args = append(args, opts.Implementer)
	}

	// Reviewer filter
	if opts.Reviewer != "" {
		query += " AND reviewer_session = ?"
		args = append(args, opts.Reviewer)
	}

	// Reviewable by (issues that can be reviewed by this session)
	// Must be in_review with implementer, and either:
	// - Minor task (always self-reviewable), OR
	// - Strict mode: session is not implementer, not creator, and not in session history
	// - Balanced mode: strict mode OR creator-only exception
	//   (creator can review if someone else implemented and creator never started/unstarted it)
	if opts.ReviewableBy != "" {
		mode := opts.ReviewPolicyMode
		if mode == "" {
			if opts.BalancedReviewPolicy {
				mode = "balanced"
			} else {
				mode = "strict"
			}
		}
		fragment, fargs := ReviewableByFilterForMode(opts.ReviewableBy, mode)
		query += fragment
		args = append(args, fargs...)
	}

	// ReadyToCloseBy: Step-2 caller path. Under strict/balanced modes the
	// composer short-circuits to `0=1`, so this is a no-op for Batch 1c
	// default wiring; it is exercised by the parity suite.
	if opts.ReadyToCloseBy != "" {
		mode := opts.ReviewPolicyMode
		if mode == "" {
			if opts.BalancedReviewPolicy {
				mode = "balanced"
			} else {
				mode = "strict"
			}
		}
		fragment, fargs := ReadyToCloseByFilter(opts.ReadyToCloseBy, mode)
		query += fragment
		args = append(args, fargs...)
	}

	// Parent filter
	if opts.ParentID != "" {
		query += " AND parent_id = ?"
		args = append(args, opts.ParentID)
	}

	// Epic filter (find all descendants of an epic)
	if opts.EpicID != "" {
		// Get all descendants recursively
		descendants, err := db.getDescendants(opts.EpicID)
		if err != nil {
			return nil, fmt.Errorf("get epic descendants: %w", err)
		}
		if len(descendants) > 0 {
			placeholders := make([]string, len(descendants))
			for i, id := range descendants {
				placeholders[i] = "?"
				args = append(args, id)
			}
			query += fmt.Sprintf(" AND id IN (%s)", strings.Join(placeholders, ","))
		} else {
			// No descendants found, return empty result
			query += " AND 1=0"
		}
	}

	// Points filter
	if opts.PointsMin > 0 {
		query += " AND points >= ?"
		args = append(args, opts.PointsMin)
	}
	if opts.PointsMax > 0 {
		query += " AND points <= ?"
		args = append(args, opts.PointsMax)
	}

	// Date filters
	if !opts.CreatedAfter.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, opts.CreatedAfter)
	}
	if !opts.CreatedBefore.IsZero() {
		query += " AND created_at <= ?"
		args = append(args, opts.CreatedBefore)
	}
	if !opts.UpdatedAfter.IsZero() {
		query += " AND updated_at >= ?"
		args = append(args, opts.UpdatedAfter)
	}
	if !opts.UpdatedBefore.IsZero() {
		query += " AND updated_at <= ?"
		args = append(args, opts.UpdatedBefore)
	}
	if !opts.ClosedAfter.IsZero() {
		query += " AND closed_at >= ?"
		args = append(args, opts.ClosedAfter)
	}
	if !opts.ClosedBefore.IsZero() {
		query += " AND closed_at <= ?"
		args = append(args, opts.ClosedBefore)
	}

	// Temporal filters (GTD deferral)
	// NOTE: dates are stored as YYYY-MM-DD in local time, so we must use
	// date('now','localtime') to compare correctly across timezones.
	if opts.DeferredOnly {
		query += " AND defer_until IS NOT NULL AND defer_until > date('now','localtime')"
	} else if opts.OverdueOnly {
		query += " AND due_date IS NOT NULL AND due_date < date('now','localtime') AND status != 'closed'"
	} else if opts.SurfacingOnly {
		query += " AND defer_until IS NOT NULL AND defer_until <= date('now','localtime') AND defer_count > 0"
	} else if opts.DueSoonDays > 0 {
		query += fmt.Sprintf(" AND due_date IS NOT NULL AND due_date >= date('now','localtime') AND due_date <= date('now','localtime','+%d days')", opts.DueSoonDays)
	} else if opts.ExcludeDeferred {
		query += " AND (defer_until IS NULL OR defer_until <= date('now','localtime'))"
	}

	// Exclude issues with open (non-closed) dependencies
	if opts.ExcludeHasOpenDeps {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM issue_dependencies d
			JOIN issues dep ON d.depends_on_id = dep.id
			WHERE d.issue_id = issues.id
			  AND d.relation_type = 'depends_on'
			  AND dep.status != 'closed'
			  AND dep.deleted_at IS NULL
		)`
	}

	// Sorting - validate column name to prevent SQL injection
	allowedSortCols := map[string]bool{
		"id": true, "title": true, "status": true, "type": true,
		"priority": true, "points": true, "created_at": true,
		"updated_at": true, "closed_at": true, "deleted_at": true,
		"defer_until": true, "due_date": true, "defer_count": true,
	}
	sortCol := "priority"
	if opts.SortBy != "" && allowedSortCols[opts.SortBy] {
		sortCol = opts.SortBy
	}
	sortDir := "ASC"
	if opts.SortDesc {
		sortDir = "DESC"
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortCol, sortDir)

	// Limit
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []models.Issue
	for rows.Next() {
		var issue models.Issue
		// NullString for every TEXT DEFAULT '' column — see GetIssue.
		var description, labels sql.NullString
		var closedAt, deletedAt, reviewedAt sql.NullTime
		var parentID, acceptance, sprint sql.NullString
		var implSession, creatorSession, reviewerSession sql.NullString
		var reviewRequestedBy, closedBy sql.NullString
		var createdBranch sql.NullString
		var pointsNull sql.NullInt64
		var deferUntil, dueDate sql.NullString

		err := rows.Scan(
			&issue.ID, &issue.Title, &description, &issue.Status, &issue.Type, &issue.Priority,
			&pointsNull, &labels, &parentID, &acceptance, &sprint,
			&implSession, &creatorSession, &reviewerSession, &reviewRequestedBy, &closedBy,
			&issue.CreatedAt, &issue.UpdatedAt, &reviewedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
			&deferUntil, &dueDate, &issue.DeferCount,
		)
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

		issues = append(issues, issue)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}
