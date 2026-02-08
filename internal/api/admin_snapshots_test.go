package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAdminSnapshotMeta_NoSnapshot(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "snap-user@test.com")

	// Create project via API
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "snap-meta-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push some events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"title":"test"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get snapshot meta (no snapshot built yet)
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp snapshotMetaResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.HeadSeq < 1 {
		t.Fatalf("expected head_seq >= 1, got %d", resp.HeadSeq)
	}
	if resp.SnapshotSeq != 0 {
		t.Fatalf("expected snapshot_seq 0 (no snapshot), got %d", resp.SnapshotSeq)
	}
	if resp.Staleness != resp.HeadSeq {
		t.Fatalf("expected staleness = head_seq (%d), got %d", resp.HeadSeq, resp.Staleness)
	}
}

func TestAdminSnapshotMeta_WithSnapshot(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "snap-with@test.com")

	// Create project via API
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "snap-with-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push events
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"one","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"two","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Build a snapshot (by requesting the sync snapshot endpoint which caches it)
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", project.ID), userToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Now get admin snapshot meta
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/meta", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp snapshotMetaResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.SnapshotSeq < 2 {
		t.Fatalf("expected snapshot_seq >= 2, got %d", resp.SnapshotSeq)
	}
	if resp.HeadSeq < 2 {
		t.Fatalf("expected head_seq >= 2, got %d", resp.HeadSeq)
	}
	if resp.Staleness != 0 {
		t.Fatalf("expected staleness 0 (snapshot is current), got %d", resp.Staleness)
	}
	// Should have entity counts
	if resp.EntityCounts["issues"] != 2 {
		t.Fatalf("expected 2 issues in snapshot, got %d", resp.EntityCounts["issues"])
	}
}

func TestAdminSnapshotMeta_NotFound(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")

	w := doRequest(srv, "GET", "/v1/admin/projects/nonexistent/snapshot/meta", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminSnapshotMeta_RequiresAdmin(t *testing.T) {
	srv, store := newTestServer(t)
	_, _ = store.CreateUser("first@test.com")
	_, token := createTestUser(t, store, "nonadmin@test.com")

	w := doRequest(srv, "GET", "/v1/admin/projects/fake/snapshot/meta", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminSnapshotMeta_RequiresScope(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server")

	w := doRequest(srv, "GET", "/v1/admin/projects/fake/snapshot/meta", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (wrong scope), got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminSnapshotQuery_Basic(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "query-user@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "query-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push issues with different statuses
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-aaa001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"open one","status":"open","type":"task","priority":"P1"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "td-aaa002",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"closed one","status":"closed","type":"bug","priority":"P2"}}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "td-aaa003",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"open two","status":"open","type":"task","priority":"P3"}}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Query for open issues (snapshot will be built on demand)
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("query: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp snapshotQueryResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 open issues, got %d", len(resp.Data))
	}
	if resp.SnapshotSeq < 3 {
		t.Fatalf("expected snapshot_seq >= 3, got %d", resp.SnapshotSeq)
	}
	if resp.HasMore {
		t.Fatalf("expected has_more=false")
	}
	if resp.NextCursor != nil {
		t.Fatalf("expected nil next_cursor")
	}
}

func TestAdminSnapshotQuery_MissingQuery(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "q-missing@test.com")

	// Create project with events
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "q-missing"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query", project.ID), token, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != ErrCodeInvalidQuery {
		t.Fatalf("expected code %q, got %q", ErrCodeInvalidQuery, resp.Error.Code)
	}
}

func TestAdminSnapshotQuery_InvalidQuery(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "q-invalid@test.com")

	// Create project and push events
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "q-invalid"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-bbb001",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"test","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Send invalid TDQ syntax (unrecognized operator)
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D%%3D+open", project.ID), adminToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminSnapshotQuery_ProjectNotFound(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")

	w := doRequest(srv, "GET", "/v1/admin/projects/nonexistent/snapshot/query?q=status+%3D+open", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminSnapshotQuery_NoEvents(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "q-empty@test.com")

	// Create project but push no events
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "q-empty"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open", project.ID), adminToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != ErrCodeSnapshotUnavailable {
		t.Fatalf("expected code %q, got %q", ErrCodeSnapshotUnavailable, resp.Error.Code)
	}
}

func TestAdminSnapshotQuery_Pagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "q-page@test.com")

	// Create project
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "q-page"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push 5 issues
	events := make([]EventInput, 5)
	for i := 0; i < 5; i++ {
		events[i] = EventInput{
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("td-pg%04d", i+1),
			Payload:         json.RawMessage(fmt.Sprintf(`{"schema_version":1,"new_data":{"title":"issue %d","status":"open","type":"task","priority":"P1"}}`, i+1)),
			ClientTimestamp:  fmt.Sprintf("2025-01-01T00:00:%02dZ", i),
		}
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1", Events: events,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Page 1: limit=2
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page1: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page1 snapshotQueryResponse
	json.NewDecoder(w.Body).Decode(&page1)
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
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2&cursor=%s", project.ID, *page1.NextCursor), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page2: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page2 snapshotQueryResponse
	json.NewDecoder(w.Body).Decode(&page2)
	if len(page2.Data) != 2 {
		t.Fatalf("page2: expected 2 issues, got %d", len(page2.Data))
	}
	if !page2.HasMore {
		t.Fatalf("page2: expected has_more=true")
	}

	// Page 3: last page
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=2&cursor=%s", project.ID, *page2.NextCursor), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page3: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var page3 snapshotQueryResponse
	json.NewDecoder(w.Body).Decode(&page3)
	if len(page3.Data) != 1 {
		t.Fatalf("page3: expected 1 issue, got %d", len(page3.Data))
	}
	if page3.HasMore {
		t.Fatalf("page3: expected has_more=false")
	}
	if page3.NextCursor != nil {
		t.Fatalf("page3: expected nil next_cursor")
	}
}

func TestAdminSnapshotQuery_LimitClamped(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:snapshots,sync")
	_, userToken := createTestUser(t, store, "q-clamp@test.com")

	// Create project with one issue
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "q-clamp"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "td-clamp1",
				Payload: json.RawMessage(`{"schema_version":1,"new_data":{"title":"test","status":"open"}}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Request with limit > 200 should be clamped
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=999", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid limit
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/snapshot/query?q=status+%%3D+open&limit=abc", project.ID), adminToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid limit, got %d: %s", w.Code, w.Body.String())
	}
}
