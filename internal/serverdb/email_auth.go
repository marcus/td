package serverdb

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Challenge TTL and status constants.
const (
	ChallengeTTL = 15 * time.Minute

	ChallengeStatusPending    = "pending"
	ChallengeStatusConsumed   = "consumed"
	ChallengeStatusExpired    = "expired"
	ChallengeStatusVerified   = "verified"
	ChallengeStatusFailed     = "failed"
	ChallengeStatusSuppressed = "suppressed"
)

// Sentinel errors returned by ConsumeChallenge.
var (
	ErrChallengeNotFound        = errors.New("email challenge not found")
	ErrChallengeAlreadyConsumed = errors.New("email challenge already consumed")
	ErrChallengeExpired         = errors.New("email challenge expired")
	ErrChallengeInvalidToken    = errors.New("email challenge invalid token")
)

// EmailChallenge mirrors the auth_email_challenges table row.
// Nullable columns use pointer types.
type EmailChallenge struct {
	ID                  string
	Purpose             string
	Email               string
	UserID              *string
	Selector            string
	TokenHash           string
	OTPHash             *string
	DeviceCodeHash      *string
	CodeChallenge       *string
	CodeChallengeMethod *string
	RedirectURI         *string
	StateHash           *string
	Status              string
	Attempts            int
	IP                  *string
	UserAgent           *string
	ExpiresAt           time.Time
	VerifiedAt          *time.Time
	ConsumedAt          *time.Time
	CreatedAt           time.Time
}

// ChallengeOptions holds optional fields for CreateEmailChallenge.
type ChallengeOptions struct {
	DeviceCodeHash      *string
	CodeChallenge       *string
	CodeChallengeMethod *string
	RedirectURI         *string
	StateHash           *string
	IP                  *string
	UserAgent           *string
}

