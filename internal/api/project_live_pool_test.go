package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tddb "github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

// seedEventsDB creates a project events.db at {baseDir}/{projectID}/events.db
// and inserts the given events via tdsync.InsertServerEvents. Returns the
// highest server_seq assigned. Caller-supplied events are wrapped in the
// {new_data, previous_data} payload envelope so they replay cleanly via
// tdsync.ApplyRemoteEvents.
func seedEventsDB(t *testing.T, baseDir, projectID string, n int) int64 {
	t.Helper()

	dir := filepath.Join(baseDir, projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}

	conn, err := tddb.OpenSQLite(filepath.Join(dir, "events.db"), tddb.OpenOptions{})
	if err != nil {
		t.Fatalf("open events.db: %v", err)
	}
	defer conn.Close()

	if err := tdsync.InitServerEventLog(conn); err != nil {
		t.Fatalf("init event log: %v", err)
	}

	if n == 0 {
		return 0
	}

	events := make([]tdsync.Event, n)
	for i := 0; i < n; i++ {
		body := map[string]any{
			"id":         fmt.Sprintf("issue-%d", i+1),
			"title":      fmt.Sprintf("seeded issue %d", i+1),
			"status":     "open",
			"priority":   "P1",
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		}
		newData, _ := json.Marshal(body)
		payload, _ := json.Marshal(map[string]json.RawMessage{
			"new_data":      newData,
			"previous_data": json.RawMessage(`null`),
		})
		events[i] = tdsync.Event{
			DeviceID:        "test-device",
			SessionID:       "test-session",
			ClientActionID:  int64(i + 1),
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        fmt.Sprintf("issue-%d", i+1),
			Payload:         payload,
			ClientTimestamp: time.Now().UTC().Truncate(time.Second),
		}
	}

	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	res, err := tdsync.InsertServerEvents(tx, events)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert events: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit events: %v", err)
	}
	if res.Accepted != n {
		t.Fatalf("seed: accepted %d, want %d", res.Accepted, n)
	}

	var maxSeq int64
	for _, ack := range res.Acks {
		if ack.ServerSeq > maxSeq {
			maxSeq = ack.ServerSeq
		}
	}
	return maxSeq
}

// queryAppliedCursor reads the highest applied_events.server_seq, or 0 when
// the table is empty.
func queryAppliedCursor(t *testing.T, db *tddb.DB) int64 {
	t.Helper()
	var seq int64
	err := db.Conn().QueryRow(`SELECT COALESCE(MAX(server_seq), 0) FROM applied_events`).Scan(&seq)
	if err != nil {
		t.Fatalf("query applied_events: %v", err)
	}
	return seq
}

