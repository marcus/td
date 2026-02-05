package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// Issue Tree Functions
// ============================================================================

// getDescendants returns all descendant IDs of a given parent ID (recursively)
func (db *DB) getDescendants(parentID string) ([]string, error) {
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

		// Get direct children of current ID
		rows, err := db.conn.Query(`SELECT id FROM issues WHERE parent_id = ? AND deleted_at IS NULL`, currentID)
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

		// Add children to queue for recursive processing
		queue = append(queue, children...)
	}

	return descendants, nil
}

// HasChildren returns true if the issue has any child issues
func (db *DB) HasChildren(issueID string) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM issues WHERE parent_id = ? AND deleted_at IS NULL`,
		issueID,
	).Scan(&count)
	return count > 0, err
}

// GetDirectChildren returns the direct children of an issue (not recursive)
func (db *DB) GetDirectChildren(issueID string) ([]*models.Issue, error) {
	rows, err := db.conn.Query(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE parent_id = ? AND deleted_at IS NULL
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var children []*models.Issue
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

		children = append(children, &issue)
	}

	return children, nil
}

// GetDescendantIssues returns all descendant issues (children, grandchildren, etc.)
// filtered by the given statuses (empty = all statuses)
func (db *DB) GetDescendantIssues(issueID string, statuses []models.Status) ([]*models.Issue, error) {
	ids, err := db.getDescendants(issueID)
	if err != nil {
		return nil, err
	}

	var issues []*models.Issue
	for _, id := range ids {
		issue, err := db.GetIssue(id)
		if err != nil {
			continue // skip missing issues
		}
		if len(statuses) == 0 {
			issues = append(issues, issue)
		} else {
			for _, s := range statuses {
				if issue.Status == s {
					issues = append(issues, issue)
					break
				}
			}
		}
	}
	return issues, nil
}

// CascadeUpParentStatus checks if all children of a parent epic have reached the target status,
// and if so, updates the parent to that status. Works recursively up the parent chain.
// Returns the number of parents that were cascaded and the list of cascaded parent IDs.
func (db *DB) CascadeUpParentStatus(issueID string, targetStatus models.Status, sessionID string) (int, []string) {
	var cascadedCount int
	var cascadedIDs []string

	_ = db.withWriteLock(func() error {
		cascadedCount, cascadedIDs = db.cascadeUpParentStatusLocked(issueID, targetStatus, sessionID)
		return nil
	})

	return cascadedCount, cascadedIDs
}

// cascadeUpParentStatusLocked is the inner implementation that assumes the write lock is held.
func (db *DB) cascadeUpParentStatusLocked(issueID string, targetStatus models.Status, sessionID string) (int, []string) {
	cascadedCount := 0
	var cascadedIDs []string

	// Get the issue to find its parent
	issue, err := db.GetIssue(issueID)
	if err != nil || issue.ParentID == "" {
		return cascadedCount, cascadedIDs
	}

	// Get the parent issue
	parent, err := db.GetIssue(issue.ParentID)
	if err != nil {
		return cascadedCount, cascadedIDs
	}

	// Only cascade to epic parents
	if parent.Type != models.TypeEpic {
		return cascadedCount, cascadedIDs
	}

	// Parent already at or beyond target status - nothing to do
	if parent.Status == targetStatus || parent.Status == models.StatusClosed {
		return cascadedCount, cascadedIDs
	}

	// Get all direct children of the parent
	children, err := db.GetDirectChildren(parent.ID)
	if err != nil || len(children) == 0 {
		return cascadedCount, cascadedIDs
	}

	// Check if all children have reached the target status (or beyond)
	allAtTarget := true
	for _, child := range children {
		if targetStatus == models.StatusInReview {
			// For in_review, check if child is in_review or closed
			if child.Status != models.StatusInReview && child.Status != models.StatusClosed {
				allAtTarget = false
				break
			}
		} else if targetStatus == models.StatusClosed {
			// For closed, child must be closed
			if child.Status != models.StatusClosed {
				allAtTarget = false
				break
			}
		}
	}

	if !allAtTarget {
		return cascadedCount, cascadedIDs
	}

	// All children at target - update parent
	parent.Status = targetStatus
	if targetStatus == models.StatusClosed {
		now := time.Now()
		parent.ClosedAt = &now
	}

	actionType := models.ActionReview
	if targetStatus == models.StatusClosed {
		actionType = models.ActionClose
	}

	if err := db.updateIssueAndLog(parent, sessionID, actionType); err != nil {
		return cascadedCount, cascadedIDs
	}

	// Add log entry
	logMsg := fmt.Sprintf("Auto-cascaded to %s (all children complete)", targetStatus)
	db.addLogEntry(parent.ID, sessionID, logMsg, models.LogTypeProgress)

	cascadedIDs = append(cascadedIDs, parent.ID)
	cascadedCount++

	// Auto-unblock issues that depend on this newly-closed parent
	if targetStatus == models.StatusClosed {
		uCount, uIDs := db.cascadeUnblockDependentsLocked(parent.ID, sessionID)
		_ = uCount
		_ = uIDs
	}

	// Recursively check parent's parent
	moreCount, moreIDs := db.cascadeUpParentStatusLocked(parent.ID, targetStatus, sessionID)
	cascadedCount += moreCount
	cascadedIDs = append(cascadedIDs, moreIDs...)

	return cascadedCount, cascadedIDs
}

// CascadeUnblockDependents checks issues that depend on closedIssueID.
// For each dependent in "blocked" status, if ALL its dependencies are now closed,
// it transitions the dependent from blocked → open.
// Returns the count and IDs of unblocked issues.
func (db *DB) CascadeUnblockDependents(closedIssueID, sessionID string) (int, []string) {
	var count int
	var ids []string

	_ = db.withWriteLock(func() error {
		count, ids = db.cascadeUnblockDependentsLocked(closedIssueID, sessionID)
		return nil
	})

	return count, ids
}

// cascadeUnblockDependentsLocked is the inner implementation that assumes the write lock is held.
func (db *DB) cascadeUnblockDependentsLocked(closedIssueID, sessionID string) (int, []string) {
	dependents, err := db.GetBlockedBy(closedIssueID)
	if err != nil || len(dependents) == 0 {
		return 0, nil
	}

	var unblockedIDs []string

	for _, depID := range dependents {
		issue, err := db.GetIssue(depID)
		if err != nil || issue == nil {
			continue
		}

		if issue.Status != models.StatusBlocked {
			continue
		}

		// Check if ALL dependencies of this issue are now closed
		deps, err := db.GetDependencies(depID)
		if err != nil {
			continue
		}

		allClosed := true
		for _, d := range deps {
			depIssue, err := db.GetIssue(d)
			if err != nil || depIssue == nil {
				allClosed = false
				break
			}
			if depIssue.Status != models.StatusClosed {
				allClosed = false
				break
			}
		}

		if !allClosed {
			continue
		}

		issue.Status = models.StatusOpen
		if err := db.updateIssueAndLog(issue, sessionID, models.ActionUnblock); err != nil {
			continue
		}

		db.addLogEntry(depID, sessionID, fmt.Sprintf("Auto-unblocked (dependency %s closed)", closedIssueID), models.LogTypeProgress)

		unblockedIDs = append(unblockedIDs, depID)
	}

	return len(unblockedIDs), unblockedIDs
}

// ============================================================================
// Dependency Functions
// ============================================================================

// AddDependency adds a dependency between issues WITHOUT logging to action_log.
// For local mutations, use AddDependencyLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) AddDependency(issueID, dependsOnID, relationType string) error {
	return db.withWriteLock(func() error {
		depID := DependencyID(issueID, dependsOnID, relationType)
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO issue_dependencies (id, issue_id, depends_on_id, relation_type)
			VALUES (?, ?, ?, ?)
		`, depID, issueID, dependsOnID, relationType)
		return err
	})
}

