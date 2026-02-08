package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
)

// SnapshotQuerySource implements query.QuerySource for read-only snapshot databases.
// It runs SQL directly against a snapshot *sql.DB (same schema as the client-side DB).
type SnapshotQuerySource struct {
	db *sql.DB
}

// NewSnapshotQuerySource wraps a read-only *sql.DB as a QuerySource.
func NewSnapshotQuerySource(sqlDB *sql.DB) *SnapshotQuerySource {
	return &SnapshotQuerySource{db: sqlDB}
}

// Ensure interface compliance at compile time.
var _ query.QuerySource = (*SnapshotQuerySource)(nil)

// issueColumns is the SELECT column list matching the scan order used throughout.
const issueColumns = `id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch`

// scanIssue scans a single issue row using the standard column order.
func scanIssue(scanner interface{ Scan(dest ...any) error }) (models.Issue, error) {
	var issue models.Issue
	var labels string
	var closedAt, deletedAt sql.NullTime
	var parentID, acceptance, sprint sql.NullString
	var implSession, creatorSession, reviewerSession sql.NullString
	var createdBranch sql.NullString
	var pointsNull sql.NullInt64

	err := scanner.Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
		&pointsNull, &labels, &parentID, &acceptance, &sprint,
		&implSession, &creatorSession, &reviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &createdBranch,
	)
	if err != nil {
		return issue, err
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

	return issue, nil
}

