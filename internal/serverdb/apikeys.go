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
	apiKeyPrefix           = "td_live_"
	impersonationKeyPrefix = "td_ipk_"
	keyLength              = 32
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
		       u.id, u.email, u.email_verified_at, u.is_admin, u.created_at, u.updated_at
		FROM api_keys ak
		JOIN users u ON u.id = ak.user_id
		WHERE ak.key_hash = ?
	`, keyHash).Scan(
		&ak.ID, &ak.UserID, &ak.KeyPrefix, &ak.Name, &ak.Scopes, &ak.ExpiresAt, &ak.LastUsedAt, &ak.CreatedAt,
		&u.ID, &u.Email, &u.EmailVerifiedAt, &u.IsAdmin, &u.CreatedAt, &u.UpdatedAt,
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

// GenerateImpersonationKey creates a new short-lived ephemeral "view-as"
// key for the target user. Uses the td_ipk_ prefix and stores the row with
// scopes = "impersonation:read". Any existing unexpired td_ipk_ keys for the
// same target user are revoked first ("last one wins").
func (db *ServerDB) GenerateImpersonationKey(targetUserID string, ttl time.Duration) (string, *APIKey, error) {
	// Validate user exists
	var exists int
	if err := db.conn.QueryRow(`SELECT 1 FROM users WHERE id = ?`, targetUserID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("user not found: %s", targetUserID)
		}
		return "", nil, fmt.Errorf("check user: %w", err)
	}

	now := time.Now().UTC()

	// Revoke any existing unexpired td_ipk_ keys for this user.
	if _, err := db.conn.Exec(
		`DELETE FROM api_keys WHERE user_id = ? AND key_prefix LIKE 'ipk_%' AND (expires_at IS NULL OR expires_at > ?)`,
		targetUserID, now,
	); err != nil {
		return "", nil, fmt.Errorf("revoke existing impersonation keys: %w", err)
	}

	id, err := generateID("ak_")
	if err != nil {
		return "", nil, fmt.Errorf("generate api key id: %w", err)
	}

	secret := make([]byte, keyLength)
	for i := range secret {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Chars))))
		if err != nil {
			return "", nil, fmt.Errorf("generate random key: %w", err)
		}
		secret[i] = base62Chars[n.Int64()]
	}

	plaintext := impersonationKeyPrefix + string(secret)
	// key_prefix is stored as "ipk_" + first 4 chars of secret to make it
	// identifiable in queries (see DELETE above) while remaining opaque.
	prefix := "ipk_" + string(secret[:4])

	hash := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(hash[:])

	expiresAt := now.Add(ttl)

	_, err = db.conn.Exec(
		`INSERT INTO api_keys (id, user_id, key_hash, key_prefix, name, scopes, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, targetUserID, keyHash, prefix, "view-as", "impersonation:read", expiresAt, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("insert impersonation key: %w", err)
	}

	ak := &APIKey{
		ID:        id,
		UserID:    targetUserID,
		KeyPrefix: prefix,
		Name:      "view-as",
		Scopes:    "impersonation:read",
		ExpiresAt: &expiresAt,
		CreatedAt: now,
	}
	return plaintext, ak, nil
}

// ExtendImpersonationKey extends the expires_at of an impersonation key by
// renewTTL, capped at cap from the key's created_at. Also updates last_used_at
// to now. Returns the new expires_at. No-ops silently (logs a warning) on
// failure — callers should not fail the request over a stats update.
func (db *ServerDB) ExtendImpersonationKey(keyID string, renewTTL, maxTTL time.Duration) {
	now := time.Now().UTC()
	var createdAt time.Time
	if err := db.conn.QueryRow(
		`SELECT created_at FROM api_keys WHERE id = ?`, keyID,
	).Scan(&createdAt); err != nil {
		slog.Warn("extend impersonation key: lookup", "key_id", keyID, "err", err)
		return
	}
	renewed := now.Add(renewTTL)
	cap := createdAt.Add(maxTTL)
	if renewed.After(cap) {
		renewed = cap
	}
	if _, err := db.conn.Exec(
		`UPDATE api_keys SET expires_at = ?, last_used_at = ? WHERE id = ?`,
		renewed, now, keyID,
	); err != nil {
		slog.Warn("extend impersonation key: update", "key_id", keyID, "err", err)
	}
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
