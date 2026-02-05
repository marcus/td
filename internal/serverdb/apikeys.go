package serverdb

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"time"
)

const (
	apiKeyPrefix = "td_live_"
	keyLength    = 32
)

var base62Chars = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

// APIKey represents a stored API key (without the plaintext secret).
type APIKey struct {
	ID         string
	UserID     string
	KeyPrefix  string
	Name       string
	Scopes     string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// GenerateAPIKey creates a new API key for the given user.
// Returns the plaintext key (shown once) and the stored APIKey record.
func (db *ServerDB) GenerateAPIKey(userID, name, scopes string, expiresAt *time.Time) (string, *APIKey, error) {
	if scopes == "" {
		scopes = "sync"
	}

	// Validate user exists
	var exists int
	if err := db.conn.QueryRow(`SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("user not found: %s", userID)
		}
		return "", nil, fmt.Errorf("check user: %w", err)
	}

	id, err := generateID("ak_")
	if err != nil {
		return "", nil, fmt.Errorf("generate api key id: %w", err)
	}

	// Generate random base62 key
	secret := make([]byte, keyLength)
	for i := range secret {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", nil, fmt.Errorf("generate random key: %w", err)
		}
		secret[i] = base62Chars[n.Int64()]
	}

	plaintext := apiKeyPrefix + string(secret)
	prefix := string(secret[:8])

	hash := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hash[:])

	now := time.Now().UTC()
	_, err = db.conn.Exec(
		`INSERT INTO api_keys (id, user_id, key_hash, key_prefix, name, scopes, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, keyHash, prefix, name, scopes, expiresAt, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert api key: %w", err)
	}

	ak := &APIKey{
		ID:        id,
		UserID:    userID,
		KeyPrefix: prefix,
		Name:      name,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	return plaintext, ak, nil
}

// VerifyAPIKey checks a plaintext key against stored hashes.
// Returns the matching APIKey and associated User, or an error.
func (db *ServerDB) VerifyAPIKey(plaintextKey string) (*APIKey, *User, error) {
	hash := sha256.Sum256([]byte(plaintextKey))
	keyHash := hex.EncodeToString(hash[:])

	ak := &APIKey{}
	u := &User{}
	err := db.conn.QueryRow(`
		SELECT ak.id, ak.user_id, ak.key_prefix, ak.name, ak.scopes, ak.expires_at, ak.last_used_at, ak.created_at,
		       u.id, u.email, u.email_verified_at, u.created_at, u.updated_at
		FROM api_keys ak
		JOIN users u ON u.id = ak.user_id
		WHERE ak.key_hash = ?
	`, keyHash).Scan(
		&ak.ID, &ak.UserID, &ak.KeyPrefix, &ak.Name, &ak.Scopes, &ak.ExpiresAt, &ak.LastUsedAt, &ak.CreatedAt,
		&u.ID, &u.Email, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		slog.Debug("api key not found", "key_hash_prefix", keyHash[:8])
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("verify api key: %w", err)
	}

	// Check expiry
	if ak.ExpiresAt != nil && ak.ExpiresAt.Before(time.Now().UTC()) {
		slog.Debug("api key expired", "key_id", ak.ID, "expires_at", ak.ExpiresAt)
		return nil, nil, nil
	}

	// Update last_used_at
	now := time.Now().UTC()
	if _, err := db.conn.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, ak.ID); err != nil {
		slog.Warn("update last_used_at", "key_id", ak.ID, "err", err)
	}
	ak.LastUsedAt = &now

	return ak, u, nil
}

// RevokeAPIKey deletes an API key, only if owned by the given user.
func (db *ServerDB) RevokeAPIKey(keyID, userID string) error {
	res, err := db.conn.Exec(`DELETE FROM api_keys WHERE id = ? AND user_id = ?`, keyID, userID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key not found or not owned by user")
	}
	return nil
}

// ListAPIKeys returns all API keys for a user (without secrets).
func (db *ServerDB) ListAPIKeys(userID string) ([]*APIKey, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, key_prefix, name, scopes, expires_at, last_used_at, created_at FROM api_keys WHERE user_id = ? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		ak := &APIKey{}
		if err := rows.Scan(&ak.ID, &ak.UserID, &ak.KeyPrefix, &ak.Name, &ak.Scopes, &ak.ExpiresAt, &ak.LastUsedAt, &ak.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, ak)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api keys: iterate: %w", err)
	}
	return keys, nil
}
