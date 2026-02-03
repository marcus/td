package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/marcus/td/internal/serverdb"
	_ "modernc.org/sqlite"
)

// newTestServer creates a Server backed by temp directories for testing.
func newTestServer(t *testing.T) (*Server, *serverdb.ServerDB) {
	t.Helper()
	tmpDir := t.TempDir()

	dbPath := filepath.Join(tmpDir, "server.db")
	store, err := serverdb.Open(dbPath)
	if err != nil {
		t.Fatalf("open server db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	projectDir := filepath.Join(tmpDir, "projects")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	cfg := Config{
		RateLimitAuth:  100000,
		RateLimitPush:  100000,
		RateLimitPull:  100000,
		RateLimitOther: 100000,
		ListenAddr:     ":0",
		ServerDBPath:   dbPath,
		ProjectDataDir: projectDir,
	}

	srv, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { srv.dbPool.CloseAll() })

	return srv, store
}

// newTestServerWithConfig creates a test server with a custom config modifier.
func newTestServerWithConfig(t *testing.T, modCfg func(*Config)) (*Server, *serverdb.ServerDB) {
	t.Helper()
	tmpDir := t.TempDir()

	dbPath := filepath.Join(tmpDir, "server.db")
	store, err := serverdb.Open(dbPath)
	if err != nil {
		t.Fatalf("open server db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	projectDir := filepath.Join(tmpDir, "projects")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	cfg := Config{
		RateLimitAuth:  100000,
		RateLimitPush:  100000,
		RateLimitPull:  100000,
		RateLimitOther: 100000,
		ListenAddr:     ":0",
		ServerDBPath:   dbPath,
		ProjectDataDir: projectDir,
	}
	if modCfg != nil {
		modCfg(&cfg)
	}

	srv, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() { srv.dbPool.CloseAll() })

	return srv, store
}

// createTestUser creates a user and API key, returning the bearer token.
func createTestUser(t *testing.T, store *serverdb.ServerDB, email string) (string, string) {
	t.Helper()
	user, err := store.CreateUser(email)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.GenerateAPIKey(user.ID, "test", "sync", nil)
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	return user.ID, token
}

func doRequest(srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}

	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "GET", "/healthz", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", resp["status"])
	}
}

func TestPushRequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/projects/fake/sync/push", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestPushSuccess(t *testing.T) {
	srv, store := newTestServer(t)
	userID, token := createTestUser(t, store, "push@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{
		Name: "test-project",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	_ = userID

	// Push events
	pushBody := PushRequest{
		DeviceID:  "dev1",
		SessionID: "sess1",
		Events: []EventInput{
			{
				ClientActionID:  1,
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        "i_001",
				Payload:         json.RawMessage(`{"title":"test"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			},
			{
				ClientActionID:  2,
				ActionType:      "update",
				EntityType:      "issues",
				EntityID:        "i_001",
				Payload:         json.RawMessage(`{"title":"updated"}`),
				ClientTimestamp: "2025-01-01T00:00:01Z",
			},
		},
	}

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pushResp PushResponse
	json.NewDecoder(w.Body).Decode(&pushResp)

	if pushResp.Accepted != 2 {
		t.Fatalf("expected 2 accepted, got %d", pushResp.Accepted)
	}
	if len(pushResp.Acks) != 2 {
		t.Fatalf("expected 2 acks, got %d", len(pushResp.Acks))
	}
	if pushResp.Acks[0].ServerSeq < 1 {
		t.Fatalf("expected server_seq >= 1, got %d", pushResp.Acks[0].ServerSeq)
	}
}

func TestPushRetryDuplicatesReturnServerSeq(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "retry@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "retry-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	pushBody := PushRequest{
		DeviceID:  "dev1",
		SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001", Payload: json.RawMessage(`{"title":"test"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_002", Payload: json.RawMessage(`{"title":"test2"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	}

	// First push — should succeed
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
	if w.Code != http.StatusOK {
		t.Fatalf("first push: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var firstResp PushResponse
	json.NewDecoder(w.Body).Decode(&firstResp)
	if firstResp.Accepted != 2 {
		t.Fatalf("first push: expected 2 accepted, got %d", firstResp.Accepted)
	}

	// Retry push (same events) — simulates crash before marking synced
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
	if w.Code != http.StatusOK {
		t.Fatalf("retry push: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var retryResp PushResponse
	json.NewDecoder(w.Body).Decode(&retryResp)

	if retryResp.Accepted != 0 {
		t.Fatalf("retry: expected 0 accepted, got %d", retryResp.Accepted)
	}
	if len(retryResp.Rejected) != 2 {
		t.Fatalf("retry: expected 2 rejected, got %d", len(retryResp.Rejected))
	}

	// Duplicate rejections must include server_seq so client can mark synced
	for i, rej := range retryResp.Rejected {
		if rej.Reason != "duplicate" {
			t.Errorf("rej[%d] reason: got %q, want 'duplicate'", i, rej.Reason)
		}
		if rej.ServerSeq <= 0 {
			t.Errorf("rej[%d] server_seq: got %d, want >0", i, rej.ServerSeq)
		}
		// Should match original ack's server_seq
		if rej.ServerSeq != firstResp.Acks[i].ServerSeq {
			t.Errorf("rej[%d] server_seq: got %d, want %d (original)", i, rej.ServerSeq, firstResp.Acks[i].ServerSeq)
		}
	}
}

func TestPullSuccess(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "pull@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "pull-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push events
	pushBody := PushRequest{
		DeviceID:  "dev1",
		SessionID: "sess1",
		Events: []EventInput{
			{
				ClientActionID:  1,
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        "i_001",
				Payload:         json.RawMessage(`{"title":"test"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			},
		},
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Pull events
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/pull?after_server_seq=0", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pull: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pullResp PullResponse
	json.NewDecoder(w.Body).Decode(&pullResp)

	if len(pullResp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pullResp.Events))
	}
	if pullResp.Events[0].EntityID != "i_001" {
		t.Fatalf("expected entity_id i_001, got %s", pullResp.Events[0].EntityID)
	}
	if pullResp.LastServerSeq < 1 {
		t.Fatalf("expected last_server_seq >= 1, got %d", pullResp.LastServerSeq)
	}
}

func TestPullPagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "page@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "page-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push 5 events
	events := make([]EventInput, 5)
	for i := range events {
		events[i] = EventInput{
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("i_%03d", i+1),
			Payload:         json.RawMessage(`{}`),
			ClientTimestamp: "2025-01-01T00:00:00Z",
		}
	}

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1", Events: events,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Pull with limit=2
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/pull?after_server_seq=0&limit=2", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pull: expected 200, got %d", w.Code)
	}

	var pullResp PullResponse
	json.NewDecoder(w.Body).Decode(&pullResp)

	if len(pullResp.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(pullResp.Events))
	}
	if !pullResp.HasMore {
		t.Fatal("expected has_more=true")
	}

	// Pull next page
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/pull?after_server_seq=%d&limit=2", project.ID, pullResp.LastServerSeq), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pull page 2: expected 200, got %d", w.Code)
	}

	var pullResp2 PullResponse
	json.NewDecoder(w.Body).Decode(&pullResp2)

	if len(pullResp2.Events) != 2 {
		t.Fatalf("expected 2 events on page 2, got %d", len(pullResp2.Events))
	}
	if !pullResp2.HasMore {
		t.Fatal("expected has_more=true on page 2")
	}
}

func TestCreateProject(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "create@test.com")

	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{
		Name:        "my-project",
		Description: "a test project",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp ProjectResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Name != "my-project" {
		t.Fatalf("expected name my-project, got %s", resp.Name)
	}
	if resp.Description != "a test project" {
		t.Fatalf("expected description 'a test project', got %s", resp.Description)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestListProjects(t *testing.T) {
	srv, store := newTestServer(t)
	userID1, token1 := createTestUser(t, store, "user1@test.com")
	_, token2 := createTestUser(t, store, "user2@test.com")
	_ = userID1

	// User 1 creates a project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "user1-project"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// User 1 should see their project
	w = doRequest(srv, "GET", "/v1/projects", token1, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var projects1 []ProjectResponse
	json.NewDecoder(w.Body).Decode(&projects1)
	if len(projects1) != 1 {
		t.Fatalf("expected 1 project for user1, got %d", len(projects1))
	}

	// User 2 should see no projects
	w = doRequest(srv, "GET", "/v1/projects", token2, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var projects2 []ProjectResponse
	json.NewDecoder(w.Body).Decode(&projects2)
	if len(projects2) != 0 {
		t.Fatalf("expected 0 projects for user2, got %d", len(projects2))
	}
}

func TestAddMember(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	user2ID, _ := createTestUser(t, store, "member@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "member-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add member
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID,
		Role:   "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var memberResp MemberResponse
	json.NewDecoder(w.Body).Decode(&memberResp)
	if memberResp.Role != "writer" {
		t.Fatalf("expected role writer, got %s", memberResp.Role)
	}
}

func TestMemberRoleEnforcement(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner2@test.com")
	user2ID, token2 := createTestUser(t, store, "writer@test.com")
	user3ID, _ := createTestUser(t, store, "reader@test.com")

	// Owner creates project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "role-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add user2 as writer
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID, Role: "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add writer: expected 201, got %d", w.Code)
	}

	// Writer tries to add a member (should fail, needs owner)
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token2, AddMemberRequest{
		UserID: user3ID, Role: "reader",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("writer adding member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListMembers(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	user2ID, _ := createTestUser(t, store, "member@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "list-members-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add member
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID, Role: "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d", w.Code)
	}

	// List members
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var members []MemberResponse
	json.NewDecoder(w.Body).Decode(&members)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// Verify roles
	roles := map[string]string{}
	for _, m := range members {
		roles[m.UserID] = m.Role
	}
	if roles[user2ID] != "writer" {
		t.Fatalf("expected user2 to be writer, got %s", roles[user2ID])
	}
}

func TestUpdateMemberRole(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	user2ID, _ := createTestUser(t, store, "member@test.com")

	// Create project and add member as reader
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "update-role-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID, Role: "reader",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", w.Code)
	}

	// Update role to writer
	w = doRequest(srv, "PATCH", fmt.Sprintf("/v1/projects/%s/members/%s", project.ID, user2ID), token1, UpdateMemberRequest{
		Role: "writer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update role: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify by listing
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, nil)
	var members []MemberResponse
	json.NewDecoder(w.Body).Decode(&members)

	for _, m := range members {
		if m.UserID == user2ID && m.Role != "writer" {
			t.Fatalf("expected writer, got %s", m.Role)
		}
	}
}

func TestRemoveMember(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	user2ID, _ := createTestUser(t, store, "member@test.com")

	// Create project and add member
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "remove-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID, Role: "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", w.Code)
	}

	// Remove member
	w = doRequest(srv, "DELETE", fmt.Sprintf("/v1/projects/%s/members/%s", project.ID, user2ID), token1, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify removed by listing
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, nil)
	var members []MemberResponse
	json.NewDecoder(w.Body).Decode(&members)
	if len(members) != 1 {
		t.Fatalf("expected 1 member after removal, got %d", len(members))
	}
}

func TestPushWithWriterSucceeds(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	_, token2 := createTestUser(t, store, "writer@test.com")
	user2ID, _ := store.GetUserByEmail("writer@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "push-writer-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add user2 as writer
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2ID.ID, Role: "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add writer: expected 201, got %d", w.Code)
	}

	// Writer pushes events
	pushBody := PushRequest{
		DeviceID:  "dev2",
		SessionID: "sess2",
		Events: []EventInput{
			{
				ClientActionID:  1,
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        "i_writer_001",
				Payload:         json.RawMessage(`{"title":"from writer"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			},
		},
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token2, pushBody)
	if w.Code != http.StatusOK {
		t.Fatalf("writer push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pushResp PushResponse
	json.NewDecoder(w.Body).Decode(&pushResp)
	if pushResp.Accepted != 1 {
		t.Fatalf("expected 1 accepted, got %d", pushResp.Accepted)
	}
}

func TestPushRateLimit(t *testing.T) {
	srv, store := newTestServerWithConfig(t, func(cfg *Config) {
		cfg.RateLimitPush = 60
	})
	_, token := createTestUser(t, store, "ratelimit@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "ratelimit-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	pushURL := fmt.Sprintf("/v1/projects/%s/sync/push", project.ID)

	// Send 61 push requests; first 60 should succeed, 61st should be 429
	for i := 1; i <= 61; i++ {
		pushBody := PushRequest{
			DeviceID:  fmt.Sprintf("dev-rl-%d", i),
			SessionID: fmt.Sprintf("sess-rl-%d", i),
			Events: []EventInput{
				{
					ClientActionID:  int64(i),
					ActionType:      "create",
					EntityType:      "issues",
					EntityID:        fmt.Sprintf("i_rl_%03d", i),
					Payload:         json.RawMessage(`{"title":"rate limit test"}`),
					ClientTimestamp: "2025-01-01T00:00:00Z",
				},
			},
		}

		w = doRequest(srv, "POST", pushURL, token, pushBody)

		if i <= 60 {
			if w.Code != http.StatusOK {
				t.Fatalf("push %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("push %d: expected 429 (rate limited), got %d: %s", i, w.Code, w.Body.String())
			}
		}
	}
}

func TestLongSessionPagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "pagination@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "pagination-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	pushURL := fmt.Sprintf("/v1/projects/%s/sync/push", project.ID)
	pullURL := fmt.Sprintf("/v1/projects/%s/sync/pull", project.ID)

	// Push 5000 events in batches of 1000 (maxPushBatch)
	totalEvents := 5000
	batchSize := 1000
	for batch := 0; batch < totalEvents/batchSize; batch++ {
		events := make([]EventInput, batchSize)
		for i := range events {
			idx := batch*batchSize + i + 1
			events[i] = EventInput{
				ClientActionID:  int64(idx),
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        fmt.Sprintf("i_pg_%05d", idx),
				Payload:         json.RawMessage(`{"title":"pagination"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			}
		}

		w = doRequest(srv, "POST", pushURL, token, PushRequest{
			DeviceID:  "dev-pg",
			SessionID: "sess-pg",
			Events:    events,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("push batch %d: expected 200, got %d: %s", batch, w.Code, w.Body.String())
		}
		var pushResp PushResponse
		json.NewDecoder(w.Body).Decode(&pushResp)
		if pushResp.Accepted != batchSize {
			t.Fatalf("push batch %d: expected %d accepted, got %d", batch, batchSize, pushResp.Accepted)
		}
	}

	// Pull with limit=1000, paginating with after_server_seq cursor
	var allPulled []PullEvent
	afterSeq := int64(0)
	pageLimit := 1000
	pages := 0

	for {
		url := fmt.Sprintf("%s?after_server_seq=%d&limit=%d", pullURL, afterSeq, pageLimit)
		w = doRequest(srv, "GET", url, token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("pull page %d: expected 200, got %d: %s", pages, w.Code, w.Body.String())
		}

		var pullResp PullResponse
		json.NewDecoder(w.Body).Decode(&pullResp)

		if len(pullResp.Events) == 0 {
			break
		}

		allPulled = append(allPulled, pullResp.Events...)
		afterSeq = pullResp.LastServerSeq
		pages++

		if pages < totalEvents/pageLimit {
			// Intermediate pages should have HasMore=true
			if !pullResp.HasMore {
				t.Fatalf("page %d: expected has_more=true", pages)
			}
		}

		if !pullResp.HasMore {
			break
		}
	}

	// Verify total pulled equals total pushed
	if len(allPulled) != totalEvents {
		t.Fatalf("expected %d events total, got %d", totalEvents, len(allPulled))
	}

	// Verify server_seqs are sequential with no gaps or duplicates
	seenSeqs := make(map[int64]bool)
	for i, ev := range allPulled {
		expectedSeq := int64(i + 1)
		if ev.ServerSeq != expectedSeq {
			t.Fatalf("event %d: expected server_seq %d, got %d", i, expectedSeq, ev.ServerSeq)
		}
		if seenSeqs[ev.ServerSeq] {
			t.Fatalf("duplicate server_seq %d", ev.ServerSeq)
		}
		seenSeqs[ev.ServerSeq] = true
	}
}

func TestPushWithReaderFails403(t *testing.T) {
	srv, store := newTestServer(t)
	_, token1 := createTestUser(t, store, "owner@test.com")
	_, token2 := createTestUser(t, store, "reader@test.com")
	user2, _ := store.GetUserByEmail("reader@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token1, CreateProjectRequest{Name: "push-reader-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add user2 as reader
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), token1, AddMemberRequest{
		UserID: user2.ID, Role: "reader",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add reader: expected 201, got %d", w.Code)
	}

	// Reader tries to push
	pushBody := PushRequest{
		DeviceID:  "dev2",
		SessionID: "sess2",
		Events: []EventInput{
			{
				ClientActionID:  1,
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        "i_reader_001",
				Payload:         json.RawMessage(`{"title":"from reader"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			},
		},
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token2, pushBody)
	if w.Code != http.StatusForbidden {
		t.Fatalf("reader push: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPullExcludeClient(t *testing.T) {
	srv, store := newTestServer(t)
	_, tokenA := createTestUser(t, store, "userA@test.com")
	_, tokenB := createTestUser(t, store, "userB@test.com")

	// Create project as user A
	w := doRequest(srv, "POST", "/v1/projects", tokenA, CreateProjectRequest{Name: "exclude-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Add user B as writer
	userB, _ := store.GetUserByEmail("userB@test.com")
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/members", project.ID), tokenA, AddMemberRequest{
		UserID: userB.ID, Role: "writer",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	deviceA := "device-A-excl"
	deviceB := "device-B-excl"

	// User A pushes 3 events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), tokenA, PushRequest{
		DeviceID: deviceA, SessionID: "sess-A",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_A1", Payload: json.RawMessage(`{"title":"A1"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_A2", Payload: json.RawMessage(`{"title":"A2"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "i_A3", Payload: json.RawMessage(`{"title":"A3"}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push A: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// User B pushes 2 events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), tokenB, PushRequest{
		DeviceID: deviceB, SessionID: "sess-B",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_B1", Payload: json.RawMessage(`{"title":"B1"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_B2", Payload: json.RawMessage(`{"title":"B2"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push B: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Pull with exclude_client=deviceA — should only get B's events
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/pull?after_server_seq=0&exclude_client=%s", project.ID, deviceA), tokenA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pull exclude A: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pullExcl PullResponse
	json.NewDecoder(w.Body).Decode(&pullExcl)

	if len(pullExcl.Events) != 2 {
		t.Fatalf("pull exclude A: expected 2 events (B's only), got %d", len(pullExcl.Events))
	}
	for _, ev := range pullExcl.Events {
		if ev.DeviceID == deviceA {
			t.Fatalf("pull exclude A: found event from device A (should be excluded): %s", ev.EntityID)
		}
		if ev.DeviceID != deviceB {
			t.Fatalf("pull exclude A: unexpected device_id %q", ev.DeviceID)
		}
	}

	// Pull without exclude_client — should get all 5 events
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/pull?after_server_seq=0", project.ID), tokenA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pull all: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pullAll PullResponse
	json.NewDecoder(w.Body).Decode(&pullAll)

	if len(pullAll.Events) != 5 {
		t.Fatalf("pull all: expected 5 events, got %d", len(pullAll.Events))
	}

	// Verify we have events from both devices
	deviceCounts := map[string]int{}
	for _, ev := range pullAll.Events {
		deviceCounts[ev.DeviceID]++
	}
	if deviceCounts[deviceA] != 3 {
		t.Fatalf("pull all: expected 3 events from device A, got %d", deviceCounts[deviceA])
	}
	if deviceCounts[deviceB] != 2 {
		t.Fatalf("pull all: expected 2 events from device B, got %d", deviceCounts[deviceB])
	}
}

func TestSnapshotEndpoint(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Snapshot with no events should 404
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("empty snapshot: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Push 3 events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"one","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"two","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "update", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"updated","status":"closed"}}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get snapshot
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check headers
	ct := w.Header().Get("Content-Type")
	if ct != "application/x-sqlite3" {
		t.Fatalf("Content-Type: got %q, want application/x-sqlite3", ct)
	}
	seqStr := w.Header().Get("X-Snapshot-Seq")
	if seqStr == "" {
		t.Fatal("missing X-Snapshot-Seq header")
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil || seq < 3 {
		t.Fatalf("X-Snapshot-Seq: got %q (parsed %d), want >= 3", seqStr, seq)
	}

	// Verify body is a valid SQLite database
	body := w.Body.Bytes()
	if len(body) < 100 {
		t.Fatalf("snapshot body too small: %d bytes", len(body))
	}
	// SQLite magic: "SQLite format 3\000"
	if string(body[:15]) != "SQLite format 3" {
		t.Fatal("snapshot body is not a valid SQLite database")
	}
}

func TestSnapshotEmpty404(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap-empty@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-empty"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Snapshot with no events should return 404
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("empty snapshot: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var errResp ErrorResponse
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "no_events" {
		t.Fatalf("expected error code 'no_events', got %q", errResp.Error.Code)
	}
}

func TestSnapshotValidSQLiteWithTables(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap-tables@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-tables"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push events covering multiple entity types
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"one","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "boards", EntityID: "b_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"name":"board1"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get snapshot
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Write snapshot to temp file and open it
	body := w.Body.Bytes()
	if string(body[:15]) != "SQLite format 3" {
		t.Fatal("snapshot body is not a valid SQLite database")
	}

	tmpFile := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(tmpFile, body, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	db, err := openSnapshotDB(tmpFile)
	if err != nil {
		t.Fatalf("open snapshot db: %v", err)
	}
	defer db.Close()

	// Verify all required tables exist
	requiredTables := []string{
		"issues", "boards", "board_issue_positions",
		"issue_session_history", "sync_state", "sync_conflicts", "action_log",
	}
	for _, table := range requiredTables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found in snapshot: %v", table, err)
		}
	}
}

func TestSnapshotBoardPositionReplay(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap-board@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-board"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push board + position events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "boards", EntityID: "b_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"name":"sprint-1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "board_issue_positions", EntityID: "bp_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"board_id":"b_001","issue_id":"i_001","position":1}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "board_issue_positions", EntityID: "bp_002",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"board_id":"b_001","issue_id":"i_002","position":2}}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get snapshot
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	tmpFile := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(tmpFile, w.Body.Bytes(), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	db, err := openSnapshotDB(tmpFile)
	if err != nil {
		t.Fatalf("open snapshot db: %v", err)
	}
	defer db.Close()

	// Verify board_issue_positions were replayed
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM board_issue_positions").Scan(&count); err != nil {
		t.Fatalf("count board_issue_positions: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 board_issue_positions, got %d", count)
	}

	// Verify position data
	var boardID, issueID string
	var position int
	err = db.QueryRow("SELECT board_id, issue_id, position FROM board_issue_positions WHERE id = ?", "bp_001").Scan(&boardID, &issueID, &position)
	if err != nil {
		t.Fatalf("query bp_001: %v", err)
	}
	if boardID != "b_001" || issueID != "i_001" || position != 1 {
		t.Fatalf("bp_001: got board_id=%s issue_id=%s position=%d, want b_001/i_001/1", boardID, issueID, position)
	}
}

func TestSnapshotXSnapshotEventIdHeader(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap-header@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-header"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push 5 events
	events := make([]EventInput, 5)
	for i := range events {
		events[i] = EventInput{
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("i_%03d", i+1),
			Payload:         json.RawMessage(`{"schema_version":1,"new_data":{"title":"test","status":"open"}}`),
			ClientTimestamp: "2025-01-01T00:00:00Z",
		}
	}

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1", Events: events,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pushResp PushResponse
	json.NewDecoder(w.Body).Decode(&pushResp)

	// Get the max server_seq from push acks
	maxSeq := pushResp.Acks[len(pushResp.Acks)-1].ServerSeq

	// Get snapshot
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify X-Snapshot-Seq matches the max server_seq
	seqStr := w.Header().Get("X-Snapshot-Seq")
	if seqStr == "" {
		t.Fatal("missing X-Snapshot-Seq header")
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		t.Fatalf("parse X-Snapshot-Seq: %v", err)
	}
	if seq != maxSeq {
		t.Fatalf("X-Snapshot-Seq: got %d, want %d (max server_seq)", seq, maxSeq)
	}
}

func TestSnapshotCaching(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "snap-cache@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "snap-cache"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"cache-test","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// First snapshot request (cache miss - builds snapshot)
	w1 := doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w1.Code != http.StatusOK {
		t.Fatalf("snapshot 1: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	body1 := w1.Body.Bytes()
	seq1 := w1.Header().Get("X-Snapshot-Seq")

	// Second snapshot request (should serve from cache)
	w2 := doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), token, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("snapshot 2: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	body2 := w2.Body.Bytes()
	seq2 := w2.Header().Get("X-Snapshot-Seq")

	// Same event ID
	if seq1 != seq2 {
		t.Fatalf("X-Snapshot-Seq mismatch: %s vs %s", seq1, seq2)
	}

	// Same content (cached file should be byte-identical)
	if len(body1) != len(body2) {
		t.Fatalf("snapshot size mismatch: %d vs %d", len(body1), len(body2))
	}

	// Verify cache file exists on disk
	cacheDir := filepath.Join(srv.config.ProjectDataDir, "snapshots", project.ID)
	cachePath := filepath.Join(cacheDir, seq1+".db")
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("cache file not found at %s", cachePath)
	}
}

// openSnapshotDB opens a snapshot SQLite file for verification.
func openSnapshotDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return db, nil
}

func TestSyncStatus(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "status@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "status-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Check status before any events
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/status", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", w.Code)
	}
	var status SyncStatusResponse
	json.NewDecoder(w.Body).Decode(&status)
	if status.EventCount != 0 {
		t.Fatalf("expected 0 events, got %d", status.EventCount)
	}

	// Push 3 events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001", Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "logs", EntityID: "l_001", Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "comments", EntityID: "c_001", Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Check status after push
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/status", project.ID), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", w.Code)
	}
	json.NewDecoder(w.Body).Decode(&status)
	if status.EventCount != 3 {
		t.Fatalf("expected 3 events, got %d", status.EventCount)
	}
	if status.LastServerSeq < 3 {
		t.Fatalf("expected last_server_seq >= 3, got %d", status.LastServerSeq)
	}
}

func TestPushRejectsOversizedBatch(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "oversize@test.com")

	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "oversize-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	pushURL := fmt.Sprintf("/v1/projects/%s/sync/push", project.ID)

	// Build a batch of 1001 events (one over the limit)
	events := make([]EventInput, 1001)
	for i := range events {
		events[i] = EventInput{
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("i_over_%05d", i+1),
			Payload:         json.RawMessage(`{"title":"oversize"}`),
			ClientTimestamp: "2025-01-01T00:00:00Z",
		}
	}

	w = doRequest(srv, "POST", pushURL, token, PushRequest{
		DeviceID:  "dev-over",
		SessionID: "sess-over",
		Events:    events,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized batch, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly 1000 should succeed
	events = events[:1000]
	w = doRequest(srv, "POST", pushURL, token, PushRequest{
		DeviceID:  "dev-over",
		SessionID: "sess-over",
		Events:    events,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for 1000-event batch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPushBatchedClientSimulation(t *testing.T) {
	// Simulates client-side batching: 1500 events pushed in 3 batches of 500.
	// Verifies acks accumulate correctly and all events are pullable.
	srv, store := newTestServer(t)
	_, token := createTestUser(t, store, "batch@test.com")

	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "batch-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	pushURL := fmt.Sprintf("/v1/projects/%s/sync/push", project.ID)
	pullURL := fmt.Sprintf("/v1/projects/%s/sync/pull", project.ID)

	totalEvents := 1500
	batchSize := 500 // matches pushBatchSize in cmd/sync.go

	// Build all events
	allEvents := make([]EventInput, totalEvents)
	for i := range allEvents {
		allEvents[i] = EventInput{
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("i_batch_%05d", i+1),
			Payload:         json.RawMessage(`{"title":"batched"}`),
			ClientTimestamp: "2025-01-01T00:00:00Z",
		}
	}

	// Push in batches (simulating client batching logic)
	var totalAccepted int
	var allAcks []AckResponse
	batchCount := 0

	for i := 0; i < len(allEvents); i += batchSize {
		end := i + batchSize
		if end > len(allEvents) {
			end = len(allEvents)
		}
		batch := allEvents[i:end]

		w = doRequest(srv, "POST", pushURL, token, PushRequest{
			DeviceID:  "dev-batch",
			SessionID: "sess-batch",
			Events:    batch,
		})
		if w.Code != http.StatusOK {
			t.Fatalf("push batch %d: expected 200, got %d: %s", batchCount, w.Code, w.Body.String())
		}

		var pushResp PushResponse
		json.NewDecoder(w.Body).Decode(&pushResp)
		totalAccepted += pushResp.Accepted
		allAcks = append(allAcks, pushResp.Acks...)
		batchCount++
	}

	// Verify batch count
	expectedBatches := (totalEvents + batchSize - 1) / batchSize
	if batchCount != expectedBatches {
		t.Fatalf("expected %d batches, got %d", expectedBatches, batchCount)
	}

	// Verify total accepted
	if totalAccepted != totalEvents {
		t.Fatalf("expected %d accepted, got %d", totalEvents, totalAccepted)
	}

	// Verify acks cover all events
	if len(allAcks) != totalEvents {
		t.Fatalf("expected %d acks, got %d", totalEvents, len(allAcks))
	}

	// Verify server_seqs in acks are sequential
	for i, ack := range allAcks {
		expectedSeq := int64(i + 1)
		if ack.ServerSeq != expectedSeq {
			t.Fatalf("ack %d: expected server_seq %d, got %d", i, expectedSeq, ack.ServerSeq)
		}
	}

	// Pull all events back and verify count
	var allPulled []PullEvent
	afterSeq := int64(0)
	for {
		url := fmt.Sprintf("%s?after_server_seq=%d&limit=1000", pullURL, afterSeq)
		w = doRequest(srv, "GET", url, token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("pull: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var pullResp PullResponse
		json.NewDecoder(w.Body).Decode(&pullResp)
		if len(pullResp.Events) == 0 {
			break
		}
		allPulled = append(allPulled, pullResp.Events...)
		afterSeq = pullResp.LastServerSeq
		if !pullResp.HasMore {
			break
		}
	}

	if len(allPulled) != totalEvents {
		t.Fatalf("expected %d pulled events, got %d", totalEvents, len(allPulled))
	}
}
