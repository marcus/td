package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
	_ "modernc.org/sqlite"
)

// QueryValidator is set by main to validate TDQ queries without import cycle.
// Returns nil if valid, error describing parse failure otherwise.
var QueryValidator func(queryStr string) error

const (
	dbFile        = ".todos/issues.db"
	idPrefix      = "td-"
	wsIDPrefix    = "ws-"
	boardIDPrefix = "bd-"
)

// NormalizeIssueID ensures an issue ID has the td- prefix
// Accepts bare hex IDs like "abc123" and returns "td-abc123"
func NormalizeIssueID(id string) string {
	if id == "" {
		return id
	}
	if !strings.HasPrefix(id, idPrefix) {
		return idPrefix + id
	}
	return id
}

// DB wraps the database connection
type DB struct {
	conn    *sql.DB
	baseDir string
}

// SearchResult holds an issue with relevance scoring for ranked search
type SearchResult struct {
	Issue      models.Issue
	Score      int    // Higher = better match (0-100)
	MatchField string // Primary field that matched: 'id', 'title', 'description', 'labels'
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

// generateBoardID generates a unique board ID
func generateBoardID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return boardIDPrefix + hex.EncodeToString(bytes), nil
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
			INSERT INTO issues (id, title, description, status, type, priority, points, labels, parent_id, acceptance, created_at, updated_at, minor, created_branch, creator_session)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, issue.ID, issue.Title, issue.Description, issue.Status, issue.Type, issue.Priority, issue.Points, labels, issue.ParentID, issue.Acceptance, issue.CreatedAt, issue.UpdatedAt, issue.Minor, issue.CreatedBranch, issue.CreatorSession)

		return err
	})
}

