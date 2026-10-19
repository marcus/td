package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func requestRevision(t *testing.T, ts *httptest.Server, method, path, revision string, body interface{}) int {
	t.Helper()
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(data))
	req.Header.Set("If-Match", `"`+revision+`"`)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestClientStaleWritesPreserveAgentChange(t *testing.T) {
	srv := newTestServerWithDB(t)
	issue := &models.Issue{Title: "Original task", Status: models.StatusInReview}
	if err := srv.db.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	loaded, _ := srv.db.GetIssue(issue.ID)
	oldRevision := issueRevision(loaded)
	loaded.Title = "Agent updated the task"
	if err := srv.db.UpdateIssueLogged(loaded, "ses_agent", models.ActionUpdate); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, tc := range []struct {
		method, path string
		body         interface{}
	}{
		{"PATCH", "/v1/issues/" + issue.ID, map[string]string{"title": "Browser drafted a different title"}},
		{"POST", "/v1/issues/" + issue.ID + "/approve", map[string]string{"reason": "Reviewed stale content"}},
		{"DELETE", "/v1/issues/" + issue.ID, nil},
	} {
		if status := requestRevision(t, ts, tc.method, tc.path, oldRevision, tc.body); status != http.StatusConflict {
			t.Fatalf("%s: got %d", tc.method, status)
		}
	}
	current, _ := srv.db.GetIssue(issue.ID)
	if current.Title != "Agent updated the task" || current.Status != models.StatusInReview || current.DeletedAt != nil {
		t.Fatalf("stale write changed task: %+v", current)
	}
	if status := requestRevision(t, ts, "PATCH", "/v1/issues/"+issue.ID, issueRevision(current), map[string]string{"description": "Explicitly reconciled"}); status != http.StatusOK {
		t.Fatalf("fresh save: %d", status)
	}
	current, _ = srv.db.GetIssue(issue.ID)
	if current.Title != "Agent updated the task" || current.Description != "Explicitly reconciled" {
		t.Fatalf("partial save lost fields: %+v", current)
	}
}

