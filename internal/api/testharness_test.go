package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

// TestHarness wraps a full Server with a real HTTP listener for integration tests.
type TestHarness struct {
	t       *testing.T
	Server  *Server
	Store   *serverdb.ServerDB
	BaseURL string
	client  *http.Client
	httpSrv *httptest.Server
}

// newTestHarness creates a TestHarness with a real HTTP server on a random port.
func newTestHarness(t *testing.T, opts ...func(*Config)) *TestHarness {
	t.Helper()

	tmpDir := t.TempDir()

	dbPath := filepath.Join(tmpDir, "server.db")
	store, err := serverdb.Open(dbPath)
	if err != nil {
		t.Fatalf("open server db: %v", err)
	}

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
	for _, opt := range opts {
		opt(&cfg)
	}

	srv, err := NewServer(cfg, store)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	httpSrv := httptest.NewServer(srv.routes())

	h := &TestHarness{
		t:       t,
		Server:  srv,
		Store:   store,
		BaseURL: httpSrv.URL,
		client:  &http.Client{},
		httpSrv: httpSrv,
	}

	t.Cleanup(func() {
		httpSrv.Close()
		srv.dbPool.CloseAll()
		store.Close()
	})

	return h
}

// Do sends an HTTP request and returns the response.
// Caller must close resp.Body unless using assertion helpers (AssertStatus,
// AssertErrorResponse, ReadJSON, AssertPaginated) which close it automatically.
func (h *TestHarness) Do(method, path, token string, body any) *http.Response {
	h.t.Helper()

	url := h.BaseURL + path

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("marshal request body: %v", err)
		}
	}

	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, &buf)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		h.t.Fatalf("create request: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("do request %s %s: %v", method, path, err)
	}

	return resp
}

// DoJSON sends an HTTP request and decodes the JSON response into out.
// Fatals if the response status is >= 400 or if JSON decoding fails.
func (h *TestHarness) DoJSON(method, path, token string, body any, out any) *http.Response {
	h.t.Helper()

	resp := h.Do(method, path, token, body)

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		h.t.Fatalf("DoJSON %s %s: expected success, got %d: %s", method, path, resp.StatusCode, respBody)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		h.t.Fatalf("decode response: %v", err)
	}

	return resp
}

// CreateUser creates a user with a sync-scoped API key.
func (h *TestHarness) CreateUser(email string) (userID, token string) {
	h.t.Helper()

	user, err := h.Store.CreateUser(email)
	if err != nil {
		h.t.Fatalf("create user: %v", err)
	}

	tok, _, err := h.Store.GenerateAPIKey(user.ID, "test", "sync", nil)
	if err != nil {
		h.t.Fatalf("generate api key: %v", err)
	}

	return user.ID, tok
}

// CreateAdminUser creates an admin user with specified scopes.
func (h *TestHarness) CreateAdminUser(email, scopes string) (userID, token string) {
	h.t.Helper()

	user, err := h.Store.CreateUser(email)
	if err != nil {
		h.t.Fatalf("create user: %v", err)
	}

	if !user.IsAdmin {
		if err := h.Store.SetUserAdmin(email, true); err != nil {
			h.t.Fatalf("set admin: %v", err)
		}
	}

	tok, _, err := h.Store.GenerateAPIKey(user.ID, "admin-test", scopes, nil)
	if err != nil {
		h.t.Fatalf("generate api key: %v", err)
	}

	return user.ID, tok
}

// CreateProject creates a project via the API. Returns project ID.
func (h *TestHarness) CreateProject(ownerToken, name string) string {
	h.t.Helper()

	var project ProjectResponse
	resp := h.DoJSON("POST", "/v1/projects", ownerToken, CreateProjectRequest{Name: name}, &project)

	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("create project: expected 201, got %d", resp.StatusCode)
	}

	return project.ID
}

// PushEvents pushes events to a project via the API.
func (h *TestHarness) PushEvents(token, projectID string, events []EventInput) {
	h.t.Helper()

	resp := h.Do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", projectID), token, PushRequest{
		DeviceID:  "test-device",
		SessionID: "test-session",
		Events:    events,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("push events: expected 200, got %d", resp.StatusCode)
	}
}

// BuildSnapshot triggers a snapshot build by calling GET /v1/projects/{id}/sync/snapshot.
func (h *TestHarness) BuildSnapshot(token, projectID string) {
	h.t.Helper()

	resp := h.Do("GET", fmt.Sprintf("/v1/projects/%s/sync/snapshot", projectID), token, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("build snapshot: expected 200, got %d", resp.StatusCode)
	}
}

// --- Response assertion helpers ---

// AssertStatus checks the HTTP status code matches expected. Reads and closes the body on failure.
func AssertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}