// countIssues returns the number of rows in the issues table.
func countIssues(t *testing.T, db *tddb.DB) int {
	t.Helper()
	var n int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM issues`).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

func TestAcquire_NewProject(t *testing.T) {
	baseDir := t.TempDir()
	pool := NewProjectLivePool(baseDir)
	t.Cleanup(func() { _ = pool.Close() })

	const projectID = "proj-new"
	// Seed events.db with zero events so the directory exists but no replay
	// work is needed. (The pool also handles the case where events.db is
	// entirely missing; covered indirectly via TestAcquire_NoEventsDB.)
	_ = seedEventsDB(t, baseDir, projectID, 0)

	db, err := pool.Acquire(projectID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { pool.Release(projectID) })

	// project.db file must exist on disk at the expected path.
	dbPath := filepath.Join(baseDir, projectID, "project.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("project.db not created: %v", err)
	}

	// Schema is present (issues table exists, queryable, empty).
	if got := countIssues(t, db); got != 0 {
		t.Errorf("issues count: got %d, want 0", got)
	}

	// applied_events table exists and is empty.
	if got := queryAppliedCursor(t, db); got != 0 {
		t.Errorf("applied cursor: got %d, want 0", got)
	}
}

func TestAcquire_NoEventsDB(t *testing.T) {
	// When events.db doesn't exist at all (brand new project, nothing seeded),
	// Acquire still bootstraps project.db with an empty schema and no replay.
	baseDir := t.TempDir()
	pool := NewProjectLivePool(baseDir)
	t.Cleanup(func() { _ = pool.Close() })

	const projectID = "proj-bare"

	db, err := pool.Acquire(projectID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { pool.Release(projectID) })

	if got := countIssues(t, db); got != 0 {
		t.Errorf("issues count: got %d, want 0", got)
	}
	if got := queryAppliedCursor(t, db); got != 0 {
		t.Errorf("applied cursor: got %d, want 0", got)
	}
}

func TestAcquire_ExistingEvents(t *testing.T) {
	baseDir := t.TempDir()
	pool := NewProjectLivePool(baseDir)
	t.Cleanup(func() { _ = pool.Close() })

	const projectID = "proj-replay"
	maxSeq := seedEventsDB(t, baseDir, projectID, 5)
	if maxSeq <= 0 {
		t.Fatalf("seed: maxSeq should be > 0, got %d", maxSeq)
	}

	db, err := pool.Acquire(projectID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { pool.Release(projectID) })

	if got := countIssues(t, db); got != 5 {
		t.Errorf("replayed issues count: got %d, want 5", got)
	}
	if got := queryAppliedCursor(t, db); got != maxSeq {
		t.Errorf("applied cursor: got %d, want %d (highest server_seq)", got, maxSeq)
	}
}

func TestAcquire_AlreadyMigrated(t *testing.T) {
	// Second Acquire of the same project must reuse the cached handle; the
	// bootstrap path must NOT run again (would re-run replay and re-create
	// the file).
	baseDir := t.TempDir()
	pool := NewProjectLivePool(baseDir)
	t.Cleanup(func() { _ = pool.Close() })

	const projectID = "proj-cache"
	_ = seedEventsDB(t, baseDir, projectID, 3)

	first, err := pool.Acquire(projectID)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	second, err := pool.Acquire(projectID)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if first != second {
		t.Errorf("expected cached handle reuse: first=%p second=%p", first, second)
	}

	pool.Release(projectID)
	pool.Release(projectID)

	// After all Releases the handle should still be open (hot pool semantics).
	// Verify by re-running a query against `first`.
	if got := countIssues(t, first); got != 3 {
		t.Errorf("post-release query: got %d, want 3", got)
	}
}

func TestConcurrentAcquire(t *testing.T) {
	// 10 goroutines Acquire the same project concurrently. The bootstrap path
	// must run exactly once: all 10 callers must see the same handle, and the
	// applied cursor must equal the seeded max (not a multiple of it, which
	// would indicate replay ran more than once and inserted duplicate events).
	baseDir := t.TempDir()
	pool := NewProjectLivePool(baseDir)
	t.Cleanup(func() { _ = pool.Close() })

	const projectID = "proj-concurrent"
	const n = 10
	maxSeq := seedEventsDB(t, baseDir, projectID, 4)

	var wg sync.WaitGroup
	handles := make([]*tddb.DB, n)
	errs := make([]error, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			db, err := pool.Acquire(projectID)
			handles[i] = db
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d acquire: %v", i, errs[i])
		}
		if handles[i] == nil {
			t.Fatalf("goroutine %d returned nil handle", i)
		}
		if handles[i] != handles[0] {
			t.Errorf("goroutine %d got distinct handle (init ran more than once)", i)
		}
	}

	for i := 0; i < n; i++ {
		pool.Release(projectID)
	}

	// If init ran more than once, replay would have re-applied the 4 seed
	// events on top of themselves; ApplyRemoteEvents is upsert-based so the
	// row count is idempotent, but the applied_events bookkeeping cursor is
	// the more precise check — it must equal the seeded max exactly.
	if got := queryAppliedCursor(t, handles[0]); got != maxSeq {
		t.Errorf("applied cursor: got %d, want %d", got, maxSeq)
	}
	if got := countIssues(t, handles[0]); got != 4 {
		t.Errorf("issues count: got %d, want 4", got)
	}
}
