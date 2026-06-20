package serverdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	InvitationStatusPending  = "pending"
	InvitationStatusAccepted = "accepted"
	InvitationStatusDeclined = "declined"
	InvitationStatusExpired  = "expired"
)

var (
	ErrInvitationNotFound      = errors.New("invitation not found")
	ErrInvitationNotPending    = errors.New("invitation not pending")
	ErrInvitationExpired       = errors.New("invitation expired")
	ErrInvitationEmailMismatch = errors.New("invitation email mismatch")
)

type Invitation struct {
	ID         string
	ProjectID  string
	Email      string
	Role       string
	InvitedBy  string
	TokenHash  string
	Status     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func scanInvitation(row interface {
	Scan(...any) error
}) (*Invitation, error) {
	inv := &Invitation{}
	err := row.Scan(
		&inv.ID,
		&inv.ProjectID,
		&inv.Email,
		&inv.Role,
		&inv.InvitedBy,
		&inv.TokenHash,
		&inv.Status,
		&inv.CreatedAt,
		&inv.ExpiresAt,
		&inv.AcceptedAt,
	)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

const invitationSelectCols = `
	id, project_id, email, role, invited_by, token_hash, status, created_at, expires_at, accepted_at`

func (db *ServerDB) CreateInvitation(projectID, email, role, invitedBy, tokenHash string, expiresAt time.Time) (*Invitation, error) {
	if !isValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s", role)
	}
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if invitedBy == "" {
		return nil, fmt.Errorf("invited_by is required")
	}
	if tokenHash == "" {
		return nil, fmt.Errorf("token_hash is required")
	}
	if !expiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("expires_at must be in the future")
	}

	var exists int
	if err := db.conn.QueryRow(`SELECT 1 FROM projects WHERE id = ? AND deleted_at IS NULL`, projectID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found: %s", projectID)
		}
		return nil, fmt.Errorf("check project: %w", err)
	}
	if err := db.conn.QueryRow(`SELECT 1 FROM users WHERE id = ?`, invitedBy).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("inviter not found: %s", invitedBy)
		}
		return nil, fmt.Errorf("check inviter: %w", err)
	}

	id, err := generateID("inv_")
	if err != nil {
		return nil, fmt.Errorf("generate invitation id: %w", err)
	}

	now := time.Now().UTC()
	_, err = db.conn.Exec(
		`INSERT INTO invitations
			(id, project_id, email, role, invited_by, token_hash, status, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, projectID, email, role, invitedBy, tokenHash, InvitationStatusPending, now, expiresAt.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert invitation: %w", err)
	}

	return &Invitation{
		ID:        id,
		ProjectID: projectID,
		Email:     email,
		Role:      role,
		InvitedBy: invitedBy,
		TokenHash: tokenHash,
		Status:    InvitationStatusPending,
		CreatedAt: now,
		ExpiresAt: expiresAt.UTC(),
	}, nil
}

func (db *ServerDB) ListProjectInvitations(projectID string) ([]*Invitation, error) {
	rows, err := db.conn.Query(
		`SELECT`+invitationSelectCols+`
		 FROM invitations
		 WHERE project_id = ?
		 ORDER BY created_at DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list project invitations: %w", err)
	}
	defer rows.Close()

	var invitations []*Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		invitations = append(invitations, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list project invitations: iterate: %w", err)
	}
	return invitations, nil
}

func (db *ServerDB) ListPendingInvitationsForEmail(email string) ([]*Invitation, error) {
	email = normalizeEmail(email)
	rows, err := db.conn.Query(
		`SELECT`+invitationSelectCols+`
		 FROM invitations
		 WHERE email = ? AND status = ? AND expires_at > ?
		 ORDER BY created_at DESC`,
		email, InvitationStatusPending, time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("list pending invitations: %w", err)
	}
	defer rows.Close()

	var invitations []*Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		invitations = append(invitations, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending invitations: iterate: %w", err)
	}
	return invitations, nil
}

