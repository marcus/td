package serverdb

import (
	"database/sql"
	"fmt"
	"time"
)

// Membership represents a user's role in a project.
type Membership struct {
	ProjectID string
	UserID    string
	Role      string
	InvitedBy string
	CreatedAt time.Time
}

// AddMember adds a user to a project with the given role.
func (db *ServerDB) AddMember(projectID, userID, role, invitedByUserID string) (*Membership, error) {
	if !isValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	// Validate project exists
	var exists int
	if err := db.conn.QueryRow(`SELECT 1 FROM projects WHERE id = ? AND deleted_at IS NULL`, projectID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found: %s", projectID)
		}
		return nil, fmt.Errorf("check project: %w", err)
	}

	// Validate user exists
	if err := db.conn.QueryRow(`SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", userID)
		}
		return nil, fmt.Errorf("check user: %w", err)
	}

	now := time.Now().UTC()
	_, err := db.conn.Exec(
		`INSERT INTO memberships (project_id, user_id, role, invited_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		projectID, userID, role, invitedByUserID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	return &Membership{
		ProjectID: projectID,
		UserID:    userID,
		Role:      role,
		InvitedBy: invitedByUserID,
		CreatedAt: now,
	}, nil
}

// GetMembership returns a user's membership in a project, or nil if not found.
func (db *ServerDB) GetMembership(projectID, userID string) (*Membership, error) {
	m := &Membership{}
	err := db.conn.QueryRow(
		`SELECT project_id, user_id, role, invited_by, created_at FROM memberships WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	).Scan(&m.ProjectID, &m.UserID, &m.Role, &m.InvitedBy, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get membership: %w", err)
	}
	return m, nil
}

// ListMembers returns all members of a project.
func (db *ServerDB) ListMembers(projectID string) ([]*Membership, error) {
	rows, err := db.conn.Query(
		`SELECT project_id, user_id, role, invited_by, created_at FROM memberships WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var members []*Membership
	for rows.Next() {
		m := &Membership{}
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.InvitedBy, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list members: iterate: %w", err)
	}
	return members, nil
}

// UpdateMemberRole changes a member's role.
func (db *ServerDB) UpdateMemberRole(projectID, userID, newRole string) error {
	if !isValidRole(newRole) {
		return fmt.Errorf("invalid role: %s", newRole)
	}

	res, err := db.conn.Exec(
		`UPDATE memberships SET role = ? WHERE project_id = ? AND user_id = ?`,
		newRole, projectID, userID,
	)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("membership not found")
	}
	return nil
}

// RemoveMember removes a user from a project.
// Fails if removing the user would leave the project with no owners.
func (db *ServerDB) RemoveMember(projectID, userID string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Check current membership within tx
	var role string
	err = tx.QueryRow(
		`SELECT role FROM memberships WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return fmt.Errorf("membership not found")
	}
	if err != nil {
		return fmt.Errorf("get membership: %w", err)
	}

	// If removing an owner, ensure at least one other owner remains
	if role == RoleOwner {
		var ownerCount int
		err := tx.QueryRow(
			`SELECT COUNT(*) FROM memberships WHERE project_id = ? AND role = 'owner'`,
			projectID,
		).Scan(&ownerCount)
		if err != nil {
			return fmt.Errorf("count owners: %w", err)
		}
		if ownerCount <= 1 {
			return fmt.Errorf("cannot remove last owner from project")
		}
	}

	_, err = tx.Exec(
		`DELETE FROM memberships WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func isValidRole(role string) bool {
	return role == RoleOwner || role == RoleWriter || role == RoleReader
}
