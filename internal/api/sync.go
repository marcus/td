package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tddb "github.com/marcus/td/internal/db"
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
	"notes":                 true,
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
	ServerSeq      int64  `json:"server_seq,omitempty"`
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
	EventCount    int64  `json:"event_count"`
	LastServerSeq int64  `json:"last_server_seq"`
	LastEventTime string `json:"last_event_time,omitempty"`
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
			ServerSeq:      r.ServerSeq,
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

// handleSyncSnapshot handles GET /v1/projects/{id}/sync/snapshot.
// Builds a snapshot database by replaying all events, then streams it to the client.
// Caches built snapshots keyed by lastSeq to avoid rebuilding on every request.
func (s *Server) handleSyncSnapshot(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")

	eventsDB, err := s.dbPool.Get(projectID)
	if err != nil {
		logFor(r.Context()).Error("get project db", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to open project database")
		return
	}

	// Get the latest server_seq
	var lastSeq int64
	if err := eventsDB.QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM events`).Scan(&lastSeq); err != nil {
		logFor(r.Context()).Error("query max seq", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "database error")
		return
	}

	if lastSeq == 0 {
		writeError(w, http.StatusNotFound, "no_events", "no events to snapshot")
		return
	}

	// Check snapshot cache
	cacheDir := filepath.Join(s.config.ProjectDataDir, "snapshots", projectID)
	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%d.db", lastSeq))

	if _, err := os.Stat(cachePath); err == nil {
		// Cache hit — serve directly
		slog.Info("snapshot cache hit", "project", projectID, "seq", lastSeq)
		serveSnapshotFile(w, r, cachePath, lastSeq)
		return
	}

	// Cache miss — build snapshot
	tmpFile, err := os.CreateTemp("", "td-snapshot-*.db")
	if err != nil {
		logFor(r.Context()).Error("create temp file", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create snapshot")
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := buildSnapshot(eventsDB, tmpPath, lastSeq); err != nil {
		logFor(r.Context()).Error("build snapshot", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to build snapshot")
		return
	}

	// Cache the built snapshot using atomic write-and-rename to avoid races.
	// Track the serve path — if caching succeeds (and moves the temp file),
	// serve from the cache path instead.
	servePath := tmpPath
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		slog.Warn("snapshot cache mkdir failed", "dir", cacheDir, "err", err)
	} else {
		tmpCachePath := cachePath + fmt.Sprintf(".tmp.%d", os.Getpid())
		if err := copyFile(tmpPath, tmpCachePath); err == nil {
			if err := os.Rename(tmpCachePath, cachePath); err != nil {
				os.Remove(tmpCachePath)
				slog.Warn("snapshot cache rename failed", "err", err)
			} else {
				cleanSnapshotCache(cacheDir, lastSeq)
				slog.Info("snapshot cached", "project", projectID, "seq", lastSeq)
				servePath = cachePath
			}
		} else {
			slog.Warn("snapshot cache write failed", "err", err)
		}
	}

	serveSnapshotFile(w, r, servePath, lastSeq)
}

// serveSnapshotFile streams a snapshot .db file as an HTTP response.
func serveSnapshotFile(w http.ResponseWriter, r *http.Request, path string, seq int64) {
	f, err := os.Open(path)
	if err != nil {
		logFor(r.Context()).Error("open snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to read snapshot")
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("X-Snapshot-Seq", strconv.FormatInt(seq, 10))
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

// cleanSnapshotCache removes cached .db files that don't match the current seq.
func cleanSnapshotCache(cacheDir string, currentSeq int64) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	currentName := fmt.Sprintf("%d.db", currentSeq)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		if e.Name() != currentName {
			old := filepath.Join(cacheDir, e.Name())
			if err := os.Remove(old); err != nil {
				slog.Warn("snapshot cache cleanup failed", "file", old, "err", err)
			} else {
				slog.Info("snapshot cache evicted", "file", e.Name())
			}
		}
	}
}

// copyFile copies src to dst atomically via a temp file + rename.
// Tries os.Rename first (fast, same filesystem), falls back to a
// write-to-temp + rename to avoid leaving partial files on failure.
func copyFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Rename failed (cross-device); do an atomic byte copy
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpDst := dst + ".tmp"
	out, err := os.Create(tmpDst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmpDst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpDst)
		return err
	}
	return os.Rename(tmpDst, dst)
}

// buildSnapshot replays events from the events DB into a new snapshot DB.
func buildSnapshot(eventsDB *sql.DB, snapshotPath string, upToSeq int64) error {
	// Create temp dir for Initialize (it creates .todos/issues.db inside)
	tmpDir, err := os.MkdirTemp("", "td-snapshot-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize with full schema + all migrations
	tdb, err := tddb.Initialize(tmpDir)
	if err != nil {
		return fmt.Errorf("init snapshot schema: %w", err)
	}
	tdb.Close()

	// Re-open the initialized DB for event replay
	tmpDBPath := filepath.Join(tmpDir, ".todos", "issues.db")
	snapDB, err := sql.Open("sqlite", tmpDBPath)
	if err != nil {
		return fmt.Errorf("open snapshot db: %w", err)
	}
	defer snapDB.Close()

	validator := func(t string) bool { return allowedEntityTypes[t] }
	afterSeq := int64(0)
	batchSize := 1000

	for {
		tx, err := eventsDB.Begin()
		if err != nil {
			return fmt.Errorf("begin event read tx: %w", err)
		}

		result, err := tdsync.GetEventsSince(tx, afterSeq, batchSize, "")
		tx.Rollback() // read-only

		if err != nil {
			return fmt.Errorf("get events after %d: %w", afterSeq, err)
		}
		if len(result.Events) == 0 {
			break
		}

		var batch []tdsync.Event
		for _, ev := range result.Events {
			if ev.ServerSeq > upToSeq {
				break
			}
			batch = append(batch, ev)
		}

		if len(batch) > 0 {
			snapTx, err := snapDB.Begin()
			if err != nil {
				return fmt.Errorf("begin snapshot tx: %w", err)
			}

			if _, err := tdsync.ApplyRemoteEvents(snapTx, batch, "", validator, nil); err != nil {
				snapTx.Rollback()
				return fmt.Errorf("apply events: %w", err)
			}

			if err := snapTx.Commit(); err != nil {
				return fmt.Errorf("commit snapshot: %w", err)
			}
		}

		afterSeq = result.LastServerSeq
		if !result.HasMore || afterSeq >= upToSeq {
			break
		}
	}

	// Checkpoint WAL to flush into main DB file before copy
	snapDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	snapDB.Close() // explicit close before copy; defer will no-op

	// Copy final DB to snapshot path
	src, err := os.Open(tmpDBPath)
	if err != nil {
		return fmt.Errorf("open temp db for copy: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(snapshotPath)
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy snapshot: %w", err)
	}

	return nil
}
