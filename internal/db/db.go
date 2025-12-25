package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
	_ "modernc.org/sqlite"
)

const (
	dbFile     = ".todos/issues.db"
	idPrefix   = "td-"
	wsIDPrefix = "ws-"
)

// DB wraps the database connection
type DB struct {
	conn    *sql.DB
	baseDir string
}

// Open opens the database and runs any pending migrations
func Open(baseDir string) (*DB, error) {
	dbPath := filepath.Join(baseDir, dbFile)

	// Check if db exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database not found: run 'td init' first")
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for concurrent reads while writes are serialized
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Set busy timeout as fallback protection (500ms, matches lock timeout)
	if _, err := conn.Exec("PRAGMA busy_timeout=500"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// Slightly faster writes, still safe with WAL
	conn.Exec("PRAGMA synchronous=NORMAL")

	db := &DB{conn: conn, baseDir: baseDir}

	// Run any pending migrations
	if _, err := db.RunMigrations(); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Initialize creates the database and runs migrations
func Initialize(baseDir string) (*DB, error) {
	dbPath := filepath.Join(baseDir, dbFile)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for concurrent reads while writes are serialized
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Set busy timeout as fallback protection (500ms, matches lock timeout)
	if _, err := conn.Exec("PRAGMA busy_timeout=500"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// Slightly faster writes, still safe with WAL
	conn.Exec("PRAGMA synchronous=NORMAL")

	// Run schema
	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	db := &DB{conn: conn, baseDir: baseDir}

	// Run migrations
	if _, err := db.RunMigrations(); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database
func (db *DB) Close() error {
	return db.conn.Close()
}

// BaseDir returns the base directory for the database
func (db *DB) BaseDir() string {
	return db.baseDir
}

// withWriteLock executes fn while holding an exclusive write lock.
// This prevents concurrent writes from multiple processes.
func (db *DB) withWriteLock(fn func() error) error {
	locker := newWriteLocker(db.baseDir)
	if err := locker.acquire(defaultTimeout); err != nil {
		return err
	}
	defer locker.release()
	return fn()
}

// columnExists checks whether a column exists on a table
func (db *DB) columnExists(table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s);", table)
	rows, err := db.conn.Query(query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

// GetSchemaVersion returns the current schema version from the database
func (db *DB) GetSchemaVersion() (int, error) {
	var version string
	err := db.conn.QueryRow("SELECT value FROM schema_info WHERE key = 'version'").Scan(&version)
	if err == sql.ErrNoRows {
		// No version set, assume version 0 (pre-migration)
		return 0, nil
	}
	if err != nil {
		// Table might not exist yet
		return 0, nil
	}
	var v int
	fmt.Sscanf(version, "%d", &v)
	return v, nil
}

// SetSchemaVersion sets the schema version in the database
func (db *DB) SetSchemaVersion(version int) error {
	return db.withWriteLock(func() error {
		return db.setSchemaVersionInternal(version)
	})
}

// setSchemaVersionInternal sets schema version without acquiring lock (for use during init)
func (db *DB) setSchemaVersionInternal(version int) error {
	_, err := db.conn.Exec(`INSERT OR REPLACE INTO schema_info (key, value) VALUES ('version', ?)`,
		fmt.Sprintf("%d", version))
	return err
}

// RunMigrations runs any pending database migrations
func (db *DB) RunMigrations() (int, error) {
	// Quick check without lock - if already at current version, skip
	currentVersion, _ := db.GetSchemaVersion()
	if currentVersion >= SchemaVersion {
		return 0, nil
	}

	// Need to run migrations - acquire lock
	var migrationsRun int
	err := db.withWriteLock(func() error {
		var err error
		migrationsRun, err = db.runMigrationsInternal()
		return err
	})
	return migrationsRun, err
}

// runMigrationsInternal runs migrations without acquiring lock (for use during init)
func (db *DB) runMigrationsInternal() (int, error) {
	// Ensure schema_info table exists
	_, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_info (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		return 0, fmt.Errorf("create schema_info: %w", err)
	}

	currentVersion, err := db.GetSchemaVersion()
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}

	migrationsRun := 0
	for _, migration := range Migrations {
		if migration.Version > currentVersion {
			if migration.Version == 4 {
				exists, err := db.columnExists("issues", "minor")
				if err != nil {
					return migrationsRun, fmt.Errorf("check column minor: %w", err)
				}
				if exists {
					if err := db.setSchemaVersionInternal(migration.Version); err != nil {
						return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
					}
					migrationsRun++
					continue
				}
			}
			if migration.Version == 5 {
				exists, err := db.columnExists("issues", "created_branch")
				if err != nil {
					return migrationsRun, fmt.Errorf("check column created_branch: %w", err)
				}
				if exists {
					if err := db.setSchemaVersionInternal(migration.Version); err != nil {
						return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
					}
					migrationsRun++
					continue
				}
			}
			if _, err := db.conn.Exec(migration.SQL); err != nil {
				return migrationsRun, fmt.Errorf("migration %d (%s): %w", migration.Version, migration.Description, err)
			}
			if err := db.setSchemaVersionInternal(migration.Version); err != nil {
				return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
			}
			migrationsRun++
		}
	}

	// If no migrations and version is 0, set to current schema version
	if currentVersion == 0 {
		if err := db.setSchemaVersionInternal(SchemaVersion); err != nil {
			return migrationsRun, err
		}
	}

	return migrationsRun, nil
}

// generateID generates a unique issue ID
func generateID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters - larger space to reduce collision risk
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(bytes), nil
}

// generateWSID generates a unique work session ID
func generateWSID() (string, error) {
	bytes := make([]byte, 2) // 4 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return wsIDPrefix + hex.EncodeToString(bytes), nil
}

// CreateIssue creates a new issue
func (db *DB) CreateIssue(issue *models.Issue) error {
	return db.withWriteLock(func() error {
		id, err := generateID()
		if err != nil {
			return err
		}
		issue.ID = id

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

		_, err = db.conn.Exec(`
			INSERT INTO issues (id, title, description, status, type, priority, points, labels, parent_id, acceptance, created_at, updated_at, minor, created_branch)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, issue.ID, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority, issue.Points, labels, issue.ParentID, issue.Acceptance, issue.CreatedAt, issue.UpdatedAt, issue.Minor, issue.CreatedBranch)

		return err
	})
}

// GetIssue retrieves an issue by ID
func (db *DB) GetIssue(id string) (*models.Issue, error) {
	var issue models.Issue
	var labels string
	var closedAt, deletedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance,
		       implementer_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
	FROM issues WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
		&issue.Points, &labels, &issue.ParentID, &issue.Acceptance,
		&issue.ImplementerSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue not found: %s", id)
	}
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

	return &issue, nil
}

// UpdateIssue updates an issue
func (db *DB) UpdateIssue(issue *models.Issue) error {
	return db.withWriteLock(func() error {
		issue.UpdatedAt = time.Now()
		labels := strings.Join(issue.Labels, ",")

		_, err := db.conn.Exec(`
			UPDATE issues SET title = ?, description = ?, status = ?, type = ?, priority = ?,
			                  points = ?, labels = ?, parent_id = ?, acceptance = ?,
			                  implementer_session = ?, reviewer_session = ?, updated_at = ?,
			                  closed_at = ?, deleted_at = ?
			WHERE id = ?
		`, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority,
			issue.Points, labels, issue.ParentID, issue.Acceptance,
			issue.ImplementerSession, issue.ReviewerSession, issue.UpdatedAt,
			issue.ClosedAt, issue.DeletedAt, issue.ID)

		return err
	})
}

// DeleteIssue soft-deletes an issue
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

// ListIssues returns issues matching the filter
func (db *DB) ListIssues(opts ListIssuesOptions) ([]models.Issue, error) {
	query := `SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance,
                 implementer_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
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
		query += " AND (title LIKE ? OR description LIKE ?)"
		searchPattern := "%" + opts.Search + "%"
		args = append(args, searchPattern, searchPattern)
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

	// Reviewable by (issues that can be reviewed by this session - different implementer OR minor task)
	if opts.ReviewableBy != "" {
		query += " AND status = ? AND implementer_session != '' AND (implementer_session != ? OR minor = 1)"
		args = append(args, models.StatusInReview, opts.ReviewableBy)
	}

	// Parent filter
	if opts.ParentID != "" {
		query += " AND parent_id = ?"
		args = append(args, opts.ParentID)
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
		"updated_at": true, "closed_at": true,
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

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&issue.Points, &labels, &issue.ParentID, &issue.Acceptance,
			&issue.ImplementerSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
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

		issues = append(issues, issue)
	}

	return issues, nil
}

// AddLog adds a log entry to an issue
func (db *DB) AddLog(log *models.Log) error {
	return db.withWriteLock(func() error {
		log.Timestamp = time.Now()

		result, err := db.conn.Exec(`
			INSERT INTO logs (issue_id, session_id, work_session_id, message, type, timestamp)
			VALUES (?, ?, ?, ?, ?, ?)
		`, log.IssueID, log.SessionID, log.WorkSessionID, log.Message, log.Type, log.Timestamp)

		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		log.ID = id

		return nil
	})
}

// GetLogs retrieves logs for an issue, including work session logs
func (db *DB) GetLogs(issueID string, limit int) ([]models.Log, error) {
	// Get logs that are either:
	// 1. Directly assigned to this issue (issue_id = ?)
	// 2. Work session logs (issue_id = '') from sessions where this issue is tagged
	query := `SELECT l.id, l.issue_id, l.session_id, l.work_session_id, l.message, l.type, l.timestamp
	          FROM logs l
	          WHERE l.issue_id = ?
	          OR (l.issue_id = '' AND l.work_session_id IN (
	              SELECT work_session_id FROM work_session_issues WHERE issue_id = ?
	          ))
	          ORDER BY l.timestamp DESC`
	args := []interface{}{issueID, issueID}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	// Reverse to get chronological order
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	return logs, nil
}

// GetLogsByWorkSession retrieves logs for a specific work session
func (db *DB) GetLogsByWorkSession(wsID string) ([]models.Log, error) {
	query := `SELECT id, issue_id, session_id, work_session_id, message, type, timestamp
	          FROM logs WHERE work_session_id = ? ORDER BY timestamp`

	rows, err := db.conn.Query(query, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// AddHandoff adds a handoff entry
func (db *DB) AddHandoff(handoff *models.Handoff) error {
	return db.withWriteLock(func() error {
		handoff.Timestamp = time.Now()

		doneJSON, _ := json.Marshal(handoff.Done)
		remainingJSON, _ := json.Marshal(handoff.Remaining)
		decisionsJSON, _ := json.Marshal(handoff.Decisions)
		uncertainJSON, _ := json.Marshal(handoff.Uncertain)

		result, err := db.conn.Exec(`
			INSERT INTO handoffs (issue_id, session_id, done, remaining, decisions, uncertain, timestamp)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, handoff.IssueID, handoff.SessionID, doneJSON, remainingJSON, decisionsJSON, uncertainJSON, handoff.Timestamp)

		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		handoff.ID = id

		return nil
	})
}

// GetLatestHandoff retrieves the latest handoff for an issue
func (db *DB) GetLatestHandoff(issueID string) (*models.Handoff, error) {
	var handoff models.Handoff
	var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string

	err := db.conn.QueryRow(`
		SELECT id, issue_id, session_id, done, remaining, decisions, uncertain, timestamp
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
		return nil, fmt.Errorf("failed to unmarshal done: %w", err)
	}
	if err := json.Unmarshal([]byte(remainingJSON), &handoff.Remaining); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remaining: %w", err)
	}
	if err := json.Unmarshal([]byte(decisionsJSON), &handoff.Decisions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decisions: %w", err)
	}
	if err := json.Unmarshal([]byte(uncertainJSON), &handoff.Uncertain); err != nil {
		return nil, fmt.Errorf("failed to unmarshal uncertain: %w", err)
	}

	return &handoff, nil
}

// GetRecentHandoffs retrieves recent handoffs across all issues
func (db *DB) GetRecentHandoffs(limit int, since time.Time) ([]models.Handoff, error) {
	var handoffs []models.Handoff

	rows, err := db.conn.Query(`
		SELECT id, issue_id, session_id, done, remaining, decisions, uncertain, timestamp
		FROM handoffs WHERE timestamp > ? ORDER BY timestamp DESC LIMIT ?
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var h models.Handoff
		var doneJSON, remainingJSON, decisionsJSON, uncertainJSON string
		err := rows.Scan(&h.ID, &h.IssueID, &h.SessionID,
			&doneJSON, &remainingJSON, &decisionsJSON, &uncertainJSON, &h.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan handoff row: %w", err)
		}
		if err := json.Unmarshal([]byte(doneJSON), &h.Done); err != nil {
			return nil, fmt.Errorf("failed to unmarshal done: %w", err)
		}
		if err := json.Unmarshal([]byte(remainingJSON), &h.Remaining); err != nil {
			return nil, fmt.Errorf("failed to unmarshal remaining: %w", err)
		}
		if err := json.Unmarshal([]byte(decisionsJSON), &h.Decisions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal decisions: %w", err)
		}
		if err := json.Unmarshal([]byte(uncertainJSON), &h.Uncertain); err != nil {
			return nil, fmt.Errorf("failed to unmarshal uncertain: %w", err)
		}
		handoffs = append(handoffs, h)
	}

	return handoffs, nil
}

// AddGitSnapshot records a git state snapshot
func (db *DB) AddGitSnapshot(snapshot *models.GitSnapshot) error {
	return db.withWriteLock(func() error {
		snapshot.Timestamp = time.Now()

		result, err := db.conn.Exec(`
			INSERT INTO git_snapshots (issue_id, event, commit_sha, branch, dirty_files, timestamp)
			VALUES (?, ?, ?, ?, ?, ?)
		`, snapshot.IssueID, snapshot.Event, snapshot.CommitSHA, snapshot.Branch, snapshot.DirtyFiles, snapshot.Timestamp)

		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		snapshot.ID = id

		return nil
	})
}

// GetStartSnapshot returns the start snapshot for an issue
func (db *DB) GetStartSnapshot(issueID string) (*models.GitSnapshot, error) {
	var snapshot models.GitSnapshot

	err := db.conn.QueryRow(`
		SELECT id, issue_id, event, commit_sha, branch, dirty_files, timestamp
		FROM git_snapshots WHERE issue_id = ? AND event = 'start' ORDER BY timestamp DESC LIMIT 1
	`, issueID).Scan(
		&snapshot.ID, &snapshot.IssueID, &snapshot.Event,
		&snapshot.CommitSHA, &snapshot.Branch, &snapshot.DirtyFiles, &snapshot.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// AddDependency adds a dependency between issues
func (db *DB) AddDependency(issueID, dependsOnID, relationType string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO issue_dependencies (issue_id, depends_on_id, relation_type)
			VALUES (?, ?, ?)
		`, issueID, dependsOnID, relationType)
		return err
	})
}

// RemoveDependency removes a dependency
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
		SELECT issue_id FROM issue_dependencies WHERE depends_on_id = ?
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

// LinkFile links a file to an issue
func (db *DB) LinkFile(issueID, filePath string, role models.FileRole, sha string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO issue_files (issue_id, file_path, role, linked_sha, linked_at)
			VALUES (?, ?, ?, ?, ?)
		`, issueID, filePath, role, sha, time.Now())
		return err
	})
}

// UnlinkFile removes a file link
func (db *DB) UnlinkFile(issueID, filePath string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM issue_files WHERE issue_id = ? AND file_path = ?`, issueID, filePath)
		return err
	})
}

// GetLinkedFiles returns files linked to an issue
func (db *DB) GetLinkedFiles(issueID string) ([]models.IssueFile, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, file_path, role, linked_sha, linked_at
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

// CreateWorkSession creates a new work session
func (db *DB) CreateWorkSession(ws *models.WorkSession) error {
	return db.withWriteLock(func() error {
		id, err := generateWSID()
		if err != nil {
			return err
		}
		ws.ID = id
		ws.StartedAt = time.Now()

		_, err = db.conn.Exec(`
			INSERT INTO work_sessions (id, name, session_id, started_at, start_sha)
			VALUES (?, ?, ?, ?, ?)
		`, ws.ID, ws.Name, ws.SessionID, ws.StartedAt, ws.StartSHA)

		return err
	})
}

// GetWorkSession retrieves a work session
func (db *DB) GetWorkSession(id string) (*models.WorkSession, error) {
	var ws models.WorkSession
	var endedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, session_id, started_at, ended_at, start_sha, end_sha
		FROM work_sessions WHERE id = ?
	`, id).Scan(&ws.ID, &ws.Name, &ws.SessionID, &ws.StartedAt, &endedAt, &ws.StartSHA, &ws.EndSHA)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("work session not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	if endedAt.Valid {
		ws.EndedAt = &endedAt.Time
	}

	return &ws, nil
}

// UpdateWorkSession updates a work session
func (db *DB) UpdateWorkSession(ws *models.WorkSession) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			UPDATE work_sessions SET name = ?, ended_at = ?, end_sha = ?
			WHERE id = ?
		`, ws.Name, ws.EndedAt, ws.EndSHA, ws.ID)
		return err
	})
}

// TagIssueToWorkSession links an issue to a work session
func (db *DB) TagIssueToWorkSession(wsID, issueID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			INSERT OR IGNORE INTO work_session_issues (work_session_id, issue_id, tagged_at)
			VALUES (?, ?, ?)
		`, wsID, issueID, time.Now())
		return err
	})
}

// UntagIssueFromWorkSession removes an issue from a work session
func (db *DB) UntagIssueFromWorkSession(wsID, issueID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM work_session_issues WHERE work_session_id = ? AND issue_id = ?`, wsID, issueID)
		return err
	})
}