// AssertErrorResponse checks the response has the expected status and error code.
func AssertErrorResponse(t *testing.T, resp *http.Response, expectedStatus int, expectedCode string) {
	t.Helper()
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		t.Fatalf("expected status %d, got %d: %s", expectedStatus, resp.StatusCode, string(body))
	}
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error.Code != expectedCode {
		t.Fatalf("expected error code %q, got %q: %s", expectedCode, errResp.Error.Code, errResp.Error.Message)
	}
}

// ReadJSON decodes a JSON response body into the given type.
func ReadJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	return out
}

// PaginatedResponse represents a paginated list response from the admin API.
type PaginatedResponse[T any] struct {
	Data       []T    `json:"data"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// AssertPaginated checks the response is a valid paginated response with expected count and has_more.
func AssertPaginated[T any](t *testing.T, resp *http.Response, expectedCount int, expectHasMore bool) PaginatedResponse[T] {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	var result PaginatedResponse[T]
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode paginated response: %v", err)
	}
	if len(result.Data) != expectedCount {
		t.Fatalf("expected %d items, got %d", expectedCount, len(result.Data))
	}
	if result.HasMore != expectHasMore {
		t.Fatalf("expected has_more=%v, got %v", expectHasMore, result.HasMore)
	}
	return result
}

// AssertCORSHeaders checks the response has the expected CORS origin header.
func AssertCORSHeaders(t *testing.T, resp *http.Response, expectedOrigin string) {
	t.Helper()
	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != expectedOrigin {
		t.Fatalf("expected Access-Control-Allow-Origin %q, got %q", expectedOrigin, origin)
	}
}

// AssertNoCORSHeaders checks the response has no CORS origin header.
func AssertNoCORSHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header, got %q", origin)
	}
}

// AssertRequiresAdminScope checks that a request with the wrong scope token gets a 403 with insufficient_admin_scope.
func (h *TestHarness) AssertRequiresAdminScope(t *testing.T, method, path, wrongScopeToken string) {
	t.Helper()
	resp := h.Do(method, path, wrongScopeToken, nil)
	AssertErrorResponse(t, resp, http.StatusForbidden, "insufficient_admin_scope")
}

// ---------------------------------------------------------------------------
// State builder — fluent API for setting up complex test scenarios
// ---------------------------------------------------------------------------

// userEntry holds user state created during a build.
type userEntry struct {
	id    string
	token string
	admin bool
}

// projectEntry holds project state created during a build.
type projectEntry struct {
	id         string
	ownerEmail string
}

// TestState is the result of a StateBuilder.Done() call.
type TestState struct {
	h        *TestHarness
	users    map[string]userEntry    // email -> userEntry
	projects map[string]projectEntry // name -> projectEntry
}

// UserToken returns the API token for the given email. Fatals if not found.
func (s *TestState) UserToken(email string) string {
	s.h.t.Helper()
	u, ok := s.users[email]
	if !ok {
		s.h.t.Fatalf("UserToken: unknown user %q", email)
	}
	return u.token
}

// UserID returns the user ID for the given email. Fatals if not found.
func (s *TestState) UserID(email string) string {
	s.h.t.Helper()
	u, ok := s.users[email]
	if !ok {
		s.h.t.Fatalf("UserID: unknown user %q", email)
	}
	return u.id
}

// AdminToken returns the API token for the given admin email. Fatals if
// the user is not found or is not an admin.
func (s *TestState) AdminToken(email string) string {
	s.h.t.Helper()
	u, ok := s.users[email]
	if !ok {
		s.h.t.Fatalf("AdminToken: unknown user %q", email)
	}
	if !u.admin {
		s.h.t.Fatalf("AdminToken: user %q is not an admin", email)
	}
	return u.token
}

// ProjectID returns the project ID for the given project name. Fatals if
// not found.
func (s *TestState) ProjectID(name string) string {
	s.h.t.Helper()
	p, ok := s.projects[name]
	if !ok {
		s.h.t.Fatalf("ProjectID: unknown project %q", name)
	}
	return p.id
}

// Harness returns the underlying TestHarness.
func (s *TestState) Harness() *TestHarness {
	return s.h
}

// StateBuilder accumulates deferred setup steps executed in order by Done().
type StateBuilder struct {
	h     *TestHarness
	steps []func(*TestState)
}

// Build returns a new StateBuilder for fluent test-state setup.
func (h *TestHarness) Build() *StateBuilder {
	return &StateBuilder{h: h}
}

// WithUser appends a step that creates a non-admin user with a sync-scoped
// API key.
func (b *StateBuilder) WithUser(email string) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		id, tok := b.h.CreateUser(email)
		s.users[email] = userEntry{id: id, token: tok}
	})
	return b
}

// WithAdmin appends a step that creates an admin user with the given scopes.
func (b *StateBuilder) WithAdmin(email, scopes string) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		id, tok := b.h.CreateAdminUser(email, scopes)
		s.users[email] = userEntry{id: id, token: tok, admin: true}
	})
	return b
}

// WithProject appends a step that creates a project owned by ownerEmail.
func (b *StateBuilder) WithProject(name, ownerEmail string) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		u, ok := s.users[ownerEmail]
		if !ok {
			b.h.t.Fatalf("WithProject: owner %q not created yet", ownerEmail)
		}
		pid := b.h.CreateProject(u.token, name)
		s.projects[name] = projectEntry{id: pid, ownerEmail: ownerEmail}
	})
	return b
}

// WithMember appends a step that adds a user as a member of a project.
// The project owner's token is used for the API call.
func (b *StateBuilder) WithMember(projectName, email, role string) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		p, ok := s.projects[projectName]
		if !ok {
			b.h.t.Fatalf("WithMember: project %q not created yet", projectName)
		}
		owner, ok := s.users[p.ownerEmail]
		if !ok {
			b.h.t.Fatalf("WithMember: owner %q not found", p.ownerEmail)
		}
		u, ok := s.users[email]
		if !ok {
			b.h.t.Fatalf("WithMember: user %q not created yet", email)
		}
		resp := b.h.Do("POST", fmt.Sprintf("/v1/projects/%s/members", p.id), owner.token,
			AddMemberRequest{UserID: u.id, Role: role})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			b.h.t.Fatalf("WithMember: expected 201, got %d", resp.StatusCode)
		}
	})
	return b
}

// WithEvents appends a step that pushes count events to the named project
// using the given user's token. Entity types cycle through issues, logs,
// and comments.
func (b *StateBuilder) WithEvents(projectName, userEmail string, count int) *StateBuilder {
	stepIdx := len(b.steps) // capture for unique device/session IDs
	b.steps = append(b.steps, func(s *TestState) {
		p, ok := s.projects[projectName]
		if !ok {
			b.h.t.Fatalf("WithEvents: project %q not created yet", projectName)
		}
		u, ok := s.users[userEmail]
		if !ok {
			b.h.t.Fatalf("WithEvents: user %q not created yet", userEmail)
		}

		entityTypes := []string{"issues", "logs", "comments"}
		events := make([]EventInput, count)
		for i := 0; i < count; i++ {
			et := entityTypes[i%3]
			events[i] = EventInput{
				ClientActionID:  int64(i + 1),
				ActionType:      "create",
				EntityType:      et,
				EntityID:        fmt.Sprintf("%s_%s_%03d", et[:1], projectName, i+1),
				Payload:         json.RawMessage(`{}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			}
		}

		deviceID := fmt.Sprintf("dev-%s-%d", projectName, stepIdx)
		sessionID := fmt.Sprintf("ses-%s-%d", projectName, stepIdx)
		resp := b.h.Do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", p.id), u.token, PushRequest{
			DeviceID:  deviceID,
			SessionID: sessionID,
			Events:    events,
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.h.t.Fatalf("WithEvents: expected 200, got %d", resp.StatusCode)
		}
	})
	return b
}