// GetIssue retrieves an issue by ID
// Accepts bare IDs without the td- prefix (e.g., "abc123" becomes "td-abc123")
func (db *DB) GetIssue(id string) (*models.Issue, error) {
	id = NormalizeIssueID(id)
	var issue models.Issue
	var labels string
	var closedAt, deletedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, title, description, status, type, priority, points, labels, parent_id, acceptance, sprint,
		       implementer_session, creator_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
	FROM issues WHERE id = ?
	`, id).Scan(
		&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
		&issue.Points, &labels, &issue.ParentID, &issue.Acceptance, &issue.Sprint,
		&issue.ImplementerSession, &issue.CreatorSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
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
		if err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&issue.Points, &labels, &issue.ParentID, &issue.Acceptance, &issue.Sprint,
			&issue.ImplementerSession, &issue.CreatorSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
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

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&issue.Points, &labels, &issue.ParentID, &issue.Acceptance, &issue.Sprint,
			&issue.ImplementerSession, &issue.CreatorSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
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

		err := rows.Scan(
			&issue.ID, &issue.Title, &issue.Description, &issue.Status, &issue.Type, &issue.Priority,
			&issue.Points, &labels, &issue.ParentID, &issue.Acceptance, &issue.Sprint,
			&issue.ImplementerSession, &issue.CreatorSession, &issue.ReviewerSession, &issue.CreatedAt, &issue.UpdatedAt, &closedAt, &deletedAt, &issue.Minor, &issue.CreatedBranch,
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

// DeleteHandoff removes a handoff by ID (for undo support)
func (db *DB) DeleteHandoff(handoffID int64) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM handoffs WHERE id = ?`, handoffID)
		return err
	})
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
		       implementer_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE status = ? AND deleted_at IS NULL ORDER BY created_at ASC LIMIT 1
	`, models.StatusOpen).Scan(
		&oldestIssue.ID, &oldestIssue.Title, &oldestIssue.Description, &oldestIssue.Status, &oldestIssue.Type,
		&oldestIssue.Priority, &oldestIssue.Points, &labels, &oldestIssue.ParentID, &oldestIssue.Acceptance, &oldestIssue.Sprint,
		&oldestIssue.ImplementerSession, &oldestIssue.ReviewerSession, &oldestIssue.CreatedAt, &oldestIssue.UpdatedAt,
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
		       implementer_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT 1
	`).Scan(
		&newestIssue.ID, &newestIssue.Title, &newestIssue.Description, &newestIssue.Status, &newestIssue.Type,
		&newestIssue.Priority, &newestIssue.Points, &labels, &newestIssue.ParentID, &newestIssue.Acceptance, &newestIssue.Sprint,
		&newestIssue.ImplementerSession, &newestIssue.ReviewerSession, &newestIssue.CreatedAt, &newestIssue.UpdatedAt,
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
		       implementer_session, reviewer_session, created_at, updated_at, closed_at, deleted_at, minor, created_branch
		FROM issues WHERE status = ? AND closed_at IS NOT NULL AND deleted_at IS NULL
		ORDER BY closed_at DESC LIMIT 1
	`, models.StatusClosed).Scan(
		&closedIssue.ID, &closedIssue.Title, &closedIssue.Description, &closedIssue.Status, &closedIssue.Type,
		&closedIssue.Priority, &closedIssue.Points, &labels, &closedIssue.ParentID, &closedIssue.Acceptance, &closedIssue.Sprint,
		&closedIssue.ImplementerSession, &closedIssue.ReviewerSession, &closedIssue.CreatedAt, &closedIssue.UpdatedAt,
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

// SearchIssues performs full-text search across issues
func (db *DB) SearchIssues(query string, opts ListIssuesOptions) ([]models.Issue, error) {
	opts.Search = query
	return db.ListIssues(opts)
}

// SearchIssuesRanked performs search with relevance scoring
func (db *DB) SearchIssuesRanked(query string, opts ListIssuesOptions) ([]SearchResult, error) {
	issues, err := db.SearchIssues(query, opts)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	results := make([]SearchResult, 0, len(issues))

	for _, issue := range issues {
		score := 0
		matchField := ""

		idLower := strings.ToLower(issue.ID)
		titleLower := strings.ToLower(issue.Title)
		descLower := strings.ToLower(issue.Description)
		labelsLower := strings.ToLower(strings.Join(issue.Labels, ","))

		// Score by match quality (highest wins)
		if strings.EqualFold(issue.ID, query) {
			score = 100
			matchField = "id"
		} else if strings.Contains(idLower, queryLower) {
			score = 90
			matchField = "id"
		} else if strings.EqualFold(issue.Title, query) {
			score = 80
			matchField = "title"
		} else if strings.HasPrefix(titleLower, queryLower) {
			score = 70
			matchField = "title"
		} else if strings.Contains(titleLower, queryLower) {
			score = 60
			matchField = "title"
		} else if strings.Contains(descLower, queryLower) {
			score = 40
			matchField = "description"
		} else if strings.Contains(labelsLower, queryLower) {
			score = 20
			matchField = "labels"
		}

		results = append(results, SearchResult{
			Issue:      issue,
			Score:      score,
			MatchField: matchField,
		})
	}

	// Sort by score DESC, then by priority ASC
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Issue.Priority < results[j].Issue.Priority
	})

	return results, nil
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

// GetRejectedInProgressIssueIDs returns IDs of in_progress issues that have a
// recent ActionReject without a subsequent ActionReview (needs rework)
func (db *DB) GetRejectedInProgressIssueIDs() (map[string]bool, error) {
	query := `
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
	`

	rows, err := db.conn.Query(query)
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

// CascadeUpParentStatus checks if all children of a parent epic have reached the target status,
// and if so, updates the parent to that status. Works recursively up the parent chain.
// Returns the number of parents that were cascaded and the list of cascaded parent IDs.
func (db *DB) CascadeUpParentStatus(issueID string, targetStatus models.Status, sessionID string) (int, []string) {
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
	prevData, _ := json.Marshal(parent)

	parent.Status = targetStatus
	if targetStatus == models.StatusClosed {
		now := time.Now()
		parent.ClosedAt = &now
	}

	if err := db.UpdateIssue(parent); err != nil {
		return cascadedCount, cascadedIDs
	}

	// Log action for undo
	newData, _ := json.Marshal(parent)
	actionType := models.ActionReview
	if targetStatus == models.StatusClosed {
		actionType = models.ActionClose
	}
	db.LogAction(&models.ActionLog{
		SessionID:    sessionID,
		ActionType:   actionType,
		EntityType:   "issue",
		EntityID:     parent.ID,
		PreviousData: string(prevData),
		NewData:      string(newData),
	})

	// Add log entry
	logMsg := fmt.Sprintf("Auto-cascaded to %s (all children complete)", targetStatus)
	db.AddLog(&models.Log{
		IssueID:   parent.ID,
		SessionID: sessionID,
		Message:   logMsg,
		Type:      models.LogTypeProgress,
	})

	cascadedIDs = append(cascadedIDs, parent.ID)
	cascadedCount++

	// Recursively check parent's parent
	moreCount, moreIDs := db.CascadeUpParentStatus(parent.ID, targetStatus, sessionID)
	cascadedCount += moreCount
	cascadedIDs = append(cascadedIDs, moreIDs...)

	return cascadedCount, cascadedIDs
}

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

// ============================================================================
// Board CRUD Functions
// ============================================================================

// CreateBoard creates a new board with a TDQ query
func (db *DB) CreateBoard(name, queryStr string) (*models.Board, error) {
	var board *models.Board
	err := db.withWriteLock(func() error {
		// Validate query syntax if not empty
		if queryStr != "" {
			if err := parseAndValidateQuery(queryStr); err != nil {
				return fmt.Errorf("invalid query: %w", err)
			}
		}

		id, err := generateBoardID()
		if err != nil {
			return err
		}

		now := time.Now()
		board = &models.Board{
			ID:        id,
			Name:      name,
			Query:     queryStr,
			IsBuiltin: false,
			ViewMode:  "swimlanes",
			CreatedAt: now,
			UpdatedAt: now,
		}

		_, err = db.conn.Exec(`
			INSERT INTO boards (id, name, query, is_builtin, view_mode, created_at, updated_at)
			VALUES (?, ?, ?, 0, ?, ?, ?)
		`, board.ID, board.Name, board.Query, board.ViewMode, board.CreatedAt, board.UpdatedAt)

		return err
	})
	return board, err
}

// parseAndValidateQuery validates TDQ syntax using the registered QueryValidator
func parseAndValidateQuery(queryStr string) error {
	if queryStr == "" {
		return nil
	}
	if QueryValidator == nil {
		return nil // No validator registered, skip validation
	}
	return QueryValidator(queryStr)
}

// GetBoard retrieves a board by ID
func (db *DB) GetBoard(id string) (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards WHERE id = ?
	`, id).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("board not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// GetBoardByName retrieves a board by name (case-insensitive)
func (db *DB) GetBoardByName(name string) (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards WHERE name = ? COLLATE NOCASE
	`, name).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("board not found: %s", name)
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// ResolveBoardRef resolves a board reference (ID or name)
func (db *DB) ResolveBoardRef(ref string) (*models.Board, error) {
	// Try by ID first
	if strings.HasPrefix(ref, boardIDPrefix) {
		return db.GetBoard(ref)
	}
	// Try by name
	return db.GetBoardByName(ref)
}

// ListBoards returns all boards sorted by last_viewed_at DESC
func (db *DB) ListBoards() ([]models.Board, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards
		ORDER BY CASE WHEN last_viewed_at IS NULL THEN 1 ELSE 0 END, last_viewed_at DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boards []models.Board
	for rows.Next() {
		var board models.Board
		var isBuiltin int
		var lastViewedAt sql.NullTime

		if err := rows.Scan(
			&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
			&board.CreatedAt, &board.UpdatedAt,
		); err != nil {
			return nil, err
		}

		board.IsBuiltin = isBuiltin == 1
		if lastViewedAt.Valid {
			board.LastViewedAt = &lastViewedAt.Time
		}

		boards = append(boards, board)
	}

	return boards, nil
}

// UpdateBoard updates a board's name and/or query
func (db *DB) UpdateBoard(board *models.Board) error {
	return db.withWriteLock(func() error {
		// Check if builtin
		var isBuiltin int
		err := db.conn.QueryRow(`SELECT is_builtin FROM boards WHERE id = ?`, board.ID).Scan(&isBuiltin)
		if err != nil {
			return fmt.Errorf("board not found: %s", board.ID)
		}
		if isBuiltin == 1 {
			return fmt.Errorf("cannot modify builtin board")
		}

		// Validate query if provided
		if board.Query != "" {
			if err := parseAndValidateQuery(board.Query); err != nil {
				return fmt.Errorf("invalid query: %w", err)
			}
		}

		board.UpdatedAt = time.Now()
		_, err = db.conn.Exec(`
			UPDATE boards SET name = ?, query = ?, updated_at = ?
			WHERE id = ?
		`, board.Name, board.Query, board.UpdatedAt, board.ID)

		return err
	})
}

// DeleteBoard deletes a board (fails for builtin boards)
func (db *DB) DeleteBoard(id string) error {
	return db.withWriteLock(func() error {
		// Check if builtin
		var isBuiltin int
		err := db.conn.QueryRow(`SELECT is_builtin FROM boards WHERE id = ?`, id).Scan(&isBuiltin)
		if err == sql.ErrNoRows {
			return fmt.Errorf("board not found: %s", id)
		}
		if err != nil {
			return err
		}
		if isBuiltin == 1 {
			return fmt.Errorf("cannot delete builtin board")
		}

		// Delete positions first
		_, err = db.conn.Exec(`DELETE FROM board_issue_positions WHERE board_id = ?`, id)
		if err != nil {
			return err
		}

		// Delete board
		_, err = db.conn.Exec(`DELETE FROM boards WHERE id = ?`, id)
		return err
	})
}

// GetLastViewedBoard returns the most recently viewed board
func (db *DB) GetLastViewedBoard() (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards
		WHERE last_viewed_at IS NOT NULL
		ORDER BY last_viewed_at DESC
		LIMIT 1
	`).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Return the builtin All Issues board
		return db.GetBoard("bd-all-issues")
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// UpdateBoardLastViewed updates the last_viewed_at timestamp for a board
func (db *DB) UpdateBoardLastViewed(boardID string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		_, err := db.conn.Exec(`UPDATE boards SET last_viewed_at = ? WHERE id = ?`, now, boardID)
		return err
	})
}

