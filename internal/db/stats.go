package db

import (
	"database/sql"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// GetStats returns database statistics
func (db *DB) GetStats() (map[string]int, error) {
	stats := make(map[string]int)

	// Total issues
	var total int
	db.conn.QueryRow(`SELECT COUNT(*) FROM issues WHERE deleted_at IS NULL`).Scan(&total)
	stats["total"] = total

	// By status
	rows, err := db.conn.Query(`SELECT status, COUNT(*) FROM issues WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		stats[status] = count
	}

	// By type
	rows, err = db.conn.Query(`SELECT type, COUNT(*) FROM issues WHERE deleted_at IS NULL GROUP BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, err
		}
		stats["type_"+typ] = count
	}

	return stats, nil
}

// GetExtendedStats returns detailed statistics for dashboard/stats displays
func (db *DB) GetExtendedStats() (*models.ExtendedStats, error) {
	stats := &models.ExtendedStats{
		ByStatus:   make(map[models.Status]int),
		ByType:     make(map[models.Type]int),
		ByPriority: make(map[models.Priority]int),
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := today.AddDate(0, 0, 1)
	weekAgo := now.AddDate(0, 0, -7)

	// Consolidate scalar counts into single query
	err := db.conn.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(points), 0),
			SUM(CASE WHEN created_at >= ? AND created_at < ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END),
			(SELECT COUNT(*) FROM logs),
			(SELECT COUNT(*) FROM handoffs)
		FROM issues WHERE deleted_at IS NULL
	`, today, tomorrow, weekAgo).Scan(
		&stats.Total, &stats.TotalPoints, &stats.CreatedToday, &stats.CreatedThisWeek,
		&stats.TotalLogs, &stats.TotalHandoffs,
	)
	if err != nil {
		return nil, err
	}

	// Consolidate GROUP BY queries using UNION ALL
	rows, err := db.conn.Query(`
		SELECT 'status' as category, status as value, COUNT(*) as cnt FROM issues WHERE deleted_at IS NULL GROUP BY status
		UNION ALL
		SELECT 'type' as category, type as value, COUNT(*) as cnt FROM issues WHERE deleted_at IS NULL GROUP BY type
		UNION ALL
		SELECT 'priority' as category, priority as value, COUNT(*) as cnt FROM issues WHERE deleted_at IS NULL GROUP BY priority
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var category, value string
		var count int
		if err := rows.Scan(&category, &value, &count); err != nil {
			return nil, err
		}
		switch category {
		case "status":
			stats.ByStatus[models.Status(value)] = count
		case "type":
			stats.ByType[models.Type(value)] = count
		case "priority":
			stats.ByPriority[models.Priority(value)] = count
		}
	}

	// Oldest open issue
	var oldestIssue models.Issue
	var labels string
	var closedAt, deletedAt sql.NullTime
	err = db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE status = ? AND deleted_at IS NULL ORDER BY created_at ASC LIMIT 1
	`, models.StatusOpen).Scan(
		&oldestIssue.ID, &oldestIssue.Title, &oldestIssue.Description, &oldestIssue.Status, &oldestIssue.Type,
		&oldestIssue.Priority, &oldestIssue.Points, &labels, &oldestIssue.ParentID, &oldestIssue.Acceptance, &oldestIssue.Sprint,
		&oldestIssue.ImplementerSession, &oldestIssue.CreatorSession, &oldestIssue.ReviewerSession, &oldestIssue.CreatedAt, &oldestIssue.UpdatedAt,
		&closedAt, &deletedAt, &oldestIssue.Minor, &oldestIssue.CreatedBranch,
	)
	if err == nil {
		if labels != "" {
			oldestIssue.Labels = strings.Split(labels, ",")
		}
		if closedAt.Valid {
			oldestIssue.ClosedAt = &closedAt.Time
		}
		if deletedAt.Valid {
			oldestIssue.DeletedAt = &deletedAt.Time
		}
		stats.OldestOpen = &oldestIssue
	}

	// Newest task (created most recently)
	var newestIssue models.Issue
	labels = ""
	closedAt = sql.NullTime{}
	deletedAt = sql.NullTime{}
	err = db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT 1
	`).Scan(
		&newestIssue.ID, &newestIssue.Title, &newestIssue.Description, &newestIssue.Status, &newestIssue.Type,
		&newestIssue.Priority, &newestIssue.Points, &labels, &newestIssue.ParentID, &newestIssue.Acceptance, &newestIssue.Sprint,
		&newestIssue.ImplementerSession, &newestIssue.CreatorSession, &newestIssue.ReviewerSession, &newestIssue.CreatedAt, &newestIssue.UpdatedAt,
		&closedAt, &deletedAt, &newestIssue.Minor, &newestIssue.CreatedBranch,
	)
	if err == nil {
		if labels != "" {
			newestIssue.Labels = strings.Split(labels, ",")
		}
		if closedAt.Valid {
			newestIssue.ClosedAt = &closedAt.Time
		}
		if deletedAt.Valid {
			newestIssue.DeletedAt = &deletedAt.Time
		}
		stats.NewestTask = &newestIssue
	}

	// Last closed issue
	var closedIssue models.Issue
	labels = ""
	closedAt = sql.NullTime{}
	deletedAt = sql.NullTime{}
	err = db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE status = ? AND closed_at IS NOT NULL AND deleted_at IS NULL
		ORDER BY closed_at DESC LIMIT 1
	`, models.StatusClosed).Scan(
		&closedIssue.ID, &closedIssue.Title, &closedIssue.Description, &closedIssue.Status, &closedIssue.Type,
		&closedIssue.Priority, &closedIssue.Points, &labels, &closedIssue.ParentID, &closedIssue.Acceptance, &closedIssue.Sprint,
		&closedIssue.ImplementerSession, &closedIssue.CreatorSession, &closedIssue.ReviewerSession, &closedIssue.CreatedAt, &closedIssue.UpdatedAt,
		&closedAt, &deletedAt, &closedIssue.Minor, &closedIssue.CreatedBranch,
	)
	if err == nil {
		if labels != "" {
			closedIssue.Labels = strings.Split(labels, ",")
		}
		if closedAt.Valid {
			closedIssue.ClosedAt = &closedAt.Time
		}
		if deletedAt.Valid {
			closedIssue.DeletedAt = &deletedAt.Time
		}
		stats.LastClosed = &closedIssue
	}

	// Derived stats
	if stats.Total > 0 {
		stats.AvgPointsPerTask = float64(stats.TotalPoints) / float64(stats.Total)
		closedCount := stats.ByStatus[models.StatusClosed]
		stats.CompletionRate = float64(closedCount) / float64(stats.Total)
	}

	// Most active session (by log count)
	var mostActiveSession string
	err = db.conn.QueryRow(`
		SELECT session_id FROM logs WHERE session_id != ''
		GROUP BY session_id ORDER BY COUNT(*) DESC LIMIT 1
	`).Scan(&mostActiveSession)
	if err == nil {
		stats.MostActiveSession = mostActiveSession
	}

	return stats, nil
}
