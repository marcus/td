package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tddb "github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

// ProjectLivePool manages per-project handles to the project-live SQLite
// database (one DB per project, located at
// `{baseDir}/{projectID}/project.db`).
//
// The project-live DB holds the full td CLI schema (issues, action_log,
// boards, ...) and is the read+write target for the Perch-shaped REST handlers
// being added in Stream 2 (see docs/td-watch-rich-ui-plan.md §6). It is
// distinct from `events.db`, which remains the authoritative event log
// managed by ProjectDBPool.
//
// On first acquire for a project, if `project.db` does not exist, the pool
// performs a one-time bootstrap:
//   1. Initialize a fresh td schema in a temporary directory via
//      `tddb.Initialize` (this guarantees the schema matches the td CLI
//      exactly, including all migrations).
//   2. Copy the resulting issues.db file to `project.db`.
//   3. Re-open `project.db` and replay every event in `events.db` (in
//      `server_seq` order) into it via `tdsync.ApplyRemoteEvents`.
//   4. Record the highest applied `server_seq` in a small `applied_events`
//      bookkeeping table inside project.db.
//
// This is the **one-time bootstrap path only**. Ongoing event application
// (post-commit promotion of action_log → events.db, and inbound /v1/sync/push
// → project.db) is the responsibility of Stream 3 and is intentionally out of
// scope here.
//
// Concurrency:
//   - Acquire/Release are safe under concurrent use. Each acquired handle is
//     refcounted; Release decrements, and Close releases all handles
//     unconditionally.
//   - First-touch initialization for a given project is serialized by a
//     per-project sync.Mutex so concurrent goroutines that all see "no
//     project.db" cooperate on a single bootstrap rather than racing.
//   - The underlying *sql.DB is opened with WAL mode and a 5s busy_timeout
//     (defaults from tddb.OpenSQLite), which lets concurrent reads proceed
//     while a single writer is active.
//
// Lifecycle decision: handles are kept open after Release (a "hot" pool).
// Refcounts are tracked for observability and so that Close can be reasoned
// about, but a zero refcount does NOT trigger close. Closing every handle on
// the last Release would cause a thrashing open/close pattern under typical
// HTTP traffic. Close (called on server shutdown) tears every handle down.
type ProjectLivePool struct {
	baseDir string

	mu      sync.Mutex
	entries map[string]*projectLiveEntry

	// initLocks serializes first-touch initialization per project so concurrent
	// Acquire calls for the same project don't race on the bootstrap path.
	initMu    sync.Mutex
	initLocks map[string]*sync.Mutex
}

type projectLiveEntry struct {
	db       *tddb.DB
	refcount int
}

// NewProjectLivePool creates a pool whose project.db files live under baseDir
// (one project directory per projectID, file named project.db inside that
// directory).
func NewProjectLivePool(baseDir string) *ProjectLivePool {
	return &ProjectLivePool{
		baseDir:   baseDir,
		entries:   make(map[string]*projectLiveEntry),
		initLocks: make(map[string]*sync.Mutex),
	}
}

// Acquire returns an opened handle to the project-live DB for projectID,
// creating and bootstrapping it from events.db on first access. Each
// successful Acquire must be paired with a Release.
func (p *ProjectLivePool) Acquire(projectID string) (*tddb.DB, error) {
	if projectID == "" {
		return nil, errors.New("project_live_pool: empty projectID")
	}

	// Fast path: handle already cached.
	p.mu.Lock()
	if entry, ok := p.entries[projectID]; ok {
		entry.refcount++
		p.mu.Unlock()
		return entry.db, nil
	}
	p.mu.Unlock()

	// Slow path: serialize first-touch initialization per project. Any other
	// goroutine that arrives concurrently will either (a) wait here, or (b)
	// observe the cached entry on its second pass below.
	initLock := p.initLockFor(projectID)
	initLock.Lock()
	defer initLock.Unlock()

	// Re-check under the init lock — another goroutine may have populated
	// the entry while we were waiting.
	p.mu.Lock()
	if entry, ok := p.entries[projectID]; ok {
		entry.refcount++
		p.mu.Unlock()
		return entry.db, nil
	}
	p.mu.Unlock()

	db, err := p.openOrBootstrap(projectID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	entry := &projectLiveEntry{db: db, refcount: 1}
	p.entries[projectID] = entry
	return db, nil
}

// Release decrements the refcount for projectID. The handle is intentionally
// kept open even at zero refcount (hot pool); Close tears everything down on
// shutdown.
func (p *ProjectLivePool) Release(projectID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[projectID]
	if !ok {
		return
	}
	if entry.refcount > 0 {
		entry.refcount--
	}
}

// Close closes every cached handle and clears the pool. Safe to call multiple
// times.
func (p *ProjectLivePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for id, entry := range p.entries {
		if err := entry.db.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close project %s: %w", id, err)
		}
		delete(p.entries, id)
	}
	return firstErr
}

// initLockFor returns the per-project sync.Mutex used to serialize first-touch
// bootstrap. The lock is created lazily on first request.
func (p *ProjectLivePool) initLockFor(projectID string) *sync.Mutex {
	p.initMu.Lock()
	defer p.initMu.Unlock()
	lk, ok := p.initLocks[projectID]
	if !ok {
		lk = &sync.Mutex{}
		p.initLocks[projectID] = lk
	}
	return lk
}