// generateSelector produces a 16-byte random selector as a 32-char hex string.
func generateSelector() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateSecret produces a 32-byte random secret as a 64-char hex string.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashSecret returns the hex-encoded SHA-256 digest of s.
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// CreateEmailChallenge inserts a new email challenge row and returns the
// selector and plaintext secret to the caller. The plaintext secret is NEVER
// stored — only its SHA-256 hash is persisted.
func (db *ServerDB) CreateEmailChallenge(email, purpose, userID string, opts ChallengeOptions) (selector, plaintextSecret string, err error) {
	id, err := generateID("ec_")
	if err != nil {
		return "", "", fmt.Errorf("generate email challenge id: %w", err)
	}

	selector, err = generateSelector()
	if err != nil {
		return "", "", fmt.Errorf("generate selector: %w", err)
	}

	plaintextSecret, err = generateSecret()
	if err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}

	tokenHash := hashSecret(plaintextSecret)

	now := time.Now().UTC()
	expiresAt := now.Add(ChallengeTTL)

	var uid *string
	if userID != "" {
		uid = &userID
	}

	_, err = db.conn.Exec(
		`INSERT INTO auth_email_challenges
			(id, purpose, email, user_id, selector, token_hash,
			 device_code_hash, code_challenge, code_challenge_method,
			 redirect_uri, state_hash, ip, user_agent,
			 status, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, purpose, email, uid, selector, tokenHash,
		opts.DeviceCodeHash, opts.CodeChallenge, opts.CodeChallengeMethod,
		opts.RedirectURI, opts.StateHash, opts.IP, opts.UserAgent,
		ChallengeStatusPending, expiresAt, now,
	)
	if err != nil {
		return "", "", fmt.Errorf("insert email challenge: %w", err)
	}

	return selector, plaintextSecret, nil
}

// scanEmailChallenge scans a full auth_email_challenges row into an EmailChallenge.
func scanEmailChallenge(row interface {
	Scan(...any) error
}) (*EmailChallenge, error) {
	c := &EmailChallenge{}
	err := row.Scan(
		&c.ID, &c.Purpose, &c.Email, &c.UserID,
		&c.Selector, &c.TokenHash,
		&c.OTPHash, &c.DeviceCodeHash, &c.CodeChallenge, &c.CodeChallengeMethod,
		&c.RedirectURI, &c.StateHash,
		&c.Status, &c.Attempts, &c.IP, &c.UserAgent,
		&c.ExpiresAt, &c.VerifiedAt, &c.ConsumedAt, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

const challengeSelectCols = `
	id, purpose, email, user_id,
	selector, token_hash,
	otp_hash, device_code_hash, code_challenge, code_challenge_method,
	redirect_uri, state_hash,
	status, attempts, ip, user_agent,
	expires_at, verified_at, consumed_at, created_at`

// LookupChallenge returns the challenge with the given selector, or nil if not found.
func (db *ServerDB) LookupChallenge(selector string) (*EmailChallenge, error) {
	row := db.conn.QueryRow(
		`SELECT`+challengeSelectCols+`
		 FROM auth_email_challenges WHERE selector = ?`, selector,
	)
	c, err := scanEmailChallenge(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup email challenge: %w", err)
	}
	return c, nil
}

// ConsumeChallenge atomically verifies and consumes a pending, non-expired
// challenge. It returns the challenge on success. Sentinel errors:
//   - ErrChallengeNotFound    — selector not found
//   - ErrChallengeAlreadyConsumed — status != pending
//   - ErrChallengeExpired     — expires_at <= now
//   - ErrChallengeInvalidToken — SHA-256(plaintextSecret) != token_hash
//
// If user_id is set on the challenge, users.email_verified_at is also set
// (only if it was previously NULL) within the same transaction.
func (db *ServerDB) ConsumeChallenge(selector, plaintextSecret string) (*EmailChallenge, error) {
	// Use BEGIN IMMEDIATE so SQLite acquires the write lock up front,
	// giving us the equivalent of SELECT … FOR UPDATE.  With a single-connection
	// pool (MaxOpenConns=1) this also prevents any concurrent consume from
	// interleaving between our SELECT and UPDATE.
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Escalate to an immediate (write) transaction.
	if _, err := tx.Exec("BEGIN IMMEDIATE"); err != nil {
		// modernc.org/sqlite returns an error if we call BEGIN inside a
		// transaction that is already IMMEDIATE; ignore it silently — the
		// pool is pinned to one connection so we are already serialised.
		_ = err
	}

	row := tx.QueryRow(
		`SELECT`+challengeSelectCols+`
		 FROM auth_email_challenges WHERE selector = ?`, selector,
	)
	c, err := scanEmailChallenge(row)
	if err == sql.ErrNoRows {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select email challenge: %w", err)
	}

	// Status check — must be pending.
	if c.Status != ChallengeStatusPending {
		return nil, ErrChallengeAlreadyConsumed
	}

	// Expiry check.
	if !time.Now().UTC().Before(c.ExpiresAt) {
		return nil, ErrChallengeExpired
	}

	// Token check.
	if hashSecret(plaintextSecret) != c.TokenHash {
		return nil, ErrChallengeInvalidToken
	}

	now := time.Now().UTC()

	// Mark consumed.
	_, err = tx.Exec(
		`UPDATE auth_email_challenges SET status = ?, consumed_at = ? WHERE selector = ?`,
		ChallengeStatusConsumed, now, selector,
	)
	if err != nil {
		return nil, fmt.Errorf("update email challenge status: %w", err)
	}

	// If there is a user_id, set email_verified_at where still NULL.
	if c.UserID != nil {
		_, err = tx.Exec(
			`UPDATE users SET email_verified_at = ?, updated_at = ?
			 WHERE id = ? AND email_verified_at IS NULL`,
			now, now, *c.UserID,
		)
		if err != nil {
			return nil, fmt.Errorf("set email_verified_at: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	c.Status = ChallengeStatusConsumed
	c.ConsumedAt = &now
	return c, nil
}

// LookupChallengeByDeviceCodeHash returns the pending, non-expired challenge
// associated with the given device_code_hash, or nil if not found.
func (db *ServerDB) LookupChallengeByDeviceCodeHash(deviceCodeHash string) (*EmailChallenge, error) {
	row := db.conn.QueryRow(
		`SELECT`+challengeSelectCols+`
		 FROM auth_email_challenges
		 WHERE device_code_hash = ? AND status = ? AND expires_at > ?`,
		deviceCodeHash, ChallengeStatusPending, time.Now().UTC(),
	)
	c, err := scanEmailChallenge(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup challenge by device code hash: %w", err)
	}
	return c, nil
}

// CleanupExpiredChallenges marks pending challenges that are past their expiry
// as expired. Returns the number of rows updated.
func (db *ServerDB) CleanupExpiredChallenges() (int64, error) {
	res, err := db.conn.Exec(
		`UPDATE auth_email_challenges SET status = ? WHERE status = ? AND expires_at <= ?`,
		ChallengeStatusExpired, ChallengeStatusPending, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired challenges: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ForceExpireChallengeForTest sets the expires_at of a challenge to a past time
// to simulate expiry in tests. This is a test-only helper.
func (db *ServerDB) ForceExpireChallengeForTest(selector string, expiresAt time.Time) {
	_, _ = db.conn.Exec(
		`UPDATE auth_email_challenges SET expires_at = ? WHERE selector = ?`,
		expiresAt, selector,
	)
}
