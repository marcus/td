package api

import (
	"log/slog"
	"sync"
	"time"
)

// ============================================================================
// Event Types
// ============================================================================

// EventType identifies the kind of realtime event.
type EventType string

const (
	// EventIssueUpserted fires when an issue is created or updated.
	EventIssueUpserted EventType = "issue.upserted"
	// EventIssueDeleted fires when an issue is deleted.
	EventIssueDeleted EventType = "issue.deleted"
	// EventRefresh fires when a client should do a full refresh (e.g. slow
	// consumer dropped an event, or the server cannot determine exact diff).
	EventRefresh EventType = "refresh"
)

// ProjectEvent is the payload broadcast to SSE subscribers for a project.
type ProjectEvent struct {
	Type        EventType `json:"type"`
	IssueID     string    `json:"issue_id,omitempty"`
	ChangeToken string    `json:"change_token"`
	Timestamp   time.Time `json:"timestamp"`
}

// ============================================================================
// SSEHub — per-project fan-out
// ============================================================================

// eventSlots is the number of normal-event slots in each subscriber channel.
// One additional slot (the "refresh slot") is always kept in reserve so that
// a subscriber whose normal slots are all occupied can still receive at most
// one pending refresh event without Broadcast blocking.
const eventSlots = 16

// SSEHub manages a set of subscriber channels for a single project. Events are
// pushed by write paths (via Broadcast) and fanned out to all registered
// clients. The hub does NOT poll; it is driven externally.
//
// Each subscriber channel has capacity eventSlots+1 (17). Broadcast treats the
// first 16 positions as normal-event slots and the 17th as a reserved refresh
// slot: when the first 16 are full it queues a refresh event instead of the
// original event. If the channel is at full capacity (all 17 slots taken, meaning
// a refresh is already pending) the event is silently dropped; the client will
// still perform a full resync when it drains the already-pending refresh.
//
// Because Broadcast holds the hub mutex for its entire duration, the len check
// is safe: only the subscriber goroutine reads from the channel concurrently,
// which can only decrease len (making the check conservative, never unsafe).
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan ProjectEvent]struct{}

	// onEmpty is called (without the lock held) when the last subscriber
	// unregisters. Used by the registry to clean up idle hubs.
	onEmpty func()
}

func newSSEHub(onEmpty func()) *SSEHub {
	return &SSEHub{
		clients: make(map[chan ProjectEvent]struct{}),
		onEmpty: onEmpty,
	}
}

// Register adds a new subscriber and returns its channel. The channel has
// capacity eventSlots+1 (17): 16 for normal events and 1 reserved for a
// refresh event when the normal slots are all full.
func (h *SSEHub) Register() chan ProjectEvent {
	ch := make(chan ProjectEvent, eventSlots+1)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	n := len(h.clients)
	h.mu.Unlock()
	slog.Debug("sse: client registered", "clients", n)
	return ch
}

// Unregister removes a subscriber channel and closes it. If this was the last
// subscriber, onEmpty is called to allow the registry to remove the idle hub.
func (h *SSEHub) Unregister(ch chan ProjectEvent) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	n := len(h.clients)
	h.mu.Unlock()
	slog.Debug("sse: client unregistered", "clients", n)

	if n == 0 && h.onEmpty != nil {
		h.onEmpty()
	}
}

// Broadcast sends ev to all registered subscribers. If a subscriber's normal
// event slots (positions 0–15) are all occupied, Broadcast queues a refresh
// event in the reserved 17th slot instead. If even that slot is taken (a
// refresh is already pending), the event is dropped; the client will recover
// when it drains the pending refresh.
//
// Broadcast holds h.mu for its entire execution, so the len check before each
// direct send is safe: the only concurrent accessor of a subscriber channel is
// its reader goroutine, which can only reduce len.
func (h *SSEHub) Broadcast(ev ProjectEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.clients {
		if len(ch) < eventSlots {
			// Normal slots available — direct send is guaranteed to succeed
			// because we hold the mutex (no concurrent writer) and len < cap.
			ch <- ev
		} else {
			// All 16 normal slots full — queue a refresh in the reserved slot.
			refresh := ProjectEvent{
				Type:        EventRefresh,
				ChangeToken: ev.ChangeToken,
				Timestamp:   ev.Timestamp,
			}
			select {
			case ch <- refresh:
			default:
				// Reserved slot already taken; a refresh is pending. Drop.
				slog.Debug("sse: refresh pending, dropping event")
			}
		}
	}
}

// ClientCount returns the number of active subscribers.
func (h *SSEHub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// ============================================================================
// SSEHubRegistry — per-project hub registry
// ============================================================================

// SSEHubRegistry maps project IDs to their SSEHub. Get-or-create is safe for
// concurrent use. Idle hubs (zero subscribers) are removed automatically when
// the last subscriber unregisters.
type SSEHubRegistry struct {
	mu   sync.RWMutex
	hubs map[string]*SSEHub
}

// NewSSEHubRegistry creates an empty registry.
func NewSSEHubRegistry() *SSEHubRegistry {
	return &SSEHubRegistry{
		hubs: make(map[string]*SSEHub),
	}
}

// GetOrCreate returns the existing hub for projectID, or creates one if none
// exists. The returned hub is safe to use immediately.
func (r *SSEHubRegistry) GetOrCreate(projectID string) *SSEHub {
	// Fast path: hub already exists.
	r.mu.RLock()
	hub, ok := r.hubs[projectID]
	r.mu.RUnlock()
	if ok {
		return hub
	}

	// Slow path: create under write lock; re-check to avoid TOCTOU.
	r.mu.Lock()
	defer r.mu.Unlock()
	if hub, ok = r.hubs[projectID]; ok {
		return hub
	}
	hub = newSSEHub(func() { r.remove(projectID) })
	r.hubs[projectID] = hub
	return hub
}

// Broadcast sends ev to all subscribers of projectID. No-ops if no hub exists.
func (r *SSEHubRegistry) Broadcast(projectID string, ev ProjectEvent) {
	r.mu.RLock()
	hub, ok := r.hubs[projectID]
	r.mu.RUnlock()
	if ok {
		hub.Broadcast(ev)
	}
}

// remove deletes the entry for projectID from the registry only when the hub
// is still idle (zero subscribers). This closes the race window that exists
// between Unregister's mu.Unlock (line ~90) and remove acquiring the write
// lock here: a concurrent GetOrCreate on the fast RLock path can retrieve the
// hub and register a new subscriber in that gap. Re-reading under the write
// lock and checking ClientCount() == 0 ensures we never evict a hub that has
// acquired a live subscriber since onEmpty was invoked.
func (r *SSEHubRegistry) remove(projectID string) {
	r.mu.Lock()
	hub, ok := r.hubs[projectID]
	if ok && hub.ClientCount() == 0 {
		delete(r.hubs, projectID)
		slog.Debug("sse: idle hub removed", "project", projectID)
	}
	r.mu.Unlock()
}

// HubCount returns the number of projects with active hubs. For observability.
func (r *SSEHubRegistry) HubCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hubs)
}
