package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/marcus/td/internal/serverdb"
)

// handleAdminListProjects returns a paginated list of all projects.
func (s *Server) handleAdminListProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	cursor := q.Get("cursor")
	includeDeleted := q.Get("include_deleted") == "true"

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	limit = serverdb.NormalizeLimit(limit)

	result, err := s.store.AdminListProjects(query, includeDeleted, limit, cursor)
	if err != nil {
		slog.Error("admin list projects", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to list projects")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleAdminGetProject returns a single project detail.
func (s *Server) handleAdminGetProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	project, err := s.store.AdminGetProject(projectID)
	if err != nil {
		slog.Error("admin get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	writeJSON(w, http.StatusOK, project)
}

// handleAdminProjectMembers returns all members of a project with user emails.
func (s *Server) handleAdminProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin project members: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	members, err := s.store.AdminListProjectMembers(projectID)
	if err != nil {
		slog.Error("admin project members", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to list members")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": members})
}

// adminSyncStatusResponse is the JSON response for admin sync status.
type adminSyncStatusResponse struct {
	EventCount    int64  `json:"event_count"`
	LastServerSeq int64  `json:"last_server_seq"`
	LastEventTime string `json:"last_event_time,omitempty"`
}

// handleAdminSyncStatus returns sync status for a project from its events.db.
func (s *Server) handleAdminSyncStatus(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify project exists in server.db
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin sync status: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	db, err := s.dbPool.Get(projectID)
	if err != nil {
		slog.Error("admin sync status: get db", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to open project database")
		return
	}

	var count int64
	var lastSeq int64
	err = db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(server_seq), 0) FROM events`).Scan(&count, &lastSeq)
	if err != nil {
		slog.Error("admin sync status: query", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "database error")
		return
	}

	resp := adminSyncStatusResponse{
		EventCount:    count,
		LastServerSeq: lastSeq,
	}

	if count > 0 {
		var ts string
		if err := db.QueryRow(`SELECT server_timestamp FROM events WHERE server_seq = ?`, lastSeq).Scan(&ts); err == nil {
			resp.LastEventTime = ts
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// adminCursorEntry is one sync cursor with distance_from_head computed.
type adminCursorEntry struct {
	ClientID         string  `json:"client_id"`
	LastEventID      int64   `json:"last_event_id"`
	LastSyncAt       *string `json:"last_sync_at"`
	DistanceFromHead int64   `json:"distance_from_head"`
}

// handleAdminSyncCursors returns all sync cursors for a project.
func (s *Server) handleAdminSyncCursors(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin sync cursors: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	// Get head seq from events.db
	var headSeq int64
	db, err := s.dbPool.Get(projectID)
	if err != nil {
		// If events.db doesn't exist, head is 0
		headSeq = 0
	} else {
		db.QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM events`).Scan(&headSeq)
	}

	// Get cursors from server.db
	cursors, err := s.store.ListSyncCursorsForProject(projectID)
	if err != nil {
		slog.Error("admin sync cursors", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to list cursors")
		return
	}

	entries := make([]adminCursorEntry, len(cursors))
	for i, c := range cursors {
		entry := adminCursorEntry{
			ClientID:         c.ClientID,
			LastEventID:      c.LastEventID,
			DistanceFromHead: headSeq - c.LastEventID,
		}
		if c.LastSyncAt != nil {
			ts := c.LastSyncAt.UTC().Format("2006-01-02T15:04:05Z")
			entry.LastSyncAt = &ts
		}
		entries[i] = entry
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": entries})
}