func TestClientRecordedApprovalRevisionGuard(t *testing.T) {
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	srv := newTestServerWithDB(t)
	id := seedInReviewIssue(t, srv.db, "ses_agent")
	original, _ := srv.db.GetIssue(id)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, env := doJSON(t, ts, "POST", "/v1/issues/"+id+"/reviews", map[string]string{
		"decision": "approved", "summary": "Verified current evidence",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("record approval: %d %+v", resp.StatusCode, env.Error)
	}
	body := map[string]string{"reason": "Close using recorded approval"}
	if status := requestRevision(t, ts, "POST", "/v1/issues/"+id+"/approve", issueRevision(original), body); status != http.StatusConflict {
		t.Fatalf("stale close after review: %d", status)
	}
	current, _ := srv.db.GetIssue(id)
	if current.Status != models.StatusInReview {
		t.Fatalf("stale action closed task: %+v", current)
	}
	if status := requestRevision(t, ts, "POST", "/v1/issues/"+id+"/approve", issueRevision(current), body); status != http.StatusOK {
		t.Fatalf("fresh close after review: %d", status)
	}
	closed, _ := srv.db.GetIssue(id)
	reviews, err := srv.db.ListIssueReviews(id)
	if closed.Status != models.StatusClosed || closed.ReviewerSession != current.ReviewerSession || err != nil || len(reviews) != 1 {
		t.Fatalf("recorded approval not preserved: %+v, %v, %v", closed, reviews, err)
	}
}

func TestClientMoveUnpositionedTasks(t *testing.T) {
	srv := newTestServerWithDB(t)
	board, err := srv.db.CreateBoard("Client ordering", "")
	if err != nil {
		t.Fatal(err)
	}
	var issues []models.Issue
	for _, title := range []string{"First task", "Second task", "Third task"} {
		issue := models.Issue{Title: title}
		if err := srv.db.CreateIssue(&issue); err != nil {
			t.Fatal(err)
		}
		issues = append(issues, issue)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, move := range []struct {
		id, before string
		want       []string
	}{
		{issues[2].ID, issues[0].ID, []string{issues[2].ID, issues[0].ID, issues[1].ID}},
		{issues[2].ID, "", []string{issues[0].ID, issues[1].ID, issues[2].ID}},
		{issues[2].ID, issues[1].ID, []string{issues[0].ID, issues[2].ID, issues[1].ID}},
	} {
		resp, env := doJSON(t, ts, "POST", "/v1/boards/"+board.ID+"/move", map[string]string{"issue_id": move.id, "before_id": move.before})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("move: %+v", env.Error)
		}
		views, err := srv.db.GetBoardIssues(board.ID, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range move.want {
			if views[i].Issue.ID != want {
				t.Fatalf("position %d = %s, want %s", i, views[i].Issue.ID, want)
			}
		}
	}
}

func TestClientParentCycleRejected(t *testing.T) {
	srv := newTestServerWithDB(t)
	parent := &models.Issue{Title: "Parent task for client test"}
	if err := srv.db.CreateIssue(parent); err != nil {
		t.Fatal(err)
	}
	child := &models.Issue{Title: "Child task for client test", ParentID: parent.ID}
	if err := srv.db.CreateIssue(child); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, parentID := range []string{parent.ID, child.ID} {
		resp, _ := doJSON(t, ts, "PATCH", "/v1/issues/"+parent.ID, map[string]string{"parent_id": parentID})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("cycle accepted: %d", resp.StatusCode)
		}
	}
}

func TestClientProjectMetadata(t *testing.T) {
	srv := newTestServerWithDB(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, env := doJSON(t, ts, "GET", "/v1/project", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata: %+v", env)
	}
	data := env.Data.(map[string]interface{})
	if data["path"] != srv.baseDir || data["session_id"] != srv.sessionID {
		t.Fatalf("identity: %+v", data)
	}
	if data["title_min_length"].(float64) <= 0 || data["title_max_length"].(float64) < data["title_min_length"].(float64) {
		t.Fatalf("title limits: %+v", data)
	}
	caps := data["capabilities"].([]interface{})
	if len(caps) != 3 || caps[0] != "issue_revisions" || caps[1] != "board_move" || caps[2] != "atomic_label_updates" {
		t.Fatalf("capabilities: %+v", caps)
	}
	for _, path := range []string{"/", "/assets/app.js", "/v1/browser", "/v1/markdown"} {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("API server exposes UI route %s: %d", path, w.Code)
		}
	}
}

func TestClientConditionalCORSPreflight(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "http://localhost:3000"})
	r := httptest.NewRequest(http.MethodOptions, "/v1/issues/td-test", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	r.Header.Set("Access-Control-Request-Headers", "content-type,if-match")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: %d", w.Code)
	}
	for _, header := range strings.Split(w.Header().Get("Access-Control-Allow-Headers"), ",") {
		if strings.EqualFold(strings.TrimSpace(header), "If-Match") {
			return
		}
	}
	t.Fatal("CORS preflight does not allow conditional writes")
}

func TestClientChildRevisionMatchesDetail(t *testing.T) {
	srv := newTestServerWithDB(t)
	parent := &models.Issue{Title: "Parent of a reviewed child"}
	if err := srv.db.CreateIssue(parent); err != nil {
		t.Fatal(err)
	}
	child := &models.Issue{Title: "Child with review metadata", ParentID: parent.ID}
	if err := srv.db.CreateIssue(child); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	child.Status = models.StatusClosed
	child.ReviewRequestedBySession = "ses_agent"
	child.ReviewerSession = "ses_reviewer"
	child.ClosedBySession = "ses_closer"
	child.ReviewedAt = &now
	child.ClosedAt = &now
	if err := srv.db.UpdateIssue(child); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, env := doJSON(t, ts, "GET", "/v1/issues/"+parent.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("parent detail: %d %+v", resp.StatusCode, env.Error)
	}
	children := env.Data.(map[string]interface{})["children"].([]interface{})
	if len(children) != 1 {
		t.Fatalf("children: %+v", children)
	}
	fromParent := children[0].(map[string]interface{})
	_, detail := doJSON(t, ts, "GET", "/v1/issues/"+child.ID, nil)
	fromDetail := detail.Data.(map[string]interface{})["issue"].(map[string]interface{})
	if fromParent["revision"] != fromDetail["revision"] {
		t.Errorf("unchanged child revision differs between parent and detail")
	}
	if status := requestRevision(t, ts, "PATCH", "/v1/issues/"+child.ID, fromParent["revision"].(string), map[string]string{"description": "Updated using child revision"}); status != http.StatusOK {
		t.Fatalf("fresh child write: %d", status)
	}
}

