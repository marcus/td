package api

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/query"
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

// snapshotQueryResponse is the JSON response for GET /v1/admin/projects/{id}/snapshot/query.
type snapshotQueryResponse struct {
	Data        []models.Issue `json:"data"`
	SnapshotSeq int64          `json:"snapshot_seq"`
	NextCursor  *string        `json:"next_cursor"`
	HasMore     bool           `json:"has_more"`
}

const (
	defaultQueryLimit = 50
	maxQueryLimit     = 200
	// Rebuild snapshot if it's more than this many events behind head.
	snapshotStalenessThreshold = 1000
)

// handleAdminSnapshotQuery executes a TDQ expression against a project's snapshot.
func (s *Server) handleAdminSnapshotQuery(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if projectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing project id")
		return
	}

	// Parse query params
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidQuery, "missing required query parameter 'q'")
		return
	}

	limit := defaultQueryLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid limit parameter")
			return
		}
		limit = parsed
		if limit > maxQueryLimit {
			limit = maxQueryLimit
		}
	}

	var offset int
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid cursor")
			return
		}
		offset, err = strconv.Atoi(string(decoded))
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid cursor")
			return
		}
	}

	// Verify project exists
	project, err := s.store.GetProject(projectID, true)
	if err != nil {
		slog.Error("snapshot query: get project", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get project")
		return
	}
	if project == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "project not found")
		return
	}

	// Get head seq from events.db
	eventsDB, err := s.dbPool.Get(projectID)
	if err != nil {
		writeError(w, http.StatusNotFound, ErrCodeSnapshotUnavailable, "no events for project")
		return
	}

	var headSeq int64
	if err := eventsDB.QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM events`).Scan(&headSeq); err != nil {
		slog.Error("snapshot query: head seq", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "database error")
		return
	}
	if headSeq == 0 {
		writeError(w, http.StatusNotFound, ErrCodeSnapshotUnavailable, "no events for project")
		return
	}

	// Find cached snapshot
	cacheDir := filepath.Join(s.config.ProjectDataDir, "snapshots", projectID)
	snapshotSeq := int64(0)
	snapshotPath := ""

	entries, err := os.ReadDir(cacheDir)
	if err == nil {
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

	// If no cached snapshot or snapshot is too stale, build a fresh one
	if snapshotPath == "" || (headSeq-snapshotSeq) > snapshotStalenessThreshold {
		newPath, newSeq, err := s.buildAndCacheSnapshot(projectID, eventsDB, headSeq, cacheDir)
		if err != nil {
			slog.Error("snapshot query: build snapshot", "project", projectID, "err", err)
			if snapshotPath == "" {
				writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to build snapshot")
				return
			}
			// Fall through with stale snapshot if rebuild failed
			slog.Warn("snapshot query: using stale snapshot", "project", projectID, "staleness", headSeq-snapshotSeq)
		} else {
			snapshotPath = newPath
			snapshotSeq = newSeq
		}
	}

	// Open snapshot read-only
	snapDB, err := sql.Open("sqlite", snapshotPath+"?mode=ro")
	if err != nil {
		slog.Error("snapshot query: open snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to open snapshot")
		return
	}
	defer snapDB.Close()

	// Create query source and execute TDQ
	src := NewSnapshotQuerySource(snapDB)

	// Execute with a high limit to get all matching results for pagination
	execOpts := query.ExecuteOptions{
		Limit: 0, // no limit - we paginate after
	}
	results, err := query.Execute(src, q, "", execOpts)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "parse error") || strings.Contains(errMsg, "validation error") {
			writeError(w, http.StatusBadRequest, ErrCodeInvalidQuery, errMsg)
			return
		}
		slog.Error("snapshot query: execute", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "query execution failed")
		return
	}

	// Apply offset+limit pagination
	total := len(results)
	if offset > total {
		offset = total
	}
	end := offset + limit
	hasMore := false
	if end > total {
		end = total
	} else if end < total {
		hasMore = true
	}
	page := results[offset:end]
	if page == nil {
		page = []models.Issue{}
	}

	var nextCursor *string
	if hasMore {
		encoded := base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(end)))
		nextCursor = &encoded
	}

	writeJSON(w, http.StatusOK, snapshotQueryResponse{
		Data:        page,
		SnapshotSeq: snapshotSeq,
		NextCursor:  nextCursor,
		HasMore:     hasMore,
	})
}

// buildAndCacheSnapshot builds a new snapshot and caches it, returning the path and seq.
func (s *Server) buildAndCacheSnapshot(projectID string, eventsDB *sql.DB, headSeq int64, cacheDir string) (string, int64, error) {
	tmpFile, err := os.CreateTemp("", "td-snapshot-*.db")
	if err != nil {
		return "", 0, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := buildSnapshot(eventsDB, tmpPath, headSeq); err != nil {
		return "", 0, fmt.Errorf("build: %w", err)
	}

	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%d.db", headSeq))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("mkdir: %w", err)
	}

	tmpCachePath := cachePath + fmt.Sprintf(".tmp.%d", os.Getpid())
	if err := copyFile(tmpPath, tmpCachePath); err != nil {
		return "", 0, fmt.Errorf("copy: %w", err)
	}
	if err := os.Rename(tmpCachePath, cachePath); err != nil {
		os.Remove(tmpCachePath)
		return "", 0, fmt.Errorf("rename: %w", err)
	}

	cleanSnapshotCache(cacheDir, headSeq)
	slog.Info("snapshot query: cached", "project", projectID, "seq", headSeq)
	return cachePath, headSeq, nil
}