// UpdateBoardViewMode updates the view_mode for a board (swimlanes or backlog)
func (db *DB) UpdateBoardViewMode(boardID, viewMode string) error {
	if viewMode != "swimlanes" && viewMode != "backlog" {
		return fmt.Errorf("invalid view mode: %s (must be 'swimlanes' or 'backlog')", viewMode)
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE boards SET view_mode = ?, updated_at = ? WHERE id = ?`,
			viewMode, time.Now(), boardID)
		return err
	})
}

// ============================================================================
// Board Position Functions
// ============================================================================

// BoardIssuePosition represents an explicit position for an issue on a board
type BoardIssuePosition struct {
	BoardID  string
	IssueID  string
	Position int
}

// SetIssuePosition sets an explicit position for an issue on a board
func (db *DB) SetIssuePosition(boardID, issueID string, position int) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Remove existing position for this issue
		_, err = tx.Exec(`DELETE FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, issueID)
		if err != nil {
			return err
		}

		// Shift positions >= target by +1 to make room
		// Use two-step approach to avoid unique constraint violations:
		// 1. Add large offset to positions being shifted (moves them out of conflict range)
		// 2. Subtract offset-1 to get final positions (large+offset -> position+1)
		const shiftOffset = 1000000
		_, err = tx.Exec(`
			UPDATE board_issue_positions
			SET position = position + ?
			WHERE board_id = ? AND position >= ?
		`, shiftOffset, boardID, position)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
			UPDATE board_issue_positions
			SET position = position - ? + 1
			WHERE board_id = ? AND position >= ?
		`, shiftOffset, boardID, shiftOffset)
		if err != nil {
			return err
		}

		// Insert the new position
		_, err = tx.Exec(`
			INSERT INTO board_issue_positions (board_id, issue_id, position, added_at)
			VALUES (?, ?, ?, ?)
		`, boardID, issueID, position, time.Now())
		if err != nil {
			return err
		}

		return tx.Commit()
	})
}

// RemoveIssuePosition removes an explicit position for an issue
func (db *DB) RemoveIssuePosition(boardID, issueID string) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, issueID)
		return err
	})
}

// GetBoardIssuePositions returns all explicit positions for a board
func (db *DB) GetBoardIssuePositions(boardID string) ([]BoardIssuePosition, error) {
	rows, err := db.conn.Query(`
		SELECT board_id, issue_id, position
		FROM board_issue_positions
		WHERE board_id = ?
		ORDER BY position ASC
	`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []BoardIssuePosition
	for rows.Next() {
		var p BoardIssuePosition
		if err := rows.Scan(&p.BoardID, &p.IssueID, &p.Position); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}

	return positions, nil
}

// SwapIssuePositions swaps the positions of two issues on a board
func (db *DB) SwapIssuePositions(boardID, id1, id2 string) error {
	id1 = NormalizeIssueID(id1)
	id2 = NormalizeIssueID(id2)
	return db.withWriteLock(func() error {
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Get positions
		var pos1, pos2 int
		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, id1).Scan(&pos1)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id1)
		}

		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, id2).Scan(&pos2)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id2)
		}

		// Swap using a temp position to avoid unique constraint
		_, err = tx.Exec(`UPDATE board_issue_positions SET position = -1 WHERE board_id = ? AND issue_id = ?`,
			boardID, id1)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ?`,
			pos1, boardID, id2)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ?`,
			pos2, boardID, id1)
		if err != nil {
			return err
		}

		return tx.Commit()
	})
}

// GetBoardIssues returns issues for a board with their positions.
// For boards with empty query, it fetches all issues directly.
// For boards with TDQ queries, callers should use ApplyBoardPositions with
// pre-executed query results to avoid circular import issues.
// Issues are returned: positioned first (by position), then unpositioned (by query order).
func (db *DB) GetBoardIssues(boardID, sessionID string, statusFilter []models.Status) ([]models.BoardIssueView, error) {
	// Get the board
	board, err := db.GetBoard(boardID)
	if err != nil {
		return nil, err
	}

	// For boards with queries, callers should use ApplyBoardPositions
	// This function only handles empty-query boards (All Issues) correctly
	if board.Query != "" {
		// Fallback: list all issues with status filter
		// NOTE: This doesn't execute the TDQ query - callers should use
		// query.Execute() + ApplyBoardPositions() for proper TDQ support
		opts := ListIssuesOptions{
			Status: statusFilter,
			SortBy: "priority",
		}
		issues, err := db.ListIssues(opts)
		if err != nil {
			return nil, err
		}
		return db.ApplyBoardPositions(boardID, issues)
	}

	// Empty query matches all issues
	opts := ListIssuesOptions{
		Status: statusFilter,
		SortBy: "priority",
	}
	issues, err := db.ListIssues(opts)
	if err != nil {
		return nil, err
	}

	return db.ApplyBoardPositions(boardID, issues)
}

// ApplyBoardPositions takes a list of issues and applies board positions.
// Issues with explicit positions are sorted by position and returned first,
// followed by unpositioned issues in their original order.
// This function should be used with query.Execute() results for boards with TDQ queries.
func (db *DB) ApplyBoardPositions(boardID string, issues []models.Issue) ([]models.BoardIssueView, error) {
	// Get explicit positions
	positions, err := db.GetBoardIssuePositions(boardID)
	if err != nil {
		return nil, err
	}

	// Build a map of issue ID to position
	positionMap := make(map[string]int)
	for _, p := range positions {
		positionMap[p.IssueID] = p.Position
	}

	// Build result with positioned and unpositioned issues
	var positioned []models.BoardIssueView
	var unpositioned []models.BoardIssueView

	for _, issue := range issues {
		view := models.BoardIssueView{
			BoardID: boardID,
			Issue:   issue,
		}
		if pos, ok := positionMap[issue.ID]; ok {
			view.Position = pos
			view.HasPosition = true
			positioned = append(positioned, view)
		} else {
			unpositioned = append(unpositioned, view)
		}
	}

	// Sort positioned by position
	sort.Slice(positioned, func(i, j int) bool {
		return positioned[i].Position < positioned[j].Position
	})

	// Combine: positioned first, then unpositioned (already in query order)
	result := append(positioned, unpositioned...)
	return result, nil
}
