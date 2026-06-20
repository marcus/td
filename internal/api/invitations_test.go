package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/marcus/td/internal/email"
	"github.com/marcus/td/internal/serverdb"
)

func createProjectForInvitationTest(t *testing.T, srv *Server, ownerToken string) ProjectResponse {
	t.Helper()
	w := doRequest(srv, "POST", "/v1/projects", ownerToken, CreateProjectRequest{Name: "invite-api-project"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return project
}

func createInvitationForTest(t *testing.T, srv *Server, ownerToken, projectID, inviteEmail, role string) InvitationResponse {
	t.Helper()
	w := doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/invitations", projectID), ownerToken, CreateInvitationRequest{
		Email: inviteEmail,
		Role:  role,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create invitation: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var inv InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&inv); err != nil {
		t.Fatalf("decode invitation: %v", err)
	}
	return inv
}

func TestCreateProjectInvitationSendsEmail(t *testing.T) {
	ms := email.NewMemorySender()
	srv, _ := newTestServerWithConfig(t, func(cfg *Config) {
		cfg.AuthWebCallbackURL = "https://watch.example.com/home/login/complete"
	})
	srv.emailSender = ms
	_, ownerToken := createTestUser(t, srv.store, "owner-invite-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)

	inv := createInvitationForTest(t, srv, ownerToken, project.ID, "Invitee@Example.com", serverdb.RoleWriter)
	if inv.Email != "invitee@example.com" || inv.Role != serverdb.RoleWriter || inv.Status != serverdb.InvitationStatusPending {
		t.Fatalf("invitation response mismatch: %#v", inv)
	}

	sent := ms.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent email, got %d", len(sent))
	}
	if sent[0].To != "invitee@example.com" {
		t.Fatalf("email To = %q", sent[0].To)
	}
	if sent[0].Purpose != "project_invitation" {
		t.Fatalf("email Purpose = %q", sent[0].Purpose)
	}
	if !strings.Contains(sent[0].Text, "https://watch.example.com/home/login?") {
		t.Fatalf("invite link should target /home/login, text=%q", sent[0].Text)
	}
	if strings.Contains(sent[0].Text, "/home/login/complete") {
		t.Fatalf("invite link should not target callback completion URL: %q", sent[0].Text)
	}
	if !strings.Contains(sent[0].Text, "invitation_id="+url.QueryEscape(inv.ID)) {
		t.Fatalf("invite link missing invitation_id: %q", sent[0].Text)
	}

	linkText := strings.TrimPrefix(strings.Split(strings.Split(sent[0].Text, "Sign in to accept: ")[1], "\n\n")[0], " ")
	linkURL, err := url.Parse(linkText)
	if err != nil {
		t.Fatalf("parse invite link: %v", err)
	}
	plaintextToken := linkURL.Query().Get("invitation_token")
	if plaintextToken == "" {
		t.Fatal("invite link missing plaintext token")
	}
	stored, err := srv.store.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get stored invitation: %v", err)
	}
	if stored.TokenHash == plaintextToken {
		t.Fatal("plaintext invitation token must not be stored")
	}
	sum := sha256.Sum256([]byte(plaintextToken))
	if stored.TokenHash != hex.EncodeToString(sum[:]) {
		t.Fatal("stored token_hash does not match plaintext invitation token")
	}
}

func TestInvitationLoginLinkFallback(t *testing.T) {
	link := buildInvitationLoginLink("", "inv_123", "user@example.com", "secret")
	if !strings.HasPrefix(link, "http://localhost:5173/home/login?") {
		t.Fatalf("fallback link = %q", link)
	}
	if !strings.Contains(link, "invitation_id=inv_123") || !strings.Contains(link, "email=user%40example.com") {
		t.Fatalf("fallback link missing query params: %q", link)
	}
}

