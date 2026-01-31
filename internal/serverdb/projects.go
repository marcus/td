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
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO projects (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, description, now, now,
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

	return &Project{ID: id, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}, nil
}

// GetProject returns a project by ID. If includeSoftDeleted is false, soft-deleted projects are excluded.
func (db *ServerDB) GetProject(id string, includeSoftDeleted bool) (*Project, error) {
	query := `SELECT id, name, description, created_at, updated_at, deleted_at FROM projects WHERE id = ?`
	if !includeSoftDeleted {
		query += ` AND deleted_at IS NULL`
	}

	p := &Project{}
	err := db.conn.QueryRow(query, id).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt)
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
		SELECT p.id, p.name, p.description, p.created_at, p.updated_at, p.deleted_at
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
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt); err != nil {
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
