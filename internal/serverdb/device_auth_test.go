package serverdb

import (
	"strings"
	"testing"
	"time"
)

func TestCreateAuthRequest(t *testing.T) {
	db := newTestDB(t)
	ar, err := db.CreateAuthRequest("test@example.com")
	if err != nil {
		t.Fatalf("create auth request: %v", err)
	}
	if !strings.HasPrefix(ar.ID, "ar_") {
		t.Errorf("unexpected id prefix: %s", ar.ID)
	}
	if len(ar.DeviceCode) != 40 {
		t.Errorf("expected 40-char device code, got %d", len(ar.DeviceCode))
	}
	if len(ar.UserCode) != 6 {
		t.Errorf("expected 6-char user code, got %d", len(ar.UserCode))
	}
	if ar.Status != AuthStatusPending {
		t.Errorf("expected pending status, got %s", ar.Status)
	}
	if ar.ExpiresAt.Before(time.Now().UTC()) {
		t.Error("expires_at should be in the future")
	}
}

func TestGetAuthRequestByDeviceCode(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")

	found, err := db.GetAuthRequestByDeviceCode(ar.DeviceCode)
	if err != nil {
		t.Fatalf("get by device code: %v", err)
	}
	if found == nil || found.ID != ar.ID {
		t.Fatal("auth request not found by device code")
	}
}

func TestGetAuthRequestByDeviceCodeNotFound(t *testing.T) {
	db := newTestDB(t)
	found, err := db.GetAuthRequestByDeviceCode("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Fatal("expected nil for missing device code")
	}
}

func TestGetAuthRequestByUserCode(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")

	found, err := db.GetAuthRequestByUserCode(ar.UserCode)
	if err != nil {
		t.Fatalf("get by user code: %v", err)
	}
	if found == nil || found.ID != ar.ID {
		t.Fatal("auth request not found by user code")
	}
}

func TestGetAuthRequestByUserCodeNotFound(t *testing.T) {
	db := newTestDB(t)
	found, err := db.GetAuthRequestByUserCode("ZZZZZZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Fatal("expected nil for missing user code")
	}
}

func TestGetAuthRequestByUserCodeExpired(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")

	// Force expiry to the past
	db.conn.Exec(`UPDATE auth_requests SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-1*time.Hour), ar.ID)

	found, err := db.GetAuthRequestByUserCode(ar.UserCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Fatal("expected nil for expired auth request")
	}
}

func TestVerifyAuthRequest(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")
	u, _ := db.CreateUser("test@example.com")

	if err := db.VerifyAuthRequest(ar.UserCode, u.ID); err != nil {
		t.Fatalf("verify: %v", err)
	}

	found, _ := db.GetAuthRequestByDeviceCode(ar.DeviceCode)
	if found.Status != AuthStatusVerified {
		t.Errorf("expected verified, got %s", found.Status)
	}
	if found.UserID == nil || *found.UserID != u.ID {
		t.Error("user_id not set correctly")
	}
	if found.VerifiedAt == nil {
		t.Error("verified_at should be set")
	}
}

func TestVerifyAuthRequestAlreadyVerified(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")
	u, _ := db.CreateUser("test@example.com")

	db.VerifyAuthRequest(ar.UserCode, u.ID)

	// Second verify should fail
	err := db.VerifyAuthRequest(ar.UserCode, u.ID)
	if err == nil {
		t.Fatal("expected error for already-verified request")
	}
}

func TestVerifyAuthRequestExpired(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")
	u, _ := db.CreateUser("test@example.com")

	// Force expiry
	db.conn.Exec(`UPDATE auth_requests SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-1*time.Hour), ar.ID)

	err := db.VerifyAuthRequest(ar.UserCode, u.ID)
	if err == nil {
		t.Fatal("expected error for expired request")
	}
}

func TestCompleteAuthRequestVerified(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")
	u, _ := db.CreateUser("test@example.com")

	db.VerifyAuthRequest(ar.UserCode, u.ID)

	completed, err := db.CompleteAuthRequest(ar.DeviceCode)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed == nil {
		t.Fatal("expected non-nil completed request")
	}
	if completed.Status != AuthStatusUsed {
		t.Errorf("expected used status, got %s", completed.Status)
	}
}

func TestCompleteAuthRequestPendingReturnsNil(t *testing.T) {
	db := newTestDB(t)
	ar, _ := db.CreateAuthRequest("test@example.com")

	completed, err := db.CompleteAuthRequest(ar.DeviceCode)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed != nil {
		t.Fatal("expected nil for pending request")
	}
}

func TestCleanupExpiredAuthRequests(t *testing.T) {
	db := newTestDB(t)
	ar1, _ := db.CreateAuthRequest("expired1@example.com")
	db.CreateAuthRequest("fresh@example.com")

	// Force ar1 to be expired
	db.conn.Exec(`UPDATE auth_requests SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-1*time.Hour), ar1.ID)

	n, err := db.CleanupExpiredAuthRequests()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 cleaned up, got %d", n)
	}

	// Verify the expired one is now marked expired
	found, _ := db.GetAuthRequestByDeviceCode(ar1.DeviceCode)
	if found.Status != AuthStatusExpired {
		t.Errorf("expected expired status, got %s", found.Status)
	}
}
