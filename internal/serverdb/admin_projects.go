package serverdb

import (
	"database/sql"
	"fmt"
	"time"
)

// AdminProject represents a project with aggregate info for admin API.
type AdminProject struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description,omitempty"`
	EventCount  int     `json:"event_count"`
	LastEventAt *string `json:"last_event_at"`
	MemberCount int     `json:"member_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	DeletedAt   *string `json:"deleted_at,omitempty"`
}

// AdminProjectMember represents a project member with user email for admin view.
type AdminProjectMember struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
	CreatedAt string `json:"created_at"`
}

// AdminListProjects returns a paginated list of all projects with aggregate counts.
func (db *ServerDB) AdminListProjects(query string, includeDeleted bool, limit int, cursor string) (*PaginatedResult[AdminProject], error) {
	baseQuery := `SELECT p.id, p.name, COALESCE(p.slug,''), p.event_count, p.last_event_at,
		(SELECT COUNT(*) FROM memberships m WHERE m.project_id = p.id) as member_count,
		p.created_at, p.updated_at, p.deleted_at
		FROM projects p WHERE 1=1`

	var args []any
	if !includeDeleted {
		baseQuery += " AND p.deleted_at IS NULL"
	}
	if query != "" {
		baseQuery += " AND p.name LIKE ?"
		args = append(args, "%"+query+"%")
	}

	scanRow := func(rows *sql.Rows) (AdminProject, string, error) {
		var p AdminProject
		var lastEventAt *time.Time
		var deletedAt *time.Time
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.EventCount, &lastEventAt, &p.MemberCount, &createdAt, &updatedAt, &deletedAt); err != nil {
			return p, "", err
		}
		p.CreatedAt = createdAt.UTC().Format("2006-01-02T15:04:05Z")
		p.UpdatedAt = updatedAt.UTC().Format("2006-01-02T15:04:05Z")
		if lastEventAt != nil {
			s := lastEventAt.UTC().Format("2006-01-02T15:04:05Z")
			p.LastEventAt = &s
		}
		if deletedAt != nil {
			s := deletedAt.UTC().Format("2006-01-02T15:04:05Z")
			p.DeletedAt = &s
		}
		return p, p.ID, nil
	}

	return PaginatedQuery(db.conn, baseQuery, args, limit, cursor, "p.id", scanRow)
}

// AdminGetProject returns a single project with aggregate info including description.
func (db *ServerDB) AdminGetProject(id string) (*AdminProject, error) {
	var p AdminProject
	var lastEventAt *time.Time
	var deletedAt *time.Time
	var createdAt, updatedAt time.Time
	err := db.conn.QueryRow(`SELECT p.id, p.name, COALESCE(p.slug,''), p.description, p.event_count, p.last_event_at,
		(SELECT COUNT(*) FROM memberships m WHERE m.project_id = p.id) as member_count,
		p.created_at, p.updated_at, p.deleted_at
		FROM projects p WHERE p.id = ?`, id).Scan(
		&p.ID, &p.Name, &p.Slug, &p.Description, &p.EventCount, &lastEventAt,
		&p.MemberCount, &createdAt, &updatedAt, &deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("admin get project: %w", err)
	}
	p.CreatedAt = createdAt.UTC().Format("2006-01-02T15:04:05Z")
	p.UpdatedAt = updatedAt.UTC().Format("2006-01-02T15:04:05Z")
	if lastEventAt != nil {
		s := lastEventAt.UTC().Format("2006-01-02T15:04:05Z")
		p.LastEventAt = &s
	}
	if deletedAt != nil {
		s := deletedAt.UTC().Format("2006-01-02T15:04:05Z")
		p.DeletedAt = &s
	}
	return &p, nil
}

// AdminListProjectMembers returns all members of a project with user email.
func (db *ServerDB) AdminListProjectMembers(projectID string) ([]AdminProjectMember, error) {
	rows, err := db.conn.Query(`SELECT m.user_id, u.email, m.role, m.invited_by, m.created_at
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		WHERE m.project_id = ?
		ORDER BY m.created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("admin list project members: %w", err)
	}
	defer rows.Close()

	var members []AdminProjectMember
	for rows.Next() {
		var m AdminProjectMember
		var createdAt time.Time
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role, &m.InvitedBy, &createdAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		m.CreatedAt = createdAt.UTC().Format("2006-01-02T15:04:05Z")
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}
	if members == nil {
		members = []AdminProjectMember{}
	}
	return members, nil
}

// ListSyncCursorsForProject returns all sync cursors for a project.
func (db *ServerDB) ListSyncCursorsForProject(projectID string) ([]SyncCursor, error) {
	rows, err := db.conn.Query(
		`SELECT project_id, client_id, last_event_id, last_sync_at FROM sync_cursors WHERE project_id = ? ORDER BY client_id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sync cursors: %w", err)
	}
	defer rows.Close()

	var cursors []SyncCursor
	for rows.Next() {
		var c SyncCursor
		if err := rows.Scan(&c.ProjectID, &c.ClientID, &c.LastEventID, &c.LastSyncAt); err != nil {
			return nil, fmt.Errorf("scan cursor: %w", err)
		}
		cursors = append(cursors, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cursors: %w", err)
	}
	if cursors == nil {
		cursors = []SyncCursor{}
	}
	return cursors, nil
}
