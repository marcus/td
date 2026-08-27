package serverdb

import (
	"database/sql"
	"fmt"
)

// AdminUser represents a user with aggregate info for the admin API.
type AdminUser struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	IsAdmin      bool    `json:"is_admin"`
	CreatedAt    string  `json:"created_at"`
	ProjectCount int     `json:"project_count"`
	LastActivity *string `json:"last_activity"`
}

// AdminUserDetail extends AdminUser with project membership details.
type AdminUserDetail struct {
	AdminUser
	Projects []UserProject `json:"projects"`
}

// UserProject represents a project membership for a user.
type UserProject struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
}

// AdminListUsers returns a paginated list of users with aggregate counts.
// The query parameter filters by email (LIKE match).
func (db *ServerDB) AdminListUsers(query string, limit int, cursor string) (*PaginatedResult[AdminUser], error) {
	baseQuery := `SELECT u.id, u.email, u.is_admin, u.created_at,
		(SELECT COUNT(*) FROM memberships m WHERE m.user_id = u.id) as project_count,
		(SELECT MAX(last_used_at) FROM api_keys ak WHERE ak.user_id = u.id) as last_activity
		FROM users u WHERE 1=1`

	var args []any
	if query != "" {
		baseQuery += " AND u.email LIKE ?"
		args = append(args, "%"+query+"%")
	}

	scanRow := func(rows *sql.Rows) (AdminUser, string, error) {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.CreatedAt, &u.ProjectCount, &u.LastActivity); err != nil {
			return u, "", err
		}
		return u, u.ID, nil
	}

	return PaginatedQuery(db.conn, baseQuery, args, limit, cursor, "u.id", scanRow)
}

// AdminGetUser returns a single user with aggregate info and project memberships.
func (db *ServerDB) AdminGetUser(id string) (*AdminUserDetail, error) {
	var u AdminUser
	err := db.conn.QueryRow(`SELECT u.id, u.email, u.is_admin, u.created_at,
		(SELECT COUNT(*) FROM memberships m WHERE m.user_id = u.id) as project_count,
		(SELECT MAX(last_used_at) FROM api_keys ak WHERE ak.user_id = u.id) as last_activity
		FROM users u WHERE u.id = ?`, id).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.CreatedAt, &u.ProjectCount, &u.LastActivity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin get user: %w", err)
	}

	// Fetch project memberships
	rows, err := db.conn.Query(`SELECT m.project_id, p.name, m.role
		FROM memberships m
		JOIN projects p ON p.id = m.project_id
		WHERE m.user_id = ? AND p.deleted_at IS NULL
		ORDER BY p.name`, id)
	if err != nil {
		return nil, fmt.Errorf("admin get user projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var projects []UserProject
	for rows.Next() {
		var p UserProject
		if err := rows.Scan(&p.ProjectID, &p.Name, &p.Role); err != nil {
			return nil, fmt.Errorf("scan user project: %w", err)
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user projects: %w", err)
	}
	if projects == nil {
		projects = []UserProject{}
	}

	return &AdminUserDetail{
		AdminUser: u,
		Projects:  projects,
	}, nil
}

// ListAPIKeysForUser returns all API keys for a user without the key_hash.
func (db *ServerDB) ListAPIKeysForUser(userID string) ([]*APIKey, error) {
	return db.ListAPIKeys(userID)
}
