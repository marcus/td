package monitor

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	"github.com/marcus/td/internal/db"
)

// sharedDB manages singleton database connections for embedded monitors.
// This prevents connection leaks when Model values are copied in Update().
//
// Problem: When a tea.Model is implemented with value receivers, each Update()
// call creates a copy of the Model. If the embedder stores &m from the returned
// model, the old model is abandoned. While the *db.DB pointer is shared between
// copies, Go's sql.DB connection pool can grow unbounded, leading to hundreds
// of open file descriptors on the same SQLite database.
//
// Solution: Use a singleton pattern where one DB connection is shared across
// all Model copies for a given database path. The connection is cached by path
// and reused, ensuring only one connection pool exists per database.
type sharedDB struct {
	mu    sync.RWMutex
	conns map[string]*sharedDBEntry
}

type sharedDBEntry struct {
	db   *db.DB
	refs int // Reference count for cleanup
}

var dbPool = &sharedDB{conns: make(map[string]*sharedDBEntry)}

// debugWriter is where dbpool diagnostics are written when
// TD_MONITOR_DBPOOL_DEBUG=1. Exposed as a package var so tests can redirect.
var debugWriter io.Writer = os.Stderr

// dbpoolDebugEnv is the env var that enables ref-count diagnostics.
const dbpoolDebugEnv = "TD_MONITOR_DBPOOL_DEBUG"

// debugLog emits a diagnostic line for dbpool operations when the
// TD_MONITOR_DBPOOL_DEBUG env var is set to "1". Silent otherwise.
// Uses runtime.Caller(2) to capture the caller of getSharedDB/releaseSharedDB
// (skipping debugLog itself + the pool function).
func debugLog(op string, path string, refsAfter int) {
	if os.Getenv(dbpoolDebugEnv) != "1" {
		return
	}
	_, file, line, ok := runtime.Caller(2)
	caller := "unknown:0"
	if ok {
		caller = fmt.Sprintf("%s:%d", file, line)
	}
	fmt.Fprintf(debugWriter, "[dbpool] op=%s path=%s refs=%d caller=%s\n", op, path, refsAfter, caller)
}

// getSharedDB returns a shared database connection for the given base directory.
// The connection is cached and reused across all Model instances for this path.
// Callers should call releaseSharedDB when done (though for embedded monitors,
// the connection typically lives for the application lifetime).
func getSharedDB(baseDir string) (*db.DB, error) {
	// Resolve the actual path (handles worktree redirects)
	resolvedDir := db.ResolveBaseDir(baseDir)

	dbPool.mu.Lock()
	defer dbPool.mu.Unlock()

	if entry, ok := dbPool.conns[resolvedDir]; ok {
		entry.refs++
		debugLog("get", resolvedDir, entry.refs)
		return entry.db, nil
	}

	// Open new connection
	database, err := db.Open(resolvedDir)
	if err != nil {
		return nil, err
	}

	// Limit connections for SQLite single-writer semantics.
	// This is critical for preventing connection pool growth.
	database.SetMaxOpenConns(1)

	dbPool.conns[resolvedDir] = &sharedDBEntry{
		db:   database,
		refs: 1,
	}
	debugLog("get", resolvedDir, 1)

	return database, nil
}

// releaseSharedDB decrements the reference count for a shared database.
// When refs reaches zero, the connection is closed and removed from the pool.
// This is typically called when an embedded monitor is explicitly closed.
func releaseSharedDB(baseDir string) error {
	resolvedDir := db.ResolveBaseDir(baseDir)

	dbPool.mu.Lock()
	defer dbPool.mu.Unlock()

	entry, ok := dbPool.conns[resolvedDir]
	if !ok {
		debugLog("release-miss", resolvedDir, 0)
		return nil
	}

	entry.refs--
	refsAfter := entry.refs
	if entry.refs <= 0 {
		err := entry.db.Close()
		delete(dbPool.conns, resolvedDir)
		debugLog("release", resolvedDir, refsAfter)
		return err
	}

	debugLog("release", resolvedDir, refsAfter)
	return nil
}

// clearDBPool closes all shared connections and clears the pool.
// This is primarily for testing purposes.
func clearDBPool() {
	dbPool.mu.Lock()
	defer dbPool.mu.Unlock()

	for path, entry := range dbPool.conns {
		entry.db.Close()
		delete(dbPool.conns, path)
	}
}
