package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAdminServerOverview(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Create some data for counts
	u2, err := store.CreateUser("user2@test.com")
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}
	_, err = store.CreateProject("proj1", "desc", u2.ID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	w := doRequest(srv, "GET", "/v1/admin/server/overview", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverOverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.UptimeSeconds <= 0 {
		t.Fatalf("expected uptime > 0, got %f", resp.UptimeSeconds)
	}
	if resp.Health != "ok" {
		t.Fatalf("expected health ok, got %q", resp.Health)
	}
	// admin@test.com (auto-admin) + user2@test.com = 2 users
	if resp.TotalUsers < 2 {
		t.Fatalf("expected >= 2 users, got %d", resp.TotalUsers)
	}
	if resp.TotalProjects < 1 {
		t.Fatalf("expected >= 1 projects, got %d", resp.TotalProjects)
	}
	// CreateProject adds owner as member
	if resp.TotalMembers < 1 {
		t.Fatalf("expected >= 1 members, got %d", resp.TotalMembers)
	}
}

func TestAdminServerOverview_RequiresAdmin(t *testing.T) {
	srv, store := newTestServer(t)
	// First user is auto-admin; create a second non-admin user
	_, _ = store.CreateUser("first@test.com")
	_, token := createTestUser(t, store, "nonadmin@test.com")

	w := doRequest(srv, "GET", "/v1/admin/server/overview", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminServerOverview_RequiresScope(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects")

	w := doRequest(srv, "GET", "/v1/admin/server/overview", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "insufficient_admin_scope" {
		t.Fatalf("expected insufficient_admin_scope, got %q", resp.Error.Code)
	}
}

func TestAdminServerConfig(t *testing.T) {
	srv, store := newTestServerWithConfig(t, func(cfg *Config) {
		cfg.AllowSignup = true
		cfg.LogLevel = "debug"
		cfg.LogFormat = "text"
		cfg.CORSAllowedOrigins = []string{"https://example.com"}
	})
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	w := doRequest(srv, "GET", "/v1/admin/server/config", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp serverConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.AllowSignup != true {
		t.Fatalf("expected allow_signup true, got %v", resp.AllowSignup)
	}
	if resp.LogLevel != "debug" {
		t.Fatalf("expected log_level debug, got %q", resp.LogLevel)
	}
	if resp.LogFormat != "text" {
		t.Fatalf("expected log_format text, got %q", resp.LogFormat)
	}
	if resp.RateLimits.Auth == 0 {
		t.Fatal("expected rate_limits.auth > 0")
	}
	if resp.RateLimits.Push == 0 {
		t.Fatal("expected rate_limits.push > 0")
	}
	if resp.RateLimits.Pull == 0 {
		t.Fatal("expected rate_limits.pull > 0")
	}
	if resp.RateLimits.Other == 0 {
		t.Fatal("expected rate_limits.other > 0")
	}
	if len(resp.CORSOrigins) != 1 || resp.CORSOrigins[0] != "https://example.com" {
		t.Fatalf("expected cors_origins [https://example.com], got %v", resp.CORSOrigins)
	}
}

func TestAdminRateLimitViolations(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Insert some rate limit events
	for i := 0; i < 3; i++ {
		if err := store.InsertRateLimitEvent(fmt.Sprintf("key_%d", i), "192.168.1.1", "push"); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}

	w := doRequest(srv, "GET", "/v1/admin/server/rate-limit-violations", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var data []map[string]any
	if err := json.Unmarshal(resp["data"], &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(data) != 3 {
		t.Fatalf("expected 3 events, got %d", len(data))
	}
}

func TestAdminRateLimitViolations_WithFilters(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Insert events with different IPs
	for i := 0; i < 3; i++ {
		if err := store.InsertRateLimitEvent("key_a", "10.0.0.1", "auth"); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := store.InsertRateLimitEvent("key_b", "10.0.0.2", "push"); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}

	// Filter by IP
	w := doRequest(srv, "GET", "/v1/admin/server/rate-limit-violations?ip=10.0.0.1", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	_ = json.NewDecoder(w.Body).Decode(&resp)
	var data []map[string]any
	_ = json.Unmarshal(resp["data"], &data)
	if len(data) != 3 {
		t.Fatalf("expected 3 events for ip 10.0.0.1, got %d", len(data))
	}

	// Filter by key_id
	w = doRequest(srv, "GET", "/v1/admin/server/rate-limit-violations?key_id=key_b", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_ = json.NewDecoder(w.Body).Decode(&resp)
	_ = json.Unmarshal(resp["data"], &data)
	if len(data) != 2 {
		t.Fatalf("expected 2 events for key_b, got %d", len(data))
	}
}

func TestAdminRateLimitViolations_Pagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	// Insert 60 events (more than default page size of 50)
	for i := 0; i < 60; i++ {
		if err := store.InsertRateLimitEvent("key_pg", "192.168.1.1", "other"); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	// First page with limit=50
	w := doRequest(srv, "GET", "/v1/admin/server/rate-limit-violations?limit=50", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page1 struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}

	if len(page1.Data) != 50 {
		t.Fatalf("expected 50 events on page 1, got %d", len(page1.Data))
	}
	if !page1.HasMore {
		t.Fatal("expected has_more=true on page 1")
	}
	if page1.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor on page 1")
	}

	// Second page using cursor
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/server/rate-limit-violations?limit=50&cursor=%s", page1.NextCursor), token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page2 struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}

	if len(page2.Data) != 10 {
		t.Fatalf("expected 10 events on page 2, got %d", len(page2.Data))
	}
	if page2.HasMore {
		t.Fatal("expected has_more=false on page 2")
	}
}
