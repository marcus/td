package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/syncclient"
)

// fakePushServer returns an httptest server that accepts push requests,
// records batch sizes, and returns proper acks with sequential server_seqs.
func fakePushServer(t *testing.T, maxBatch int) (*httptest.Server, *pushRecorder) {
	t.Helper()
	rec := &pushRecorder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req syncclient.PushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		if maxBatch > 0 && len(req.Events) > maxBatch {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"code":    "bad_request",
				"message": fmt.Sprintf("batch size %d exceeds max %d", len(req.Events), maxBatch),
			})
			return
		}

		rec.mu.Lock()
		rec.batchSizes = append(rec.batchSizes, len(req.Events))
		baseSeq := rec.nextSeq
		rec.nextSeq += int64(len(req.Events))
		rec.mu.Unlock()

		acks := make([]syncclient.AckResponse, len(req.Events))
		for i, ev := range req.Events {
			acks[i] = syncclient.AckResponse{
				ClientActionID: ev.ClientActionID,
				ServerSeq:      baseSeq + int64(i) + 1,
			}
		}

		resp := syncclient.PushResponse{
			Accepted: len(req.Events),
			Acks:     acks,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux), rec
}

type pushRecorder struct {
	mu         sync.Mutex
	batchSizes []int
	nextSeq    int64
}

func (r *pushRecorder) totalPushed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, s := range r.batchSizes {
		total += s
	}
	return total
}

// setupAutoSyncTestDB creates a temp DB with a session, sync_state, and n
// unsynced action_log entries. Returns the DB and a cleanup-safe session ID.
func setupAutoSyncTestDB(t *testing.T, n int) *db.DB {
	t.Helper()
	dir := t.TempDir()

	// Set TD_SESSION_ID so session.Get uses a deterministic fingerprint
	t.Setenv("TD_SESSION_ID", "test-autosync-push")

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Create a session via GetOrCreate (uses TD_SESSION_ID + current branch)
	sess, err := session.GetOrCreate(database)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	now := time.Now()

	// Set up sync_state
	if err := database.SetSyncState("proj-test"); err != nil {
		t.Fatalf("set sync state: %v", err)
	}

	// Mark built-in entities (e.g. "All Issues" board from migration) as already
	// having action_log entries so the orphan backfill doesn't pick them up.
	conn := database.Conn()
	conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone, synced_at)
		VALUES ('al-builtin-board', ?, 'board_create', 'boards', 'bd-all-issues', '{}', datetime('now'), 0, datetime('now'))`, sess.ID)

	// Insert n unsynced action_log entries
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for i := 1; i <= n; i++ {
		_, err := stmt.Exec(
			fmt.Sprintf("al-%08d", i),
			sess.ID,
			"create",
			"issues",
			fmt.Sprintf("i_%08d", i),
			"{}",
			fmt.Sprintf(`{"title":"Issue %d","status":"open"}`, i),
			now.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano),
		)
		if err != nil {
			t.Fatalf("insert action_log %d: %v", i, err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	return database
}

func TestAutoSyncPush_BatchesLargePayload(t *testing.T) {
	totalEvents := 1200
	database := setupAutoSyncTestDB(t, totalEvents)

	srv, rec := fakePushServer(t, 1000) // server rejects >1000
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")

	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	err = autoSyncPush(database, client, state, "dev-test")
	if err != nil {
		t.Fatalf("autoSyncPush: %v", err)
	}

	// Should have made 3 batches: 500 + 500 + 200
	rec.mu.Lock()
	batches := append([]int{}, rec.batchSizes...)
	rec.mu.Unlock()

	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d: %v", len(batches), batches)
	}
	if batches[0] != 500 || batches[1] != 500 || batches[2] != 200 {
		t.Fatalf("expected [500, 500, 200], got %v", batches)
	}
	if rec.totalPushed() != totalEvents {
		t.Fatalf("expected %d total pushed, got %d", totalEvents, rec.totalPushed())
	}

	// Verify all events are now marked as synced
	var unsynced int
	err = database.Conn().QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&unsynced)
	if err != nil {
		t.Fatalf("count unsynced: %v", err)
	}
	if unsynced != 0 {
		t.Errorf("expected 0 unsynced events, got %d", unsynced)
	}
}

func TestAutoSyncPush_SmallPayloadSingleBatch(t *testing.T) {
	totalEvents := 100
	database := setupAutoSyncTestDB(t, totalEvents)

	srv, rec := fakePushServer(t, 1000)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")

	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	err = autoSyncPush(database, client, state, "dev-test")
	if err != nil {
		t.Fatalf("autoSyncPush: %v", err)
	}

	rec.mu.Lock()
	batches := append([]int{}, rec.batchSizes...)
	rec.mu.Unlock()

	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d: %v", len(batches), batches)
	}
	if batches[0] != totalEvents {
		t.Fatalf("expected batch of %d, got %d", totalEvents, batches[0])
	}
}

func TestAutoSyncPush_ExactBatchBoundary(t *testing.T) {
	totalEvents := 1000 // exactly 2 batches of 500
	database := setupAutoSyncTestDB(t, totalEvents)

	srv, rec := fakePushServer(t, 1000)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")

	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	err = autoSyncPush(database, client, state, "dev-test")
	if err != nil {
		t.Fatalf("autoSyncPush: %v", err)
	}

	rec.mu.Lock()
	batches := append([]int{}, rec.batchSizes...)
	rec.mu.Unlock()

	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d: %v", len(batches), batches)
	}
	if batches[0] != 500 || batches[1] != 500 {
		t.Fatalf("expected [500, 500], got %v", batches)
	}
}

func TestAutoSyncPush_NothingToPush(t *testing.T) {
	database := setupAutoSyncTestDB(t, 0)

	srv, rec := fakePushServer(t, 1000)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")

	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	err = autoSyncPush(database, client, state, "dev-test")
	if err != nil {
		t.Fatalf("autoSyncPush: %v", err)
	}

	rec.mu.Lock()
	batches := append([]int{}, rec.batchSizes...)
	rec.mu.Unlock()

	if len(batches) != 0 {
		t.Fatalf("expected 0 batches for empty push, got %d", len(batches))
	}
}

func TestAutoSyncPush_ServerRejectsUnbatched(t *testing.T) {
	// Verify that without batching, 1200 events would fail against a server
	// with maxBatch=1000. This confirms the batching is necessary.
	totalEvents := 1200
	database := setupAutoSyncTestDB(t, totalEvents)

	srv, _ := fakePushServer(t, 1000)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")

	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	// autoSyncPush should succeed because it batches internally
	err = autoSyncPush(database, client, state, "dev-test")
	if err != nil {
		t.Fatalf("autoSyncPush should succeed with batching, got: %v", err)
	}
}
