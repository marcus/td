package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tddb "github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
)

// ProjectDBPool manages per-project SQLite connections for event logs.
type ProjectDBPool struct {
	mu      sync.RWMutex
	dbs     map[string]*sql.DB
	dataDir string
}

// NewProjectDBPool creates a new pool that stores project databases under dataDir.
func NewProjectDBPool(dataDir string) *ProjectDBPool {
	return &ProjectDBPool{
		dbs:     make(map[string]*sql.DB),
		dataDir: dataDir,
	}
}

// Get returns the database connection for the given project, opening it lazily
// and initializing the event log schema if needed.
func (p *ProjectDBPool) Get(projectID string) (*sql.DB, error) {
	p.mu.RLock()
	db, ok := p.dbs[projectID]
	p.mu.RUnlock()
	if ok {
		return db, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if db, ok := p.dbs[projectID]; ok {
		return db, nil
	}

	dbPath := filepath.Join(p.dataDir, projectID, "events.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("project database not found: %s", projectID)
	}

	db, err := openProjectDB(dbPath)
	if err != nil {
		return nil, err
	}

	p.dbs[projectID] = db
	return db, nil
}

// Create creates a new project database directory and initializes the event log.
func (p *ProjectDBPool) Create(projectID string) (*sql.DB, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If already created, return existing connection
	if db, ok := p.dbs[projectID]; ok {
		return db, nil
	}

	dir := filepath.Join(p.dataDir, projectID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	dbPath := filepath.Join(dir, "events.db")
	db, err := openProjectDB(dbPath)
	if err != nil {
		return nil, err
	}

	p.dbs[projectID] = db
	return db, nil
}

// Delete closes the connection for a project (if open), removes it from the pool,
// and removes the project database directory from disk.
func (p *ProjectDBPool) Delete(projectID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if db, ok := p.dbs[projectID]; ok {
		db.Close()
		delete(p.dbs, projectID)
	}

	dir := filepath.Join(p.dataDir, projectID)
	return os.RemoveAll(dir)
}

// CloseAll closes all open project database connections.
// Uses PASSIVE (not TRUNCATE) so the shutdown checkpoint doesn't stall when
// another process still holds the -shm. SQLite's autocheckpoint handles
// routine WAL maintenance; aggressive TRUNCATE on exit is unnecessary.
func (p *ProjectDBPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, db := range p.dbs {
		_, _ = db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
		db.Close()
		delete(p.dbs, id)
	}
}

// openProjectDB opens a SQLite connection for a project event log with standard pragmas.
func openProjectDB(dbPath string) (*sql.DB, error) {
	db, err := tddb.OpenSQLite(dbPath, tddb.OpenOptions{})
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}

	if err := tdsync.InitServerEventLog(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init event log: %w", err)
	}

	return db, nil
}