func TestProjectInvitationRequiresOwner(t *testing.T) {
	srv, store := newTestServer(t)
	_, ownerToken := createTestUser(t, store, "owner-requires-api@example.com")
	writerID, writerToken := createTestUser(t, store, "writer-requires-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)
	if _, err := store.AddMember(project.ID, writerID, serverdb.RoleWriter, ""); err != nil {
		t.Fatalf("add writer: %v", err)
	}

	w := doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/invitations", project.ID), writerToken, CreateInvitationRequest{
		Email: "invitee@example.com",
		Role:  serverdb.RoleReader,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("writer invite: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListOwnInvitationsOnly(t *testing.T) {
	srv, store := newTestServer(t)
	srv.emailSender = email.NewMemorySender()
	_, ownerToken := createTestUser(t, store, "owner-own-list@example.com")
	_, inviteeToken := createTestUser(t, store, "own-list@example.com")
	_, otherToken := createTestUser(t, store, "other-list@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)

	ownInvite := createInvitationForTest(t, srv, ownerToken, project.ID, "own-list@example.com", serverdb.RoleReader)
	_ = createInvitationForTest(t, srv, ownerToken, project.ID, "other-list@example.com", serverdb.RoleWriter)

	w := doRequest(srv, "GET", "/v1/invitations", inviteeToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list own invitations: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var own []InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&own); err != nil {
		t.Fatalf("decode own invitations: %v", err)
	}
	if len(own) != 1 || own[0].ID != ownInvite.ID {
		t.Fatalf("own invitations mismatch: %#v", own)
	}

	w = doRequest(srv, "GET", "/v1/invitations", otherToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list other invitations: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var other []InvitationResponse
	if err := json.NewDecoder(w.Body).Decode(&other); err != nil {
		t.Fatalf("decode other invitations: %v", err)
	}
	if len(other) != 1 || other[0].Email != "other-list@example.com" {
		t.Fatalf("other invitations mismatch: %#v", other)
	}
}

func TestAcceptInvitationCreatesMembership(t *testing.T) {
	srv, store := newTestServer(t)
	srv.emailSender = email.NewMemorySender()
	_, ownerToken := createTestUser(t, store, "owner-accept-api@example.com")
	inviteeID, inviteeToken := createTestUser(t, store, "accept-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)
	inv := createInvitationForTest(t, srv, ownerToken, project.ID, "accept-api@example.com", serverdb.RoleWriter)

	w := doRequest(srv, "POST", fmt.Sprintf("/v1/invitations/%s/accept", inv.ID), inviteeToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("accept invitation: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	m, err := store.GetMembership(project.ID, inviteeID)
	if err != nil {
		t.Fatalf("get membership: %v", err)
	}
	if m == nil || m.Role != serverdb.RoleWriter {
		t.Fatalf("membership mismatch after accept: %#v", m)
	}
}

func TestWrongEmailCannotAcceptInvitation(t *testing.T) {
	srv, store := newTestServer(t)
	srv.emailSender = email.NewMemorySender()
	_, ownerToken := createTestUser(t, store, "owner-wrong-api@example.com")
	_, wrongToken := createTestUser(t, store, "wrong-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)
	inv := createInvitationForTest(t, srv, ownerToken, project.ID, "right-api@example.com", serverdb.RoleReader)

	w := doRequest(srv, "POST", fmt.Sprintf("/v1/invitations/%s/accept", inv.ID), wrongToken, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("wrong email accept: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeclineInvitationWorks(t *testing.T) {
	srv, store := newTestServer(t)
	srv.emailSender = email.NewMemorySender()
	_, ownerToken := createTestUser(t, store, "owner-decline-api@example.com")
	_, inviteeToken := createTestUser(t, store, "decline-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)
	inv := createInvitationForTest(t, srv, ownerToken, project.ID, "decline-api@example.com", serverdb.RoleReader)

	w := doRequest(srv, "POST", fmt.Sprintf("/v1/invitations/%s/decline", inv.ID), inviteeToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("decline invitation: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	declined, err := store.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get invitation: %v", err)
	}
	if declined.Status != serverdb.InvitationStatusDeclined {
		t.Fatalf("status = %s, want declined", declined.Status)
	}
}

func TestDeleteInvitationWorks(t *testing.T) {
	srv, store := newTestServer(t)
	srv.emailSender = email.NewMemorySender()
	_, ownerToken := createTestUser(t, store, "owner-delete-api@example.com")
	project := createProjectForInvitationTest(t, srv, ownerToken)
	inv := createInvitationForTest(t, srv, ownerToken, project.ID, "delete-api@example.com", serverdb.RoleReader)

	w := doRequest(srv, "DELETE", fmt.Sprintf("/v1/projects/%s/invitations/%s", project.ID, inv.ID), ownerToken, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete invitation: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	found, err := store.GetInvitation(inv.ID)
	if err != nil {
		t.Fatalf("get invitation: %v", err)
	}
	if found != nil {
		t.Fatalf("deleted invitation still found: %#v", found)
	}
}
