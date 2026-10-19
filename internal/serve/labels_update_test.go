package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestUpdateIssueAtomicLabelsComment(t *testing.T) {
	srv := newTestServerWithDB(t)
	issue := &models.Issue{Title: "Atomic labels through API", Labels: []string{"for-marcus", "keep"}}
	if err := srv.db.CreateIssue(issue); err != nil {
		t.Fatal(err)
	}
	notifyCount := 0
	ctx := srv.handlerContext()
	ctx.Config.NotifyChange = func() { notifyCount++ }
	loaded, _ := srv.db.GetIssue(issue.ID)
	revision := issueRevision(loaded)
	request := func(body, match string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPatch, "/v1/issues/"+issue.ID, strings.NewReader(body))
		r.SetPathValue("id", issue.ID)
		if match != "" {
			r.Header.Set("If-Match", `"`+match+`"`)
		}
		w := httptest.NewRecorder()
		HandleUpdateIssue(ctx, w, r)
		return w
	}
	w := request(`{"labels_add":["ready-for-clara","ready-for-clara"],"labels_remove":["for-marcus","absent"],"comment":"Ready again"}`, revision)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	current, _ := srv.db.GetIssue(issue.ID)
	if !reflect.DeepEqual(current.Labels, []string{"keep", "ready-for-clara"}) {
		t.Fatal(current.Labels)
	}
	comments, _ := srv.db.GetComments(issue.ID)
	if len(comments) != 1 || comments[0].Text != "Ready again" {
		t.Fatal(comments)
	}
	if notifyCount != 1 {
		t.Fatalf("notify count=%d", notifyCount)
	}
	var response struct {
		Data struct {
			Issue IssueDTO `json:"issue"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Data.Issue.Labels, current.Labels) {
		t.Fatal("response differs from stored issue")
	}
	w = request(`{"labels_add":["stale"],"comment":"Must not land"}`, revision)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", w.Code, w.Body.String())
	}
	for _, bad := range []string{
		`{"labels":[],"labels_add":["bad"],"comment":"No write"}`,
		`{"labels_add":[" x "],"labels_remove":["x"]}`,
		`{"labels_add":[""]}`,
		`{"labels_add":["a,b"]}`,
		`{"labels_add":["invalid"],"comment":" "}`,
	} {
		w = request(bad, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid request %s: %d %s", bad, w.Code, w.Body.String())
		}
	}
	after, _ := srv.db.GetIssue(issue.ID)
	if issueRevision(after) != issueRevision(current) {
		t.Fatal("rejected request partially wrote issue")
	}
	comments, _ = srv.db.GetComments(issue.ID)
	if len(comments) != 1 {
		t.Fatal("rejected request wrote comment")
	}
	if notifyCount != 1 {
		t.Fatal("rejected request sent change notification")
	}
	w = request(`{"comment":"Comment only"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("comment only: %d %s", w.Code, w.Body.String())
	}
	comments, _ = srv.db.GetComments(issue.ID)
	if len(comments) != 2 {
		t.Fatal(comments)
	}
	w = request(`{"labels":[]}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	after, _ = srv.db.GetIssue(issue.ID)
	if len(after.Labels) != 0 {
		t.Fatal(after.Labels)
	}
}
