package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// newTestServerWithDB creates a Server backed by a real temp database.
func newTestServerWithDB(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewServer(database, tmpDir, "ses_test123", ServeConfig{})
}

// doJSON sends a JSON request and decodes the envelope response.
func doJSON(t *testing.T, ts *httptest.Server, method, path string, body interface{}) (*http.Response, Envelope) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	resp.Body.Close()
	return resp, env
}

// ============================================================================
// POST /v1/issues — Create
// ============================================================================

func TestCreateIssue_ValidMinimal(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title: "Fix the login timeout issue",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	data, ok := env.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is not a map: %T", env.Data)
	}
	issue, ok := data["issue"].(map[string]interface{})
	if !ok {
		t.Fatalf("data.issue is not a map: %T", data["issue"])
	}

	// Verify defaults
	if issue["type"] != "task" {
		t.Errorf("type = %v, want task", issue["type"])
	}
	if issue["priority"] != "P2" {
		t.Errorf("priority = %v, want P2", issue["priority"])
	}
	if issue["status"] != "open" {
		t.Errorf("status = %v, want open", issue["status"])
	}
	if issue["title"] != "Fix the login timeout issue" {
		t.Errorf("title = %v, want 'Fix the login timeout issue'", issue["title"])
	}
	// creator_session should be the server's session
	if issue["creator_session"] != "ses_test123" {
		t.Errorf("creator_session = %v, want ses_test123", issue["creator_session"])
	}
}