// GetWorkSessionIssues returns issues tagged to a work session
func (db *DB) GetWorkSessionIssues(wsID string) ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT issue_id FROM work_session_issues WHERE work_session_id = ? ORDER BY tagged_at
	`, wsID)
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

// ListWorkSessions returns recent work sessions
func (db *DB) ListWorkSessions(limit int) ([]models.WorkSession, error) {
	query := `SELECT id, name, session_id, started_at, ended_at, start_sha, end_sha
	          FROM work_sessions ORDER BY started_at DESC`
	args := []interface{}{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.WorkSession
	for rows.Next() {
		var ws models.WorkSession
		var endedAt sql.NullTime

		if err := rows.Scan(&ws.ID, &ws.Name, &ws.SessionID, &ws.StartedAt, &endedAt, &ws.StartSHA, &ws.EndSHA); err != nil {
			return nil, err
		}

		if endedAt.Valid {
			ws.EndedAt = &endedAt.Time
		}

		sessions = append(sessions, ws)
	}

	return sessions, nil
}

// AddComment adds a comment to an issue
func (db *DB) AddComment(comment *models.Comment) error {
	return db.withWriteLock(func() error {
		comment.CreatedAt = time.Now()

		result, err := db.conn.Exec(`
			INSERT INTO comments (issue_id, session_id, text, created_at)
			VALUES (?, ?, ?, ?)
		`, comment.IssueID, comment.SessionID, comment.Text, comment.CreatedAt)

		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		comment.ID = id

		return nil
	})
}

// GetComments retrieves comments for an issue
func (db *DB) GetComments(issueID string) ([]models.Comment, error) {
	rows, err := db.conn.Query(`
		SELECT id, issue_id, session_id, text, created_at
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

