package serverdb

import (
	"strings"
	"testing"
	"time"
)

func TestInvitationsSchemaVersionAndTable(t *testing.T) {
	db := newTestDB(t)

	if got := db.getSchemaVersion(); got != ServerSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, ServerSchemaVersion)
	}

	var tableName string
	if err := db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'invitations'`).Scan(&tableName); err != nil {
		t.Fatalf("invitations table missing: %v", err)
	}

	for _, indexName := range []string{
		"idx_invitations_project",
		"idx_invitations_email_status",
		"idx_invitations_cleanup",
		"idx_invitations_token_hash",
	} {
		var found string
		if err := db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, indexName).Scan(&found); err != nil {
			t.Fatalf("index %s missing: %v", indexName, err)
		}
	}
}

func TestInvitationCreateListAndAccept(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner@example.com")
	invitee, _ := db.CreateUser("Invitee@Example.com")
	p, _ := db.CreateProject("invite-project", "", owner.ID)

	inv, err := db.CreateInvitation(p.ID, "INVITEE@example.com", RoleWriter, owner.ID, "hash-create-accept", time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if inv.Email != "invitee@example.com" {
		t.Fatalf("email = %q, want lowercased", inv.Email)
	}
	if !strings.HasPrefix(inv.ID, "inv_") {
		t.Fatalf("id prefix = %q, want inv_", inv.ID)
	}

	if m, err := db.GetMembership(p.ID, invitee.ID); err != nil {
		t.Fatalf("get membership before accept: %v", err)
	} else if m != nil {
		t.Fatal("membership should not exist before accept")
	}

	projectInvites, err := db.ListProjectInvitations(p.ID)
	if err != nil {
		t.Fatalf("list project invitations: %v", err)
	}
	if len(projectInvites) != 1 || projectInvites[0].ID != inv.ID {
		t.Fatalf("project invitations mismatch: %#v", projectInvites)
	}

	pending, err := db.ListPendingInvitationsForEmail("invitee@example.com")
	if err != nil {
		t.Fatalf("list pending invitations: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != inv.ID {
		t.Fatalf("pending invitations mismatch: %#v", pending)
	}

	exists, err := db.HasPendingInvitationForEmail("INVITEE@example.com")
	if err != nil {
		t.Fatalf("pending exists: %v", err)
	}
	if !exists {
		t.Fatal("expected pending invitation for invitee")
	}

	m, err := db.AcceptInvitation(inv.ID, invitee.ID, "invitee@example.com")
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if m == nil || m.ProjectID != p.ID || m.UserID != invitee.ID || m.Role != RoleWriter {
		t.Fatalf("membership mismatch after accept: %#v", m)
	}

	accepted, err := db.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get accepted invitation: %v", err)
	}
	if accepted.Status != InvitationStatusAccepted || accepted.AcceptedAt == nil {
		t.Fatalf("invitation not accepted: %#v", accepted)
	}
}

func TestAcceptInvitationExistingMembershipIsSafe(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner-existing@example.com")
	invitee, _ := db.CreateUser("member-existing@example.com")
	p, _ := db.CreateProject("existing-member", "", owner.ID)
	if _, err := db.AddMember(p.ID, invitee.ID, RoleReader, owner.ID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	inv, err := db.CreateInvitation(p.ID, invitee.Email, RoleWriter, owner.ID, "hash-existing-member", time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	m, err := db.AcceptInvitation(inv.ID, invitee.ID, invitee.Email)
	if err != nil {
		t.Fatalf("accept existing membership invitation: %v", err)
	}
	if m.Role != RoleReader {
		t.Fatalf("existing membership role should be preserved, got %s", m.Role)
	}

	members, err := db.ListMembers(p.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected owner + invitee only, got %d", len(members))
	}
}

func TestDeclineInvitation(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner-decline@example.com")
	invitee, _ := db.CreateUser("decline@example.com")
	p, _ := db.CreateProject("decline-project", "", owner.ID)
	inv, err := db.CreateInvitation(p.ID, invitee.Email, RoleReader, owner.ID, "hash-decline", time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if err := db.DeclineInvitation(inv.ID, "other@example.com"); err != ErrInvitationEmailMismatch {
		t.Fatalf("wrong email error = %v, want ErrInvitationEmailMismatch", err)
	}
	if err := db.DeclineInvitation(inv.ID, invitee.Email); err != nil {
		t.Fatalf("decline invitation: %v", err)
	}
	declined, err := db.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get declined invitation: %v", err)
	}
	if declined.Status != InvitationStatusDeclined {
		t.Fatalf("status = %s, want declined", declined.Status)
	}
}

func TestDeleteInvitation(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner-delete@example.com")
	p, _ := db.CreateProject("delete-project", "", owner.ID)
	inv, err := db.CreateInvitation(p.ID, "delete@example.com", RoleReader, owner.ID, "hash-delete", time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if err := db.DeleteInvitation(p.ID, inv.ID); err != nil {
		t.Fatalf("delete invitation: %v", err)
	}
	found, err := db.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get deleted invitation: %v", err)
	}
	if found != nil {
		t.Fatalf("deleted invitation still found: %#v", found)
	}
	if err := db.DeleteInvitation(p.ID, inv.ID); err != ErrInvitationNotFound {
		t.Fatalf("delete missing error = %v, want ErrInvitationNotFound", err)
	}
}

func TestExpiredInvitationCannotBeAcceptedOrListedPending(t *testing.T) {
	db := newTestDB(t)
	owner, _ := db.CreateUser("owner-expired@example.com")
	invitee, _ := db.CreateUser("expired@example.com")
	p, _ := db.CreateProject("expired-project", "", owner.ID)
	inv, err := db.CreateInvitation(p.ID, invitee.Email, RoleReader, owner.ID, "hash-expired", time.Now().UTC().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	_, err = db.conn.Exec(`UPDATE invitations SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Hour), inv.ID)
	if err != nil {
		t.Fatalf("force expire invitation: %v", err)
	}

	pending, err := db.ListPendingInvitationsForEmail(invitee.Email)
	if err != nil {
		t.Fatalf("list pending invitations: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expired invitation should not be listed as pending: %#v", pending)
	}

	if _, err := db.AcceptInvitation(inv.ID, invitee.ID, invitee.Email); err != ErrInvitationExpired {
		t.Fatalf("accept expired error = %v, want ErrInvitationExpired", err)
	}
	expired, err := db.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get expired invitation: %v", err)
	}
	if expired.Status != InvitationStatusExpired {
		t.Fatalf("status = %s, want expired", expired.Status)
	}
}
