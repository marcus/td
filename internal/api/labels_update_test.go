package api

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/marcus/td/internal/serve"
)

func TestProjectUpdateAtomicLabelsComment(t *testing.T) {
	h := newBroadcastHarness(t)
	headers := map[string]string{HeaderTdWatchSession: "ses-atomic-labels"}
	resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", h.pid), h.ownerTok, serve.IssueCreateBody{Title: "Hosted atomic label update", Labels: []string{"keep", "for-marcus"}}, headers)
	var created struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, resp, &created)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sse := newSSEReader(t, h.baseURL, h.pid, h.ownerTok, ctx)
	defer sse.close()
	sse.waitForPing(t, 2*time.Second)
	comment := "Ready through hosted API"
	resp = h.do(t, "PATCH", fmt.Sprintf("/v1/projects/%s/issues/%s", h.pid, created.Issue.ID), h.ownerTok, serve.IssueUpdateBody{LabelsAdd: []string{"ready-for-clara"}, LabelsRemove: []string{"for-marcus"}, Comment: &comment}, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d", resp.StatusCode)
	}
	var updated struct {
		Issue serve.IssueDTO `json:"issue"`
	}
	readEnvelope(t, resp, &updated)
	if !reflect.DeepEqual(updated.Issue.Labels, []string{"keep", "ready-for-clara"}) {
		t.Fatal(updated.Issue.Labels)
	}
	if !sse.waitForData(time.Second, created.Issue.ID) {
		t.Fatal("missing issue upsert notification")
	}
	resp = h.do(t, "GET", fmt.Sprintf("/v1/projects/%s/issues/%s", h.pid, created.Issue.ID), h.ownerTok, nil, headers)
	var detail struct {
		Comments []struct {
			Text string `json:"text"`
		} `json:"comments"`
	}
	readEnvelope(t, resp, &detail)
	if len(detail.Comments) != 1 || detail.Comments[0].Text != comment {
		t.Fatalf("detail after upsert=%+v", detail)
	}
	events := readPromotedEvents(t, openEventsDBRO(t, h.dataDir, h.pid))
	issueUpdates, commentCreates := 0, 0
	for _, event := range events {
		if event.EntityType == "issues" && event.EntityID == created.Issue.ID && event.ActionType == "update" {
			issueUpdates++
		}
		if event.EntityType == "comments" && event.ActionType == "create" {
			commentCreates++
		}
	}
	if issueUpdates != 1 || commentCreates != 1 {
		t.Fatalf("promoted update/comment counts=%d/%d; events=%+v", issueUpdates, commentCreates, events)
	}
}
