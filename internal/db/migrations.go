package db

import (
	"database/sql"
	"fmt"
)

// columnExists checks whether a column exists on a table
func (db *DB) columnExists(table, column string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s);", table)
	rows, err := db.conn.Query(query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}

// tableExists checks whether a table exists in the database
func (db *DB) tableExists(table string) (bool, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetSchemaVersion returns the current schema version from the database
func (db *DB) GetSchemaVersion() (int, error) {
	var version string
	err := db.conn.QueryRow("SELECT value FROM schema_info WHERE key = 'version'").Scan(&version)
	if err == sql.ErrNoRows {
		// No version set, assume version 0 (pre-migration)
		return 0, nil
	}
	if err != nil {
		// Table might not exist yet
		return 0, nil
	}
	var v int
	fmt.Sscanf(version, "%d", &v)
	return v, nil
}

// SetSchemaVersion sets the schema version in the database
func (db *DB) SetSchemaVersion(version int) error {
	return db.withWriteLock(func() error {
		return db.setSchemaVersionInternal(version)
	})
}

// setSchemaVersionInternal sets schema version without acquiring lock (for use during init)
func (db *DB) setSchemaVersionInternal(version int) error {
	_, err := db.conn.Exec(`INSERT OR REPLACE INTO schema_info (key, value) VALUES ('version', ?)`,
		fmt.Sprintf("%d", version))
	return err
}

// RunMigrations runs any pending database migrations
func (db *DB) RunMigrations() (int, error) {
	// Quick check without lock - if already at current version, skip
	currentVersion, _ := db.GetSchemaVersion()
	if currentVersion >= SchemaVersion {
		return 0, nil
	}

	// Need to run migrations - acquire lock
	var migrationsRun int
	err := db.withWriteLock(func() error {
		var err error
		migrationsRun, err = db.runMigrationsInternal()
		return err
	})
	return migrationsRun, err
}

// runMigrationsInternal runs migrations without acquiring lock (for use during init)
func (db *DB) runMigrationsInternal() (int, error) {
	// Ensure schema_info table exists
	_, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_info (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	if err != nil {
		return 0, fmt.Errorf("create schema_info: %w", err)
	}

	currentVersion, err := db.GetSchemaVersion()
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}

	migrationsRun := 0
	for _, migration := range Migrations {
		if migration.Version > currentVersion {
			if migration.Version == 4 {
				exists, err := db.columnExists("issues", "minor")
				if err != nil {
					return migrationsRun, fmt.Errorf("check column minor: %w", err)
				}
				if exists {
					if err := db.setSchemaVersionInternal(migration.Version); err != nil {
						return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
					}
					migrationsRun++
					continue
				}
			}
			if migration.Version == 5 {
				exists, err := db.columnExists("issues", "created_branch")
				if err != nil {
					return migrationsRun, fmt.Errorf("check column created_branch: %w", err)
				}
				if exists {
					if err := db.setSchemaVersionInternal(migration.Version); err != nil {
						return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
					}
					migrationsRun++
					continue
				}
			}
			if migration.Version == 13 {
				if err := db.migrateV13Sessions(); err != nil {
					return migrationsRun, fmt.Errorf("migration 13 (sessions): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if _, err := db.conn.Exec(migration.SQL); err != nil {
				return migrationsRun, fmt.Errorf("migration %d (%s): %w", migration.Version, migration.Description, err)
			}
			if err := db.setSchemaVersionInternal(migration.Version); err != nil {
				return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
			}
			migrationsRun++
		}
	}

	// If no migrations and version is 0, set to current schema version
	if currentVersion == 0 {
		if err := db.setSchemaVersionInternal(SchemaVersion); err != nil {
			return migrationsRun, err
		}
	}

	return migrationsRun, nil
}

// migrateV13Sessions handles the v13 migration which extends the sessions table.
// If the sessions table is missing (bad prior migration), it creates it fresh.
func (db *DB) migrateV13Sessions() error {
	exists, err := db.tableExists("sessions")
	if err != nil {
		return fmt.Errorf("check sessions table: %w", err)
	}

	if !exists {
		// No sessions table — create it fresh with all v13 columns
		_, err := db.conn.Exec(`
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    branch TEXT DEFAULT '',
    agent_type TEXT DEFAULT '',
    agent_pid INTEGER DEFAULT 0,
    context_id TEXT DEFAULT '',
    previous_session_id TEXT DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    last_activity DATETIME
);
CREATE INDEX IF NOT EXISTS idx_sessions_branch ON sessions(branch);
CREATE INDEX IF NOT EXISTS idx_sessions_branch_agent ON sessions(branch, agent_type, agent_pid);
`)
		return err
	}

	// Sessions table exists — recreate with new columns, preserving data
	_, err = db.conn.Exec(`
CREATE TABLE sessions_new (
    id TEXT PRIMARY KEY,
    name TEXT DEFAULT '',
    branch TEXT DEFAULT '',
    agent_type TEXT DEFAULT '',
    agent_pid INTEGER DEFAULT 0,
    context_id TEXT DEFAULT '',
    previous_session_id TEXT DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    last_activity DATETIME
);
INSERT INTO sessions_new (id, name, context_id, previous_session_id, started_at, ended_at)
    SELECT id, name, context_id, previous_session_id, started_at, ended_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;
CREATE INDEX IF NOT EXISTS idx_sessions_branch ON sessions(branch);
CREATE INDEX IF NOT EXISTS idx_sessions_branch_agent ON sessions(branch, agent_type, agent_pid);
`)
	return err
}
