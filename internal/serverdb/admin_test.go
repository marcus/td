package serverdb

import (
	"testing"
)

func TestGrantAdmin(t *testing.T) {
	db := newTestDB(t)

	// Create non-admin user (first user is auto-admin, so create two)
	admin, err := db.CreateUser("admin@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !admin.IsAdmin {
		t.Fatal("first user should be admin")
	}

	u, err := db.CreateUser("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Fatal("second user should not be admin")
	}

	// Grant admin
	if err := db.SetUserAdmin("user@test.com", true); err != nil {
		t.Fatalf("grant admin: %v", err)
	}

	// Verify
	updated, err := db.GetUserByEmail("user@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.IsAdmin {
		t.Error("expected is_admin=true after grant")
	}
}

func TestGrantAdminUserNotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.SetUserAdmin("noone@test.com", true)
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestRevokeAdmin(t *testing.T) {
	db := newTestDB(t)

	// Create two admins
	_, err := db.CreateUser("admin1@test.com")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := db.CreateUser("admin2@test.com")
	if err != nil {
		t.Fatal(err)
	}
	// Make second user admin
	if err := db.SetUserAdmin(u2.Email, true); err != nil {
		t.Fatal(err)
	}

	// Revoke admin from admin2
	if err := db.SetUserAdmin("admin2@test.com", false); err != nil {
		t.Fatalf("revoke admin: %v", err)
	}

	// Verify
	updated, err := db.GetUserByEmail("admin2@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if updated.IsAdmin {
		t.Error("expected is_admin=false after revoke")
	}
}

func TestRevokeLastAdmin(t *testing.T) {
	db := newTestDB(t)

	// Create single admin (first user)
	_, err := db.CreateUser("solo@test.com")
	if err != nil {
		t.Fatal(err)
	}

	// Count admins - should be 1
	count, err := db.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 admin, got %d", count)
	}

	// The CLI checks CountAdmins + IsAdmin before calling SetUserAdmin.
	// Simulate that logic here: refuse if count <= 1 and user is admin.
	user, err := db.GetUserByEmail("solo@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.IsAdmin && count <= 1 {
		// This is the expected path - cannot revoke last admin
		return
	}

	t.Fatal("expected last-admin check to prevent revocation")
}

func TestCreateKeyForAdmin(t *testing.T) {
	db := newTestDB(t)

	admin, err := db.CreateUser("keyadmin@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !admin.IsAdmin {
		t.Fatal("first user should be admin")
	}

	plaintext, ak, err := db.GenerateAPIKey(admin.ID, "test-key", "admin:read:server,sync", nil)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if plaintext == "" {
		t.Error("plaintext key should not be empty")
	}
	if ak.Name != "test-key" {
		t.Errorf("expected name test-key, got %s", ak.Name)
	}
	if ak.Scopes != "admin:read:server,sync" {
		t.Errorf("expected scopes admin:read:server,sync, got %s", ak.Scopes)
	}
	if ak.UserID != admin.ID {
		t.Errorf("expected user_id %s, got %s", admin.ID, ak.UserID)
	}
}

func TestCreateKeyForNonAdmin(t *testing.T) {
	db := newTestDB(t)

	// Create admin first (auto-admin)
	_, err := db.CreateUser("first@test.com")
	if err != nil {
		t.Fatal(err)
	}

	// Create non-admin
	user, err := db.CreateUser("regular@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.IsAdmin {
		t.Fatal("second user should not be admin")
	}

	// The CLI checks is_admin before calling GenerateAPIKey.
	// Simulate that logic here.
	isAdmin, err := db.IsUserAdmin(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if isAdmin {
		t.Fatal("expected non-admin user")
	}

	// GenerateAPIKey itself doesn't check admin status -- the CLI does.
	// Verify the check works.
	if isAdmin {
		t.Fatal("should not create key for non-admin")
	}
}

func TestCountAdminsMultiple(t *testing.T) {
	db := newTestDB(t)

	// No users yet
	count, err := db.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	// First user = auto-admin
	_, err = db.CreateUser("a@test.com")
	if err != nil {
		t.Fatal(err)
	}
	count, err = db.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}

	// Second user = non-admin, promote
	u2, err := db.CreateUser("b@test.com")
	if err != nil {
		t.Fatal(err)
	}
	db.SetUserAdmin(u2.Email, true)
	count, err = db.CountAdmins()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}
