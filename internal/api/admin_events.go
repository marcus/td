package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
)

// adminEvent represents a single event for the admin API.
type adminEvent struct {
	ServerSeq       int64           `json:"server_seq"`
	DeviceID        string          `json:"device_id"`
	SessionID       string          `json:"session_id"`
	ClientActionID  int64           `json:"client_action_id"`
	ActionType      string          `json:"action_type"`
	EntityType      string          `json:"entity_type"`
	EntityID        string          `json:"entity_id"`
	Payload         json.RawMessage `json:"payload"`
	ClientTimestamp string          `json:"client_timestamp"`
	ServerTimestamp string          `json:"server_timestamp"`
}

// adminEventsResponse is the paginated response for event listing.
type adminEventsResponse struct {
	Data    []adminEvent `json:"data"`
	HasMore bool         `json:"has_more"`
}

const (
	defaultEventLimit = 100
	maxEventLimit     = 1000
)

// handleAdminProjectEvents handles GET /v1/admin/projects/{id}/events.
func (s *Server) handleAdminProjectEvents(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin events: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		slog.Error("admin events: get db", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to open project database")
		return
	}

	q := r.URL.Query()

	// Parse limit
	limit := defaultEventLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxEventLimit {
		limit = maxEventLimit
	}

	// Parse after_seq
	afterSeq := int64(0)
	if v := q.Get("after_seq"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			afterSeq = n
		}
	}

	// Build dynamic WHERE clause
	query := `SELECT server_seq, device_id, session_id, client_action_id, action_type, entity_type, entity_id, payload, client_timestamp, server_timestamp FROM events WHERE server_seq > ?`
	args := []any{afterSeq}

	if v := q.Get("entity_type"); v != "" {
		if !isValidEntityType(v) {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid entity_type: "+v)
			return
		}
		query += " AND entity_type = ?"
		args = append(args, v)
	}
	if v := q.Get("action_type"); v != "" {
		query += " AND action_type = ?"
		args = append(args, v)
	}
	if v := q.Get("from"); v != "" {
		query += " AND server_timestamp >= ?"
		args = append(args, v)
	}
	if v := q.Get("to"); v != "" {
		query += " AND server_timestamp <= ?"
		args = append(args, v)
	}
	if v := q.Get("device_id"); v != "" {
		query += " AND device_id = ?"
		args = append(args, v)
	}
	if v := q.Get("session_id"); v != "" {
		query += " AND session_id = ?"
		args = append(args, v)
	}
	if v := q.Get("entity_id"); v != "" {
		query += " AND entity_id = ?"
		args = append(args, v)
	}

	query += " ORDER BY server_seq ASC LIMIT ?"
	args = append(args, limit+1) // Fetch one extra to determine has_more

	rows, err := db.Query(query, args...)
	if err != nil {
		slog.Error("admin events: query", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to query events")
		return
	}
	defer rows.Close()

	var events []adminEvent
	for rows.Next() {
		var ev adminEvent
		if err := rows.Scan(&ev.ServerSeq, &ev.DeviceID, &ev.SessionID, &ev.ClientActionID, &ev.ActionType, &ev.EntityType, &ev.EntityID, &ev.Payload, &ev.ClientTimestamp, &ev.ServerTimestamp); err != nil {
			slog.Error("admin events: scan", "err", err)
			writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to read events")
			return
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		slog.Error("admin events: iterate", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to read events")
		return
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	if events == nil {
		events = []adminEvent{}
	}

	writeJSON(w, http.StatusOK, adminEventsResponse{
		Data:    events,
		HasMore: hasMore,
	})
}

// handleAdminProjectEvent handles GET /v1/admin/projects/{id}/events/{server_seq}.
func (s *Server) handleAdminProjectEvent(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	seqStr := r.PathValue("server_seq")
	if projectID == "" || seqStr == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id or server_seq")
		return
	}

	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid server_seq")
		return
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin event: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		slog.Error("admin event: get db", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to open project database")
		return
	}

	var ev adminEvent
	err = db.QueryRow(
		`SELECT server_seq, device_id, session_id, client_action_id, action_type, entity_type, entity_id, payload, client_timestamp, server_timestamp FROM events WHERE server_seq = ?`,
		seq,
	).Scan(&ev.ServerSeq, &ev.DeviceID, &ev.SessionID, &ev.ClientActionID, &ev.ActionType, &ev.EntityType, &ev.EntityID, &ev.Payload, &ev.ClientTimestamp, &ev.ServerTimestamp)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "event not found")
		return
	}
	if err != nil {
		slog.Error("admin event: query", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get event")
		return
	}

	writeJSON(w, http.StatusOK, ev)
}

// handleAdminEntityTypes handles GET /v1/admin/entity-types.
func (s *Server) handleAdminEntityTypes(w http.ResponseWriter, r *http.Request) {
	types := make([]string, 0, len(allowedEntityTypes))
	for t := range allowedEntityTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	writeJSON(w, http.StatusOK, map[string]any{"entity_types": types})
}
