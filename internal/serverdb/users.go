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
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateUser inserts a new user with the given email (lowercased).
func (db *ServerDB) CreateUser(email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	id, err := generateID("u_")
	if err != nil {
		return nil, fmt.Errorf("generate user id: %w", err)
	}

	now := time.Now().UTC()
	_, err = db.conn.Exec(
		`INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, email, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	return &User{ID: id, Email: email, CreatedAt: now, UpdatedAt: now}, nil
}

// GetUserByID returns the user with the given ID, or nil if not found.
func (db *ServerDB) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, email_verified_at, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
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
		`SELECT id, email, email_verified_at, created_at, updated_at FROM users WHERE LOWER(email) = ?`, email,
	).Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
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
	rows, err := db.conn.Query(`SELECT id, email, email_verified_at, created_at, updated_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