func TestClientRecordReviewRevisionGuard(t *testing.T) {
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "trusted")
	for _, decision := range []string{"approved", "changes_requested"} {
		t.Run(decision, func(t *testing.T) {
			srv := newTestServerWithDB(t)
			id := seedInReviewIssue(t, srv.db, "ses_agent")
			original, _ := srv.db.GetIssue(id)
			staleRevision := issueRevision(original)
			original.Title = "Agent changed the review evidence"
			if err := srv.db.UpdateIssueLogged(original, "ses_agent", models.ActionUpdate); err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			body := map[string]string{"decision": decision, "summary": "Reviewed the task evidence"}
			if status := requestRevision(t, ts, "POST", "/v1/issues/"+id+"/reviews", staleRevision, body); status != http.StatusConflict {
				t.Fatalf("stale record-only review: %d", status)
			}
			reviews, err := srv.db.ListIssueReviews(id)
			if err != nil || len(reviews) != 0 {
				t.Fatalf("stale request recorded reviews: %+v, %v", reviews, err)
			}
			current, _ := srv.db.GetIssue(id)
			if current.Title != original.Title || current.Status != models.StatusInReview {
				t.Fatalf("stale request changed task: %+v", current)
			}
			if status := requestRevision(t, ts, "POST", "/v1/issues/"+id+"/reviews", issueRevision(current), body); status != http.StatusCreated {
				t.Fatalf("fresh record-only review: %d", status)
			}
		})
	}
}

func TestClientMoveIgnoresHiddenClosedTasks(t *testing.T) {
	srv := newTestServerWithDB(t)
	board, err := srv.db.CreateBoard("Client closed tasks", "")
	if err != nil {
		t.Fatal(err)
	}
	var issues []models.Issue
	for i, status := range []models.Status{models.StatusClosed, models.StatusOpen, models.StatusClosed, models.StatusOpen, models.StatusOpen} {
		issue := models.Issue{Title: "Closed filter task " + string(rune('A'+i)), Status: status}
		if err := srv.db.CreateIssue(&issue); err != nil {
			t.Fatal(err)
		}
		issues = append(issues, issue)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	countPositions := func() int {
		views, err := srv.db.GetBoardIssues(board.ID, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, v := range views {
			if v.HasPosition {
				n++
			}
		}
		return n
	}

	// Default view hides closed tasks: moving to the end positions only open ones.
	resp, env := doJSON(t, ts, "POST", "/v1/boards/"+board.ID+"/move", map[string]string{"issue_id": issues[1].ID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %+v", env.Error)
	}
	if got := countPositions(); got != 3 {
		t.Fatalf("positioned %d tasks, want the 3 open ones", got)
	}

	// A closed anchor still resolves, and include_closed matches a view showing closed tasks.
	resp, env = doJSON(t, ts, "POST", "/v1/boards/"+board.ID+"/move", map[string]interface{}{"issue_id": issues[4].ID, "before_id": issues[2].ID, "include_closed": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move before closed task: %+v", env.Error)
	}
	views, err := srv.db.GetBoardIssues(board.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range views {
		if v.Issue.ID == issues[4].ID {
			if views[i+1].Issue.ID != issues[2].ID {
				t.Fatalf("moved task is followed by %s, want %s", views[i+1].Issue.ID, issues[2].ID)
			}
			return
		}
	}
	t.Fatal("moved task missing from board")
}
