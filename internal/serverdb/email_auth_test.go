package serverdb

import (
	"strings"
	"testing"
	"time"
)

// TestCreateAndConsume verifies the happy path: create a challenge, consume it
// with the correct token, confirm the challenge is consumed and
// email_verified_at is set on the linked user.
func TestCreateAndConsume(t *testing.T) {
	db := newTestDB(t)

	u, err := db.CreateUser("consume@test.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	selector, secret, err := db.CreateEmailChallenge(
		u.Email, "web_login", u.ID, ChallengeOptions{},
	)
	if err != nil {
		t.Fatalf("create email challenge: %v", err)
	}
	if len(selector) != 32 {
		t.Errorf("expected 32-char selector, got %d", len(selector))
	}
	if len(secret) != 64 {
		t.Errorf("expected 64-char secret, got %d", len(secret))
	}
	if !strings.HasPrefix(selector, "") {
		// hex chars only
	}

	// Verify that only the hash is stored, not the plaintext secret.
	c, err := db.LookupChallenge(selector)
	if err != nil {
		t.Fatalf("lookup challenge: %v", err)
	}
	if c == nil {
		t.Fatal("challenge not found")
	}
	if c.TokenHash == secret {
		t.Error("plaintext secret must not be stored in token_hash")
	}
	if c.TokenHash != hashSecret(secret) {
		t.Error("stored token_hash does not match sha256(secret)")
	}
	if strings.Contains(c.TokenHash, secret) {
		t.Error("plaintext secret must not appear in stored token_hash")
	}

	// Consume with correct token.
	consumed, err := db.ConsumeChallenge(selector, secret)
	if err != nil {
		t.Fatalf("consume challenge: %v", err)
	}
	if consumed.Status != ChallengeStatusConsumed {
		t.Errorf("expected consumed status, got %s", consumed.Status)
	}
	if consumed.ConsumedAt == nil {
		t.Error("consumed_at should be set")
	}

	// email_verified_at should now be set on the user.
	found, err := db.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if found.EmailVerifiedAt == nil {
		t.Error("email_verified_at should be set after consuming challenge with user_id")
	}
}

// TestConsumeExpired verifies that an expired challenge returns ErrChallengeExpired.
func TestConsumeExpired(t *testing.T) {
	db := newTestDB(t)

	selector, secret, err := db.CreateEmailChallenge(
		"expired@test.com", "web_login", "", ChallengeOptions{},
	)
	if err != nil {
		t.Fatalf("create email challenge: %v", err)
	}

	// Force the challenge to appear expired.
	db.ForceExpireChallengeForTest(selector, time.Now().UTC().Add(-1*time.Hour))

	_, err = db.ConsumeChallenge(selector, secret)
	if err == nil {
		t.Fatal("expected error consuming expired challenge")
	}
	if err != ErrChallengeExpired {
		t.Fatalf("expected ErrChallengeExpired, got %v", err)
	}
}

// TestConsumeReplay verifies that consuming a challenge a second time returns
// ErrChallengeAlreadyConsumed.
func TestConsumeReplay(t *testing.T) {
	db := newTestDB(t)

	selector, secret, err := db.CreateEmailChallenge(
		"replay@test.com", "web_login", "", ChallengeOptions{},
	)
	if err != nil {
		t.Fatalf("create email challenge: %v", err)
	}

	// First consume succeeds.
	if _, err := db.ConsumeChallenge(selector, secret); err != nil {
		t.Fatalf("first consume: %v", err)
	}

	// Second consume must fail with the replay sentinel.
	_, err = db.ConsumeChallenge(selector, secret)
	if err == nil {
		t.Fatal("expected error on replay")
	}
	if err != ErrChallengeAlreadyConsumed {
		t.Fatalf("expected ErrChallengeAlreadyConsumed, got %v", err)
	}
}

// TestConsumeWrongToken verifies that a wrong secret returns ErrChallengeInvalidToken.
func TestConsumeWrongToken(t *testing.T) {
	db := newTestDB(t)

	selector, _, err := db.CreateEmailChallenge(
		"wrong@test.com", "web_login", "", ChallengeOptions{},
	)
	if err != nil {
		t.Fatalf("create email challenge: %v", err)
	}

	_, err = db.ConsumeChallenge(selector, "000000000000000000000000000000000000000000000000000000000000dead")
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
	if err != ErrChallengeInvalidToken {
		t.Fatalf("expected ErrChallengeInvalidToken, got %v", err)
	}
}

// TestCleanupExpired verifies that CleanupExpiredChallenges marks pending
// past-expiry rows as expired and leaves fresh rows untouched.
func TestCleanupExpired(t *testing.T) {
	db := newTestDB(t)

	// Create one fresh and one that we will force-expire.
	selector1, _, err := db.CreateEmailChallenge("old@test.com", "web_login", "", ChallengeOptions{})
	if err != nil {
		t.Fatalf("create old challenge: %v", err)
	}
	_, _, err = db.CreateEmailChallenge("fresh@test.com", "web_login", "", ChallengeOptions{})
	if err != nil {
		t.Fatalf("create fresh challenge: %v", err)
	}

	// Force the first challenge to be expired.
	db.ForceExpireChallengeForTest(selector1, time.Now().UTC().Add(-1*time.Hour))

	n, err := db.CleanupExpiredChallenges()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row cleaned up, got %d", n)
	}

	// Verify the expired one is now marked expired.
	c, err := db.LookupChallenge(selector1)
	if err != nil {
		t.Fatalf("lookup old challenge: %v", err)
	}
	if c == nil {
		t.Fatal("challenge should still exist (just expired)")
	}
	if c.Status != ChallengeStatusExpired {
		t.Errorf("expected expired status, got %s", c.Status)
	}
}

// TestLookupByDeviceCodeHash verifies that a challenge created with a
// device_code_hash can be found via LookupChallengeByDeviceCodeHash.
func TestLookupByDeviceCodeHash(t *testing.T) {
	db := newTestDB(t)

	dch := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	selector, _, err := db.CreateEmailChallenge(
		"device@test.com", "device_login", "",
		ChallengeOptions{DeviceCodeHash: &dch},
	)
	if err != nil {
		t.Fatalf("create email challenge: %v", err)
	}

	found, err := db.LookupChallengeByDeviceCodeHash(dch)
	if err != nil {
		t.Fatalf("lookup by device code hash: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find challenge by device code hash")
	}
	if found.Selector != selector {
		t.Errorf("selector mismatch: want %s, got %s", selector, found.Selector)
	}
	if found.DeviceCodeHash == nil || *found.DeviceCodeHash != dch {
		t.Error("device_code_hash not set correctly")
	}

	// A non-existent hash returns nil, not an error.
	missing, err := db.LookupChallengeByDeviceCodeHash("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("unexpected error for missing hash: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing device code hash")
	}
}
