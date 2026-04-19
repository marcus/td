package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tddb "github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/serve"
	"github.com/marcus/td/internal/serverdb"
)

// projectRoutesHarness wires up a Server, a real httptest listener, an owner
// user (admin-bypass — first user is auto-admin), and a project. Returns the
// harness and the project id.
type projectRoutesHarness struct {
	srv       *Server
	store     *serverdb.ServerDB
	httpSrv   *httptest.Server
	baseURL   string
	owner     string // user id
	ownerTok  string // bearer token
	stranger  string // user id of a non-member
	strangTok string
	pid       string
	dataDir   string
}

func newProjectRoutesHarness(t *testing.T) *projectRoutesHarness {
	t.Helper()
	srv, store := newTestServer(t)
	httpSrv := httptest.NewServer(srv.routes())
	t.Cleanup(httpSrv.Close)

	// First user becomes admin automatically; create a non-admin owner so the
	// membership check is exercised against a real role rather than admin-bypass.
	_, _ = createTestUser(t, store, "first@test.com")
	ownerID, ownerTok := createTestUser(t, store, "owner@test.com")
	strangerID, strangerTok := createTestUser(t, store, "stranger@test.com")

	p, err := store.CreateProject("p", "", ownerID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &projectRoutesHarness{
		srv:       srv,
		store:     store,
		httpSrv:   httpSrv,
		baseURL:   httpSrv.URL,
		owner:     ownerID,
		ownerTok:  ownerTok,
		stranger:  strangerID,
		strangTok: strangerTok,
		pid:       p.ID,
		dataDir:   srv.config.ProjectDataDir,
	}
}

func (h *projectRoutesHarness) do(t *testing.T, method, path, token string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, h.baseURL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// readEnvelope decodes a serve.Envelope into the given out target via the
// data field. Fails the test on non-OK envelopes.
func readEnvelope(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env serve.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, body)
	}
	if !env.OK {
		t.Fatalf("envelope not ok (status=%d): %+v body=%s", resp.StatusCode, env.Error, body)
	}
	if out == nil {
		return
	}
	dataBytes, _ := json.Marshal(env.Data)
	if err := json.Unmarshal(dataBytes, out); err != nil {
		t.Fatalf("decode data: %v (data=%s)", err, dataBytes)
	}
}

// openProjectDB opens the on-disk project.db so tests can verify side effects
// (action_log session_id, etc.) without going back through the API.
func (h *projectRoutesHarness) openProjectDB(t *testing.T) *tddb.DB {
	t.Helper()
	dbPath := filepath.Join(h.dataDir, h.pid, "project.db")
	conn, err := tddb.OpenSQLite(dbPath, tddb.OpenOptions{})
	if err != nil {
		t.Fatalf("open project.db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return tddb.NewWithConn(conn, filepath.Dir(dbPath))
}

// --- TestCreateIssue_RoundTrip --------------------------------------------

func TestCreateIssue_RoundTrip(t *testing.T) {
	h := newProjectRoutesHarness(t)

	// First user is admin and bypasses membership; use admin token to create
	// the issue. (Owner token would also work since we made owner the project
	// creator.)
	body := serve.IssueCreateBody{
		Title:    "First issue created via REST",
		Type:     "task",
		Priority: "P1",
	}
	resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, body, map[string]string{HeaderTdWatchSession: "ses1"})
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /issues: status=%d body=%s", resp.StatusCode, respBody)
	}
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, resp, &created)
	if created.Issue.ID == "" {
		t.Fatal("issue ID not returned")
	}
	if created.Issue.Title != "First issue created via REST" {
		t.Errorf("title = %q, want First issue created via REST", created.Issue.Title)
	}

	// Round-trip: GET the same issue.
	getResp := h.do(t, "GET",
		fmt.Sprintf("/v1/projects/%s/issues/%s", h.pid, created.Issue.ID),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	if getResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()
		t.Fatalf("GET /issues/%s: status=%d body=%s", created.Issue.ID, getResp.StatusCode, respBody)
	}
	var got struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, getResp, &got)
	if got.Issue.ID != created.Issue.ID {
		t.Errorf("GET issue id = %q, want %q", got.Issue.ID, created.Issue.ID)
	}
	if got.Issue.Status != "open" {
		t.Errorf("GET issue status = %q, want open", got.Issue.Status)
	}
}

// --- TestUpdateIssue ------------------------------------------------------

func TestUpdateIssue(t *testing.T) {
	h := newProjectRoutesHarness(t)

	// Create an issue first.
	createResp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, serve.IssueCreateBody{Title: "Original title for update test", Type: "task"},
		map[string]string{HeaderTdWatchSession: "ses1"})
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, createResp, &created)

	// PATCH title.
	newTitle := "Updated title via REST patch"
	patchBody := serve.IssueUpdateBody{Title: &newTitle}
	patchResp := h.do(t, "PATCH",
		fmt.Sprintf("/v1/projects/%s/issues/%s", h.pid, created.Issue.ID),
		h.ownerTok, patchBody, map[string]string{HeaderTdWatchSession: "ses1"})
	if patchResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(patchResp.Body)
		patchResp.Body.Close()
		t.Fatalf("PATCH /issues: status=%d body=%s", patchResp.StatusCode, respBody)
	}
	patchResp.Body.Close()

	// GET reflects the update.
	getResp := h.do(t, "GET",
		fmt.Sprintf("/v1/projects/%s/issues/%s", h.pid, created.Issue.ID),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	var got struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, getResp, &got)
	if got.Issue.Title != newTitle {
		t.Errorf("post-PATCH title = %q, want %q", got.Issue.Title, newTitle)
	}
}

// --- TestTransition_StartIssue --------------------------------------------

