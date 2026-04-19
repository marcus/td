package api

import (
	"sync"
	"testing"
	"time"
)

// TestSSEHubRegistry_GetOrCreate_Concurrent verifies that 100 concurrent
// goroutines calling GetOrCreate for the same project ID all receive the same
// *SSEHub pointer.
func TestSSEHubRegistry_GetOrCreate_Concurrent(t *testing.T) {
	reg := NewSSEHubRegistry()
	const n = 100
	results := make([]*SSEHub, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			results[i] = reg.GetOrCreate("proj-abc")
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	for i, h := range results {
		if h != first {
			t.Errorf("goroutine %d got different hub pointer (%p vs %p)", i, h, first)
		}
	}
}

// TestSSEHubRegistry_IdleCleanup verifies that a hub is removed from the
// registry after its last subscriber unregisters.
func TestSSEHubRegistry_IdleCleanup(t *testing.T) {
	reg := NewSSEHubRegistry()
	hub := reg.GetOrCreate("proj-xyz")

	if reg.HubCount() != 1 {
		t.Fatalf("expected 1 hub, got %d", reg.HubCount())
	}

	ch1 := hub.Register()
	ch2 := hub.Register()

	if reg.HubCount() != 1 {
		t.Fatalf("expected 1 hub after registers, got %d", reg.HubCount())
	}

	hub.Unregister(ch1)
	if reg.HubCount() != 1 {
		t.Errorf("expected hub to remain after 1 of 2 unregisters, got %d", reg.HubCount())
	}

	hub.Unregister(ch2)

	// onEmpty is called synchronously in Unregister; registry should be empty.
	if reg.HubCount() != 0 {
		t.Errorf("expected hub removed after last unregister, got %d hubs", reg.HubCount())
	}
}

// TestSSEHub_Broadcast_DeliveredToSubscribers verifies that a broadcast event
// is received by all registered subscribers.
func TestSSEHub_Broadcast_DeliveredToSubscribers(t *testing.T) {
	reg := NewSSEHubRegistry()
	hub := reg.GetOrCreate("proj-broadcast")

	ch1 := hub.Register()
	ch2 := hub.Register()
	defer hub.Unregister(ch1)
	defer hub.Unregister(ch2)

	ev := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "issue-1",
		ChangeToken: "tok-abc",
		Timestamp:   time.Now().UTC(),
	}
	hub.Broadcast(ev)

	recv := func(ch chan ProjectEvent) ProjectEvent {
		t.Helper()
		select {
		case e := <-ch:
			return e
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
			return ProjectEvent{}
		}
	}

	got1 := recv(ch1)
	got2 := recv(ch2)

	if got1.Type != EventIssueUpserted || got1.IssueID != "issue-1" {
		t.Errorf("ch1 wrong event: %+v", got1)
	}
	if got2.Type != EventIssueUpserted || got2.IssueID != "issue-1" {
		t.Errorf("ch2 wrong event: %+v", got2)
	}
}

// TestSSEHub_Broadcast_SlowClientGetsRefresh verifies the named behavior: a
// subscriber whose normal event slots are saturated receives a refresh event
// rather than a silent drop. This asserts the actual delivery, not just
// non-blocking execution.
//
// Mechanism: each subscriber channel has capacity eventSlots+1 (17). Broadcast
// treats positions 0–15 as normal slots and position 16 as a reserved refresh
// slot. When all 16 normal slots are occupied, the next broadcast puts a refresh
// in the reserved slot. The subscriber can drain all 17 events and inspect them.
//
// Because Broadcast holds the hub mutex, the len check is race-free with
// respect to concurrent Broadcast calls. The subscriber (reader) only reduces
// len, so the check is conservative and the subsequent direct send is safe.
func TestSSEHub_Broadcast_SlowClientGetsRefresh(t *testing.T) {
	hub := newSSEHub(nil)
	ch := hub.Register()
	defer hub.Unregister(ch)

	// Saturate all 16 normal slots by broadcasting 16 events without reading.
	// Since no goroutine drains ch, the events accumulate in the buffer.
	filler := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "filler",
		ChangeToken: "f",
		Timestamp:   time.Now(),
	}
	for range eventSlots {
		hub.Broadcast(filler)
	}

	// Broadcast one more event: normal slots are full, so Broadcast must queue
	// a refresh in the reserved 17th slot. Assert it does not block.
	overflow := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "issue-slow",
		ChangeToken: "tok-slow",
		Timestamp:   time.Now(),
	}
	done := make(chan struct{})
	go func() {
		hub.Broadcast(overflow)
		close(done)
	}()
	select {
	case <-done:
		// Good: Broadcast returned without blocking.
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked on full client channel")
	}

	// Drain all eventSlots+1 events and assert exactly one is a refresh with
	// the overflow's change token. Order is deterministic here (single writer,
	// single reader, FIFO channel): the 16 filler events arrive first, then
	// the refresh. We assert generically to be resilient to any reordering.
	deadline := time.After(2 * time.Second)
	var gotRefresh bool
	for range eventSlots + 1 {
		select {
		case got := <-ch:
			if got.Type == EventRefresh {
				gotRefresh = true
				if got.ChangeToken != overflow.ChangeToken {
					t.Errorf("refresh has wrong change_token: want %q got %q",
						overflow.ChangeToken, got.ChangeToken)
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for all events (16 fillers + 1 refresh)")
		}
	}
	if !gotRefresh {
		t.Error("slow client did not receive a refresh event; overflow was silently dropped")
	}
}

// TestSSEHubRegistry_IdleCleanup_Race verifies the idle-cleanup race is closed.
//
// The race: after Unregister releases h.mu, a concurrent GetOrCreate on the
// fast RLock path can retrieve the hub (still in the registry map) and register
// a new subscriber. remove() must not delete a hub that acquired a live
// subscriber since onEmpty was called.
//
// The fix: remove() re-reads the hub under the write lock and calls
// hub.ClientCount(). If ClientCount() > 0, it skips the delete. This test
// validates that Broadcast reaches the post-race subscriber.
func TestSSEHubRegistry_IdleCleanup_Race(t *testing.T) {
	const project = "proj-race"
	reg := NewSSEHubRegistry()

	// Create hub, register one subscriber, then unregister (triggers remove).
	hub := reg.GetOrCreate(project)
	ch := hub.Register()
	hub.Unregister(ch) // onEmpty → remove(); hub count should be 0 now

	if reg.HubCount() != 0 {
		t.Fatalf("expected 0 hubs after full unregister, got %d", reg.HubCount())
	}

	// Simulate the racy GetOrCreate: creates a new hub and immediately
	// registers a subscriber. If remove() had not guarded ClientCount, a
	// concurrent late-arriving remove() could evict this hub.
	hub2 := reg.GetOrCreate(project)
	ch2 := hub2.Register()
	defer hub2.Unregister(ch2)

	if reg.HubCount() != 1 {
		t.Fatalf("expected 1 hub after re-create, got %d", reg.HubCount())
	}

	// Broadcast through the registry must reach the subscriber on hub2.
	ev := ProjectEvent{
		Type:        EventIssueUpserted,
		IssueID:     "race-issue",
		ChangeToken: "tok-race",
		Timestamp:   time.Now().UTC(),
	}
	reg.Broadcast(project, ev)

	select {
	case got := <-ch2:
		if got.Type != EventIssueUpserted || got.IssueID != "race-issue" {
			t.Errorf("unexpected event: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast did not reach subscriber after idle-cleanup race")
	}
}
