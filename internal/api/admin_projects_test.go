package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

func TestAdminListProjects(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	// Create a non-admin user and some projects
	u, err := store.CreateUser("proj-owner@test.com")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.CreateProjectWithID("p_001", "alpha-project", "desc1", u.ID)
	_, _ = store.CreateProjectWithID("p_002", "beta-project", "desc2", u.ID)

	w := doRequest(srv, "GET", "/v1/admin/projects", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data    []serverdb.AdminProject `json:"data"`
		HasMore bool                    `json:"has_more"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) < 2 {
		t.Fatalf("expected >= 2 projects, got %d", len(resp.Data))
	}
}

func TestAdminListProjects_Search(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	u, _ := store.CreateUser("search-owner@test.com")
	_, _ = store.CreateProjectWithID("p_s1", "search-alpha", "desc", u.ID)
	_, _ = store.CreateProjectWithID("p_s2", "search-beta", "desc", u.ID)
	_, _ = store.CreateProjectWithID("p_s3", "other-project", "desc", u.ID)

	w := doRequest(srv, "GET", "/v1/admin/projects?q=search", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []serverdb.AdminProject `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 matching projects, got %d", len(resp.Data))
	}
}

func TestAdminListProjects_IncludeDeleted(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	u, _ := store.CreateUser("del-owner@test.com")
	_, _ = store.CreateProjectWithID("p_d1", "active-proj", "desc", u.ID)
	_, _ = store.CreateProjectWithID("p_d2", "deleted-proj", "desc", u.ID)
	_ = store.SoftDeleteProject("p_d2")

	// Without include_deleted
	w := doRequest(srv, "GET", "/v1/admin/projects", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp1 struct {
		Data []serverdb.AdminProject `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp1)
	for _, p := range resp1.Data {
		if p.ID == "p_d2" {
			t.Fatal("deleted project should not appear without include_deleted")
		}
	}

	// With include_deleted=true
	w = doRequest(srv, "GET", "/v1/admin/projects?include_deleted=true", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp2 struct {
		Data []serverdb.AdminProject `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp2)
	found := false
	for _, p := range resp2.Data {
		if p.ID == "p_d2" {
			found = true
			if p.DeletedAt == nil {
				t.Fatal("deleted_at should not be nil for deleted project")
			}
		}
	}
	if !found {
		t.Fatal("deleted project should appear with include_deleted=true")
	}
}

func TestAdminGetProject(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	u, _ := store.CreateUser("get-owner@test.com")
	_, _ = store.CreateProjectWithID("p_get1", "detail-project", "detailed desc", u.ID)

	w := doRequest(srv, "GET", "/v1/admin/projects/p_get1", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.AdminProject
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "p_get1" {
		t.Fatalf("expected id p_get1, got %s", resp.ID)
	}
	if resp.Name != "detail-project" {
		t.Fatalf("expected name detail-project, got %s", resp.Name)
	}
	if resp.Description != "detailed desc" {
		t.Fatalf("expected description 'detailed desc', got %s", resp.Description)
	}
	if resp.MemberCount != 1 {
		t.Fatalf("expected 1 member (owner), got %d", resp.MemberCount)
	}
}

func TestAdminGetProject_NotFound(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	w := doRequest(srv, "GET", "/v1/admin/projects/nonexistent", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminProjectMembers(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	u1, _ := store.CreateUser("member-owner@test.com")
	u2, _ := store.CreateUser("member-writer@test.com")
	_, _ = store.CreateProjectWithID("p_mem1", "member-project", "desc", u1.ID)
	_, _ = store.AddMember("p_mem1", u2.ID, "writer", u1.ID)

	w := doRequest(srv, "GET", "/v1/admin/projects/p_mem1/members", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []serverdb.AdminProjectMember `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 members, got %d", len(resp.Data))
	}

	// Verify emails are present
	emails := map[string]bool{}
	for _, m := range resp.Data {
		emails[m.Email] = true
	}
	if !emails["member-owner@test.com"] {
		t.Fatal("expected member-owner@test.com in members")
	}
	if !emails["member-writer@test.com"] {
		t.Fatal("expected member-writer@test.com in members")
	}
}

func TestAdminSyncStatus(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")
	_, userToken := createTestUser(t, store, "sync-user@test.com")

	// Create project via API (so events.db is created)
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "sync-status-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push some events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"title":"test"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{"title":"test2"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Admin checks sync status
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/sync/status", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp adminSyncStatusResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.EventCount != 2 {
		t.Fatalf("expected 2 events, got %d", resp.EventCount)
	}
	if resp.LastServerSeq < 2 {
		t.Fatalf("expected last_server_seq >= 2, got %d", resp.LastServerSeq)
	}
}

func TestAdminSyncCursors(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")
	_, userToken := createTestUser(t, store, "cursor-user@test.com")

	// Create project via API
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "cursor-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"title":"test"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{"title":"test2"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "i_003",
				Payload: json.RawMessage(`{"title":"test3"}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Upsert some cursors
	_ = store.UpsertSyncCursor(project.ID, "client-A", 3)
	_ = store.UpsertSyncCursor(project.ID, "client-B", 1)

	// Admin checks cursors
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/sync/cursors", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []adminCursorEntry `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 cursors, got %d", len(resp.Data))
	}

	// Check distance_from_head
	for _, c := range resp.Data {
		if c.ClientID == "client-A" {
			if c.DistanceFromHead != 0 {
				t.Fatalf("client-A: expected distance 0, got %d", c.DistanceFromHead)
			}
		} else if c.ClientID == "client-B" {
			if c.DistanceFromHead != 2 {
				t.Fatalf("client-B: expected distance 2, got %d", c.DistanceFromHead)
			}
		}
	}
}

func TestAdminProjects_RequiresAdmin(t *testing.T) {
	srv, store := newTestServer(t)
	_, _ = store.CreateUser("first@test.com")
	_, token := createTestUser(t, store, "nonadmin@test.com")

	w := doRequest(srv, "GET", "/v1/admin/projects", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminProjects_RequiresScope(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server")

	w := doRequest(srv, "GET", "/v1/admin/projects", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (wrong scope), got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminListProjects_MemberCount(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects,sync")

	u1, _ := store.CreateUser("mc-owner@test.com")
	u2, _ := store.CreateUser("mc-writer@test.com")
	u3, _ := store.CreateUser("mc-reader@test.com")
	_, _ = store.CreateProjectWithID("p_mc1", "mc-project", "desc", u1.ID)
	_, _ = store.AddMember("p_mc1", u2.ID, "writer", u1.ID)
	_, _ = store.AddMember("p_mc1", u3.ID, "reader", u1.ID)

	w := doRequest(srv, "GET", "/v1/admin/projects?q=mc-project", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []serverdb.AdminProject `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 project, got %d", len(resp.Data))
	}
	if resp.Data[0].MemberCount != 3 {
		t.Fatalf("expected 3 members, got %d", resp.Data[0].MemberCount)
	}
}
