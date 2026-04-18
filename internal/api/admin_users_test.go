package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

func TestAdminListUsers(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create additional users
	_, _ = store.CreateUser("alice@test.com")
	_, _ = store.CreateUser("bob@test.com")

	w := doRequest(srv, "GET", "/v1/admin/users", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.PaginatedResult[serverdb.AdminUser]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Data) < 3 {
		t.Fatalf("expected >= 3 users, got %d", len(resp.Data))
	}

	// Verify fields are populated
	for _, u := range resp.Data {
		if u.ID == "" {
			t.Fatal("expected non-empty user id")
		}
		if u.Email == "" {
			t.Fatal("expected non-empty email")
		}
		if u.CreatedAt == "" {
			t.Fatal("expected non-empty created_at")
		}
	}
}

func TestAdminListUsers_Search(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	_, _ = store.CreateUser("alice@example.com")
	_, _ = store.CreateUser("bob@example.com")

	// Search for alice
	w := doRequest(srv, "GET", "/v1/admin/users?q=alice", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.PaginatedResult[serverdb.AdminUser]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 user matching 'alice', got %d", len(resp.Data))
	}
	if resp.Data[0].Email != "alice@example.com" {
		t.Fatalf("expected alice@example.com, got %s", resp.Data[0].Email)
	}
}

func TestAdminListUsers_Pagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create enough users to paginate (admin + 5 more = 6 total)
	for i := 0; i < 5; i++ {
		_, _ = store.CreateUser(fmt.Sprintf("user%d@test.com", i))
	}

	// Page 1 with limit=3
	w := doRequest(srv, "GET", "/v1/admin/users?limit=3", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 serverdb.PaginatedResult[serverdb.AdminUser]
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}

	if len(page1.Data) != 3 {
		t.Fatalf("expected 3 users on page 1, got %d", len(page1.Data))
	}
	if !page1.HasMore {
		t.Fatal("expected has_more=true on page 1")
	}
	if page1.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor on page 1")
	}

	// Page 2
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/users?limit=3&cursor=%s", page1.NextCursor), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 serverdb.PaginatedResult[serverdb.AdminUser]
	if err := json.NewDecoder(w.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}

	if len(page2.Data) != 3 {
		t.Fatalf("expected 3 users on page 2, got %d", len(page2.Data))
	}

	// Ensure no overlap between pages
	page1IDs := map[string]bool{}
	for _, u := range page1.Data {
		page1IDs[u.ID] = true
	}
	for _, u := range page2.Data {
		if page1IDs[u.ID] {
			t.Fatalf("user %s appears on both pages", u.ID)
		}
	}
}

func TestAdminGetUser(t *testing.T) {
	srv, store := newTestServer(t)
	adminID, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create a user with a project
	user, _ := store.CreateUser("detail@test.com")
	proj, _ := store.CreateProject("test-proj", "desc", user.ID)
	_ = proj

	w := doRequest(srv, "GET", fmt.Sprintf("/v1/admin/users/%s", user.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.AdminUserDetail
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ID != user.ID {
		t.Fatalf("expected id %s, got %s", user.ID, resp.ID)
	}
	if resp.Email != "detail@test.com" {
		t.Fatalf("expected email detail@test.com, got %s", resp.Email)
	}
	if resp.ProjectCount != 1 {
		t.Fatalf("expected project_count 1, got %d", resp.ProjectCount)
	}
	if len(resp.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(resp.Projects))
	}
	if resp.Projects[0].Name != "test-proj" {
		t.Fatalf("expected project name test-proj, got %s", resp.Projects[0].Name)
	}
	if resp.Projects[0].Role != "owner" {
		t.Fatalf("expected role owner, got %s", resp.Projects[0].Role)
	}

	_ = adminID

	// Non-existent user returns 404
	w = doRequest(srv, "GET", "/v1/admin/users/u_nonexistent", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminUserKeys(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create user with API keys
	user, _ := store.CreateUser("keys@test.com")
	_, _, _ = store.GenerateAPIKey(user.ID, "key-one", "sync", nil)
	_, _, _ = store.GenerateAPIKey(user.ID, "key-two", "sync", nil)

	w := doRequest(srv, "GET", fmt.Sprintf("/v1/admin/users/%s/keys", user.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(resp.Data))
	}

	// Verify key_hash is NOT present in any key
	for i, key := range resp.Data {
		if _, exists := key["key_hash"]; exists {
			t.Fatalf("key %d: key_hash should NOT be in response", i)
		}
		if key["key_prefix"] == nil || key["key_prefix"] == "" {
			t.Fatalf("key %d: expected non-empty key_prefix", i)
		}
		if key["name"] == nil || key["name"] == "" {
			t.Fatalf("key %d: expected non-empty name", i)
		}
	}

	// Non-existent user returns 404
	w = doRequest(srv, "GET", "/v1/admin/users/u_nonexistent/keys", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing user keys, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminAuthEvents(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Insert auth events
	_ = store.InsertAuthEvent("req_1", "user1@test.com", "started", `{}`)
	_ = store.InsertAuthEvent("req_1", "user1@test.com", "code_verified", `{}`)
	_ = store.InsertAuthEvent("req_2", "user2@test.com", "started", `{}`)
	_ = store.InsertAuthEvent("req_2", "user2@test.com", "failed", `{}`)

	// List all events
	w := doRequest(srv, "GET", "/v1/admin/auth/events", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.PaginatedResult[serverdb.AuthEvent]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Data) != 4 {
		t.Fatalf("expected 4 auth events, got %d", len(resp.Data))
	}

	// Filter by status (event_type)
	w = doRequest(srv, "GET", "/v1/admin/auth/events?status=started", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 'started' events, got %d", len(resp.Data))
	}
	for _, e := range resp.Data {
		if e.EventType != "started" {
			t.Fatalf("expected event_type 'started', got %q", e.EventType)
		}
	}

	// Filter by email
	w = doRequest(srv, "GET", "/v1/admin/auth/events?email=user1", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 events for user1, got %d", len(resp.Data))
	}
}

func TestAdminUserEndpoints_RequireAdmin(t *testing.T) {
	srv, store := newTestServer(t)
	// First user is auto-admin; create a second non-admin user
	_, _ = store.CreateUser("first@test.com")
	_, nonAdminToken := createTestUser(t, store, "nonadmin@test.com")

	endpoints := []string{
		"/v1/admin/users",
		"/v1/admin/users/u_fake",
		"/v1/admin/users/u_fake/keys",
		"/v1/admin/auth/events",
	}

	for _, ep := range endpoints {
		w := doRequest(srv, "GET", ep, nonAdminToken, nil)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403 for non-admin, got %d: %s", ep, w.Code, w.Body.String())
		}
	}
}

func TestAdminListUsers_ProjectCount(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create user who owns 2 projects
	user, _ := store.CreateUser("multi@test.com")
	_, _ = store.CreateProject("proj1", "", user.ID)
	_, _ = store.CreateProject("proj2", "", user.ID)

	w := doRequest(srv, "GET", "/v1/admin/users?q=multi", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverdb.PaginatedResult[serverdb.AdminUser]
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 user, got %d", len(resp.Data))
	}
	if resp.Data[0].ProjectCount != 2 {
		t.Fatalf("expected project_count 2, got %d", resp.Data[0].ProjectCount)
	}
}
