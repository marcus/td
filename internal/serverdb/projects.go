package serverdb

import (
	"database/sql"
	"fmt"
	"time"
)

// Project represents a sync project.
type Project struct {
	ID          string
	Name        string
	Description string
	Slug        string
	EventCount  int
	LastEventAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// CreateProject creates a new project and adds the owner as a member in a single transaction.
func (db *ServerDB) CreateProject(name, description, ownerUserID string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	id, err := generateID("p_")
	if err != nil {
		return nil, fmt.Errorf("generate project id: %w", err)
	}

	now := time.Now().UTC()

	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	slug, err := uniqueSlugTx(tx, slugBase(name, id), "")
	if err != nil {
		return nil, fmt.Errorf("generate slug: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO projects (id, name, description, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, description, slug, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO memberships (project_id, user_id, role, invited_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, ownerUserID, RoleOwner, "", now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert owner membership: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &Project{ID: id, Name: name, Description: description, Slug: slug, CreatedAt: now, UpdatedAt: now}, nil
}

// CreateProjectWithID creates a new project using a pre-generated ID and adds the owner as a member.
func (db *ServerDB) CreateProjectWithID(id, name, description, ownerUserID string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	now := time.Now().UTC()

	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	slug, err := uniqueSlugTx(tx, slugBase(name, id), "")
	if err != nil {
		return nil, fmt.Errorf("generate slug: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO projects (id, name, description, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, description, slug, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO memberships (project_id, user_id, role, invited_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, ownerUserID, RoleOwner, "", now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert owner membership: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &Project{ID: id, Name: name, Description: description, Slug: slug, CreatedAt: now, UpdatedAt: now}, nil
}

// GetProject returns a project by ID. If includeSoftDeleted is false, soft-deleted projects are excluded.
func (db *ServerDB) GetProject(id string, includeSoftDeleted bool) (*Project, error) {
	query := `SELECT id, name, description, COALESCE(slug,''), event_count, last_event_at, created_at, updated_at, deleted_at FROM projects WHERE id = ?`
	if !includeSoftDeleted {
		query += ` AND deleted_at IS NULL`
	}

	p := &Project{}
	err := db.conn.QueryRow(query, id).Scan(&p.ID, &p.Name, &p.Description, &p.Slug, &p.EventCount, &p.LastEventAt, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

// ListProjectsForUser returns all non-deleted projects the user is a member of.
func (db *ServerDB) ListProjectsForUser(userID string) ([]*Project, error) {
	rows, err := db.conn.Query(`
		SELECT p.id, p.name, p.description, COALESCE(p.slug,''), p.event_count, p.last_event_at, p.created_at, p.updated_at, p.deleted_at
		FROM projects p
		JOIN memberships m ON m.project_id = p.id
		WHERE m.user_id = ? AND p.deleted_at IS NULL
		ORDER BY p.created_at
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p := &Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Slug, &p.EventCount, &p.LastEventAt, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list projects: iterate: %w", err)
	}
	return projects, nil
}

// UpdateProject updates a project's name and description.
func (db *ServerDB) UpdateProject(id, name, description string) (*Project, error) {
	now := time.Now().UTC()
	res, err := db.conn.Exec(
		`UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		name, description, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("project not found: %s", id)
	}
	return db.GetProject(id, false)
}

// SoftDeleteProject marks a project as deleted.
func (db *ServerDB) SoftDeleteProject(id string) error {
	now := time.Now().UTC()
	res, err := db.conn.Exec(
		`UPDATE projects SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		now, now, id,
	)
	if err != nil {
		return fmt.Errorf("soft delete project: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project not found: %s", id)
	}
	return nil
}

// UpdateProjectEventCount atomically increments event_count and updates last_event_at.
func (db *ServerDB) UpdateProjectEventCount(projectID string, newEvents int, lastEventAt time.Time) error {
	_, err := db.conn.Exec(
		`UPDATE projects SET event_count = event_count + ?, last_event_at = ? WHERE id = ?`,
		newEvents, lastEventAt, projectID,
	)
	return err
}

// GetProjectEventCount returns the cached event count and last event timestamp for a project.
func (db *ServerDB) GetProjectEventCount(projectID string) (int, *time.Time, error) {
	var count int
	var lastEventAt *time.Time
	err := db.conn.QueryRow(
		`SELECT event_count, last_event_at FROM projects WHERE id = ?`, projectID,
	).Scan(&count, &lastEventAt)
	if err == sql.ErrNoRows {
		return 0, nil, fmt.Errorf("project not found: %s", projectID)
	}
	if err != nil {
		return 0, nil, fmt.Errorf("get project event count: %w", err)
	}
	return count, lastEventAt, nil
}

// CountProjects returns the total number of non-deleted projects.
func (db *ServerDB) CountProjects() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM projects WHERE deleted_at IS NULL").Scan(&count)
	return count, err
}

// BackfillProjectSlugs assigns a slug to every project that has a NULL or
// empty slug. Projects are processed in created_at ASC order so the
// deterministic ordering means the same project always wins the base slug.
// This is safe to call on every startup — projects that already have a slug
// are skipped.
func (db *ServerDB) BackfillProjectSlugs() error {
	rows, err := db.conn.Query(
		`SELECT id, name FROM projects WHERE slug IS NULL OR slug = '' ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return fmt.Errorf("backfill slugs: query: %w", err)
	}
	defer rows.Close()

	type projectRow struct {
		id   string
		name string
	}
	var pending []projectRow
	for rows.Next() {
		var r projectRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return fmt.Errorf("backfill slugs: scan: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("backfill slugs: iterate: %w", err)
	}

	for _, r := range pending {
		slug, err := uniqueSlug(db.conn, slugBase(r.name, r.id), r.id)
		if err != nil {
			return fmt.Errorf("backfill slugs: generate for %s: %w", r.id, err)
		}
		if _, err := db.conn.Exec(
			`UPDATE projects SET slug = ? WHERE id = ? AND (slug IS NULL OR slug = '')`,
			slug, r.id,
		); err != nil {
			return fmt.Errorf("backfill slugs: update %s: %w", r.id, err)
		}
	}
	return nil
}
