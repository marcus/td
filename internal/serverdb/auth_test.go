package serverdb

import (
	"testing"
)

func setupAuthTest(t *testing.T) (*ServerDB, *User, *User, *User, *User, *Project) {
	t.Helper()
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner@test.com")
	writer, _ := db.CreateUser("writer@test.com")
	reader, _ := db.CreateUser("reader@test.com")
	outsider, _ := db.CreateUser("outsider@test.com")

	p, _ := db.CreateProject("proj", "", owner.ID)
	db.AddMember(p.ID, writer.ID, RoleWriter, owner.ID)
	db.AddMember(p.ID, reader.ID, RoleReader, owner.ID)

	return db, owner, writer, reader, outsider, p
}

func TestAuthorizeOwner(t *testing.T) {
	db, owner, _, _, _, p := setupAuthTest(t)

	// Owner can do everything
	for _, role := range []string{RoleOwner, RoleWriter, RoleReader} {
		if err := db.Authorize(p.ID, owner.ID, role); err != nil {
			t.Errorf("owner should have %s: %v", role, err)
		}
	}
}

func TestAuthorizeWriter(t *testing.T) {
	db, _, writer, _, _, p := setupAuthTest(t)

	if err := db.Authorize(p.ID, writer.ID, RoleWriter); err != nil {
		t.Errorf("writer should have writer: %v", err)
	}
	if err := db.Authorize(p.ID, writer.ID, RoleReader); err != nil {
		t.Errorf("writer should have reader: %v", err)
	}
	if err := db.Authorize(p.ID, writer.ID, RoleOwner); err == nil {
		t.Error("writer should NOT have owner")
	}
}

func TestAuthorizeReader(t *testing.T) {
	db, _, _, reader, _, p := setupAuthTest(t)

	if err := db.Authorize(p.ID, reader.ID, RoleReader); err != nil {
		t.Errorf("reader should have reader: %v", err)
	}
	if err := db.Authorize(p.ID, reader.ID, RoleWriter); err == nil {
		t.Error("reader should NOT have writer")
	}
	if err := db.Authorize(p.ID, reader.ID, RoleOwner); err == nil {
		t.Error("reader should NOT have owner")
	}
}

func TestAuthorizeNonMember(t *testing.T) {
	db, _, _, _, outsider, p := setupAuthTest(t)

	for _, role := range []string{RoleOwner, RoleWriter, RoleReader} {
		if err := db.Authorize(p.ID, outsider.ID, role); err == nil {
			t.Errorf("outsider should NOT have %s", role)
		}
	}
}

func TestCanPushEvents(t *testing.T) {
	db, owner, writer, reader, outsider, p := setupAuthTest(t)

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{"owner can push", owner.ID, false},
		{"writer can push", writer.ID, false},
		{"reader cannot push", reader.ID, true},
		{"outsider cannot push", outsider.ID, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.CanPushEvents(p.ID, tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanPushEvents() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestCanPullEvents(t *testing.T) {
	db, owner, writer, reader, outsider, p := setupAuthTest(t)

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{"owner can pull", owner.ID, false},
		{"writer can pull", writer.ID, false},
		{"reader can pull", reader.ID, false},
		{"outsider cannot pull", outsider.ID, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.CanPullEvents(p.ID, tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanPullEvents() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestCanManageMembers(t *testing.T) {
	db, owner, writer, reader, outsider, p := setupAuthTest(t)

	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{"owner can manage", owner.ID, false},
		{"writer cannot manage", writer.ID, true},
		{"reader cannot manage", reader.ID, true},
		{"outsider cannot manage", outsider.ID, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.CanManageMembers(p.ID, tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanManageMembers() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestCanDeleteProject(t *testing.T) {
	db, owner, writer, _, _, p := setupAuthTest(t)

	if err := db.CanDeleteProject(p.ID, owner.ID); err != nil {
		t.Errorf("owner should be able to delete: %v", err)
	}
	if err := db.CanDeleteProject(p.ID, writer.ID); err == nil {
		t.Error("writer should NOT be able to delete")
	}
}