// SearchIssues performs full-text search across issues
func (db *DB) SearchIssues(query string, opts ListIssuesOptions) ([]models.Issue, error) {
	opts.Search = query
	return db.ListIssues(opts)
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

// LogAction records an action for undo support
func (db *DB) LogAction(action *models.ActionLog) error {
	return db.withWriteLock(func() error {
		action.Timestamp = time.Now()

		result, err := db.conn.Exec(`
			INSERT INTO action_log (session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		`, action.SessionID, action.ActionType, action.EntityType, action.EntityID, action.PreviousData, action.NewData, action.Timestamp)

		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		action.ID = id

		return nil
	})
}

// GetLastAction returns the most recent undoable action for a session
func (db *DB) GetLastAction(sessionID string) (*models.ActionLog, error) {
	var action models.ActionLog
	var undone int

	err := db.conn.QueryRow(`
		SELECT id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		WHERE session_id = ? AND undone = 0
		ORDER BY timestamp DESC LIMIT 1
	`, sessionID).Scan(
		&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
		&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	action.Undone = undone == 1
	return &action, nil
}

// MarkActionUndone marks an action as undone
func (db *DB) MarkActionUndone(actionID int64) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE action_log SET undone = 1 WHERE id = ?`, actionID)
		return err
	})
}

// GetRecentActions returns recent actions for a session
func (db *DB) GetRecentActions(sessionID string, limit int) ([]models.ActionLog, error) {
	query := `
		SELECT id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		WHERE session_id = ?
		ORDER BY timestamp DESC`
	args := []interface{}{sessionID}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.ActionLog
	for rows.Next() {
		var action models.ActionLog
		var undone int
		err := rows.Scan(
			&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
			&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
		)
		if err != nil {
			return nil, err
		}
		action.Undone = undone == 1
		actions = append(actions, action)
	}

	return actions, nil
}

// GetActiveSessions returns distinct session IDs with activity since the given time
func (db *DB) GetActiveSessions(since time.Time) ([]string, error) {
	query := `SELECT session_id FROM logs
	          WHERE session_id != '' AND timestamp > ?
	          GROUP BY session_id
	          ORDER BY MAX(timestamp) DESC`

	rows, err := db.conn.Query(query, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			continue
		}
		if sessionID != "" {
			sessions = append(sessions, sessionID)
		}
	}

	return sessions, nil
}

// GetRecentLogsAll returns recent logs across all issues
func (db *DB) GetRecentLogsAll(limit int) ([]models.Log, error) {
	query := `SELECT id, issue_id, session_id, work_session_id, message, type, timestamp
	          FROM logs ORDER BY timestamp DESC`
	args := []interface{}{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.Log
	for rows.Next() {
		var log models.Log
		err := rows.Scan(&log.ID, &log.IssueID, &log.SessionID, &log.WorkSessionID, &log.Message, &log.Type, &log.Timestamp)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetRecentActionsAll returns recent action_log entries across all sessions
func (db *DB) GetRecentActionsAll(limit int) ([]models.ActionLog, error) {
	query := `
		SELECT id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone
		FROM action_log
		ORDER BY timestamp DESC`
	args := []interface{}{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []models.ActionLog
	for rows.Next() {
		var action models.ActionLog
		var undone int
		err := rows.Scan(
			&action.ID, &action.SessionID, &action.ActionType, &action.EntityType,
			&action.EntityID, &action.PreviousData, &action.NewData, &action.Timestamp, &undone,
		)
		if err != nil {
			return nil, err
		}
		action.Undone = undone == 1
		actions = append(actions, action)
	}

	return actions, nil
}

// GetRecentCommentsAll returns recent comments across all issues
func (db *DB) GetRecentCommentsAll(limit int) ([]models.Comment, error) {
	query := `SELECT id, issue_id, session_id, text, created_at
	          FROM comments ORDER BY created_at DESC`
	args := []interface{}{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.conn.Query(query, args...)
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
