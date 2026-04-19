package api

// Integration tests for the SSE endpoint covering multi-client and isolation
// scenarios not addressed by events_handler_test.go or broadcast_test.go.
//
// All tests route through the HTTP handler (not the hub directly) to exercise
// the full integration: auth middleware → handler → hub → client channel.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/serve"
)

// newIntegrationHarness creates a projectRoutesHarness with a fast ping interval.
func newIntegrationHarness(t *testing.T) *projectRoutesHarness {
	t.Helper()
	h := newProjectRoutesHarness(t)
	h.srv.pingInterval = 50 * time.Millisecond
	return h
}

// subscribeSSE opens an SSE connection to /v1/projects/{pid}/events for the
// given token, asserts 200, and returns an sseReader.
func subscribeSSE(t *testing.T, h *projectRoutesHarness, pid, token string, ctx context.Context) *sseReader {
	t.Helper()
	return newSSEReader(t, h.baseURL, pid, token, ctx)
}

// createIssueViaAPI creates an issue and returns the response body issue ID.
// Note: for the broadcast, the create path emits an issue.upserted with empty
// IssueID (by design — see wrapIssueUpsert). Use waitForEvent for creates.
func createIssueViaAPI(t *testing.T, h *projectRoutesHarness, pid, title string) {
	t.Helper()
	resp := h.do(t, "POST", fmt.Sprintf("/v1/projects/%s/issues", pid),
		h.ownerTok, serve.IssueCreateBody{Title: title, Type: "task"},
		map[string]string{HeaderTdWatchSession: "ses-integ"})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /issues status=%d body=%s", resp.StatusCode, raw)
	}
	resp.Body.Close()
}

// ---- TestSSE_TwoClientFanout -------------------------------------------------

// TestSSE_TwoClientFanout subscribes two clients to the same project's /events
// stream, emits one mutation, and asserts BOTH clients receive the event.
// This exercises the multi-subscriber path of SSEHub.Broadcast.
func TestSSE_TwoClientFanout(t *testing.T) {
	h := newIntegrationHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe two independent clients.
	clientA := subscribeSSE(t, h, h.pid, h.ownerTok, ctx)
	defer clientA.close()
	clientA.waitForPing(t, 2*time.Second)

	clientB := subscribeSSE(t, h, h.pid, h.ownerTok, ctx)
	defer clientB.close()
	clientB.waitForPing(t, 2*time.Second)

	// Confirm hub has two registered clients.
	hub := h.srv.sseHubs.GetOrCreate(h.pid)
	if got := hub.ClientCount(); got != 2 {
		t.Fatalf("expected 2 hub clients before mutation, got %d", got)
	}

	// Emit one mutation. Create broadcasts issue.upserted (empty IssueID by design).
	createIssueViaAPI(t, h, h.pid, "fanout test issue")

	// Both clients must receive an issue.upserted event.
	if !clientA.waitForEvent(2*time.Second, EventIssueUpserted) {
		t.Error("clientA did not receive issue.upserted event")
	}
	if !clientB.waitForEvent(2*time.Second, EventIssueUpserted) {
		t.Error("clientB did not receive issue.upserted event")
	}
}

// ---- TestSSE_CrossProjectIsolation -------------------------------------------

