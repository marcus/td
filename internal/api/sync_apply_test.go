package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// makeIssueEvent builds a single EventInput that creates an `issues` row.
// Payload uses the {new_data, previous_data} wrapper that ApplyRemoteEvents
// expects (matches what td CLI's GetPendingEvents emits).
func makeIssueEvent(t *testing.T, clientActionID int64, issueID, title, status string) EventInput {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	newData, err := json.Marshal(map[string]any{
		"id":         issueID,
		"title":      title,
		"status":     status,
		"priority":   "P1",
		"created_at": now,
		"updated_at": now,
	})
	if err != nil {
		t.Fatalf("marshal new_data: %v", err)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{
		"new_data":      newData,
		"previous_data": json.RawMessage(`null`),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return EventInput{
		ClientActionID:  clientActionID,
		ActionType:      "create",
		EntityType:      "issues",
		EntityID:        issueID,
		Payload:         payload,
		ClientTimestamp: now,
	}
}

// pushIssues sends a /v1/sync/push request with the given device/session and
// events. Uses h.Do so we can assert status precisely; returns the parsed
// PushResponse.
func pushIssues(t *testing.T, h *TestHarness, token, projectID, deviceID, sessionID string, events []EventInput) PushResponse {
	t.Helper()
	resp := h.Do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", projectID), token, PushRequest{
		DeviceID:  deviceID,
		SessionID: sessionID,
		Events:    events,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("push: expected 200, got %d", resp.StatusCode)
	}
	var pr PushResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	return pr
}

// queryIssueIDs returns issue IDs from the project.db for projectID, ordered
// by id ascending (deterministic).
func queryIssueIDs(t *testing.T, h *TestHarness, projectID string) []string {
	t.Helper()
	db, err := h.Server.projectLivePool.Acquire(projectID)
	if err != nil {
		t.Fatalf("acquire project.db: %v", err)
	}
	defer h.Server.projectLivePool.Release(projectID)

	rows, err := db.Conn().Query(`SELECT id FROM issues ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query issues: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return ids
}

// queryAppliedCursorViaHarness reads applied_events.MAX(server_seq) for the
// given project. Acquires + releases the pool handle.
func queryAppliedCursorViaHarness(t *testing.T, h *TestHarness, projectID string) int64 {
	t.Helper()
	db, err := h.Server.projectLivePool.Acquire(projectID)
	if err != nil {
		t.Fatalf("acquire project.db: %v", err)
	}
	defer h.Server.projectLivePool.Release(projectID)

	var seq int64
	if err := db.Conn().QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM applied_events`).Scan(&seq); err != nil {
		t.Fatalf("query applied_events: %v", err)
	}
	return seq
}

func TestPush_AppliesToProjectDB(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("alice@test.com").
		WithProject("p", "alice@test.com").
		Done()
	pid := state.ProjectID("p")
	tok := state.UserToken("alice@test.com")

	events := []EventInput{
		makeIssueEvent(t, 1, "iss-1", "first", "open"),
		makeIssueEvent(t, 2, "iss-2", "second", "open"),
		makeIssueEvent(t, 3, "iss-3", "third", "open"),
	}
	pr := pushIssues(t, h, tok, pid, "dev-A", "ses-A", events)
	if pr.Accepted != 3 {
		t.Fatalf("accepted: got %d, want 3", pr.Accepted)
	}

	got := queryIssueIDs(t, h, pid)
	want := []string{"iss-1", "iss-2", "iss-3"}
	if len(got) != len(want) {
		t.Fatalf("issue ids: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("issue[%d]: got %q, want %q", i, got[i], want[i])
		}
	}

	if cur := queryAppliedCursorViaHarness(t, h, pid); cur != pr.Acks[len(pr.Acks)-1].ServerSeq {
		t.Errorf("cursor: got %d, want %d", cur, pr.Acks[len(pr.Acks)-1].ServerSeq)
	}
}

func TestPush_IsIdempotent(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("alice@test.com").
		WithProject("p", "alice@test.com").
		Done()
	pid := state.ProjectID("p")
	tok := state.UserToken("alice@test.com")

	events := []EventInput{
		makeIssueEvent(t, 1, "iss-1", "first", "open"),
		makeIssueEvent(t, 2, "iss-2", "second", "open"),
	}

	// First push: 2 events accepted.
	pr1 := pushIssues(t, h, tok, pid, "dev-A", "ses-A", events)
	if pr1.Accepted != 2 {
		t.Fatalf("first push accepted: got %d, want 2", pr1.Accepted)
	}
	cursor1 := queryAppliedCursorViaHarness(t, h, pid)
	if cursor1 != pr1.Acks[len(pr1.Acks)-1].ServerSeq {
		t.Fatalf("first cursor: got %d, want %d", cursor1, pr1.Acks[len(pr1.Acks)-1].ServerSeq)
	}
	rowsAfter1 := queryIssueIDs(t, h, pid)
	if len(rowsAfter1) != 2 {
		t.Fatalf("after first push: got %d rows, want 2", len(rowsAfter1))
	}

	// Second push: identical (device, session, client_action_id) — events.db
	// dedupes (rejects as duplicates with existing server_seq), and our
	// cursor-skip path means project.db sees no new applies.
	pr2 := pushIssues(t, h, tok, pid, "dev-A", "ses-A", events)
	if pr2.Accepted != 0 {
		t.Errorf("second push accepted: got %d, want 0 (all dupes)", pr2.Accepted)
	}
	if len(pr2.Rejected) != 2 {
		t.Errorf("second push rejected: got %d, want 2", len(pr2.Rejected))
	}
	for _, rj := range pr2.Rejected {
		if rj.Reason != "duplicate" {
			t.Errorf("expected duplicate reason, got %q", rj.Reason)
		}
	}

	cursor2 := queryAppliedCursorViaHarness(t, h, pid)
	if cursor2 != cursor1 {
		t.Errorf("cursor advanced on duplicate push: %d -> %d", cursor1, cursor2)
	}
	rowsAfter2 := queryIssueIDs(t, h, pid)
	if len(rowsAfter2) != 2 {
		t.Errorf("after duplicate push: got %d rows, want 2 (no duplicates)", len(rowsAfter2))
	}
}

func TestPush_AppliesIncrementally(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("alice@test.com").
		WithProject("p", "alice@test.com").
		Done()
	pid := state.ProjectID("p")
	tok := state.UserToken("alice@test.com")

	first := []EventInput{
		makeIssueEvent(t, 1, "iss-1", "first", "open"),
		makeIssueEvent(t, 2, "iss-2", "second", "open"),
	}
	pr1 := pushIssues(t, h, tok, pid, "dev-A", "ses-A", first)
	if pr1.Accepted != 2 {
		t.Fatalf("first push accepted: got %d, want 2", pr1.Accepted)
	}
	if got := len(queryIssueIDs(t, h, pid)); got != 2 {
		t.Fatalf("after first: got %d issues, want 2", got)
	}

	second := []EventInput{
		makeIssueEvent(t, 3, "iss-3", "third", "open"),
		makeIssueEvent(t, 4, "iss-4", "fourth", "open"),
		makeIssueEvent(t, 5, "iss-5", "fifth", "open"),
	}
	pr2 := pushIssues(t, h, tok, pid, "dev-A", "ses-A", second)
	if pr2.Accepted != 3 {
		t.Fatalf("second push accepted: got %d, want 3", pr2.Accepted)
	}

	got := queryIssueIDs(t, h, pid)
	want := []string{"iss-1", "iss-2", "iss-3", "iss-4", "iss-5"}
	if len(got) != len(want) {
		t.Fatalf("issue ids: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("issue[%d]: got %q, want %q", i, got[i], want[i])
		}
	}

	cursor := queryAppliedCursorViaHarness(t, h, pid)
	wantCursor := pr2.Acks[len(pr2.Acks)-1].ServerSeq
	if cursor != wantCursor {
		t.Errorf("cursor: got %d, want %d (highest server_seq from second push)", cursor, wantCursor)
	}
}

func TestPush_ApplyError_DoesNotAdvanceCursor(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithUser("alice@test.com").
		WithProject("p", "alice@test.com").
		Done()
	pid := state.ProjectID("p")
	tok := state.UserToken("alice@test.com")

	// Seed one good event so the cursor has a known prior value.
	good := []EventInput{makeIssueEvent(t, 1, "iss-1", "good", "open")}
	prGood := pushIssues(t, h, tok, pid, "dev-A", "ses-A", good)
	if prGood.Accepted != 1 {
		t.Fatalf("seed push accepted: got %d, want 1", prGood.Accepted)
	}
	cursorBefore := queryAppliedCursorViaHarness(t, h, pid)
	if cursorBefore == 0 {
		t.Fatalf("cursor before bad push: got 0, want >0")
	}
	rowsBefore := len(queryIssueIDs(t, h, pid))

	// Construct a bad event: payload is malformed JSON. Push must still get
	// past server-side validation (entity_type is valid, timestamp parses) so
	// it lands in events.db, and ApplyRemoteEvents's per-event unmarshal then
	// fails — but ApplyRemoteEvents records that as a Failed event without
	// returning an error. The cursor still advances even when individual
	// events fail to apply, because applied_events tracks "we processed this
	// seq", not "every event applied cleanly". This is the same behavior the
	// bootstrap replay path has.
	//
	// To trigger an actual error from applyAcceptedEventsToProjectDB so the
	// cursor stays unchanged, we close the project_live_pool's handle so the
	// next Acquire returns an error. That covers the "apply step fails →
	// client retries → events.db has the row → next push reapplies" loop.
	//
	// (A payload-level malformation can't satisfy this case because
	// ApplyRemoteEvents swallows per-event errors. The contract we care about
	// here is: if the apply step as a whole errors, the cursor stays put and
	// the push handler returns 500.)
	if err := h.Server.projectLivePool.Close(); err != nil {
		t.Fatalf("close project_live_pool: %v", err)
	}
	// Replace it with a pool whose baseDir is not writable so Acquire fails
	// during bootstrap (open existing project.db still works, but we want a
	// fresh init that goes wrong). Simpler: point at a path that exists as
	// a regular file (not a directory) — MkdirAll inside openOrBootstrap
	// will fail.
	h.Server.projectLivePool = NewProjectLivePool("/dev/null/not-a-dir")

	bad := []EventInput{makeIssueEvent(t, 99, "iss-99", "after-error", "open")}
	resp := h.Do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", pid), tok, PushRequest{
		DeviceID:  "dev-A",
		SessionID: "ses-A",
		Events:    bad,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 when apply fails, got %d", resp.StatusCode)
	}

	// Restore the pool to its real baseDir so we can inspect state and verify
	// the retry path. The new pool opens the existing project.db (no
	// bootstrap because the file is already there from the first successful
	// push), so cursor + rows reflect state immediately before the failed
	// push.
	h.Server.projectLivePool = NewProjectLivePool(h.Server.config.ProjectDataDir)

	cursorAfter := queryAppliedCursorViaHarness(t, h, pid)
	if cursorAfter != cursorBefore {
		t.Errorf("cursor advanced after apply error: %d -> %d", cursorBefore, cursorAfter)
	}
	rowsAfter := len(queryIssueIDs(t, h, pid))
	if rowsAfter != rowsBefore {
		t.Errorf("rows changed after apply error: %d -> %d (project.db should be untouched)", rowsBefore, rowsAfter)
	}

	// Retry path: push the same event again. events.db dedupes (returns the
	// already-assigned server_seq), so PushResult.Accepted is 0 and our
	// helper short-circuits — project.db never sees the missed event from
	// just events.db. This is the documented limitation: applyAccepted only
	// applies events that THIS push accepted. The buildSnapshot fallback
	// (plan §7.2) is the recovery valve for events stranded in events.db.
	//
	// Push a NEW event — the helper will see cursor=1 in project.db and the
	// new event's server_seq=3 (the previous failed one took seq=2), so it
	// applies seq=3. seq=2 is permanently stranded in events.db until
	// buildSnapshot or a future bootstrap rebuild reapplies the full log.
	// What we're proving here is: after a failure, the next successful push
	// still works, the cursor moves forward, and we don't double-count.
	next := []EventInput{makeIssueEvent(t, 100, "iss-100", "after-recovery", "open")}
	prNext := pushIssues(t, h, tok, pid, "dev-A", "ses-A", next)
	if prNext.Accepted != 1 {
		t.Fatalf("retry push accepted: got %d, want 1", prNext.Accepted)
	}
	cursorRetry := queryAppliedCursorViaHarness(t, h, pid)
	if cursorRetry <= cursorBefore {
		t.Errorf("cursor after retry: got %d, want > %d", cursorRetry, cursorBefore)
	}
	if cursorRetry != prNext.Acks[0].ServerSeq {
		t.Errorf("cursor after retry: got %d, want %d", cursorRetry, prNext.Acks[0].ServerSeq)
	}
}