func TestHandleSetFocus_DoesNotNotifyForSetOrClear(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{Title: "Focus mutation notify test"}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	notifyCount := 0
	ctx := HandlerContext{
		DB:         database,
		SessionID:  "ses_focus_notify",
		WorktreeID: worktreeIDForBaseDir(tmpDir),
		BaseDir:    tmpDir,
		Config: HandlerConfig{
			NotifyChange: func() { notifyCount++ },
		},
	}

	for _, body := range []interface{}{
		map[string]interface{}{"issue_id": issue.ID},
		map[string]interface{}{"issue_id": nil},
	} {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
		req := httptest.NewRequest("PUT", "/v1/focus", &buf)
		rec := httptest.NewRecorder()

		HandleSetFocus(ctx, rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	if notifyCount != 0 {
		t.Fatalf("NotifyChange called %d times, want 0", notifyCount)
	}
}

func TestHandleSetFocus_NoLocalRootUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	req := httptest.NewRequest("PUT", "/v1/focus", bytes.NewBufferString(`{"issue_id":null}`))
	rec := httptest.NewRecorder()

	HandleSetFocus(HandlerContext{
		DB:        database,
		SessionID: "ses_no_root",
		BaseDir:   "",
	}, rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestCreateIssue_AllFields(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:       "Implement user authentication flow",
		Description: "Full OAuth2 flow",
		Type:        "feature",
		Priority:    "P1",
		Points:      5,
		Labels:      []string{"auth", "security"},
		Acceptance:  "Users can log in via OAuth",
		Sprint:      "sprint-1",
		Minor:       true,
		DeferUntil:  "2026-04-01",
		DueDate:     "2026-05-01",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})

	if issue["type"] != "feature" {
		t.Errorf("type = %v, want feature", issue["type"])
	}
	if issue["priority"] != "P1" {
		t.Errorf("priority = %v, want P1", issue["priority"])
	}
	if issue["points"] != float64(5) {
		t.Errorf("points = %v, want 5", issue["points"])
	}
	if issue["minor"] != true {
		t.Errorf("minor = %v, want true", issue["minor"])
	}
	if issue["defer_until"] != "2026-04-01" {
		t.Errorf("defer_until = %v, want 2026-04-01", issue["defer_until"])
	}
	if issue["due_date"] != "2026-05-01" {
		t.Errorf("due_date = %v, want 2026-05-01", issue["due_date"])
	}
}

func TestCreateIssue_StoryNormalizedToFeature(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title: "Implement story normalization test",
		Type:  "story",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	if issue["type"] != "feature" {
		t.Errorf("type = %v, want feature (story should normalize to feature)", issue["type"])
	}
}

func TestCreateIssue_NumericPriorityNormalized(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:    "Test numeric priority normalization",
		Priority: "0",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	if issue["priority"] != "P0" {
		t.Errorf("priority = %v, want P0 (0 should normalize to P0)", issue["priority"])
	}
}

func TestCreateIssue_MissingTitle(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Type: "bug",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestCreateIssue_InvalidType(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title: "Test issue with invalid type value",
		Type:  "invalid_type",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestCreateIssue_InvalidPriority(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:    "Test issue with invalid priority",
		Priority: "P9",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestCreateIssue_InvalidPoints(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:  "Test issue with non-fibonacci points",
		Points: 4, // not Fibonacci
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestCreateIssue_InvalidDate(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:      "Test issue with bad date format",
		DeferUntil: "not-a-date",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestCreateIssue_InvalidJSON(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/issues", bytes.NewBufferString("{not valid json"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestCreateIssue_ParentNotFound(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title:    "Child issue with missing parent",
		ParentID: "td-nonexistent",
	}

	resp, env := doJSON(t, ts, "POST", "/v1/issues", body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if env.Error == nil || env.Error.Code != ErrNotFound {
		t.Errorf("error.code = %v, want %s", env.Error, ErrNotFound)
	}
}

func TestCreateIssue_WithValidParent(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create parent issue first
	parentBody := IssueCreateBody{
		Title: "Epic: parent issue for child test",
		Type:  "epic",
	}
	_, parentEnv := doJSON(t, ts, "POST", "/v1/issues", parentBody)
	parentData := parentEnv.Data.(map[string]interface{})
	parentIssue := parentData["issue"].(map[string]interface{})
	parentID := parentIssue["id"].(string)

	// Create child with parent
	childBody := IssueCreateBody{
		Title:    "Child task under the parent epic",
		ParentID: parentID,
	}
	resp, env := doJSON(t, ts, "POST", "/v1/issues", childBody)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	if issue["parent_id"] != parentID {
		t.Errorf("parent_id = %v, want %s", issue["parent_id"], parentID)
	}
}

// ============================================================================
// PATCH /v1/issues/{id} — Update
// ============================================================================

// createTestIssue creates an issue via the API and returns its ID.
func createTestIssue(t *testing.T, ts *httptest.Server, title string) string {
	t.Helper()
	body := IssueCreateBody{Title: title}
	_, env := doJSON(t, ts, "POST", "/v1/issues", body)
	if !env.OK {
		t.Fatalf("failed to create test issue: %+v", env.Error)
	}
	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	return issue["id"].(string)
}

func TestUpdateIssue_PartialUpdate(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Original title for partial update test")

	// Update only the priority
	newPriority := "P0"
	body := IssueUpdateBody{
		Priority: &newPriority,
	}

	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+id, body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})

	// Changed field
	if issue["priority"] != "P0" {
		t.Errorf("priority = %v, want P0", issue["priority"])
	}
	// Unchanged field preserved
	if issue["title"] != "Original title for partial update test" {
		t.Errorf("title = %v, want original", issue["title"])
	}
	// Default preserved
	if issue["type"] != "task" {
		t.Errorf("type = %v, want task (unchanged)", issue["type"])
	}
}

func TestUpdateIssue_MultipleFields(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Issue to update with multiple fields")

	newTitle := "Updated title for multi-field test"
	newDesc := "New description"
	newType := "bug"
	newPoints := 8
	newMinor := true
	body := IssueUpdateBody{
		Title:       &newTitle,
		Description: &newDesc,
		Type:        &newType,
		Points:      &newPoints,
		Minor:       &newMinor,
	}

	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+id, body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})

	if issue["title"] != newTitle {
		t.Errorf("title = %v, want %s", issue["title"], newTitle)
	}
	if issue["description"] != newDesc {
		t.Errorf("description = %v, want %s", issue["description"], newDesc)
	}
	if issue["type"] != "bug" {
		t.Errorf("type = %v, want bug", issue["type"])
	}
	if issue["points"] != float64(8) {
		t.Errorf("points = %v, want 8", issue["points"])
	}
	if issue["minor"] != true {
		t.Errorf("minor = %v, want true", issue["minor"])
	}
}

func TestUpdateIssue_NotFound(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	newPriority := "P0"
	body := IssueUpdateBody{Priority: &newPriority}

	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/td-nonexistent", body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if env.Error == nil || env.Error.Code != ErrNotFound {
		t.Errorf("error.code = %v, want %s", env.Error, ErrNotFound)
	}
}

func TestUpdateIssue_InvalidField(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Issue for invalid update validation test")

	badPoints := 4 // not Fibonacci
	body := IssueUpdateBody{Points: &badPoints}

	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+id, body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if env.Error == nil || env.Error.Code != ErrValidation {
		t.Errorf("error.code = %v, want %s", env.Error, ErrValidation)
	}
}

func TestUpdateIssue_ClearDeferUntil(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create with defer_until
	createBody := IssueCreateBody{
		Title:      "Issue with defer to clear later",
		DeferUntil: "2026-04-01",
	}
	_, createEnv := doJSON(t, ts, "POST", "/v1/issues", createBody)
	data := createEnv.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	id := issue["id"].(string)

	// Verify it was set
	if issue["defer_until"] != "2026-04-01" {
		t.Fatalf("defer_until not set initially: %v", issue["defer_until"])
	}

	// Clear it by setting empty string
	empty := ""
	updateBody := IssueUpdateBody{DeferUntil: &empty}
	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+id, updateBody)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	data = env.Data.(map[string]interface{})
	issue = data["issue"].(map[string]interface{})
	if issue["defer_until"] != nil {
		t.Errorf("defer_until = %v, want nil (cleared)", issue["defer_until"])
	}
}

func TestUpdateIssue_SetParentID(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	parentID := createTestIssue(t, ts, "Parent issue for update parent test")
	childID := createTestIssue(t, ts, "Child issue to set parent on update")

	body := IssueUpdateBody{ParentID: &parentID}
	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+childID, body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	data := env.Data.(map[string]interface{})
	issue := data["issue"].(map[string]interface{})
	if issue["parent_id"] != parentID {
		t.Errorf("parent_id = %v, want %s", issue["parent_id"], parentID)
	}
}

func TestUpdateIssue_InvalidParent(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Issue with invalid parent on update")

	badParent := "td-nonexistent"
	body := IssueUpdateBody{ParentID: &badParent}
	resp, env := doJSON(t, ts, "PATCH", "/v1/issues/"+id, body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if env.Error == nil || env.Error.Code != ErrNotFound {
		t.Errorf("error.code = %v, want %s", env.Error, ErrNotFound)
	}
}

func TestUpdateIssue_InvalidJSON(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Issue for invalid JSON update test")

	req, _ := http.NewRequest("PATCH", ts.URL+"/v1/issues/"+id, bytes.NewBufferString("{broken"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ============================================================================
// DELETE /v1/issues/{id} — Soft Delete
// ============================================================================

func TestDeleteIssue_Success(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	id := createTestIssue(t, ts, "Issue to delete in soft-delete test")

	resp, env := doJSON(t, ts, "DELETE", "/v1/issues/"+id, nil)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	data := env.Data.(map[string]interface{})
	if data["deleted"] != true {
		t.Errorf("deleted = %v, want true", data["deleted"])
	}

	// Verify the issue is soft-deleted in the database
	issue, err := srv.db.GetIssue(id)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.DeletedAt == nil {
		t.Error("issue.DeletedAt should be non-nil after soft delete")
	}
}

func TestDeleteIssue_NotFound(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, env := doJSON(t, ts, "DELETE", "/v1/issues/td-nonexistent", nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if env.Error == nil || env.Error.Code != ErrNotFound {
		t.Errorf("error.code = %v, want %s", env.Error, ErrNotFound)
	}
}

// ============================================================================
// Action Log Verification
// ============================================================================

func TestCreateIssue_LogsAction(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := IssueCreateBody{
		Title: "Issue to verify action log entry",
	}
	_, env := doJSON(t, ts, "POST", "/v1/issues", body)
	if !env.OK {
		t.Fatalf("ok = false, error = %+v", env.Error)
	}

	// Verify action was logged by checking recent actions for the session
	actions, err := srv.db.GetRecentActions("ses_test123", 10)
	if err != nil {
		t.Fatalf("get recent actions: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("expected at least one action log entry")
	}

	found := false
	for _, a := range actions {
		if a.ActionType == models.ActionCreate && a.EntityType == "issue" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a 'create' action for entity type 'issue' in the action log")
	}
}