// projectDBPath returns the absolute path to project.db for the given project.
func (p *ProjectLivePool) projectDBPath(projectID string) string {
	return filepath.Join(p.baseDir, projectID, "project.db")
}

// eventsDBPath returns the absolute path to events.db for the given project.
// events.db may not exist yet for brand-new projects with zero events.
func (p *ProjectLivePool) eventsDBPath(projectID string) string {
	return filepath.Join(p.baseDir, projectID, "events.db")
}

// openOrBootstrap opens an existing project.db or, if it doesn't exist, runs
// the bootstrap path: init schema → copy to project.db → replay events.db.
// Caller must hold the per-project init lock.
func (p *ProjectLivePool) openOrBootstrap(projectID string) (*tddb.DB, error) {
	dbPath := p.projectDBPath(projectID)
	if _, err := os.Stat(dbPath); err == nil {
		return openExistingProjectDB(filepath.Dir(dbPath))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat project.db: %w", err)
	}

	// Bootstrap: init a fresh td schema in a temp dir, then move issues.db
	// into project.db at the canonical location.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir project dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "td-project-live-init-*")
	if err != nil {
		return nil, fmt.Errorf("mktemp init dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tdb, err := tddb.Initialize(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("initialize td schema: %w", err)
	}
	// Close so we can copy the underlying file safely.
	if err := tdb.Close(); err != nil {
		return nil, fmt.Errorf("close init db: %w", err)
	}

	srcDB := filepath.Join(tmpDir, ".todos", "issues.db")
	if err := copyFile(srcDB, dbPath); err != nil {
		return nil, fmt.Errorf("copy init db to project.db: %w", err)
	}

	db, err := openExistingProjectDB(filepath.Dir(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open project.db post-init: %w", err)
	}

	// Create the applied_events bookkeeping table. Idempotent.
	if _, err := db.Conn().Exec(`
		CREATE TABLE IF NOT EXISTS applied_events (
			server_seq INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create applied_events: %w", err)
	}

	if err := p.replayEvents(projectID, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("replay events.db: %w", err)
	}

	return db, nil
}

// openExistingProjectDB opens an already-initialized project.db at
// `{projectDir}/project.db`. We bypass tddb.Open (which expects
// `.todos/issues.db`) and use OpenSQLite directly with the same FK + WAL
// pragma policy the td CLI uses today; the schema migration step is unneeded
// because the file was created via tddb.Initialize during bootstrap.
func openExistingProjectDB(projectDir string) (*tddb.DB, error) {
	dbPath := filepath.Join(projectDir, "project.db")
	conn, err := tddb.OpenSQLite(dbPath, tddb.OpenOptions{})
	if err != nil {
		return nil, fmt.Errorf("open project.db: %w", err)
	}
	return tddb.NewWithConn(conn, projectDir), nil
}

// replayEvents reads every row from the project's events.db (if it exists)
// in server_seq order and applies it to the freshly-initialized project.db
// using the same machinery buildSnapshot uses (tdsync.ApplyRemoteEvents).
// On completion, the highest applied server_seq is recorded in
// applied_events as the bootstrap cursor.
//
// This is the one-time bootstrap path. Ongoing event application post-bootstrap
// happens in Stream 3 and is out of scope here.
func (p *ProjectLivePool) replayEvents(projectID string, projectDB *tddb.DB) error {
	eventsPath := p.eventsDBPath(projectID)
	if _, err := os.Stat(eventsPath); errors.Is(err, os.ErrNotExist) {
		// Brand-new project with no events yet — nothing to replay.
		return nil
	} else if err != nil {
		return fmt.Errorf("stat events.db: %w", err)
	}

	eventsDB, err := tddb.OpenSQLite(eventsPath, tddb.OpenOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("open events.db: %w", err)
	}
	defer eventsDB.Close()

	validator := func(t string) bool { return isValidEntityType(t) }
	const batchSize = 1000
	afterSeq := int64(0)
	highest := int64(0)

	for {
		readTx, err := eventsDB.Begin()
		if err != nil {
			return fmt.Errorf("begin events read tx: %w", err)
		}
		result, err := tdsync.GetEventsSince(readTx, afterSeq, batchSize, "")
		_ = readTx.Rollback()
		if err != nil {
			return fmt.Errorf("get events after %d: %w", afterSeq, err)
		}
		if len(result.Events) == 0 {
			break
		}

		applyTx, err := projectDB.Conn().Begin()
		if err != nil {
			return fmt.Errorf("begin project apply tx: %w", err)
		}
		if _, err := tdsync.ApplyRemoteEvents(applyTx, result.Events, "", validator, nil); err != nil {
			_ = applyTx.Rollback()
			return fmt.Errorf("apply events: %w", err)
		}
		if err := applyTx.Commit(); err != nil {
			return fmt.Errorf("commit apply tx: %w", err)
		}

		for _, ev := range result.Events {
			if ev.ServerSeq > highest {
				highest = ev.ServerSeq
			}
		}

		afterSeq = result.LastServerSeq
		if !result.HasMore {
			break
		}
	}

	if highest > 0 {
		if _, err := projectDB.Conn().Exec(
			`INSERT OR REPLACE INTO applied_events(server_seq, applied_at) VALUES (?, datetime('now'))`,
			highest,
		); err != nil {
			return fmt.Errorf("record applied cursor: %w", err)
		}
	}
	return nil
}