func TestTransition_StartIssue(t *testing.T) {
	h := newProjectRoutesHarness(t)

	createResp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, serve.IssueCreateBody{Title: "Will be started via REST"},
		map[string]string{HeaderTdWatchSession: "ses1"})
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, createResp, &created)

	startResp := h.do(t, "POST",
		fmt.Sprintf("/v1/projects/%s/issues/%s/start", h.pid, created.Issue.ID),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	if startResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(startResp.Body)
		startResp.Body.Close()
		t.Fatalf("POST /start: status=%d body=%s", startResp.StatusCode, respBody)
	}
	var started struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, startResp, &started)
	if started.Issue.Status != "in_progress" {
		t.Errorf("post-start status = %q, want in_progress", started.Issue.Status)
	}
}

// --- TestListIssues_Filters ------------------------------------------------

func TestListIssues_Filters(t *testing.T) {
	h := newProjectRoutesHarness(t)

	// Seed three issues with different priorities.
	for _, prio := range []string{"P0", "P1", "P2"} {
		resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
			h.ownerTok,
			serve.IssueCreateBody{Title: "Seed issue with priority " + prio, Priority: prio},
			map[string]string{HeaderTdWatchSession: "ses1"})
		if resp.StatusCode != http.StatusCreated {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("seed create %s: status=%d body=%s", prio, resp.StatusCode, respBody)
		}
		resp.Body.Close()
	}

	// List all (no filter): should see 3.
	listResp := h.do(t, "GET", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	var allList struct {
		Issues []serve.IssueDTO `json:"issues"`
		Total  int              `json:"total"`
	}
	readEnvelope(t, listResp, &allList)
	if allList.Total != 3 {
		t.Errorf("unfiltered total = %d, want 3", allList.Total)
	}

	// Filter by priority=P0.
	filterResp := h.do(t, "GET",
		fmt.Sprintf("/v1/projects/%s/issues?priority=P0", h.pid),
		h.ownerTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	var p0List struct {
		Issues []serve.IssueDTO `json:"issues"`
		Total  int              `json:"total"`
	}
	readEnvelope(t, filterResp, &p0List)
	if p0List.Total != 1 {
		t.Errorf("priority=P0 total = %d, want 1", p0List.Total)
	}
	if len(p0List.Issues) == 1 && p0List.Issues[0].Priority != "P0" {
		t.Errorf("priority=P0 issue priority = %q, want P0", p0List.Issues[0].Priority)
	}
}

// --- TestUnauthorized_NoMembership ----------------------------------------

func TestUnauthorized_NoMembership(t *testing.T) {
	h := newProjectRoutesHarness(t)

	// stranger has no membership and is not admin.
	resp := h.do(t, "GET", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.strangTok, nil, map[string]string{HeaderTdWatchSession: "ses1"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("non-member GET: status=%d body=%s", resp.StatusCode, body)
	}

	// stranger trying a write also forbidden.
	createResp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		h.strangTok, serve.IssueCreateBody{Title: "stranger should not be able to land this"},
		map[string]string{HeaderTdWatchSession: "ses1"})
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("non-member POST: status=%d body=%s", createResp.StatusCode, body)
	}
}

// --- TestImpersonationHeaders ---------------------------------------------

func TestImpersonationHeaders(t *testing.T) {
	h := newProjectRoutesHarness(t)
	targetID, _ := createTestUser(t, h.store, "target@test.com")
	if _, err := h.store.AddMember(h.pid, targetID, serverdb.RoleWriter, h.owner); err != nil {
		t.Fatalf("add target writer: %v", err)
	}

	// Admin (first user) impersonates another user via headers. The route
	// stack is requireAuth -> requireProjectMembership (target membership) ->
	// resolveTdWatchSession -> wrap. resolveTdWatchSession should encode the
	// session_id as twa_<adminSession>_as_<targetUserID>.
	//
	// Need the admin token from the first user we created. Re-create here
	// using the harness setup pattern: first@test.com was made by
	// newProjectRoutesHarness already.
	users, err := h.store.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	var adminTok string
	for _, u := range users {
		if u.Email == "first@test.com" {
			tok, _, err := h.store.GenerateAPIKey(u.ID, "test-admin", "sync", nil)
			if err != nil {
				t.Fatalf("generate admin key: %v", err)
			}
			adminTok = tok
			break
		}
	}
	if adminTok == "" {
		t.Fatal("could not find admin user")
	}

	createResp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid),
		adminTok, serve.IssueCreateBody{Title: "impersonated write from admin"},
		map[string]string{
			HeaderTdWatchSession:     "adminSes",
			HeaderTdWatchImpersonate: targetID,
		})
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		createResp.Body.Close()
		t.Fatalf("impersonation POST: status=%d body=%s", createResp.StatusCode, body)
	}
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, createResp, &created)

	// Inspect action_log for the issue: session_id MUST be twa_*_as_* form.
	db := h.openProjectDB(t)
	rows, err := db.Conn().Query(
		`SELECT session_id, action_type, entity_type, entity_id FROM action_log WHERE entity_id = ? ORDER BY rowid`,
		created.Issue.ID)
	if err != nil {
		t.Fatalf("query action_log: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var sessionID, actionType, entityType, entityID string
		if err := rows.Scan(&sessionID, &actionType, &entityType, &entityID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Logf("action_log row: session=%s action=%s entity=%s/%s", sessionID, actionType, entityType, entityID)
		if !strings.HasPrefix(sessionID, "twa_") || !strings.Contains(sessionID, "_as_"+targetID) {
			t.Errorf("action_log session_id = %q, want twa_*_as_%s prefix", sessionID, targetID)
			continue
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Fatalf("no action_log row matched twa_*_as_%s shape", targetID)
	}
}