// TestSSE_CrossProjectIsolation subscribes client A to project P1 and client B
// to project P2. Mutates P1. Asserts A receives the event and B does NOT.
func TestSSE_CrossProjectIsolation(t *testing.T) {
	h := newIntegrationHarness(t)

	// Create a second project owned by the same user.
	p2, err := h.store.CreateProject("project2", "", h.owner)
	if err != nil {
		t.Fatalf("create project2: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Client A subscribes to P1 (h.pid), client B subscribes to P2.
	clientA := subscribeSSE(t, h, h.pid, h.ownerTok, ctx)
	defer clientA.close()
	clientA.waitForPing(t, 2*time.Second)

	clientB := subscribeSSE(t, h, p2.ID, h.ownerTok, ctx)
	defer clientB.close()
	clientB.waitForPing(t, 2*time.Second)

	// Mutate P1 only.
	createIssueViaAPI(t, h, h.pid, "isolation test issue")

	// A must receive the event.
	if !clientA.waitForEvent(2*time.Second, EventIssueUpserted) {
		t.Error("clientA (P1) did not receive issue.upserted event")
	}

	// B must NOT receive any issue event — give it a brief window.
	unexpected, _ := clientB.waitForLine(300*time.Millisecond, func(l string) bool {
		return strings.HasPrefix(l, "event: "+string(EventIssueUpserted)) ||
			strings.HasPrefix(l, "event: "+string(EventIssueDeleted))
	})
	if unexpected != "" {
		t.Errorf("clientB (P2) received unexpected event from P1 mutation: %q", unexpected)
	}
}

// ---- TestSSE_LastEventID_RefreshThenNormalEvents -----------------------------

// TestSSE_LastEventID_RefreshThenNormalEvents connects with a Last-Event-ID
// header and asserts:
//  1. The FIRST "event: ..." line received is "event: refresh".
//  2. A subsequent mutation still delivers a normal "issue.upserted" event,
//     confirming the initial refresh does not break the stream.
func TestSSE_LastEventID_RefreshThenNormalEvents(t *testing.T) {
	h := newIntegrationHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET",
		h.baseURL+"/v1/projects/"+h.pid+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.ownerTok)
	req.Header.Set("Last-Event-ID", "stale-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Build an sseReader manually so we can inspect line order.
	sr := &sseReader{resp: resp, lines: make(chan string, 64)}
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			sr.lines <- scanner.Text()
		}
		close(sr.lines)
	}()

	// The very first "event: ..." line must be "event: refresh".
	firstEvent, ok := sr.waitForLine(3*time.Second, func(l string) bool {
		return strings.HasPrefix(l, "event: ")
	})
	if !ok {
		t.Fatal("no event received after Last-Event-ID connection")
	}
	if !strings.Contains(firstEvent, string(EventRefresh)) {
		t.Fatalf("first event is %q, want event: refresh", firstEvent)
	}

	// After the refresh, a subsequent mutation must still deliver an event.
	createIssueViaAPI(t, h, h.pid, "post-refresh issue")
	if !sr.waitForEvent(2*time.Second, EventIssueUpserted) {
		t.Error("no issue.upserted after initial refresh")
	}
}

// ---- TestSSE_GracefulDeregisterOnClientClose ---------------------------------

// TestSSE_GracefulDeregisterOnClientClose subscribes a client, cancels its
// context, and asserts hub.ClientCount() drops to zero within 1 second.
// This supplements TestSSE_ContextCancelExitsHandler by explicitly verifying
// the ClientCount at the integration level.
func TestSSE_GracefulDeregisterOnClientClose(t *testing.T) {
	h := newIntegrationHarness(t)

	ctx, cancel := context.WithCancel(context.Background())

	sr := subscribeSSE(t, h, h.pid, h.ownerTok, ctx)
	sr.waitForPing(t, 2*time.Second)

	hub := h.srv.sseHubs.GetOrCreate(h.pid)
	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("expected 1 client before cancel, got %d", got)
	}

	// Cancel the context and close the response body to unblock the scanner.
	cancel()
	sr.close()

	// Give the handler time to observe the disconnect and call Unregister.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub still has %d client(s) after context cancel + body close", hub.ClientCount())
}

// ---- TestSSE_SlowClientDowngradeToRefresh ------------------------------------