// ListIssues queries the issues table with filters from ListIssuesOptions.
func (s *SnapshotQuerySource) ListIssues(opts db.ListIssuesOptions) ([]models.Issue, error) {
	q := `SELECT ` + issueColumns + ` FROM issues WHERE 1=1`
	var args []interface{}

	// Deleted filter
	if opts.OnlyDeleted {
		q += " AND deleted_at IS NOT NULL"
	} else if !opts.IncludeDeleted {
		q += " AND deleted_at IS NULL"
	}

	// Status filter
	if len(opts.Status) > 0 {
		placeholders := make([]string, len(opts.Status))
		for i, st := range opts.Status {
			placeholders[i] = "?"
			args = append(args, st)
		}
		q += fmt.Sprintf(" AND status IN (%s)", strings.Join(placeholders, ","))
	}

	// Type filter
	if len(opts.Type) > 0 {
		placeholders := make([]string, len(opts.Type))
		for i, t := range opts.Type {
			placeholders[i] = "?"
			args = append(args, t)
		}
		q += fmt.Sprintf(" AND type IN (%s)", strings.Join(placeholders, ","))
	}

	// ID filter
	if len(opts.IDs) > 0 {
		placeholders := make([]string, len(opts.IDs))
		for i, id := range opts.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		q += fmt.Sprintf(" AND id IN (%s)", strings.Join(placeholders, ","))
	}

	// Priority filter
	if opts.Priority != "" {
		if strings.HasPrefix(opts.Priority, "<=") {
			prio := strings.TrimPrefix(opts.Priority, "<=")
			q += " AND priority <= ?"
			args = append(args, prio)
		} else if strings.HasPrefix(opts.Priority, ">=") {
			prio := strings.TrimPrefix(opts.Priority, ">=")
			q += " AND priority >= ?"
			args = append(args, prio)
		} else {
			q += " AND priority = ?"
			args = append(args, opts.Priority)
		}
	}

	// Labels filter
	if len(opts.Labels) > 0 {
		for _, label := range opts.Labels {
			q += " AND (labels LIKE ? OR labels LIKE ? OR labels LIKE ? OR labels = ?)"
			args = append(args, label+",%", "%,"+label+",%", "%,"+label, label)
		}
	}

	// Search filter
	if opts.Search != "" {
		q += " AND (id LIKE ? OR title LIKE ? OR description LIKE ?)"
		searchPattern := "%" + opts.Search + "%"
		args = append(args, searchPattern, searchPattern, searchPattern)
	}

	// Implementer filter
	if opts.Implementer != "" {
		q += " AND implementer_session = ?"
		args = append(args, opts.Implementer)
	}

	// Reviewer filter
	if opts.Reviewer != "" {
		q += " AND reviewer_session = ?"
		args = append(args, opts.Reviewer)
	}

	// ReviewableBy filter
	if opts.ReviewableBy != "" {
		q += ` AND status = ? AND implementer_session != '' AND (
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
		q += " AND parent_id = ?"
		args = append(args, opts.ParentID)
	}

	// Epic filter (recursive descendants)
	if opts.EpicID != "" {
		descendants, err := s.getDescendants(opts.EpicID)
		if err != nil {
			return nil, fmt.Errorf("get epic descendants: %w", err)
		}
		if len(descendants) > 0 {
			placeholders := make([]string, len(descendants))
			for i, id := range descendants {
				placeholders[i] = "?"
				args = append(args, id)
			}
			q += fmt.Sprintf(" AND id IN (%s)", strings.Join(placeholders, ","))
		} else {
			q += " AND 1=0"
		}
	}

	// Points filter
	if opts.PointsMin > 0 {
		q += " AND points >= ?"
		args = append(args, opts.PointsMin)
	}
	if opts.PointsMax > 0 {
		q += " AND points <= ?"
		args = append(args, opts.PointsMax)
	}

	// Date filters
	if !opts.CreatedAfter.IsZero() {
		q += " AND created_at >= ?"
		args = append(args, opts.CreatedAfter)
	}
	if !opts.CreatedBefore.IsZero() {
		q += " AND created_at <= ?"
		args = append(args, opts.CreatedBefore)
	}
	if !opts.UpdatedAfter.IsZero() {
		q += " AND updated_at >= ?"
		args = append(args, opts.UpdatedAfter)
	}
	if !opts.UpdatedBefore.IsZero() {
		q += " AND updated_at <= ?"
		args = append(args, opts.UpdatedBefore)
	}
	if !opts.ClosedAfter.IsZero() {
		q += " AND closed_at >= ?"
		args = append(args, opts.ClosedAfter)
	}
	if !opts.ClosedBefore.IsZero() {
		q += " AND closed_at <= ?"
		args = append(args, opts.ClosedBefore)
	}

	// Sorting
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
	q += fmt.Sprintf(" ORDER BY %s %s", sortCol, sortDir)

	// Limit
	if opts.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []models.Issue
	for rows.Next() {
		issue, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// GetIssue retrieves a single issue by ID.
func (s *SnapshotQuerySource) GetIssue(id string) (*models.Issue, error) {
	id = db.NormalizeIssueID(id)

	row := s.db.QueryRow(`SELECT `+issueColumns+` FROM issues WHERE id = ?`, id)
	issue, err := scanIssue(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	return &issue, nil
}

// GetLogs retrieves logs for an issue, including work session logs.
func (s *SnapshotQuerySource) GetLogs(issueID string, limit int) ([]models.Log, error) {
	q := `SELECT CAST(l.id AS TEXT), l.issue_id, l.session_id, l.work_session_id, l.message, l.type, l.timestamp
	      FROM logs l
	      WHERE l.issue_id = ?
	      OR (l.issue_id = '' AND l.work_session_id IN (
	          SELECT work_session_id FROM work_session_issues WHERE issue_id = ?
	      ))
	      ORDER BY l.timestamp DESC`
	args := []interface{}{issueID, issueID}

	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		if err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	// Reverse to chronological order
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	return logs, nil
}

// GetComments retrieves comments for an issue.
func (s *SnapshotQuerySource) GetComments(issueID string) ([]models.Comment, error) {
	rows, err := s.db.Query(`
		SELECT CAST(id AS TEXT), issue_id, session_id, text, created_at
		FROM comments WHERE issue_id = ? ORDER BY created_at
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.SessionID, &c.Text, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// GetLatestHandoff retrieves the latest handoff for an issue.
func (s *SnapshotQuerySource) GetLatestHandoff(issueID string) (*models.Handoff, error) {
	var handoff models.Handoff
	var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string

	err := s.db.QueryRow(`
		SELECT CAST(id AS TEXT), issue_id, session_id, done, remaining, decisions, uncertain, timestamp
		FROM handoffs WHERE issue_id = ? ORDER BY timestamp DESC LIMIT 1
	`, issueID).Scan(
		&handoff.ID, &handoff.IssueID, &handoff.SessionID,
		&doneJSON, &remainingJSON, &decisionsJSON, &uncertainJSON, &handoff.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(doneJSON), &handoff.Done); err != nil {
		return nil, fmt.Errorf("unmarshal done: %w", err)
	}
	if err := json.Unmarshal([]byte(remainingJSON), &handoff.Remaining); err != nil {
		return nil, fmt.Errorf("unmarshal remaining: %w", err)
	}
	if err := json.Unmarshal([]byte(decisionsJSON), &handoff.Decisions); err != nil {
		return nil, fmt.Errorf("unmarshal decisions: %w", err)
	}
	if err := json.Unmarshal([]byte(uncertainJSON), &handoff.Uncertain); err != nil {
		return nil, fmt.Errorf("unmarshal uncertain: %w", err)
	}

	return &handoff, nil
}

// GetLinkedFiles returns files linked to an issue.
func (s *SnapshotQuerySource) GetLinkedFiles(issueID string) ([]models.IssueFile, error) {
	rows, err := s.db.Query(`
		SELECT CAST(id AS TEXT), issue_id, file_path, role, linked_sha, linked_at
		FROM issue_files WHERE issue_id = ? ORDER BY role, file_path
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []models.IssueFile
	for rows.Next() {
		var f models.IssueFile
		if err := rows.Scan(&f.ID, &f.IssueID, &f.FilePath, &f.Role, &f.LinkedSHA, &f.LinkedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// GetDependencies returns IDs of issues that issueID depends on.
func (s *SnapshotQuerySource) GetDependencies(issueID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT depends_on_id FROM issue_dependencies WHERE issue_id = ? AND relation_type = 'depends_on'
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// GetRejectedInProgressIssueIDs returns IDs of in_progress issues that have a
// recent reject action without a subsequent review action (needs rework).
func (s *SnapshotQuerySource) GetRejectedInProgressIssueIDs() (map[string]bool, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT i.id FROM issues i
		WHERE i.status = 'in_progress' AND i.deleted_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM action_log al
			WHERE al.entity_id = i.id AND al.action_type = 'reject' AND al.undone = 0
			  AND NOT EXISTS (
				SELECT 1 FROM action_log al2
				WHERE al2.entity_id = i.id AND al2.action_type = 'review'
				  AND al2.timestamp > al.timestamp
			  )
		  )
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, nil
}

// GetIssuesWithOpenDeps returns issue IDs that have at least one non-closed dependency.
func (s *SnapshotQuerySource) GetIssuesWithOpenDeps() (map[string]bool, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT d.issue_id
		FROM issue_dependencies d
		JOIN issues i ON d.depends_on_id = i.id
		WHERE d.relation_type = 'depends_on'
		  AND i.status != 'closed'
		  AND i.deleted_at IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			return nil, err
		}
		result[issueID] = true
	}
	return result, nil
}

// getDescendants returns all descendant issue IDs of a parent (BFS).
func (s *SnapshotQuerySource) getDescendants(parentID string) ([]string, error) {
	var descendants []string
	visited := make(map[string]bool)
	queue := []string{parentID}

	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]

		if visited[currentID] {
			continue
		}
		visited[currentID] = true

		rows, err := s.db.Query(`SELECT id FROM issues WHERE parent_id = ? AND deleted_at IS NULL`, currentID)
		if err != nil {
			return nil, err
		}

		var children []string
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return nil, err
			}
			children = append(children, childID)
			descendants = append(descendants, childID)
		}
		rows.Close()

		queue = append(queue, children...)
	}

	return descendants, nil
}
