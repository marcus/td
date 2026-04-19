package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// defaultPingInterval is sent to keep-alive SSE connections through proxies.
const defaultPingInterval = 15 * time.Second

// handleProjectEvents handles GET /v1/projects/{id}/events.
//
// It upgrades the connection to an SSE stream, registers the caller with the
// per-project SSEHub, and fans out ProjectEvents until the client disconnects
// or the server shuts down. A ping comment is written every pingInterval to
// prevent intermediary timeouts.
//
// Auth: requireProjectAuth(RoleReader) must wrap this handler (see routes()).
func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify the ResponseWriter supports flushing (required for SSE).
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "streaming not supported")
		return
	}

	// SSE response headers — must be set before any write.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Get or create the hub for this project.
	hub := s.sseHubs.GetOrCreate(projectID)
	ch := hub.Register()
	defer hub.Unregister(ch)

	log := logFor(r.Context())
	log.Debug("sse: client connected", "pid", projectID)

	// If the client sent a Last-Event-ID header it has stale state — send an
	// immediate refresh so it can re-fetch before waiting for the next event.
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		ev := ProjectEvent{
			Type:        EventRefresh,
			ChangeToken: lastID,
			Timestamp:   time.Now().UTC(),
		}
		if err := writeSSEEvent(w, ev); err != nil {
			log.Debug("sse: write refresh failed", "err", err)
			return
		}
		flusher.Flush()
	}

	ping := time.NewTicker(s.pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			log.Debug("sse: client disconnected", "pid", projectID)
			return

		case ev, open := <-ch:
			if !open {
				// Hub closed the channel (should not happen in normal flow).
				return
			}
			if err := writeSSEEvent(w, ev); err != nil {
				log.Debug("sse: write event failed", "err", err, "pid", projectID)
				return
			}
			flusher.Flush()

		case <-ping.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				log.Debug("sse: write ping failed", "err", err, "pid", projectID)
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a single SSE event to w in the format:
//
//	event: <type>
//	data: <json>
//	id: <change_token>
//	(blank line)
func writeSSEEvent(w http.ResponseWriter, ev ProjectEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\nid: %s\n\n",
		ev.Type, data, ev.ChangeToken); err != nil {
		return err
	}
	return nil
}

// logSSEClientCount logs the number of active SSE clients for a project.
// Kept as a helper for future observability use.
func logSSEClientCount(log *slog.Logger, hub *SSEHub, projectID string) {
	log.Debug("sse: clients", "pid", projectID, "count", hub.ClientCount())
}
