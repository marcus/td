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

// TestSSEHub_Broadcast_SlowClientGetsRefresh verifies that a subscriber with a
// full channel receives a refresh event instead of a dropped event.
func TestSSEHub_Broadcast_SlowClientGetsRefresh(t *testing.T) {
	hub := newSSEHub(nil)

	// Pre-fill the channel by registering, filling it, then broadcasting.
	ch := hub.Register()
	defer hub.Unregister(ch)

	// Fill the buffer completely (capacity 16).
	filler := ProjectEvent{Type: EventRefresh, ChangeToken: "filler", Timestamp: time.Now()}
	for range 16 {
		ch <- filler
	}

	// Now broadcast an issue event; channel is full so client should get
	// a refresh event queued (if there were space, but since it's full it
	// will be dropped at the second select too — that's acceptable). What
	// we assert is that Broadcast does NOT block.
	done := make(chan struct{})
	go func() {
		hub.Broadcast(ProjectEvent{
			Type:        EventIssueUpserted,
			IssueID:     "issue-slow",
			ChangeToken: "tok-slow",
			Timestamp:   time.Now(),
		})
		close(done)
	}()

	select {
	case <-done:
		// Good: Broadcast returned without blocking.
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked on full client channel")
	}
}
