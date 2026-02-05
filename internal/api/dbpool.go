package api

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tdsync "github.com/marcus/td/internal/sync"
	_ "modernc.org/sqlite"
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

// CloseAll closes all open project database connections.
func (p *ProjectDBPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, db := range p.dbs {
		db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		db.Close()
		delete(p.dbs, id)
	}
}

// openProjectDB opens a SQLite connection for a project event log with standard pragmas.
func openProjectDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open project db: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	db.Exec("PRAGMA synchronous=NORMAL")
	db.Exec("PRAGMA foreign_keys=ON")

	if err := tdsync.InitServerEventLog(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init event log: %w", err)
	}

	return db, nil
}
