// Package db provides the SQLite persistence layer for td, handling issue
// storage, migrations, multi-process locking, and query execution.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marcus/td/internal/workdir"
	_ "modernc.org/sqlite"
)

// QueryValidator is set by main to validate TDQ queries without import cycle.
// Returns nil if valid, error describing parse failure otherwise.
var QueryValidator func(queryStr string) error

const (
	dbFile = ".todos/issues.db"
)

// DB wraps the database connection
type DB struct {
	conn    *sql.DB
	baseDir string
}

// ResolveBaseDir checks for a .td-root file in the given directory.
// If found, it returns the path contained in that file (pointing to the main
// worktree's root). Otherwise, returns the original baseDir unchanged.
// This enables git worktrees to share a single td database with the main repo.
func ResolveBaseDir(baseDir string) string {
	return workdir.ResolveBaseDir(baseDir)
}

// openConn opens a SQLite connection with safe defaults for multi-process access.
//
// FK enforcement (PRAGMA foreign_keys=ON) is the default from OpenSQLite.
// Migration 30 (td-4846e6) cleans up pre-existing orphans and adds
// ON DELETE CASCADE to child tables before this was flipped on.
func openConn(dbPath string) (*sql.DB, error) {
	return OpenSQLite(dbPath, OpenOptions{})
}

// Open opens the database and runs any pending migrations
func Open(baseDir string) (*DB, error) {
	// Check for worktree redirection via .td-root
	baseDir = ResolveBaseDir(baseDir)
	dbPath := filepath.Join(baseDir, dbFile)

	// Check if db exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database not found: run 'td init' first")
	}

	conn, err := openConn(dbPath)
	if err != nil {
		return nil, err
	}

	db := &DB{conn: conn, baseDir: baseDir}

	// Run any pending migrations
	if _, err := db.RunMigrations(); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Initialize creates the database and runs migrations
func Initialize(baseDir string) (*DB, error) {
	// Check for worktree redirection via .td-root
	baseDir = ResolveBaseDir(baseDir)
	dbPath := filepath.Join(baseDir, dbFile)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := openConn(dbPath)
	if err != nil {
		return nil, err
	}

	// Run schema
	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}

	db := &DB{conn: conn, baseDir: baseDir}

	// Run migrations
	if _, err := db.RunMigrations(); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
// It performs a PASSIVE checkpoint first to flush the WAL into the main DB
// file where possible without blocking readers/writers in other processes.
// TRUNCATE was avoided here because it can fail or stall when another td
// process still holds the -shm; SQLite autocheckpoints at 1000 pages so the
// aggressive variant is unnecessary on exit.
func (db *DB) Close() error {
	// Best-effort checkpoint — ignore errors (DB might already be in a bad state)
	db.conn.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	return db.conn.Close()
}

// SetMaxOpenConns sets the maximum number of open connections to the database.
// For SQLite with single-writer semantics, this should typically be set to 1
// to prevent connection pool growth in long-running applications.
func (db *DB) SetMaxOpenConns(n int) {
	db.conn.SetMaxOpenConns(n)
}

// BaseDir returns the base directory for the database
func (db *DB) BaseDir() string {
	return db.baseDir
}

// withWriteLock serializes writes across concurrent td CLI processes on
// .todos/issues.db using a file lock at .todos/db.lock.
//
// Scope: this lock ONLY coordinates writers to the CLI's issues.db. It does
// NOT coordinate with the API server (internal/api/dbpool.go and
// internal/serverdb), which writes to separate databases —
// {dataDir}/server.db and {dataDir}/{projectID}/events.db — and relies on
// SQLite's internal locking. If you add a new writer to .todos/issues.db
// from outside the CLI, you must also go through this lock (or an
// equivalent flock on .todos/db.lock); otherwise cross-process writes can
// race despite SQLite's own locking, which is optimistic under WAL.
func (db *DB) withWriteLock(fn func() error) error {
	locker := newWriteLocker(db.baseDir)
	if err := locker.acquire(defaultTimeout); err != nil {
		return err
	}
	defer locker.release()
	return fn()
}
