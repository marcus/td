package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

func TestIntegration_AdminServerOverview(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user1@test.com").
		WithUser("user2@test.com").
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithProject("proj1", "user1@test.com").
		Done()

	var overview serverOverviewResponse
	h.DoJSON("GET", "/v1/admin/server/overview", state.AdminToken("admin@test.com"), nil, &overview)

	if overview.Health != "ok" {
		t.Fatalf("expected health ok, got %q", overview.Health)
	}
	// admin + user1 + user2 = 3
	if overview.TotalUsers < 3 {
		t.Fatalf("expected >= 3 users, got %d", overview.TotalUsers)
	}
	if overview.TotalProjects < 1 {
		t.Fatalf("expected >= 1 project, got %d", overview.TotalProjects)
	}
}

func TestIntegration_AdminListProjects_Pagination(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("owner@test.com").
		WithAdmin("admin@test.com", "admin:read:projects,sync").
		WithProject("proj-a", "owner@test.com").
		WithProject("proj-b", "owner@test.com").
		WithProject("proj-c", "owner@test.com").
		Done()

	token := state.AdminToken("admin@test.com")

	// Page 1: limit=2
	resp := h.Do("GET", "/v1/admin/projects?limit=2", token, nil)
	page1 := AssertPaginated[serverdb.AdminProject](t, resp, 2, true)

	// Page 2: use cursor
	resp = h.Do("GET", fmt.Sprintf("/v1/admin/projects?limit=2&cursor=%s", page1.NextCursor), token, nil)
	AssertPaginated[serverdb.AdminProject](t, resp, 1, false)
}

func TestIntegration_AdminProjectEvents_Filter(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:events,sync").
		WithProject("proj1", "user@test.com").
		WithEvents("proj1", "user@test.com", 9). // 3 issues, 3 logs, 3 comments (cycles)
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")

	var events adminEventsResponse
	h.DoJSON("GET", fmt.Sprintf("/v1/admin/projects/%s/events?entity_type=issues", pid), token, nil, &events)

	if len(events.Data) != 3 {
		t.Fatalf("expected 3 issues events, got %d", len(events.Data))
	}
	for _, e := range events.Data {
		if e.EntityType != "issues" {
			t.Fatalf("expected entity_type issues, got %s", e.EntityType)
		}
	}
}

func TestIntegration_AdminScopeEnforcement(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server"). // only server scope
		Done()

	token := state.AdminToken("admin@test.com")

	// Should fail for projects endpoint (needs admin:read:projects)
	h.AssertRequiresAdminScope(t, "GET", "/v1/admin/projects", token)
}

func TestIntegration_CORSHeaders(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t, func(cfg *Config) {
		cfg.CORSAllowedOrigins = []string{"https://admin.example.com"}
	})
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		Done()

	token := state.AdminToken("admin@test.com")

	// Request WITH matching origin
	req, err := http.NewRequest("GET", h.BaseURL+"/v1/admin/server/overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://admin.example.com")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	AssertStatus(t, resp, 200)
	AssertCORSHeaders(t, resp, "https://admin.example.com")

	// Request WITHOUT origin — no CORS headers
	req2, err := http.NewRequest("GET", h.BaseURL+"/v1/admin/server/overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	AssertStatus(t, resp2, 200)
	AssertNoCORSHeaders(t, resp2)
}

func TestIntegration_NonAdminDenied(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	// First user is auto-admin, consume it
	h.CreateUser("first@test.com")
	_, regularToken := h.CreateUser("regular@test.com")

	resp := h.Do("GET", "/v1/admin/server/overview", regularToken, nil)
	AssertErrorResponse(t, resp, http.StatusForbidden, "insufficient_admin_scope")
}
