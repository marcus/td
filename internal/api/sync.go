package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	tdsync "github.com/marcus/td/internal/sync"
)

// Allowed entity types for validation.
var allowedEntityTypes = map[string]bool{
	"issues":                true,
	"logs":                  true,
	"handoffs":              true,
	"comments":              true,
	"sessions":              true,
	"boards":                true,
	"board_issue_positions": true,
	"work_sessions":         true,
	"work_session_issues":   true,
	"issue_files":           true,
	"issue_dependencies":    true,
	"git_snapshots":         true,
	"issue_session_history": true,
}

func isValidEntityType(et string) bool {
	return allowedEntityTypes[et]
}

// PushRequest is the JSON body for POST /v1/projects/{id}/sync/push.
type PushRequest struct {
	DeviceID  string       `json:"device_id"`
	SessionID string       `json:"session_id"`
	Events    []EventInput `json:"events"`
}

// EventInput represents a single event in a push request.
type EventInput struct {
	ClientActionID  int64           `json:"client_action_id"`
	ActionType      string          `json:"action_type"`
	EntityType      string          `json:"entity_type"`
	EntityID        string          `json:"entity_id"`
	Payload         json.RawMessage `json:"payload"`
	ClientTimestamp string          `json:"client_timestamp"`
}

const (
	maxPushBatch = 1000
	maxPullLimit = 10000
	defPullLimit = 1000
)

// PushResponse is the JSON response for a push request.
type PushResponse struct {
	Accepted int              `json:"accepted"`
	Acks     []AckResponse    `json:"acks"`
	Rejected []RejectResponse `json:"rejected,omitempty"`
}

// AckResponse is a single acknowledged event.
type AckResponse struct {
	ClientActionID int64 `json:"client_action_id"`
	ServerSeq      int64 `json:"server_seq"`
}

// RejectResponse is a single rejected event.
type RejectResponse struct {
	ClientActionID int64  `json:"client_action_id"`
	Reason         string `json:"reason"`
}

// PullResponse is the JSON response for a pull request.
type PullResponse struct {
	Events        []PullEvent `json:"events"`
	LastServerSeq int64       `json:"last_server_seq"`
	HasMore       bool        `json:"has_more"`
}

// PullEvent is a single event in a pull response.
type PullEvent struct {
	ServerSeq       int64           `json:"server_seq"`
	DeviceID        string          `json:"device_id"`
	SessionID       string          `json:"session_id"`
	ClientActionID  int64           `json:"client_action_id"`
	ActionType      string          `json:"action_type"`
	EntityType      string          `json:"entity_type"`
	EntityID        string          `json:"entity_id"`
	Payload         json.RawMessage `json:"payload"`
	ClientTimestamp string          `json:"client_timestamp"`
}

// SyncStatusResponse is the JSON response for GET /v1/projects/{id}/sync/status.
type SyncStatusResponse struct {
	EventCount     int64  `json:"event_count"`
	LastServerSeq  int64  `json:"last_server_seq"`
	LastEventTime  string `json:"last_event_time,omitempty"`
}

// handleSyncPush handles POST /v1/projects/{id}/sync/push.
func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	var req PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	if req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_id is required")
		return
	}
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "session_id is required")
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "events array is empty")
		return
	}
	if len(req.Events) > maxPushBatch {
		writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("batch size %d exceeds max %d", len(req.Events), maxPushBatch))
		return
	}

	// Validate entity types
	for _, ev := range req.Events {
		if !isValidEntityType(ev.EntityType) {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("invalid entity_type: %s", ev.EntityType))
			return
		}
	}

	// Convert to sync.Event
	events := make([]tdsync.Event, len(req.Events))
	for i, ev := range req.Events {
		ts, err := time.Parse(time.RFC3339, ev.ClientTimestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339Nano, ev.ClientTimestamp)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("invalid timestamp for action %d", ev.ClientActionID))
				return
			}
		}
		events[i] = tdsync.Event{
			ClientActionID:  ev.ClientActionID,
			DeviceID:        req.DeviceID,
			SessionID:       req.SessionID,
			ActionType:      ev.ActionType,
			EntityType:      ev.EntityType,
			EntityID:        ev.EntityID,
			Payload:         ev.Payload,
			ClientTimestamp: ts,
		}
	}

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		logFor(r.Context()).Error("get project db", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to open project database")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		logFor(r.Context()).Error("begin tx", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "database error")
		return
	}
	defer tx.Rollback()

	result, err := tdsync.InsertServerEvents(tx, events)
	if err != nil {
		logFor(r.Context()).Error("insert events", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to insert events")
		return
	}

	if err := tx.Commit(); err != nil {
		logFor(r.Context()).Error("commit tx", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to commit")
		return
	}

	s.metrics.RecordPushEvents(int64(result.Accepted))

	resp := PushResponse{Accepted: result.Accepted}
	for _, a := range result.Acks {
		resp.Acks = append(resp.Acks, AckResponse{
			ClientActionID: a.ClientActionID,
			ServerSeq:      a.ServerSeq,
		})
	}
	for _, r := range result.Rejected {
		resp.Rejected = append(resp.Rejected, RejectResponse{
			ClientActionID: r.ClientActionID,
			Reason:         r.Reason,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSyncPull handles GET /v1/projects/{id}/sync/pull.
func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	s.metrics.RecordPullRequest()
	projectID := r.PathValue("id")

	afterSeq := int64(0)
	if v := r.URL.Query().Get("after_server_seq"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid after_server_seq")
			return
		}
		afterSeq = n
	}

	limit := defPullLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid limit")
			return
		}
		if n > maxPullLimit {
			n = maxPullLimit
		}
		limit = n
	}

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		logFor(r.Context()).Error("get project db", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to open project database")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		logFor(r.Context()).Error("begin tx", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "database error")
		return
	}
	defer tx.Rollback()

	excludeClient := r.URL.Query().Get("exclude_client")
	result, err := tdsync.GetEventsSince(tx, afterSeq, limit, excludeClient)
	if err != nil {
		logFor(r.Context()).Error("get events", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query events")
		return
	}

	tx.Rollback() // read-only, just release

	resp := PullResponse{
		LastServerSeq: result.LastServerSeq,
		HasMore:       result.HasMore,
		Events:        make([]PullEvent, len(result.Events)),
	}
	for i, ev := range result.Events {
		resp.Events[i] = PullEvent{
			ServerSeq:       ev.ServerSeq,
			DeviceID:        ev.DeviceID,
			SessionID:       ev.SessionID,
			ClientActionID:  ev.ClientActionID,
			ActionType:      ev.ActionType,
			EntityType:      ev.EntityType,
			EntityID:        ev.EntityID,
			Payload:         ev.Payload,
			ClientTimestamp: ev.ClientTimestamp.Format(time.RFC3339Nano),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSyncStatus handles GET /v1/projects/{id}/sync/status.
func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		logFor(r.Context()).Error("get project db", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to open project database")
		return
	}

	var count int64
	var lastSeq int64

	err = db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(server_seq), 0) FROM events`).Scan(&count, &lastSeq)
	if err != nil {
		logFor(r.Context()).Error("query event count", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "database error")
		return
	}

	resp := SyncStatusResponse{
		EventCount:    count,
		LastServerSeq: lastSeq,
	}

	if count > 0 {
		var ts string
		err = db.QueryRow(`SELECT server_timestamp FROM events WHERE server_seq = ?`, lastSeq).Scan(&ts)
		if err == nil {
			resp.LastEventTime = ts
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