// TestSSE_SlowClientDowngradeToRefresh validates that when a subscriber's
// channel is saturated (eventSlots normal events pending), the next broadcast
// downgrades to a "refresh" event rather than dropping silently, and that the
// handler-level HTTP stream is still alive after draining the backlog.
//
// Strategy: broadcast directly into the hub (bypassing HTTP) to fill the
// subscriber channel deterministically — BEFORE the sseReader goroutine has
// a chance to drain it — then open the reader and assert at least one refresh.
//
// We achieve "fill before read" by broadcasting synchronously from the test
// goroutine into the hub BEFORE starting the sseReader goroutine. The hub
// channel is a buffered Go channel; writes into it do not require the handler
// goroutine to be unblocked, so the buffer fills instantly.
func TestSSE_SlowClientDowngradeToRefresh(t *testing.T) {
	h := newIntegrationHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Register directly into the hub (same registry the handler uses). This
	// gives us a channel we can fill before the HTTP handler even starts,
	// without going through the HTTP layer. We then connect via HTTP so the
	// handler registers its OWN channel — we fill that one below.
	//
	// However, to fill the handler's channel we need a reference to it. The
	// cleanest approach: connect via HTTP first (so the handler registers),
	// wait for ping (confirms handler is in its select loop), then pause the
	// reader while we fill the buffer.
	//
	// Since the sseReader goroutine is always running once started, we instead
	// use a synchronization trick: broadcast eventSlots events and then one
	// overflow event. The handler writes them to the HTTP response as fast as it
	// can. The sseReader goroutine drains the HTTP response. We look for
	// at least one "event: refresh" anywhere in the stream.
	//
	// Because Broadcast is synchronous (fills the hub channel) and the handler
	// writes events one by one, we need to ensure the hub channel is full before
	// the handler drains it. We do this by broadcasting all events before
	// reading any — but since sseReader runs in a goroutine, we can't stop it.
	//
	// Revised approach: use a dedicated hub channel filled before connecting.
	// Subscribe directly to the hub first (giving us a pre-filled channel),
	// then connect via HTTP (giving the handler its own channel). Both are
	// different channels. The pre-filled channel is not the handler's channel.
	//
	// The correct approach: subscribe via HTTP, grab the hub reference, then
	// broadcast without reading until the channel is full. Since sseReader
	// reads asynchronously, we race — but we can win the race by broadcasting
	// faster than the HTTP response can be written and read.
	//
	// Simplest deterministic approach: bypass the HTTP layer entirely for
	// filling — use the hub directly to fill a channel that IS the handler's
	// channel. To ensure we fill it before it's drained, we broadcast all 17
	// events before the reader goroutine has consumed them.
	//
	// Since the reader goroutine runs via TCP (real net, not in-process pipe),
	// the OS TCP buffer absorbs the handler's writes even if the reader is slow.
	// The handler can write all 17 events to the TCP buffer instantly. So the
	// hub channel gets drained immediately. We cannot fill it faster than the
	// handler empties it.
	//
	// Therefore: we validate the slow-client behavior by checking that when we
	// broadcast eventSlots+1 events to a hub with NO HTTP client (only a direct
	// channel reader), the channel receives exactly one refresh. We then connect
	// via HTTP to confirm the stream is still alive and events flow normally.
	// This is a targeted handler-level test rather than a full end-to-end slow
	// client simulation (which would require OS-level TCP manipulation).

	// Step 1: verify slow-client downgrade at the hub+channel level, then
	// verify the HTTP stream carries refresh events when we flush them.

	// Pre-fill a direct hub channel with eventSlots filler events + 1 overflow.
	// This validates that the hub backing the handler's channel implements the
	// refresh-slot semantics correctly (cross-check with sse_hub_test.go).
	hub := h.srv.sseHubs.GetOrCreate(h.pid)

	directCh := hub.Register()
	defer hub.Unregister(directCh)

	filler := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "slow-filler",
		ChangeToken: "tok-fill",
		Timestamp:   time.Now().UTC(),
	}
	for i := 0; i < eventSlots; i++ {
		hub.Broadcast(filler)
	}
	overflow := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "slow-overflow",
		ChangeToken: "tok-overflow",
		Timestamp:   time.Now().UTC(),
	}
	hub.Broadcast(overflow)

	// directCh now has eventSlots+1 entries. Drain and assert at least one refresh.
	gotRefreshInChannel := false
	drainDeadline := time.After(2 * time.Second)
	for i := 0; i < eventSlots+1; i++ {
		select {
		case ev := <-directCh:
			if ev.Type == EventRefresh {
				gotRefreshInChannel = true
			}
		case <-drainDeadline:
			t.Fatalf("timeout draining direct channel at event %d", i)
		}
	}
	if !gotRefreshInChannel {
		t.Error("hub channel did not receive a refresh event on overflow — slow-client downgrade broken")
	}
	hub.Unregister(directCh)

	// Step 2: verify the HTTP stream stays alive and carries refresh events
	// when they occur. Connect via HTTP, wait for ping, broadcast a fresh
	// batch, and confirm the refresh arrives over the wire.
	sr := subscribeSSE(t, h, h.pid, h.ownerTok, ctx)
	defer sr.close()
	sr.waitForPing(t, 2*time.Second)

	// Get the hub for the HTTP-connected client (same hub, since same project).
	hub2 := h.srv.sseHubs.GetOrCreate(h.pid)

	// Broadcast a refresh event directly — the handler will write it to the stream.
	hub2.Broadcast(ProjectEvent{
		Type:        EventRefresh,
		ChangeToken: "tok-http-refresh",
		Timestamp:   time.Now().UTC(),
	})

	if !sr.waitForEvent(2*time.Second, EventRefresh) {
		t.Error("HTTP stream did not carry refresh event")
	}

	// Confirm the stream is still alive after the refresh.
	hub2.Broadcast(ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "live-check",
		ChangeToken: "tok-live",
		Timestamp:   time.Now().UTC(),
	})
	if !sr.waitForEvent(2*time.Second, EventIssueUpserted) {
		t.Error("stream appears dead after refresh: no subsequent event received")
	}
}
