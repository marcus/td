package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAdminProjectEvents_Basic(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "event-user@test.com")

	// Create project via API
	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "events-test"})
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
				Payload: json.RawMessage(`{"title":"one"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "logs", EntityID: "l_001",
				Payload: json.RawMessage(`{"text":"log"}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "update", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"title":"updated"}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List all events
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 events, got %d", len(resp.Data))
	}
	if resp.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestAdminProjectEvents_FilterByEntityType(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "filter-user@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "filter-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "logs", EntityID: "l_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Filter by entity_type=issues
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?entity_type=issues", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 issues events, got %d", len(resp.Data))
	}
	for _, e := range resp.Data {
		if e.EntityType != "issues" {
			t.Fatalf("expected entity_type issues, got %s", e.EntityType)
		}
	}
}

func TestAdminProjectEvents_FilterByActionType(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "action-user@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "action-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "update", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
			{ClientActionID: 3, ActionType: "create", EntityType: "issues", EntityID: "i_002",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:02Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Filter by action_type=update
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?action_type=update", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 update event, got %d", len(resp.Data))
	}
}

func TestAdminProjectEvents_Pagination(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "page-user@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "page-events"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push 5 events
	events := make([]EventInput, 5)
	for i := range events {
		events[i] = EventInput{
			ClientActionID: int64(i + 1), ActionType: "create", EntityType: "issues",
			EntityID: fmt.Sprintf("i_%03d", i+1), Payload: json.RawMessage(`{}`),
			ClientTimestamp: "2025-01-01T00:00:00Z",
		}
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1", Events: events,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Page 1: limit=2
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?limit=2", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d", w.Code)
	}
	var page1 adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&page1)
	if len(page1.Data) != 2 {
		t.Fatalf("page 1: expected 2 events, got %d", len(page1.Data))
	}
	if !page1.HasMore {
		t.Fatal("page 1: expected has_more=true")
	}

	// Page 2: after_seq from last event
	lastSeq := page1.Data[len(page1.Data)-1].ServerSeq
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?limit=2&after_seq=%d", project.ID, lastSeq), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d", w.Code)
	}
	var page2 adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&page2)
	if len(page2.Data) != 2 {
		t.Fatalf("page 2: expected 2 events, got %d", len(page2.Data))
	}
	if !page2.HasMore {
		t.Fatal("page 2: expected has_more=true")
	}

	// Page 3: last page
	lastSeq = page2.Data[len(page2.Data)-1].ServerSeq
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?limit=2&after_seq=%d", project.ID, lastSeq), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page 3: expected 200, got %d", w.Code)
	}
	var page3 adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&page3)
	if len(page3.Data) != 1 {
		t.Fatalf("page 3: expected 1 event, got %d", len(page3.Data))
	}
	if page3.HasMore {
		t.Fatal("page 3: expected has_more=false")
	}
}

func TestAdminProjectEvent_Single(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "single-user@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "single-event"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{"title":"test event"}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Get single event by server_seq=1
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events/1", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var ev adminEvent
	_ = json.NewDecoder(w.Body).Decode(&ev)
	if ev.ServerSeq != 1 {
		t.Fatalf("expected server_seq 1, got %d", ev.ServerSeq)
	}
	if ev.EntityType != "issues" {
		t.Fatalf("expected entity_type issues, got %s", ev.EntityType)
	}
	if ev.EntityID != "i_001" {
		t.Fatalf("expected entity_id i_001, got %s", ev.EntityID)
	}
}

func TestAdminProjectEvent_NotFound(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "nf-user@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "nf-event"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push one event
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Try to get non-existent event
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events/999", project.ID), adminToken, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminEntityTypes(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")

	w := doRequest(srv, "GET", "/v1/admin/entity-types", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		EntityTypes []string `json:"entity_types"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.EntityTypes) == 0 {
		t.Fatal("expected non-empty entity_types")
	}

	// Check a few expected types
	typeSet := map[string]bool{}
	for _, et := range resp.EntityTypes {
		typeSet[et] = true
	}
	for _, expected := range []string{"issues", "logs", "boards", "comments", "handoffs"} {
		if !typeSet[expected] {
			t.Errorf("expected entity type %q in response", expected)
		}
	}
}

func TestAdminProjectEvents_InvalidEntityType(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "invalid-et@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "invalid-et"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push one event to create the events.db
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "dev1", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_001",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", w.Code)
	}

	// Try with invalid entity type
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?entity_type=bogus", project.ID), adminToken, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminProjectEvents_RequiresAdmin(t *testing.T) {
	srv, store := newTestServer(t)
	_, _ = store.CreateUser("first@test.com")
	_, token := createTestUser(t, store, "nonadmin@test.com")

	w := doRequest(srv, "GET", "/v1/admin/projects/fake/events", token, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminProjectEvents_FilterByDeviceID(t *testing.T) {
	srv, store := newTestServer(t)
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "admin:read:events,sync")
	_, userToken := createTestUser(t, store, "dev-filter@test.com")

	w := doRequest(srv, "POST", "/v1/projects", userToken, CreateProjectRequest{Name: "dev-filter"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var project ProjectResponse
	_ = json.NewDecoder(w.Body).Decode(&project)

	// Push from device A
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "devA", SessionID: "sess1",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_A1",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push A: expected 200, got %d", w.Code)
	}

	// Push from device B
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), userToken, PushRequest{
		DeviceID: "devB", SessionID: "sess2",
		Events: []EventInput{
			{ClientActionID: 1, ActionType: "create", EntityType: "issues", EntityID: "i_B1",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:00Z"},
			{ClientActionID: 2, ActionType: "create", EntityType: "issues", EntityID: "i_B2",
				Payload: json.RawMessage(`{}`), ClientTimestamp: "2025-01-01T00:00:01Z"},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("push B: expected 200, got %d", w.Code)
	}

	// Filter by device_id=devB
	w = doRequest(srv, "GET", fmt.Sprintf("/v1/admin/projects/%s/events?device_id=devB", project.ID), adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp adminEventsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 events from devB, got %d", len(resp.Data))
	}
}
