package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
	tdsync "github.com/marcus/td/internal/sync"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

// ============================================================================
// SSE Event Types
// ============================================================================

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	ID    string // change_token used as event ID
	Event string // "refresh" or "ping"
	Data  string // JSON payload
}

// refreshData is the JSON payload for a refresh event.
type refreshData struct {
	ChangeToken string `json:"change_token"`
	Timestamp   string `json:"timestamp"`
}

// pingData is the JSON payload for a ping event.
type pingData struct {
	ChangeToken string `json:"change_token"`
}

// ============================================================================
// SSE Hub
// ============================================================================

// SSEHub manages connected SSE clients and broadcasts events.
type SSEHub struct {
	db           *db.DB
	pollInterval time.Duration

	mu      sync.Mutex
	clients map[chan SSEEvent]struct{}

	cancel context.CancelFunc
	done   chan struct{}
}

// NewSSEHub creates a new SSEHub with the given database and poll interval.
func NewSSEHub(database *db.DB, pollInterval time.Duration) *SSEHub {
	return &SSEHub{
		db:           database,
		pollInterval: pollInterval,
		clients:      make(map[chan SSEEvent]struct{}),
		done:         make(chan struct{}),
	}
}

// Start begins the background polling goroutine that checks for change_token
// updates and sends periodic pings.
func (h *SSEHub) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)

	go h.run(ctx)
}

// Stop shuts down the SSE hub, closing all client channels and stopping the
// polling goroutine.
func (h *SSEHub) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	<-h.done
}

// register adds a client channel and returns it.
func (h *SSEHub) register() chan SSEEvent {
	ch := make(chan SSEEvent, 16) // buffered to avoid blocking broadcasts
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	slog.Debug("sse: client registered", "clients", h.clientCount())
	return ch
}

// unregister removes a client channel and closes it.
func (h *SSEHub) unregister(ch chan SSEEvent) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
	slog.Debug("sse: client unregistered", "clients", h.clientCount())
}

// clientCount returns the number of connected clients (for logging).
func (h *SSEHub) clientCount() int {
	// Caller must NOT hold the lock if calling from outside locked section.
	// This is only called from within locked sections or for logging after unlock.
	return len(h.clients)
}

// Broadcast sends a refresh event to all connected clients with the given
// change token.
func (h *SSEHub) Broadcast(changeToken string) {
	data, _ := json.Marshal(refreshData{
		ChangeToken: changeToken,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})

	event := SSEEvent{
		ID:    changeToken,
		Event: "refresh",
		Data:  string(data),
	}

	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// Client too slow, skip this event (next poll or broadcast will catch up)
			slog.Debug("sse: dropped event for slow client")
		}
	}
	h.mu.Unlock()
}

// run is the background goroutine that polls the change_token and sends pings.
func (h *SSEHub) run(ctx context.Context) {
	defer close(h.done)

	pollTicker := time.NewTicker(h.pollInterval)
	defer pollTicker.Stop()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	lastToken, _ := h.db.GetChangeToken()

	for {
		select {
		case <-ctx.Done():
			h.closeAllClients()
			return

		case <-pollTicker.C:
			token, err := h.db.GetChangeToken()
			if err != nil {
				slog.Debug("sse: poll change_token error", "err", err)
				continue
			}
			if token != lastToken {
				lastToken = token
				h.Broadcast(token)
			}

		case <-pingTicker.C:
			token, _ := h.db.GetChangeToken()
			lastToken = token

			data, _ := json.Marshal(pingData{
				ChangeToken: token,
			})

			event := SSEEvent{
				ID:    token,
				Event: "ping",
				Data:  string(data),
			}

			h.mu.Lock()
			for ch := range h.clients {
				select {
				case ch <- event:
				default:
				}
			}
			h.mu.Unlock()
		}
	}
}

// closeAllClients closes all registered client channels.
func (h *SSEHub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}

// ============================================================================
// SSE HTTP Handler
// ============================================================================