// RemoveDependency removes a dependency WITHOUT logging to action_log.
// For local mutations, use RemoveDependencyLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) RemoveDependency(issueID, dependsOnID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM issue_dependencies WHERE issue_id = ? AND depends_on_id = ?`, issueID, dependsOnID)
		return err
	})
}

// GetDependencies returns what an issue depends on
func (db *DB) GetDependencies(issueID string) ([]string, error) {
	rows, err := db.conn.Query(`
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

// GetBlockedBy returns what issues are blocked by this issue
func (db *DB) GetBlockedBy(issueID string) ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT issue_id FROM issue_dependencies WHERE depends_on_id = ? AND relation_type = 'depends_on'
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocked []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		blocked = append(blocked, id)
	}
	return blocked, nil
}

// GetAllDependencies returns all dependency relationships as a map
func (db *DB) GetAllDependencies() (map[string][]string, error) {
	rows, err := db.conn.Query(`
		SELECT issue_id, depends_on_id FROM issue_dependencies WHERE relation_type = 'depends_on'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deps := make(map[string][]string)
	for rows.Next() {
		var issueID, depID string
		if err := rows.Scan(&issueID, &depID); err != nil {
			return nil, err
		}
		deps[issueID] = append(deps[issueID], depID)
	}
	return deps, nil
}

// GetIssuesWithOpenDeps returns a set of issue IDs that have at least one open (non-closed) dependency.
// This is used by the is_ready() and has_open_deps() query functions.
func (db *DB) GetIssuesWithOpenDeps() (map[string]bool, error) {
	rows, err := db.conn.Query(`
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

// GetIssueStatuses fetches statuses for multiple issues in a single query
func (db *DB) GetIssueStatuses(ids []string) (map[string]models.Status, error) {
	if len(ids) == 0 {
		return make(map[string]models.Status), nil
	}

	// Dedupe IDs
	seen := make(map[string]bool)
	uniqueIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		nid := NormalizeIssueID(id)
		if !seen[nid] {
			seen[nid] = true
			uniqueIDs = append(uniqueIDs, nid)
		}
	}

	placeholders := make([]string, len(uniqueIDs))
	args := make([]interface{}, len(uniqueIDs))
	for i, id := range uniqueIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, status FROM issues WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := make(map[string]models.Status)
	for rows.Next() {
		var id string
		var status models.Status
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		statuses[id] = status
	}
	return statuses, nil
}

// ============================================================================
// Issue File Functions
// ============================================================================

// LinkFile links a file to an issue WITHOUT logging to action_log.
// For local mutations, use LinkFileLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) LinkFile(issueID, filePath string, role models.FileRole, sha string) error {
	return db.withWriteLock(func() error {
		id := IssueFileID(issueID, filePath)
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO issue_files (id, issue_id, file_path, role, linked_sha, linked_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, issueID, filePath, role, sha, time.Now())
		return err
	})
}

// UnlinkFile removes a file link WITHOUT logging to action_log.
// For local mutations, use UnlinkFileLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) UnlinkFile(issueID, filePath string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM issue_files WHERE issue_id = ? AND file_path = ?`, issueID, filePath)
		return err
	})
}