// WithSnapshot appends a step that builds a cached snapshot for the named
// project using the owner's token.
func (b *StateBuilder) WithSnapshot(projectName string) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		p, ok := s.projects[projectName]
		if !ok {
			b.h.t.Fatalf("WithSnapshot: project %q not created yet", projectName)
		}
		owner, ok := s.users[p.ownerEmail]
		if !ok {
			b.h.t.Fatalf("WithSnapshot: owner %q not found", p.ownerEmail)
		}
		b.h.BuildSnapshot(owner.token, p.id)
	})
	return b
}

// WithAuthEvents appends a step that inserts count auth events directly
// into the database.
func (b *StateBuilder) WithAuthEvents(count int) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		for i := 0; i < count; i++ {
			err := b.h.Store.InsertAuthEvent(
				fmt.Sprintf("authreq-%03d", i+1),
				fmt.Sprintf("authuser%d@test.com", i+1),
				"started",
				"{}",
			)
			if err != nil {
				b.h.t.Fatalf("WithAuthEvents: %v", err)
			}
		}
	})
	return b
}

// WithRateLimitEvents appends a step that inserts count rate-limit events
// directly into the database.
func (b *StateBuilder) WithRateLimitEvents(count int) *StateBuilder {
	b.steps = append(b.steps, func(s *TestState) {
		for i := 0; i < count; i++ {
			err := b.h.Store.InsertRateLimitEvent(
				fmt.Sprintf("key-%03d", i+1),
				fmt.Sprintf("192.168.1.%d", i+1),
				"push",
			)
			if err != nil {
				b.h.t.Fatalf("WithRateLimitEvents: %v", err)
			}
		}
	})
	return b
}

// Done executes all accumulated steps in order and returns the resulting
// TestState.
func (b *StateBuilder) Done() *TestState {
	b.h.t.Helper()

	s := &TestState{
		h:        b.h,
		users:    make(map[string]userEntry),
		projects: make(map[string]projectEntry),
	}
	for _, step := range b.steps {
		step(s)
	}
	return s
}
