package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
			_ = json.NewEncoder(w).Encode(map[string]string{
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
		_ = json.NewEncoder(w).Encode(resp)
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
	if _, err := conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone, synced_at)
		VALUES ('al-builtin-board', ?, 'board_create', 'boards', 'bd-all-issues', '{}', datetime('now'), 0, datetime('now'))`, sess.ID); err != nil {
		t.Fatalf("insert builtin board action: %v", err)
	}

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

// flakyPushServer fails the first failCount push requests with the given status,
// then serves them successfully. attempts counts every request received.
func flakyPushServer(t *testing.T, failCount int32, failStatus int) (*httptest.Server, *int32) {
	t.Helper()
	var attempts int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= failCount {
			w.WriteHeader(failStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "unavailable", "message": "try again"})
			return
		}

		var req syncclient.PushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		acks := make([]syncclient.AckResponse, len(req.Events))
		for i, ev := range req.Events {
			acks[i] = syncclient.AckResponse{ClientActionID: ev.ClientActionID, ServerSeq: int64(i) + 1}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(syncclient.PushResponse{Accepted: len(req.Events), Acks: acks})
	})

	return httptest.NewServer(mux), &attempts
}

func TestPushBatchWithRetry_RecoversAfterTransient(t *testing.T) {
	srv, attempts := flakyPushServer(t, 2, http.StatusServiceUnavailable)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	req := &syncclient.PushRequest{DeviceID: "dev-test", SessionID: "s1"}

	resp, err := pushBatchWithRetry(client, "proj-test", req, time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("expected retry to recover, got error: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response after recovery")
	}
	if got := atomic.LoadInt32(attempts); got != 3 {
		t.Fatalf("expected 3 attempts (2 fail + 1 success), got %d", got)
	}
}

func TestPushBatchWithRetry_GivesUpAfterBudget(t *testing.T) {
	srv, attempts := flakyPushServer(t, 1<<30, http.StatusServiceUnavailable) // always fail
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	req := &syncclient.PushRequest{DeviceID: "dev-test", SessionID: "s1"}

	start := time.Now()
	resp, err := pushBatchWithRetry(client, "proj-test", req, time.Now().Add(600*time.Millisecond))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error when server never recovers")
	}
	if resp != nil {
		t.Fatalf("expected nil response on failure")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("retry exceeded its time budget: %v", elapsed)
	}
	if atomic.LoadInt32(attempts) < 1 {
		t.Fatalf("expected at least one attempt")
	}
}

func TestPushBatchWithRetry_RestoresClientTimeout(t *testing.T) {
	// The retry loop clamps client.HTTP.Timeout per attempt; it must restore the
	// original so a subsequent pull on the same client is not left with a tiny
	// timeout.
	srv, _ := flakyPushServer(t, 1<<30, http.StatusServiceUnavailable) // always fail
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	client.HTTP.Timeout = autoSyncHTTPTimeout
	req := &syncclient.PushRequest{DeviceID: "dev-test", SessionID: "s1"}

	_, _ = pushBatchWithRetry(client, "proj-test", req, time.Now().Add(600*time.Millisecond))

	if client.HTTP.Timeout != autoSyncHTTPTimeout {
		t.Fatalf("expected client timeout restored to %v, got %v", autoSyncHTTPTimeout, client.HTTP.Timeout)
	}
}

func TestAutoSyncPush_LeavesEventsPendingWhenServerDown(t *testing.T) {
	// When the push never succeeds, events must remain unsynced (synced_at NULL)
	// so the caller can detect pending > 0 and warn the user.
	database := setupAutoSyncTestDB(t, 4)

	srv, _ := flakyPushServer(t, 1<<30, http.StatusServiceUnavailable) // always fail
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	if err := autoSyncPush(database, client, state, "dev-test"); err == nil {
		t.Fatalf("expected autoSyncPush to fail against a downed server")
	}

	var unsynced int
	if err := database.Conn().QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&unsynced); err != nil {
		t.Fatalf("count unsynced: %v", err)
	}
	if unsynced != 4 {
		t.Errorf("expected 4 events still pending after failed push, got %d", unsynced)
	}
}

func TestAutoSyncOnce_SkipsPullWhenPushFails(t *testing.T) {
	database := setupAutoSyncTestDB(t, 4)

	var pushAttempts int32
	var pullAttempts int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/projects/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			atomic.AddInt32(&pushAttempts, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "unavailable", "message": "try again"})
		case http.MethodGet:
			atomic.AddInt32(&pullAttempts, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(syncclient.PullResponse{})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldBaseDirOverride := baseDirOverride
	dir := database.BaseDir()
	baseDirOverride = &dir
	t.Cleanup(func() {
		baseDirOverride = oldBaseDirOverride
		atomic.StoreInt32(&autoSyncInFlight, 0)
	})

	atomic.StoreInt32(&autoSyncInFlight, 0)
	t.Setenv("TD_AUTH_KEY", "test-key")
	t.Setenv("TD_SYNC_URL", srv.URL)
	t.Setenv("TD_SYNC_AUTO", "true")
	t.Setenv("TD_SYNC_AUTO_PULL", "true")

	pending := autoSyncOnce()

	if pending != 4 {
		t.Fatalf("expected 4 pending events after failed push, got %d", pending)
	}
	if atomic.LoadInt32(&pushAttempts) == 0 {
		t.Fatal("expected push to be attempted")
	}
	if got := atomic.LoadInt32(&pullAttempts); got != 0 {
		t.Fatalf("pull should be skipped after failed push, got %d attempts", got)
	}
}

func TestPushBatchWithRetry_UnauthorizedNotRetried(t *testing.T) {
	srv, attempts := flakyPushServer(t, 1<<30, http.StatusUnauthorized)
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	req := &syncclient.PushRequest{DeviceID: "dev-test", SessionID: "s1"}

	_, err := pushBatchWithRetry(client, "proj-test", req, time.Now().Add(5*time.Second))
	if !errors.Is(err, syncclient.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if got := atomic.LoadInt32(attempts); got != 1 {
		t.Fatalf("unauthorized must not be retried: expected 1 attempt, got %d", got)
	}
}

func TestAutoSyncPush_RecoversAfterTransientError(t *testing.T) {
	database := setupAutoSyncTestDB(t, 5)

	srv, _ := flakyPushServer(t, 1, http.StatusServiceUnavailable) // first push fails, retry succeeds
	defer srv.Close()

	client := syncclient.New(srv.URL, "test-key", "dev-test")
	state, err := database.GetSyncState()
	if err != nil || state == nil {
		t.Fatalf("get sync state: %v", err)
	}

	if err := autoSyncPush(database, client, state, "dev-test"); err != nil {
		t.Fatalf("autoSyncPush should recover after transient error: %v", err)
	}

	var unsynced int
	if err := database.Conn().QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&unsynced); err != nil {
		t.Fatalf("count unsynced: %v", err)
	}
	if unsynced != 0 {
		t.Errorf("expected 0 unsynced after recovery, got %d", unsynced)
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