// GetLinkedFiles returns files linked to an issue
func (db *DB) GetLinkedFiles(issueID string) ([]models.IssueFile, error) {
	rows, err := db.conn.Query(`
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

// ============================================================================
// Issue Session Functions
// ============================================================================

// RecordSessionAction logs a session's interaction with an issue
func (db *DB) RecordSessionAction(issueID, sessionID string, action models.IssueSessionAction) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		id, err := generateID()
		if err != nil {
			return err
		}

		_, err = db.conn.Exec(`
			INSERT INTO issue_session_history (id, issue_id, session_id, action, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, id, issueID, sessionID, action, time.Now())
		return err
	})
}

// WasSessionInvolved checks if a session ever interacted with an issue
func (db *DB) WasSessionInvolved(issueID, sessionID string) (bool, error) {
	issueID = NormalizeIssueID(issueID)
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM issue_session_history
		WHERE issue_id = ? AND session_id = ?
	`, issueID, sessionID).Scan(&count)
	return count > 0, err
}

// GetSessionHistory returns all session interactions for an issue
func (db *DB) GetSessionHistory(issueID string) ([]models.IssueSessionHistory, error) {
	issueID = NormalizeIssueID(issueID)
	rows, err := db.conn.Query(`
		SELECT id, issue_id, session_id, action, created_at
		FROM issue_session_history
		WHERE issue_id = ?
		ORDER BY created_at ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []models.IssueSessionHistory
	for rows.Next() {
		var h models.IssueSessionHistory
		if err := rows.Scan(&h.ID, &h.IssueID, &h.SessionID, &h.Action, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}

	return history, nil
}

// GetIssueSessionLog returns issues touched by a session
func (db *DB) GetIssueSessionLog(sessionID string) ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT DISTINCT issue_id FROM logs WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
