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

// SSEHub manages a set of subscriber channels for a single project. Events are
// pushed by write paths (via Broadcast) and fanned out to all registered
// clients. The hub does NOT poll; it is driven externally.
//
// Channels are buffered (capacity 16). If a client channel is full the event is
// dropped and a refresh event is queued instead so the client can recover.
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

// Register adds a new subscriber and returns its channel.
func (h *SSEHub) Register() chan ProjectEvent {
	ch := make(chan ProjectEvent, 16)
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

// Broadcast sends ev to all registered subscribers. Slow clients (full channel)
// are skipped and receive a refresh event instead so they can catch up without
// blocking the write path.
func (h *SSEHub) Broadcast(ev ProjectEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
			// Channel full — send refresh so client knows it missed something.
			refresh := ProjectEvent{
				Type:        EventRefresh,
				ChangeToken: ev.ChangeToken,
				Timestamp:   ev.Timestamp,
			}
			select {
			case ch <- refresh:
			default:
				// Still full — give up; client will reconnect.
				slog.Debug("sse: dropped refresh for slow client")
			}
		}
	}
}

// ClientCount returns the number of active subscribers. Useful for logging.
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

// remove deletes the entry for projectID from the registry. Called by the hub's
// onEmpty callback (without the hub's lock held, but we need the registry lock).
func (r *SSEHubRegistry) remove(projectID string) {
	r.mu.Lock()
	delete(r.hubs, projectID)
	r.mu.Unlock()
	slog.Debug("sse: idle hub removed", "project", projectID)
}

// HubCount returns the number of projects with active hubs. For observability.
func (r *SSEHubRegistry) HubCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hubs)
}
