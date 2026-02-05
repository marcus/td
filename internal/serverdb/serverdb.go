package serverdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ServerDB wraps the server database connection
type ServerDB struct {
	conn *sql.DB
	path string
}

// Open opens the server database and runs any pending migrations.
// If the database file does not exist, it is created and initialized.
func Open(dbPath string) (*ServerDB, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	conn.Exec("PRAGMA synchronous=NORMAL")
	conn.Exec("PRAGMA foreign_keys=ON")

	// Run schema
	if _, err := conn.Exec(serverSchema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	db := &ServerDB{conn: conn, path: dbPath}

	if _, err := db.RunMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

// Ping checks the database connection is alive.
func (db *ServerDB) Ping() error {
	return db.conn.Ping()
}

// Close checkpoints the WAL and closes the database connection.
func (db *ServerDB) Close() error {
	db.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return db.conn.Close()
}

// RunMigrations runs any pending database migrations.
func (db *ServerDB) RunMigrations() (int, error) {
	// Ensure schema_info exists
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_info (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return 0, fmt.Errorf("create schema_info: %w", err)
	}

	currentVersion := db.getSchemaVersion()

	if currentVersion >= ServerSchemaVersion {
		return 0, nil
	}

	migrationsRun := 0
	for _, m := range Migrations {
		if m.Version > currentVersion {
			if _, err := db.conn.Exec(m.SQL); err != nil {
				return migrationsRun, fmt.Errorf("migration %d (%s): %w", m.Version, m.Description, err)
			}
			if err := db.setSchemaVersion(m.Version); err != nil {
				return migrationsRun, fmt.Errorf("set version %d: %w", m.Version, err)
			}
			migrationsRun++
		}
	}

	// Set to current version if fresh DB
	if currentVersion == 0 {
		if err := db.setSchemaVersion(ServerSchemaVersion); err != nil {
			return migrationsRun, err
		}
	}

	return migrationsRun, nil
}

func (db *ServerDB) getSchemaVersion() int {
	var version string
	err := db.conn.QueryRow("SELECT value FROM schema_info WHERE key = 'version'").Scan(&version)
	if err != nil {
		return 0
	}
	var v int
	fmt.Sscanf(version, "%d", &v)
	return v
}

func (db *ServerDB) setSchemaVersion(version int) error {
	_, err := db.conn.Exec(`INSERT OR REPLACE INTO schema_info (key, value) VALUES ('version', ?)`,
		fmt.Sprintf("%d", version))
	return err
}

// NewID generates a project ID (exported for callers that need to pre-generate IDs).
func NewID() string {
	id, err := generateID("p_")
	if err != nil {
		// crypto/rand failure is fatal
		panic("generate id: " + err.Error())
	}
	return id
}

// generateID creates a prefixed ID with 8 random hex chars.
func generateID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s", prefix, hex.EncodeToString(b)), nil
}
