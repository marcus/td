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
	tdevents "github.com/marcus/td/internal/events"
	tdsync "github.com/marcus/td/internal/sync"
)

// isValidEntityType validates entity types using the centralized taxonomy.
// Accepts both singular and plural forms for backward compatibility.
func isValidEntityType(et string) bool {
	_, ok := tdevents.NormalizeEntityType(et)
	return ok
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

	// Convert to sync.Event with normalization
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

		// Normalize entity and action types to canonical forms.
		// Skip action type normalization if already canonical to avoid
		// double-normalization (e.g. "delete" → "soft_delete").
		canonicalEntity, _ := tdevents.NormalizeEntityType(ev.EntityType)
		canonicalAction := ev.ActionType
		if !tdevents.IsValidActionType(ev.ActionType) {
			canonicalAction = string(tdevents.NormalizeActionType(ev.ActionType))
		}

		events[i] = tdsync.Event{
			ClientActionID:  ev.ClientActionID,
			DeviceID:        req.DeviceID,
			SessionID:       req.SessionID,
			ActionType:      string(canonicalAction),
			EntityType:      string(canonicalEntity),
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
	defer func() { _ = tx.Rollback() }()

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

	// Apply accepted events to project.db (live state). Same machinery the
	// pool's bootstrap replay uses (tdsync.ApplyRemoteEvents). Plan §7.2.
	//
	// Failure handling: events.db is already committed above, so the next push
	// (or a CLI retry) will see these events again and reapply via the
	// idempotent cursor logic in applyAcceptedEventsToProjectDB. We surface
	// 500 here so the client retries; meanwhile project.db is behind for this
	// project. buildSnapshot remains the recovery valve per plan §7.2.
	if s.projectLivePool != nil && result.Accepted > 0 {
		if err := applyAcceptedEventsToProjectDB(s.projectLivePool, projectID, events, result); err != nil {
			logFor(r.Context()).Error("apply push to project.db", "project", projectID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to apply events to project state")
			return
		}
	}

	// Update cached event count in server.db
	if result.Accepted > 0 {
		if err := s.store.UpdateProjectEventCount(projectID, result.Accepted, time.Now().UTC()); err != nil {
			logFor(r.Context()).Warn("update project event count", "project", projectID, "err", err)
		}
	}

	// Upsert sync cursor for this device. Tracks the device as a "sync client"
	// for the admin UI and records its last-known server_seq position.
	// Use max server_seq from acks (or duplicate rejections) so the cursor
	// never moves backwards even if the client retries already-acked batches.
	var maxSeq int64
	for _, a := range result.Acks {
		if a.ServerSeq > maxSeq {
			maxSeq = a.ServerSeq
		}
	}
	for _, rj := range result.Rejected {
		if rj.ServerSeq > maxSeq {
			maxSeq = rj.ServerSeq
		}
	}
	if maxSeq > 0 {
		if err := s.store.UpsertSyncCursor(projectID, req.DeviceID, maxSeq); err != nil {
			logFor(r.Context()).Warn("upsert sync cursor on push", "project", projectID, "device", req.DeviceID, "err", err)
		}
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
	defer func() { _ = tx.Rollback() }()

	excludeClient := r.URL.Query().Get("exclude_client")
	result, err := tdsync.GetEventsSince(tx, afterSeq, limit, excludeClient)
	if err != nil {
		logFor(r.Context()).Error("get events", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to query events")
		return
	}

	_ = tx.Rollback() // read-only, just release

	// Upsert sync cursor for the pulling device so it shows up in the admin
	// "Sync Clients" tab. The pull endpoint uses `exclude_client` to carry
	// the caller's device_id; we treat that as the client_id for the cursor.
	// Only advance when we know the head — result.LastServerSeq is the
	// highest server_seq observed by the pull (may be > returned events if
	// we excluded the caller's own writes).
	if excludeClient != "" && result.LastServerSeq > 0 {
		if err := s.store.UpsertSyncCursor(projectID, excludeClient, result.LastServerSeq); err != nil {
			logFor(r.Context()).Warn("upsert sync cursor on pull", "project", projectID, "device", excludeClient, "err", err)
		}
	}

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

	// Cache miss — use singleflight to deduplicate concurrent builds for the same snapshot.
	// Without this, two concurrent requests race on file renames and one gets a 500.
	sfKey := fmt.Sprintf("%s:%d", projectID, lastSeq)
	result, err, _ := s.snapshotGroup.Do(sfKey, func() (any, error) {
		// Double-check cache inside singleflight (another request may have just cached it)
		if _, err := os.Stat(cachePath); err == nil {
			return cachePath, nil
		}

		tmpFile, err := os.CreateTemp("", "td-snapshot-*.db")
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()

		if err := buildSnapshot(eventsDB, tmpPath, lastSeq); err != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("build snapshot: %w", err)
		}

		// Cache the built snapshot using atomic write-and-rename.
		// Clean up tmpPath if it's not the final serve path (copyFile may rename it away).
		servePath := tmpPath
		defer func() {
			if servePath != tmpPath {
				os.Remove(tmpPath) // no-op if already renamed away
			}
		}()
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			slog.Warn("snapshot cache mkdir failed", "dir", cacheDir, "err", err)
		} else {
			tmpCachePath := cachePath + fmt.Sprintf(".tmp.%d", os.Getpid())
			if err := copyFile(tmpPath, tmpCachePath); err == nil {
				// copyFile may have used os.Rename (fast path), which moves
				// tmpPath away. Update servePath immediately so we can still
				// serve the data even if the next rename fails.
				servePath = tmpCachePath
				if err := os.Rename(tmpCachePath, cachePath); err != nil {
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
		return servePath, nil
	})
	if err != nil {
		logFor(r.Context()).Error("build snapshot", "project", projectID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to build snapshot")
		return
	}

	// Note: if caching failed entirely, servePath points to a temp file that won't
	// be cleaned up here. With singleflight, multiple callers share the same path,
	// so no single caller can safely delete it. The OS temp directory handles cleanup.
	serveSnapshotFile(w, r, result.(string), lastSeq)
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

	stat, err := f.Stat()
	if err != nil {
		logFor(r.Context()).Error("stat snapshot", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to stat snapshot")
		return
	}
	w.Header().Set("Content-Type", "application/x-sqlite3")
	w.Header().Set("X-Snapshot-Seq", strconv.FormatInt(seq, 10))
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
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

	// Re-open the initialized DB for event replay. FK enforcement is kept
	// OFF here because replay walks the event log in server_seq order and
	// may legitimately encounter child rows (e.g. board_issue_positions)
	// before their parents — events can arrive from multiple devices out of
	// causal order. The final CLI issues.db (opened via openConn) enforces
	// FKs on writes; this snapshot DB is a transient mirror we stream to
	// clients. (td-4846e6)
	tmpDBPath := filepath.Join(tmpDir, ".todos", "issues.db")
	snapDB, err := tddb.OpenSQLite(tmpDBPath, tddb.OpenOptions{DisableForeignKeys: true})
	if err != nil {
		return fmt.Errorf("open snapshot db: %w", err)
	}
	defer snapDB.Close()

	validator := func(t string) bool { return isValidEntityType(t) }
	afterSeq := int64(0)
	batchSize := 1000

	for {
		tx, err := eventsDB.Begin()
		if err != nil {
			return fmt.Errorf("begin event read tx: %w", err)
		}

		result, err := tdsync.GetEventsSince(tx, afterSeq, batchSize, "")
		_ = tx.Rollback() // read-only

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
				_ = snapTx.Rollback()
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

	// Checkpoint WAL to flush into main DB file before copy.
	// TRUNCATE (not PASSIVE) is intentional here: this path immediately
	// copies the main DB file to snapshotPath, and we want every pending
	// write merged in — leaving data in the -wal/-shm sidecars would
	// produce a truncated snapshot. This is the one site that keeps
	// TRUNCATE; Close() paths (db.go, serverdb.go, dbpool.go) use PASSIVE.
	if _, err := snapDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		slog.Warn("snapshot WAL checkpoint failed", "err", err)
	}
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
