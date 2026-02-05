package serverdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

// AuthRequest represents a device authorization request.
type AuthRequest struct {
	ID         string
	Email      string
	DeviceCode string
	UserCode   string
	Status     string
	UserID     *string
	APIKeyID   *string
	ExpiresAt  time.Time
	VerifiedAt *time.Time
	CreatedAt  time.Time
}

const (
	AuthStatusPending  = "pending"
	AuthStatusVerified = "verified"
	AuthStatusExpired  = "expired"
	AuthStatusUsed     = "used"
	AuthRequestTTL     = 15 * time.Minute
	PollInterval       = 5
)

// userCodeChars excludes ambiguous characters (0, 1, I, L, O).
var userCodeChars = []byte("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

// generateUserCode creates a 6-character user code from the allowed charset.
func generateUserCode() (string, error) {
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeChars))))
		if err != nil {
			return "", err
		}
		code[i] = userCodeChars[n.Int64()]
	}
	return string(code), nil
}

// generateDeviceCode creates a 40-hex-character device code.
func generateDeviceCode() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateAuthRequest creates a new device auth request for the given email.
func (db *ServerDB) CreateAuthRequest(email string) (*AuthRequest, error) {
	id, err := generateID("ar_")
	if err != nil {
		return nil, fmt.Errorf("generate auth request id: %w", err)
	}

	deviceCode, err := generateDeviceCode()
	if err != nil {
		return nil, fmt.Errorf("generate device code: %w", err)
	}

	userCode, err := generateUserCode()
	if err != nil {
		return nil, fmt.Errorf("generate user code: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(AuthRequestTTL)

	_, err = db.conn.Exec(
		`INSERT INTO auth_requests (id, email, device_code, user_code, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, email, deviceCode, userCode, AuthStatusPending, expiresAt, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert auth request: %w", err)
	}

	return &AuthRequest{
		ID:         id,
		Email:      email,
		DeviceCode: deviceCode,
		UserCode:   userCode,
		Status:     AuthStatusPending,
		ExpiresAt:  expiresAt,
		CreatedAt:  now,
	}, nil
}

// GetAuthRequestByDeviceCode returns the auth request with the given device code, or nil.
func (db *ServerDB) GetAuthRequestByDeviceCode(deviceCode string) (*AuthRequest, error) {
	ar := &AuthRequest{}
	err := db.conn.QueryRow(
		`SELECT id, email, device_code, user_code, status, user_id, api_key_id, expires_at, verified_at, created_at
		 FROM auth_requests WHERE device_code = ?`, deviceCode,
	).Scan(&ar.ID, &ar.Email, &ar.DeviceCode, &ar.UserCode, &ar.Status,
		&ar.UserID, &ar.APIKeyID, &ar.ExpiresAt, &ar.VerifiedAt, &ar.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auth request by device code: %w", err)
	}
	return ar, nil
}

// GetAuthRequestByUserCode returns the pending, non-expired auth request with the given user code, or nil.
func (db *ServerDB) GetAuthRequestByUserCode(userCode string) (*AuthRequest, error) {
	ar := &AuthRequest{}
	err := db.conn.QueryRow(
		`SELECT id, email, device_code, user_code, status, user_id, api_key_id, expires_at, verified_at, created_at
		 FROM auth_requests WHERE user_code = ? AND status = ? AND expires_at > ?`,
		userCode, AuthStatusPending, time.Now().UTC(),
	).Scan(&ar.ID, &ar.Email, &ar.DeviceCode, &ar.UserCode, &ar.Status,
		&ar.UserID, &ar.APIKeyID, &ar.ExpiresAt, &ar.VerifiedAt, &ar.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auth request by user code: %w", err)
	}
	return ar, nil
}

// VerifyAuthRequest marks a pending auth request as verified with the given user ID.
func (db *ServerDB) VerifyAuthRequest(userCode, userID string) error {
	now := time.Now().UTC()
	res, err := db.conn.Exec(
		`UPDATE auth_requests SET status = ?, user_id = ?, verified_at = ?
		 WHERE user_code = ? AND status = ? AND expires_at > ?`,
		AuthStatusVerified, userID, now, userCode, AuthStatusPending, now,
	)
	if err != nil {
		return fmt.Errorf("verify auth request: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("auth request not found, already verified, or expired")
	}
	return nil
}

// CompleteAuthRequest transitions a verified auth request to used and returns it.
// Returns nil if the request is not in verified status.
func (db *ServerDB) CompleteAuthRequest(deviceCode string) (*AuthRequest, error) {
	res, err := db.conn.Exec(
		`UPDATE auth_requests SET status = ? WHERE device_code = ? AND status = ?`,
		AuthStatusUsed, deviceCode, AuthStatusVerified,
	)
	if err != nil {
		return nil, fmt.Errorf("complete auth request: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	return db.GetAuthRequestByDeviceCode(deviceCode)
}

// SetAuthRequestAPIKey sets the API key ID on an auth request.
func (db *ServerDB) SetAuthRequestAPIKey(id, apiKeyID string) error {
	_, err := db.conn.Exec(
		`UPDATE auth_requests SET api_key_id = ? WHERE id = ?`,
		apiKeyID, id,
	)
	if err != nil {
		return fmt.Errorf("set auth request api key: %w", err)
	}
	return nil
}

// CleanupExpiredAuthRequests marks pending auth requests past their expiry as expired.
func (db *ServerDB) CleanupExpiredAuthRequests() (int64, error) {
	res, err := db.conn.Exec(
		`UPDATE auth_requests SET status = ? WHERE status = ? AND expires_at <= ?`,
		AuthStatusExpired, AuthStatusPending, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired auth requests: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
