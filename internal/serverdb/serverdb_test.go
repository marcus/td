package serverdb

import (
	"strings"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *ServerDB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- User tests ---

func TestCreateUser(t *testing.T) {
	db := newTestDB(t)
	u, err := db.CreateUser("Alice@Example.COM")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email not lowercased: %s", u.Email)
	}
	if !strings.HasPrefix(u.ID, "u_") {
		t.Errorf("unexpected id prefix: %s", u.ID)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	db := newTestDB(t)
	_, err := db.CreateUser("dup@test.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateUser("dup@test.com")
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
}

func TestCreateUserEmptyEmail(t *testing.T) {
	db := newTestDB(t)
	_, err := db.CreateUser("")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestGetUserByID(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("test@test.com")
	found, err := db.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != u.ID {
		t.Fatal("user not found by id")
	}
}

func TestGetUserByIDNotFound(t *testing.T) {
	db := newTestDB(t)
	found, err := db.GetUserByID("u_nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if found != nil {
		t.Fatal("expected nil for missing user")
	}
}

func TestGetUserByEmail(t *testing.T) {
	db := newTestDB(t)
	db.CreateUser("find@test.com")
	found, err := db.GetUserByEmail("FIND@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.Email != "find@test.com" {
		t.Fatal("user not found by email")
	}
}

func TestListUsers(t *testing.T) {
	db := newTestDB(t)
	db.CreateUser("a@test.com")
	db.CreateUser("b@test.com")
	users, err := db.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestSetEmailVerified(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("verify@test.com")
	if err := db.SetEmailVerified(u.ID); err != nil {
		t.Fatal(err)
	}
	found, _ := db.GetUserByID(u.ID)
	if found.EmailVerifiedAt == nil {
		t.Fatal("email_verified_at should be set")
	}
}

func TestSetEmailVerifiedNotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.SetEmailVerified("u_nonexistent")
	if err == nil {
		t.Fatal("expected error for missing user")
	}
}

// --- API Key tests ---

func TestGenerateAndVerifyAPIKey(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("key@test.com")

	plaintext, ak, err := db.GenerateAPIKey(u.ID, "test key", "sync", nil)
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if !strings.HasPrefix(plaintext, "td_live_") {
		t.Errorf("unexpected key prefix: %s", plaintext[:10])
	}
	if !strings.HasPrefix(ak.ID, "ak_") {
		t.Errorf("unexpected id prefix: %s", ak.ID)
	}

	// Verify
	verifiedKey, verifiedUser, err := db.VerifyAPIKey(plaintext)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verifiedKey.ID != ak.ID {
		t.Error("key ID mismatch")
	}
	if verifiedUser.ID != u.ID {
		t.Error("user ID mismatch")
	}
}

func TestVerifyAPIKeyInvalid(t *testing.T) {
	db := newTestDB(t)
	ak, u, err := db.VerifyAPIKey("td_live_invalidkeyhere")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ak != nil || u != nil {
		t.Fatal("expected nil result for invalid key")
	}
}

func TestVerifyAPIKeyExpired(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("expired@test.com")
	past := time.Now().Add(-24 * time.Hour)
	plaintext, _, err := db.GenerateAPIKey(u.ID, "expired", "sync", &past)
	if err != nil {
		t.Fatal(err)
	}
	ak, verifiedUser, err := db.VerifyAPIKey(plaintext)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ak != nil || verifiedUser != nil {
		t.Fatal("expected nil result for expired key")
	}
}

func TestRevokeAPIKey(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("revoke@test.com")
	_, ak, _ := db.GenerateAPIKey(u.ID, "to-revoke", "sync", nil)

	if err := db.RevokeAPIKey(ak.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	keys, _ := db.ListAPIKeys(u.ID)
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys after revoke, got %d", len(keys))
	}
}

func TestRevokeAPIKeyWrongUser(t *testing.T) {
	db := newTestDB(t)
	u1, _ := db.CreateUser("owner@test.com")
	u2, _ := db.CreateUser("other@test.com")
	_, ak, _ := db.GenerateAPIKey(u1.ID, "mine", "sync", nil)

	err := db.RevokeAPIKey(ak.ID, u2.ID)
	if err == nil {
		t.Fatal("expected error revoking another user's key")
	}
}

func TestListAPIKeys(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("list@test.com")
	db.GenerateAPIKey(u.ID, "key1", "sync", nil)
	db.GenerateAPIKey(u.ID, "key2", "sync", nil)

	keys, err := db.ListAPIKeys(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

// --- Project tests ---

func TestCreateProject(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("proj@test.com")

	p, err := db.CreateProject("my project", "desc", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.ID, "p_") {
		t.Errorf("unexpected project id prefix: %s", p.ID)
	}

	// Owner membership should exist
	m, _ := db.GetMembership(p.ID, u.ID)
	if m == nil || m.Role != RoleOwner {
		t.Fatal("owner membership not created")
	}
}

func TestCreateProjectEmptyName(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("proj2@test.com")
	_, err := db.CreateProject("", "desc", u.ID)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestGetProject(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("get@test.com")
	p, _ := db.CreateProject("proj", "desc", u.ID)

	found, err := db.GetProject(p.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.ID != p.ID {
		t.Fatal("project not found")
	}
}

func TestGetProjectNotFound(t *testing.T) {
	db := newTestDB(t)
	found, err := db.GetProject("p_nope", false)
	if err != nil {
		t.Fatal(err)
	}
	if found != nil {
		t.Fatal("expected nil")
	}
}

func TestListProjectsForUser(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("list@test.com")
	db.CreateProject("p1", "", u.ID)
	db.CreateProject("p2", "", u.ID)

	projects, err := db.ListProjectsForUser(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
}

func TestUpdateProject(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("upd@test.com")
	p, _ := db.CreateProject("old", "old desc", u.ID)

	updated, err := db.UpdateProject(p.ID, "new", "new desc")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "new" || updated.Description != "new desc" {
		t.Fatal("project not updated")
	}
}

func TestSoftDeleteProject(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("del@test.com")
	p, _ := db.CreateProject("doomed", "", u.ID)

	if err := db.SoftDeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}

	// Should not appear in normal lookup
	found, _ := db.GetProject(p.ID, false)
	if found != nil {
		t.Fatal("soft-deleted project should not appear")
	}

	// Should appear with includeSoftDeleted
	found, _ = db.GetProject(p.ID, true)
	if found == nil {
		t.Fatal("soft-deleted project should appear with flag")
	}
}

// --- Membership tests ---

func TestAddAndListMembers(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner@test.com")
	writer, _ := db.CreateUser("writer@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	_, err := db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	members, _ := db.ListMembers(p.ID)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestAddMemberInvalidRole(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("inv@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)
	u2, _ := db.CreateUser("inv2@test.com")

	_, err := db.AddMember(p.ID, u2.ID, "admin", u.ID)
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestUpdateMemberRole(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	reader, _ := db.CreateUser("r@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)
	db.AddMember(p.ID, reader.ID, RoleReader, owner.ID)

	if err := db.UpdateMemberRole(p.ID, reader.ID, RoleWriter); err != nil {
		t.Fatal(err)
	}
	m, _ := db.GetMembership(p.ID, reader.ID)
	if m.Role != RoleWriter {
		t.Fatalf("expected writer, got %s", m.Role)
	}
}

func TestRemoveMember(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)
	db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)

	if err := db.RemoveMember(p.ID, writer.ID); err != nil {
		t.Fatal(err)
	}
	m, _ := db.GetMembership(p.ID, writer.ID)
	if m != nil {
		t.Fatal("membership should be removed")
	}
}

func TestRemoveLastOwner(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("solo@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	err := db.RemoveMember(p.ID, owner.ID)
	if err == nil {
		t.Fatal("expected error removing last owner")
	}
	if !strings.Contains(err.Error(), "last owner") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddMemberDuplicate(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	_, err := db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AddMember(p.ID, writer.ID, RoleReader, owner.ID)
	if err == nil {
		t.Fatal("expected error adding duplicate member")
	}
}

func TestAddMemberVerifyAccess(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	// Before adding, writer has no access
	m, _ := db.GetMembership(p.ID, writer.ID)
	if m != nil {
		t.Fatal("writer should not be a member yet")
	}
	if err := db.CanPushEvents(p.ID, writer.ID); err == nil {
		t.Fatal("writer should not be able to push before being added")
	}

	// Add as writer
	_, err := db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Now writer has push access
	if err := db.CanPushEvents(p.ID, writer.ID); err != nil {
		t.Fatalf("writer should be able to push: %v", err)
	}
}

func TestRoleBasedAuthorization_WriterCanPush_ReaderCannot(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	reader, _ := db.CreateUser("r@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)
	db.AddMember(p.ID, reader.ID, RoleReader, owner.ID)

	// Writer can push
	if err := db.CanPushEvents(p.ID, writer.ID); err != nil {
		t.Fatalf("writer should push: %v", err)
	}
	// Reader cannot push
	if err := db.CanPushEvents(p.ID, reader.ID); err == nil {
		t.Fatal("reader should NOT push")
	}
	// Reader can pull
	if err := db.CanPullEvents(p.ID, reader.ID); err != nil {
		t.Fatalf("reader should pull: %v", err)
	}
}

func TestRoleUpgradeTakesEffect(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	user, _ := db.CreateUser("u@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	db.AddMember(p.ID, user.ID, RoleReader, owner.ID)

	// Reader cannot push
	if err := db.CanPushEvents(p.ID, user.ID); err == nil {
		t.Fatal("reader should not push")
	}

	// Upgrade to writer
	if err := db.UpdateMemberRole(p.ID, user.ID, RoleWriter); err != nil {
		t.Fatal(err)
	}

	// Now can push
	if err := db.CanPushEvents(p.ID, user.ID); err != nil {
		t.Fatalf("upgraded writer should push: %v", err)
	}
}

func TestMemberRemovalRevokesAccess(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)

	// Writer has access
	if err := db.CanPushEvents(p.ID, writer.ID); err != nil {
		t.Fatalf("writer should push: %v", err)
	}

	// Remove
	if err := db.RemoveMember(p.ID, writer.ID); err != nil {
		t.Fatal(err)
	}

	// Access revoked
	if err := db.CanPushEvents(p.ID, writer.ID); err == nil {
		t.Fatal("removed member should not push")
	}
	if err := db.CanPullEvents(p.ID, writer.ID); err == nil {
		t.Fatal("removed member should not pull")
	}
}

func TestCannotRemoveLastOwner_WithMultipleMembers(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("o@test.com")
	writer, _ := db.CreateUser("w@test.com")
	p, _ := db.CreateProject("proj", "", owner.ID)

	db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)

	// Cannot remove sole owner even with other members
	err := db.RemoveMember(p.ID, owner.ID)
	if err == nil {
		t.Fatal("expected error removing last owner")
	}
	if !strings.Contains(err.Error(), "last owner") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Writer can still be removed
	if err := db.RemoveMember(p.ID, writer.ID); err != nil {
		t.Fatalf("should remove writer: %v", err)
	}
}

func TestRemoveOwnerWhenMultipleOwners(t *testing.T) {
	db := newTestDB(t)
	owner1, _ := db.CreateUser("o1@test.com")
	owner2, _ := db.CreateUser("o2@test.com")
	p, _ := db.CreateProject("proj", "", owner1.ID)

	db.AddMember(p.ID, owner2.ID, RoleOwner, owner1.ID)

	// Can remove one owner when another exists
	if err := db.RemoveMember(p.ID, owner1.ID); err != nil {
		t.Fatalf("should remove owner when another exists: %v", err)
	}

	// Verify owner2 remains
	m, _ := db.GetMembership(p.ID, owner2.ID)
	if m == nil || m.Role != RoleOwner {
		t.Fatal("remaining owner should still be present")
	}
}

// --- Sync cursor tests ---

func TestUpsertAndGetSyncCursor(t *testing.T) {
	db := newTestDB(t)
	u, _ := db.CreateUser("sync@test.com")
	p, _ := db.CreateProject("proj", "", u.ID)

	if err := db.UpsertSyncCursor(p.ID, "device-1", 42); err != nil {
		t.Fatal(err)
	}

	c, err := db.GetSyncCursor(p.ID, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.LastEventID != 42 {
		t.Fatal("cursor not found or wrong value")
	}

	// Upsert again
	db.UpsertSyncCursor(p.ID, "device-1", 100)
	c, _ = db.GetSyncCursor(p.ID, "device-1")
	if c.LastEventID != 100 {
		t.Fatalf("expected 100, got %d", c.LastEventID)
	}
}

func TestGetSyncCursorNotFound(t *testing.T) {
	db := newTestDB(t)
	c, err := db.GetSyncCursor("p_nope", "device-nope")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		t.Fatal("expected nil")
	}
}

// --- Schema version tests ---

func TestSchemaVersion(t *testing.T) {
	db := newTestDB(t)
	v := db.getSchemaVersion()
	if v != ServerSchemaVersion {
		t.Fatalf("expected version %d, got %d", ServerSchemaVersion, v)
	}
}