func (db *ServerDB) GetInvitation(id string) (*Invitation, error) {
	row := db.conn.QueryRow(
		`SELECT`+invitationSelectCols+`
		 FROM invitations
		 WHERE id = ?`,
		id,
	)
	inv, err := scanInvitation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get invitation: %w", err)
	}
	return inv, nil
}

func (db *ServerDB) AcceptInvitation(invitationID, userID, email string) (*Membership, error) {
	email = normalizeEmail(email)
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inv, err := scanInvitation(tx.QueryRow(
		`SELECT`+invitationSelectCols+`
		 FROM invitations
		 WHERE id = ?`,
		invitationID,
	))
	if err == sql.ErrNoRows {
		return nil, ErrInvitationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select invitation: %w", err)
	}
	if inv.Status != InvitationStatusPending {
		return nil, ErrInvitationNotPending
	}
	if !time.Now().UTC().Before(inv.ExpiresAt) {
		if _, err := tx.Exec(`UPDATE invitations SET status = ? WHERE id = ? AND status = ?`, InvitationStatusExpired, invitationID, InvitationStatusPending); err != nil {
			return nil, fmt.Errorf("mark invitation expired: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit expired invitation: %w", err)
		}
		return nil, ErrInvitationExpired
	}
	if normalizeEmail(inv.Email) != email {
		return nil, ErrInvitationEmailMismatch
	}

	var userEmail string
	if err := tx.QueryRow(`SELECT email FROM users WHERE id = ?`, userID).Scan(&userEmail); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found: %s", userID)
		}
		return nil, fmt.Errorf("check user: %w", err)
	}
	if normalizeEmail(userEmail) != email {
		return nil, ErrInvitationEmailMismatch
	}

	now := time.Now().UTC()
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO memberships (project_id, user_id, role, invited_by, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		inv.ProjectID, userID, inv.Role, inv.InvitedBy, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert membership: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE invitations SET status = ?, accepted_at = ? WHERE id = ?`,
		InvitationStatusAccepted, now, invitationID,
	)
	if err != nil {
		return nil, fmt.Errorf("mark invitation accepted: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return db.GetMembership(inv.ProjectID, userID)
}

func (db *ServerDB) DeclineInvitation(invitationID, email string) error {
	email = normalizeEmail(email)
	now := time.Now().UTC()
	res, err := db.conn.Exec(
		`UPDATE invitations
		 SET status = ?
		 WHERE id = ? AND email = ? AND status = ? AND expires_at > ?`,
		InvitationStatusDeclined, invitationID, email, InvitationStatusPending, now,
	)
	if err != nil {
		return fmt.Errorf("decline invitation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}

	inv, err := db.GetInvitation(invitationID)
	if err != nil {
		return err
	}
	if inv == nil {
		return ErrInvitationNotFound
	}
	if normalizeEmail(inv.Email) != email {
		return ErrInvitationEmailMismatch
	}
	if inv.Status != InvitationStatusPending {
		return ErrInvitationNotPending
	}
	if !time.Now().UTC().Before(inv.ExpiresAt) {
		if _, err := db.conn.Exec(`UPDATE invitations SET status = ? WHERE id = ? AND status = ?`, InvitationStatusExpired, invitationID, InvitationStatusPending); err != nil {
			return fmt.Errorf("mark invitation expired: %w", err)
		}
	}
	return ErrInvitationExpired
}

func (db *ServerDB) DeleteInvitation(projectID, invitationID string) error {
	res, err := db.conn.Exec(`DELETE FROM invitations WHERE project_id = ? AND id = ?`, projectID, invitationID)
	if err != nil {
		return fmt.Errorf("delete invitation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInvitationNotFound
	}
	return nil
}

func (db *ServerDB) HasPendingInvitationForEmail(email string) (bool, error) {
	email = normalizeEmail(email)
	var exists int
	err := db.conn.QueryRow(
		`SELECT 1 FROM invitations WHERE email = ? AND status = ? AND expires_at > ? LIMIT 1`,
		email, InvitationStatusPending, time.Now().UTC(),
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check pending invitation: %w", err)
	}
	return true, nil
}
