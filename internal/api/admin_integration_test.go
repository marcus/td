package api

import (
	"encoding/json"
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

// ---------------------------------------------------------------------------
// Snapshot Meta (td-69e6f0)
// ---------------------------------------------------------------------------

func TestIntegration_SnapshotMeta_EmptyProject(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")

	var resp snapshotMetaResponse
	h.DoJSON("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid), token, nil, &resp)

	if resp.SnapshotSeq != 0 {
		t.Fatalf("expected snapshot_seq=0, got %d", resp.SnapshotSeq)
	}
	if resp.HeadSeq != 0 {
		t.Fatalf("expected head_seq=0, got %d", resp.HeadSeq)
	}
	if resp.Staleness != 0 {
		t.Fatalf("expected staleness=0, got %d", resp.Staleness)
	}
}

func TestIntegration_SnapshotMeta_WithEvents(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		WithEvents("proj1", "user@test.com", 6).
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")

	var resp snapshotMetaResponse
	h.DoJSON("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid), token, nil, &resp)

	if resp.SnapshotSeq != 0 {
		t.Fatalf("expected snapshot_seq=0 (no snapshot built), got %d", resp.SnapshotSeq)
	}
	if resp.HeadSeq < 1 {
		t.Fatalf("expected head_seq>0, got %d", resp.HeadSeq)
	}
	if resp.Staleness != resp.HeadSeq {
		t.Fatalf("expected staleness=%d (=head_seq), got %d", resp.HeadSeq, resp.Staleness)
	}
}

func TestIntegration_SnapshotMeta_WithSnapshot(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	// Push issues with explicit payloads so snapshot has real entities
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")
	userToken := state.UserToken("user@test.com")

	// Push 3 issue-creation events with full payloads
	h.PushEvents(userToken, pid, []EventInput{
		{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-smws001",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"one","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "td-smws002",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"two","status":"open","type":"task","priority":"P2"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "td-smws003",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"three","status":"closed","type":"bug","priority":"P3"}}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
	})

	// Build the snapshot
	h.BuildSnapshot(userToken, pid)

	var resp snapshotMetaResponse
	h.DoJSON("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid), token, nil, &resp)

	if resp.SnapshotSeq == 0 {
		t.Fatalf("expected snapshot_seq>0 after building snapshot, got 0")
	}
	if resp.SnapshotSeq != resp.HeadSeq {
		t.Fatalf("expected snapshot_seq(%d)==head_seq(%d)", resp.SnapshotSeq, resp.HeadSeq)
	}
	if resp.Staleness != 0 {
		t.Fatalf("expected staleness=0, got %d", resp.Staleness)
	}
	if resp.EntityCounts["issues"] != 3 {
		t.Fatalf("expected 3 issues in entity_counts, got %d", resp.EntityCounts["issues"])
	}
}

func TestIntegration_SnapshotMeta_ScopeEnforcement(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:server,sync"). // wrong scope
		WithProject("proj1", "user@test.com").
		Done()

	pid := state.ProjectID("proj1")
	token := state.AdminToken("admin@test.com")

	h.AssertRequiresAdminScope(t, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid), token)
}

// ---------------------------------------------------------------------------
// Snapshot Query (td-f7848d)
// ---------------------------------------------------------------------------

func TestIntegration_SnapshotQuery_StatusFilter(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")
	userToken := state.UserToken("user@test.com")

	// Push issues with mixed statuses: 3 open, 2 closed
	h.PushEvents(userToken, pid, []EventInput{
		{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-sqsf001",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"open1","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "td-sqsf002",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"closed1","status":"closed","type":"bug","priority":"P2"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "td-sqsf003",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"open2","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		{ClientActionID: 4, ActionType: "create", EntityType: "issues", EntityID: "td-sqsf004",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"closed2","status":"closed","type":"task","priority":"P3"}}`), ClientTimestamp: "2025-01-01T00:00:03Z"},
		{ClientActionID: 5, ActionType: "create", EntityType: "issues", EntityID: "td-sqsf005",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"open3","status":"open","type":"feature","priority":"P2"}}`), ClientTimestamp: "2025-01-01T00:00:04Z"},
	})

	// Query for open issues (snapshot auto-built)
	var resp snapshotQueryResponse
	h.DoJSON("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open", pid), token, nil, &resp)

	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 open issues, got %d", len(resp.Data))
	}
	for _, issue := range resp.Data {
		if issue.Status != "open" {
			t.Fatalf("expected all results to be open, got status=%q for %s", issue.Status, issue.ID)
		}
	}
}

func TestIntegration_SnapshotQuery_Pagination(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")
	userToken := state.UserToken("user@test.com")

	// Push 5 open issues
	events := make([]EventInput, 5)
	for i := 0; i < 5; i++ {
		events[i] = EventInput{
			ClientActionID: int64(i + 1),
			ActionType:     "create",
			EntityType:     "issues",
			EntityID:       fmt.Sprintf("td-sqpg%04d", i+1),
			Payload: json.RawMessage(fmt.Sprintf(
				`{"schema_version":1,"new_data":{"title":"issue %d","status":"open","type":"task","priority":"P1"}}`, i+1)),
			ClientTimestamp: fmt.Sprintf("2025-01-01T00:00:%02dZ", i),
		}
	}
	h.PushEvents(userToken, pid, events)

	// Page 1: limit=2
	resp1 := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2", pid), token, nil)
	page1 := ReadJSON[snapshotQueryResponse](t, resp1)
	if len(page1.Data) != 2 {
		t.Fatalf("page1: expected 2 issues, got %d", len(page1.Data))
	}
	if !page1.HasMore {
		t.Fatalf("page1: expected has_more=true")
	}
	if page1.NextCursor == nil {
		t.Fatalf("page1: expected non-nil next_cursor")
	}

	// Page 2: use cursor
	resp2 := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2&cursor=%s", pid, *page1.NextCursor), token, nil)
	page2 := ReadJSON[snapshotQueryResponse](t, resp2)
	if len(page2.Data) != 2 {
		t.Fatalf("page2: expected 2 issues, got %d", len(page2.Data))
	}
	if !page2.HasMore {
		t.Fatalf("page2: expected has_more=true")
	}

	// Page 3: last page
	resp3 := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2&cursor=%s", pid, *page2.NextCursor), token, nil)
	page3 := ReadJSON[snapshotQueryResponse](t, resp3)
	if len(page3.Data) != 1 {
		t.Fatalf("page3: expected 1 issue, got %d", len(page3.Data))
	}
	if page3.HasMore {
		t.Fatalf("page3: expected has_more=false")
	}
	if page3.NextCursor != nil {
		t.Fatalf("page3: expected nil next_cursor")
	}

	// Verify no duplicate IDs across pages
	seen := map[string]bool{}
	for _, page := range []snapshotQueryResponse{page1, page2, page3} {
		for _, issue := range page.Data {
			if seen[issue.ID] {
				t.Fatalf("duplicate issue %s across pages", issue.ID)
			}
			seen[issue.ID] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("expected 5 unique issues across all pages, got %d", len(seen))
	}
}

func TestIntegration_SnapshotQuery_EmptyProject(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")

	resp := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open", pid), token, nil)
	AssertErrorResponse(t, resp, http.StatusNotFound, ErrCodeSnapshotUnavailable)
}

func TestIntegration_SnapshotQuery_InvalidTDQ(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")
	userToken := state.UserToken("user@test.com")

	// Push at least one event so the project has data (otherwise we get snapshot_unavailable)
	h.PushEvents(userToken, pid, []EventInput{
		{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-sqiq001",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"test","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
	})

	// Send invalid TDQ syntax (double equals is not valid)
	resp := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D%%3D+open", pid), token, nil)
	AssertErrorResponse(t, resp, http.StatusBadRequest, ErrCodeInvalidQuery)
}

func TestIntegration_SnapshotQuery_ScopeEnforcement(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:server,sync"). // wrong scope
		WithProject("proj1", "user@test.com").
		Done()

	pid := state.ProjectID("proj1")
	token := state.AdminToken("admin@test.com")

	h.AssertRequiresAdminScope(t, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open", pid), token)
}

// ---------------------------------------------------------------------------
// Error Code Consistency (td-5b8076)
// ---------------------------------------------------------------------------

func TestIntegration_ErrorCodes_NotFound(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,admin:read:projects,admin:read:events,admin:read:snapshots,sync").
		Done()

	token := state.AdminToken("admin@test.com")
	fakeID := "nonexistent-project-id"

	// All project-scoped endpoints should return not_found for a nonexistent project ID
	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", fmt.Sprintf("/v1/admin/projects/%s", fakeID)},
		{"GET", fmt.Sprintf("/v1/admin/projects/%s/members", fakeID)},
		{"GET", fmt.Sprintf("/v1/admin/projects/%s/sync/status", fakeID)},
		{"GET", fmt.Sprintf("/v1/admin/projects/%s/events", fakeID)},
		{"GET", fmt.Sprintf("/v1/admin/projects/%s/events/1", fakeID)},
		{"GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", fakeID)},
	}

	for _, ep := range endpoints {
		resp := h.Do(ep.method, ep.path, token, nil)
		AssertErrorResponse(t, resp, http.StatusNotFound, ErrCodeNotFound)
	}

	// Also check user not found
	resp := h.Do("GET", "/v1/admin/users/nonexistent-user-id", token, nil)
	AssertErrorResponse(t, resp, http.StatusNotFound, ErrCodeNotFound)
}

func TestIntegration_ErrorCodes_BadRequest(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("admin@test.com", "admin:read:snapshots,admin:read:events,sync").
		WithProject("proj1", "user@test.com").
		Done()

	token := state.AdminToken("admin@test.com")
	pid := state.ProjectID("proj1")
	userToken := state.UserToken("user@test.com")

	// Push one event so snapshot query does not return snapshot_unavailable
	h.PushEvents(userToken, pid, []EventInput{
		{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-ecbr001",
			Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"test","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
	})

	// Missing required 'q' param for snapshot query returns invalid_query
	resp := h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query", pid), token, nil)
	AssertErrorResponse(t, resp, http.StatusBadRequest, ErrCodeInvalidQuery)

	// Invalid limit param returns bad_request
	resp = h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=abc", pid), token, nil)
	AssertErrorResponse(t, resp, http.StatusBadRequest, ErrCodeBadRequest)

	// Invalid entity_type filter on events returns bad_request
	resp = h.Do("GET", fmt.Sprintf("/v1/admin/projects/%s/events?entity_type=invalid_entity", pid), token, nil)
	AssertErrorResponse(t, resp, http.StatusBadRequest, ErrCodeBadRequest)
}

func TestIntegration_ErrorCodes_ScopeConsistency(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("user@test.com").
		WithAdmin("server-admin@test.com", "admin:read:server,sync").
		WithAdmin("project-admin@test.com", "admin:read:projects,sync").
		WithAdmin("events-admin@test.com", "admin:read:events,sync").
		WithAdmin("snapshot-admin@test.com", "admin:read:snapshots,sync").
		WithProject("proj1", "user@test.com").
		Done()

	pid := state.ProjectID("proj1")

	// admin:read:server token should be denied for projects, events, and snapshot endpoints
	serverToken := state.AdminToken("server-admin@test.com")
	serverDenied := []string{
		"/v1/admin/projects",
		fmt.Sprintf("/v1/admin/projects/%s/events", pid),
		fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid),
	}
	for _, path := range serverDenied {
		resp := h.Do("GET", path, serverToken, nil)
		AssertErrorResponse(t, resp, http.StatusForbidden, ErrCodeInsufficientAdminScope)
	}

	// admin:read:projects token should be denied for server, events, and snapshot endpoints
	projectToken := state.AdminToken("project-admin@test.com")
	projectDenied := []string{
		"/v1/admin/server/overview",
		fmt.Sprintf("/v1/admin/projects/%s/events", pid),
		fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid),
	}
	for _, path := range projectDenied {
		resp := h.Do("GET", path, projectToken, nil)
		AssertErrorResponse(t, resp, http.StatusForbidden, ErrCodeInsufficientAdminScope)
	}

	// admin:read:events token should be denied for server, projects listing, and snapshots
	eventsToken := state.AdminToken("events-admin@test.com")
	eventsDenied := []string{
		"/v1/admin/server/overview",
		"/v1/admin/projects",
		fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", pid),
	}
	for _, path := range eventsDenied {
		resp := h.Do("GET", path, eventsToken, nil)
		AssertErrorResponse(t, resp, http.StatusForbidden, ErrCodeInsufficientAdminScope)
	}

	// admin:read:snapshots token should be denied for server, projects listing, and events
	snapshotToken := state.AdminToken("snapshot-admin@test.com")
	snapshotDenied := []string{
		"/v1/admin/server/overview",
		"/v1/admin/projects",
		fmt.Sprintf("/v1/admin/projects/%s/events", pid),
	}
	for _, path := range snapshotDenied {
		resp := h.Do("GET", path, snapshotToken, nil)
		AssertErrorResponse(t, resp, http.StatusForbidden, ErrCodeInsufficientAdminScope)
	}
}
