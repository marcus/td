package api

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// snapshotMetaResponse is the JSON response for GET /v1/admin/projects/{id}/snapshot/meta.
type snapshotMetaResponse struct {
	SnapshotSeq  int64          `json:"snapshot_seq"`
	HeadSeq      int64          `json:"head_seq"`
	Staleness    int64          `json:"staleness"`
	EntityCounts map[string]int `json:"entity_counts"`
}

// handleAdminSnapshotMeta returns metadata about the cached snapshot for a project.
func (s *Server) handleAdminSnapshotMeta(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("admin snapshot meta: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	// Get head seq from events.db
	var headSeq int64
	eventsDB, err := s.dbPool.Get(projectID)
	if err != nil {
		// Events DB might not exist yet - head is 0
		headSeq = 0
	} else {
		eventsDB.QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM events`).Scan(&headSeq)
	}

	// Find cached snapshot
	cacheDir := filepath.Join(s.config.ProjectDataDir, "snapshots", projectID)
	snapshotSeq := int64(0)
	snapshotPath := ""

	entries, err := os.ReadDir(cacheDir)
	if err == nil {
		// Find the highest-numbered .db file
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") || strings.Contains(e.Name(), ".tmp") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".db")
			if seq, err := strconv.ParseInt(name, 10, 64); err == nil && seq > snapshotSeq {
				snapshotSeq = seq
				snapshotPath = filepath.Join(cacheDir, e.Name())
			}
		}
	}

	resp := snapshotMetaResponse{
		SnapshotSeq:  snapshotSeq,
		HeadSeq:      headSeq,
		Staleness:    headSeq - snapshotSeq,
		EntityCounts: map[string]int{},
	}

	// Count entities in the snapshot if it exists
	if snapshotPath != "" {
		counts, err := countSnapshotEntities(snapshotPath)
		if err != nil {
			slog.Warn("admin snapshot meta: count entities", "err", err)
		} else {
			resp.EntityCounts = counts
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// countSnapshotEntities opens a snapshot database and counts rows per entity type.
func countSnapshotEntities(path string) (map[string]int, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	counts := make(map[string]int)
	tables := []string{"issues", "logs", "comments", "handoffs", "boards", "board_issue_positions", "work_sessions", "sessions", "notes"}

	for _, table := range tables {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			// Table might not exist in the snapshot - skip
			continue
		}
		if count > 0 {
			counts[table] = count
		}
	}

	return counts, nil
}

// handleAdminSnapshotQuery is a placeholder for the TDQ-powered snapshot query endpoint.
func (s *Server) handleAdminSnapshotQuery(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, ErrCodeNotImplemented,
		"TDQ server-side query requires refactoring internal/query to accept arbitrary *sql.DB; not yet available")
}
