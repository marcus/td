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
	Status         []models.Status
	Type           []models.Type
	Priority       string
	Labels         []string
	IncludeDeleted bool
	OnlyDeleted    bool
	Search         string
	Implementer    string
	Reviewer       string
	ReviewableBy   string // Issues that this session can review
	ParentID       string
	EpicID         string // Filter by epic (parent_id matches epic, recursively)
	PointsMin      int
	PointsMax      int
	CreatedAfter   time.Time
	CreatedBefore  time.Time
	UpdatedAfter   time.Time
	UpdatedBefore  time.Time
	ClosedAfter    time.Time
	ClosedBefore   time.Time
	SortBy         string
	SortDesc       bool
	Limit          int
	IDs            []string
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

		// Retry loop for rare ID collisions (6 hex chars = 16.7M keyspace)
		const maxRetries = 3
		for attempt := 0; attempt < maxRetries; attempt++ {
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
	issue.Points = int(pointsNull.Int64)

	if labels != "" {
		issue.Labels = strings.Split(labels, ",")
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
	issue.CreatedBranch = createdBranch.String

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
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
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
		var labels string
		var closedAt, deletedAt sql.NullTime
		var parentID, acceptance, sprint sql.NullString
		var implSession, creatorSession, reviewerSession sql.NullString
		var createdBranch sql.NullString
		var pointsNull sql.NullInt64
		if err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&pointsNull, &labels, &parentID, &acceptance, &sprint,
			&implSession, &creatorSession, &reviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
		); err != nil {
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
		issues = append(issues, issue)
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

	return titles, nil
}

// UpdateIssue updates an issue WITHOUT logging to action_log.
// For local mutations, use UpdateIssueLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) UpdateIssue(issue *models.Issue) error {
	return db.withWriteLock(func() error {
		issue.UpdatedAt = time.Now()
		labels := strings.Join(issue.Labels, ",")

		_, err := db.conn.Exec(`
			UPDATE issues SET title = ?, description = ?, status = ?, type = ?, priority = ?,
			                  points = ?, labels = ?, parent_id = ?, acceptance = ?, sprint = ?,
			                  implementer_session = ?, reviewer_session = ?, updated_at = ?,
			                  closed_at = ?, deleted_at = ?
			WHERE id = ?
		`, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority,
			issue.Points, labels, issue.ParentID, issue.Acceptance, issue.Sprint,
			issue.ImplementerSession, issue.ReviewerSession, issue.UpdatedAt,
			issue.ClosedAt, issue.DeletedAt, issue.ID)

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

// ListIssues returns issues matching the filter
func (db *DB) ListIssues(opts ListIssuesOptions) ([]models.Issue, error) {
	query := `SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
                 implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
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
	// - Session is not implementer, not creator, and not in session history
	if opts.ReviewableBy != "" {
		query += ` AND status = ? AND implementer_session != '' AND (
			minor = 1 OR (
				implementer_session != ?
				AND (creator_session = '' OR creator_session != ?)
				AND NOT EXISTS (
					SELECT 1 FROM issue_session_history
					WHERE issue_id = issues.id AND session_id = ?
				)
			)
		)`
		args = append(args, models.StatusInReview, opts.ReviewableBy, opts.ReviewableBy, opts.ReviewableBy)
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

	// Sorting - validate column name to prevent SQL injection
	allowedSortCols := map[string]bool{
		"id": true, "title": true, "status": true, "type": true,
		"priority": true, "points": true, "created_at": true,
		"updated_at": true, "closed_at": true, "deleted_at": true,
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
		var labels string
		var closedAt, deletedAt sql.NullTime
		var parentID, acceptance, sprint sql.NullString
		var implSession, creatorSession, reviewerSession sql.NullString
		var createdBranch sql.NullString
		var pointsNull sql.NullInt64

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&pointsNull, &labels, &parentID, &acceptance, &sprint,
			&implSession, &creatorSession, &reviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
		)
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

		issues = append(issues, issue)
	}

	return issues, nil
}