// handleEvents is the HTTP handler for GET /v1/events (SSE endpoint).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Verify streaming support
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, ErrInternal, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Disable response buffering at the http.Server level by wrapping
	// with an http.ResponseController to override write deadline for this
	// long-lived connection.
	rc := http.NewResponseController(w)
	// Remove write deadline for SSE connections
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Debug("sse: failed to clear write deadline", "err", err)
	}

	// Register this client with the hub
	hub := s.sseHub
	if hub == nil {
		WriteError(w, ErrInternal, "event stream unavailable", http.StatusInternalServerError)
		return
	}
	ch := hub.register()
	defer hub.unregister(ch)

	// Check Last-Event-ID for reconnect support
	lastEventID := r.Header.Get("Last-Event-ID")
	currentToken, _ := s.db.GetChangeToken()

	if lastEventID != "" && lastEventID != currentToken {
		// Client reconnecting with a stale token — send immediate refresh
		writeSSEEvent(w, flusher, SSEEvent{
			ID:    currentToken,
			Event: "refresh",
			Data: marshalJSON(refreshData{
				ChangeToken: currentToken,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}),
		})
	} else {
		// New connection — send initial ping so client knows it's connected
		writeSSEEvent(w, flusher, SSEEvent{
			ID:    currentToken,
			Event: "ping",
			Data: marshalJSON(pingData{
				ChangeToken: currentToken,
			}),
		})
	}

	// Stream events from the hub channel until client disconnects
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				// Channel closed (hub shutting down)
				return
			}
			writeSSEEvent(w, flusher, event)
		}
	}
}

// writeSSEEvent writes a single SSE event to the response writer and flushes.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event SSEEvent) {
	fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Event, event.Data)
	flusher.Flush()
}

// marshalJSON is a helper that marshals to JSON, returning "{}" on error.
func marshalJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ============================================================================
// Post-Write Notification
// ============================================================================

// NotifyChange is called after successful write operations. It:
// 1. Gets the current change_token
// 2. Broadcasts a refresh event to all SSE clients
// 3. Triggers a debounced autosync
func (s *Server) NotifyChange() {
	token, err := s.db.GetChangeToken()
	if err != nil {
		slog.Debug("serve: NotifyChange get token", "err", err)
		return
	}

	// Broadcast to SSE clients
	if s.sseHub != nil {
		s.sseHub.Broadcast(token)
	}

	// Trigger debounced autosync
	go s.autoSyncDebounced()
}

// ============================================================================
// Autosync (server-side, mirrors cmd/autosync.go pattern)
// ============================================================================

var (
	serveAutoSyncMu       sync.Mutex
	serveLastAutoSyncAt   time.Time
	serveAutoSyncInFlight int32 // atomic: 1 = sync running
)

// autoSyncDebounced triggers a push+pull sync with debouncing.
func (s *Server) autoSyncDebounced() {
	debounce := syncconfig.GetAutoSyncDebounce()
	serveAutoSyncMu.Lock()
	if time.Since(serveLastAutoSyncAt) < debounce {
		serveAutoSyncMu.Unlock()
		return
	}
	serveLastAutoSyncAt = time.Now()
	serveAutoSyncMu.Unlock()

	s.autoSyncOnce()
}

// autoSyncOnce runs a single push+pull cycle. Guards against concurrent runs.
func (s *Server) autoSyncOnce() {
	if !atomic.CompareAndSwapInt32(&serveAutoSyncInFlight, 0, 1) {
		slog.Debug("serve autosync: skipped, in flight")
		return
	}
	defer atomic.StoreInt32(&serveAutoSyncInFlight, 0)

	if !syncconfig.GetAutoSyncEnabled() {
		return
	}
	if !syncconfig.IsAuthenticated() {
		return
	}

	syncState, err := s.db.GetSyncState()
	if err != nil || syncState == nil || syncState.SyncDisabled {
		return
	}

	deviceID, err := syncconfig.GetDeviceID()
	if err != nil {
		return
	}

	serverURL := syncconfig.GetServerURL()
	apiKey := syncconfig.GetAPIKey()
	client := syncclient.New(serverURL, apiKey, deviceID)
	client.HTTP.Timeout = 5 * time.Second

	sess, err := session.Get(s.db)
	if err != nil {
		slog.Debug("serve autosync: get session", "err", err)
		return
	}

	// Push
	if err := serveAutoSyncPush(s.db, client, syncState, deviceID, sess.ID); err != nil {
		slog.Debug("serve autosync: push", "err", err)
	}

	// Pull
	if syncconfig.GetAutoSyncPull() {
		if err := serveAutoSyncPull(s.db, client, syncState, deviceID); err != nil {
			slog.Debug("serve autosync: pull", "err", err)
		}
	}
}

