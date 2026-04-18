package serverdb

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// User represents a registered user.
type User struct {
	ID              string
	Email           string
	EmailVerifiedAt *time.Time
	IsAdmin         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateUser inserts a new user with the given email (lowercased).
// The first user created is automatically made an admin.
func (db *ServerDB) CreateUser(email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	id, err := generateID("u_")
	if err != nil {
		return nil, fmt.Errorf("generate user id: %w", err)
	}

	// Use a transaction to atomically check count and insert
	// to prevent TOCTOU race where two concurrent requests both become admin
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// First user becomes admin
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	isAdmin := count == 0

	now := time.Now().UTC()
	_, err = tx.Exec(
		`INSERT INTO users (id, email, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, email, isAdmin, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &User{ID: id, Email: email, IsAdmin: isAdmin, CreatedAt: now, UpdatedAt: now}, nil
}

// GetUserByID returns the user with the given ID, or nil if not found.
func (db *ServerDB) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, email_verified_at, is_admin, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

// GetUserByEmail returns the user with the given email (case-insensitive), or nil if not found.
func (db *ServerDB) GetUserByEmail(email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, email_verified_at, is_admin, created_at, updated_at FROM users WHERE LOWER(email) = ?`, email,
	).Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

// ListUsers returns all users.
func (db *ServerDB) ListUsers() ([]*User, error) {
	rows, err := db.conn.Query(`SELECT id, email, email_verified_at, is_admin, created_at, updated_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: iterate: %w", err)
	}
	return users, nil
}

// SetEmailVerified marks the user's email as verified.
func (db *ServerDB) SetEmailVerified(userID string) error {
	now := time.Now().UTC()
	res, err := db.conn.Exec(
		`UPDATE users SET email_verified_at = ?, updated_at = ? WHERE id = ?`,
		now, now, userID,
	)
	if err != nil {
		return fmt.Errorf("set email verified: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}
	return nil
}

// SetUserAdmin sets or clears the admin flag for a user identified by email.
func (db *ServerDB) SetUserAdmin(email string, isAdmin bool) error {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := db.conn.Exec(`UPDATE users SET is_admin = ? WHERE email = ?`, isAdmin, email)
	if err != nil {
		return fmt.Errorf("set user admin: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found: %s", email)
	}
	return nil
}

// IsUserAdmin returns whether the user with the given ID is an admin.
func (db *ServerDB) IsUserAdmin(userID string) (bool, error) {
	var isAdmin bool
	err := db.conn.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("user not found: %s", userID)
	}
	if err != nil {
		return false, fmt.Errorf("is user admin: %w", err)
	}
	return isAdmin, nil
}

// CountUsers returns the total number of users.
func (db *ServerDB) CountUsers() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// CountAdmins returns the number of users with admin privileges.
func (db *ServerDB) CountAdmins() (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return count, nil
}