// serveAutoSyncPush pushes pending events. Simplified version of cmd/autosync.go.
func serveAutoSyncPush(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID, sessionID string) error {
	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	events, err := tdsync.GetPendingEvents(tx, deviceID, sessionID)
	if err != nil {
		return fmt.Errorf("get pending: %w", err)
	}
	if len(events) == 0 {
		return nil
	}

	pushReq := &syncclient.PushRequest{
		DeviceID:  deviceID,
		SessionID: sessionID,
	}
	for _, ev := range events {
		pushReq.Events = append(pushReq.Events, syncclient.EventInput{
			ClientActionID:  ev.ClientActionID,
			ActionType:      ev.ActionType,
			EntityType:      ev.EntityType,
			EntityID:        ev.EntityID,
			Payload:         ev.Payload,
			ClientTimestamp: ev.ClientTimestamp.Format(time.RFC3339),
		})
	}

	pushResp, err := client.Push(state.ProjectID, pushReq)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}

	var acks []tdsync.Ack
	for _, a := range pushResp.Acks {
		acks = append(acks, tdsync.Ack{ClientActionID: a.ClientActionID, ServerSeq: a.ServerSeq})
	}
	for _, r := range pushResp.Rejected {
		if r.Reason == "duplicate" && r.ServerSeq > 0 {
			acks = append(acks, tdsync.Ack{ClientActionID: r.ClientActionID, ServerSeq: r.ServerSeq})
		}
	}

	if err := tdsync.MarkEventsSynced(tx, acks); err != nil {
		return fmt.Errorf("mark synced: %w", err)
	}

	var maxActionID int64
	for _, a := range acks {
		if a.ClientActionID > maxActionID {
			maxActionID = a.ClientActionID
		}
	}
	if maxActionID > 0 {
		if _, err := tx.Exec(`UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP`, maxActionID); err != nil {
			return fmt.Errorf("update state: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	slog.Debug("serve autosync: pushed", "events", len(acks))
	return nil
}

// serveAutoSyncPull pulls remote events. Simplified version of cmd/autosync.go.
func serveAutoSyncPull(database *db.DB, client *syncclient.Client, state *db.SyncState, deviceID string) error {
	lastSeq := state.LastPulledServerSeq

	for {
		pullResp, err := client.Pull(state.ProjectID, lastSeq, 1000, deviceID)
		if err != nil {
			return fmt.Errorf("pull: %w", err)
		}
		if len(pullResp.Events) == 0 {
			break
		}

		events := make([]tdsync.Event, len(pullResp.Events))
		for i, pe := range pullResp.Events {
			clientTS, err := time.Parse(time.RFC3339Nano, pe.ClientTimestamp)
			if err != nil {
				clientTS, _ = time.Parse(time.RFC3339, pe.ClientTimestamp)
			}
			events[i] = tdsync.Event{
				ServerSeq:       pe.ServerSeq,
				DeviceID:        pe.DeviceID,
				SessionID:       pe.SessionID,
				ClientActionID:  pe.ClientActionID,
				ActionType:      pe.ActionType,
				EntityType:      pe.EntityType,
				EntityID:        pe.EntityID,
				Payload:         pe.Payload,
				ClientTimestamp: clientTS,
			}
		}

		conn := database.Conn()
		tx, err := conn.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		// Accept all entity types in SSE path (no feature gating for live sync)
		allowAll := func(string) bool { return true }
		if _, err := tdsync.ApplyRemoteEvents(tx, events, deviceID, allowAll, state.LastSyncAt); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply events: %w", err)
		}

		if _, err := tx.Exec(`UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP`, pullResp.LastServerSeq); err != nil {
			tx.Rollback()
			return fmt.Errorf("update sync state: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}

		lastSeq = pullResp.LastServerSeq
		slog.Debug("serve autosync: pulled", "events", len(pullResp.Events))

		if !pullResp.HasMore {
			break
		}
	}
	return nil
}
