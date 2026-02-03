package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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
			if migration.Version == 20 {
				if err := db.migrateLegacyActionLogCompositeIDs(); err != nil {
					return migrationsRun, fmt.Errorf("migration 20 (action_log normalization): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 19 {
				if err := db.migrateFilePathsToRelative(); err != nil {
					return migrationsRun, fmt.Errorf("migration 19 (relative file paths): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 25 {
				if err := db.migrateBoardPositionSoftDelete(); err != nil {
					return migrationsRun, fmt.Errorf("migration 25 (board position soft delete): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 26 {
				if err := db.migrateActionLogNotNullID(); err != nil {
					return migrationsRun, fmt.Errorf("migration 26 (action_log NOT NULL id): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 24 {
				if err := db.migrateWorkSessionIssueIDs(); err != nil {
					return migrationsRun, fmt.Errorf("migration 24 (work_session_issue IDs): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 18 {
				if err := db.migrateDeterministicIDs(); err != nil {
					return migrationsRun, fmt.Errorf("migration 18 (deterministic IDs): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 16 {
				if err := db.migrateSyncState(); err != nil {
					return migrationsRun, fmt.Errorf("migration 16 (sync_state): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 15 {
				if err := db.migrateToTextIDs(); err != nil {
					return migrationsRun, fmt.Errorf("migration 15 (text IDs): %w", err)
				}
				if err := db.setSchemaVersionInternal(migration.Version); err != nil {
					return migrationsRun, fmt.Errorf("set version %d: %w", migration.Version, err)
				}
				migrationsRun++
				continue
			}
			if migration.Version == 13 || migration.Version == 14 {
				if err := db.ensureSessionsTable(); err != nil {
					return migrationsRun, fmt.Errorf("migration %d (sessions): %w", migration.Version, err)
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

// ensureSessionsTable ensures the sessions table exists with all required columns.
// Handles three cases:
//  1. Table missing entirely — create fresh
//  2. Table exists but missing new columns (branch, agent_type, etc.) — recreate preserving data
//  3. Table already correct — no-op
func (db *DB) ensureSessionsTable() error {
	exists, err := db.tableExists("sessions")
	if err != nil {
		return fmt.Errorf("check sessions table: %w", err)
	}

	if !exists {
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

	// Table exists — check if it already has the new columns
	hasBranch, err := db.columnExists("sessions", "branch")
	if err != nil {
		return fmt.Errorf("check branch column: %w", err)
	}
	if hasBranch {
		// Already has new columns — just ensure indexes exist
		_, err = db.conn.Exec(`
CREATE INDEX IF NOT EXISTS idx_sessions_branch ON sessions(branch);
CREATE INDEX IF NOT EXISTS idx_sessions_branch_agent ON sessions(branch, agent_type, agent_pid);
`)
		return err
	}

	// Old schema — recreate with new columns, preserving data
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

// generateTextID creates a prefixed text ID with 4 random bytes (8 hex chars)
func generateTextID(prefix string) (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// migrateToTextIDs converts 6 tables from INTEGER AUTOINCREMENT PKs to TEXT PKs.
// For each table: create new table with TEXT PK, copy data with generated text IDs,
// update action_log.entity_id references, drop old table, rename new table.
// Wrapped in a transaction for crash safety -- partial migration would corrupt the DB.
func (db *DB) migrateToTextIDs() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // no-op after commit

	if err := db.migrateToTextIDsTx(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (db *DB) migrateToTextIDsTx(tx *sql.Tx) error {
	type tableMigration struct {
		name       string
		prefix     string
		entityType string // for action_log.entity_id rewriting; empty to skip
		createSQL  string
		insertSQL  string // SELECT portion to copy non-id columns
	}

	migrations := []tableMigration{
		{
			name:       "logs",
			prefix:     "lg-",
			entityType: "", // logs aren't referenced in action_log.entity_id
			createSQL: `CREATE TABLE logs_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT DEFAULT '',
				session_id TEXT NOT NULL,
				work_session_id TEXT DEFAULT '',
				message TEXT NOT NULL,
				type TEXT NOT NULL DEFAULT 'progress',
				timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			insertSQL: "SELECT id, issue_id, session_id, work_session_id, message, type, timestamp FROM logs",
		},
		{
			name:       "handoffs",
			prefix:     "ho-",
			entityType: "handoff",
			createSQL: `CREATE TABLE handoffs_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				session_id TEXT NOT NULL,
				done TEXT DEFAULT '[]',
				remaining TEXT DEFAULT '[]',
				decisions TEXT DEFAULT '[]',
				uncertain TEXT DEFAULT '[]',
				timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			insertSQL: "SELECT id, issue_id, session_id, done, remaining, decisions, uncertain, timestamp FROM handoffs",
		},
		{
			name:       "git_snapshots",
			prefix:     "gs-",
			entityType: "",
			createSQL: `CREATE TABLE git_snapshots_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				event TEXT NOT NULL,
				commit_sha TEXT NOT NULL,
				branch TEXT NOT NULL,
				dirty_files INTEGER DEFAULT 0,
				timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			insertSQL: "SELECT id, issue_id, event, commit_sha, branch, dirty_files, timestamp FROM git_snapshots",
		},
		{
			name:       "issue_files",
			prefix:     "if-",
			entityType: "",
			createSQL: `CREATE TABLE issue_files_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				file_path TEXT NOT NULL,
				role TEXT NOT NULL DEFAULT 'implementation',
				linked_sha TEXT DEFAULT '',
				linked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(issue_id, file_path)
			)`,
			insertSQL: "SELECT id, issue_id, file_path, role, linked_sha, linked_at FROM issue_files",
		},
		{
			name:       "comments",
			prefix:     "cm-",
			entityType: "",
			createSQL: `CREATE TABLE comments_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				session_id TEXT NOT NULL,
				text TEXT NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			insertSQL: "SELECT id, issue_id, session_id, text, created_at FROM comments",
		},
		{
			name:       "action_log",
			prefix:     "al-",
			entityType: "",
			createSQL: `CREATE TABLE action_log_new (
				id TEXT PRIMARY KEY,
				session_id TEXT NOT NULL,
				action_type TEXT NOT NULL,
				entity_type TEXT NOT NULL,
				entity_id TEXT NOT NULL,
				previous_data TEXT DEFAULT '',
				new_data TEXT DEFAULT '',
				timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				undone INTEGER DEFAULT 0
			)`,
			insertSQL: "SELECT id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone FROM action_log",
		},
	}

	// Build old-to-new ID mapping for action_log.entity_id rewriting
	idMap := make(map[string]map[string]string) // entityType -> oldID -> newID

	for _, m := range migrations {
		// Check if old table exists with integer PK (idempotency)
		hasIntPK, err := db.tableHasIntegerPKTx(tx, m.name)
		if err != nil {
			return fmt.Errorf("check %s PK type: %w", m.name, err)
		}
		if !hasIntPK {
			continue // Already migrated or fresh DB
		}

		// Drop any leftover temp table from previous failed migrations
		if _, err := tx.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s_new", m.name)); err != nil {
			return fmt.Errorf("drop leftover %s_new: %w", m.name, err)
		}

		// Create new table
		if _, err := tx.Exec(m.createSQL); err != nil {
			return fmt.Errorf("create %s_new: %w", m.name, err)
		}

		// Read ALL old rows into memory first (can't hold an open cursor
		// and execute INSERTs simultaneously within the same transaction)
		rows, err := tx.Query(m.insertSQL)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.name, err)
		}

		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return fmt.Errorf("columns %s: %w", m.name, err)
		}
		nCols := len(cols)

		var allRows [][]interface{}
		for rows.Next() {
			vals := make([]interface{}, nCols)
			ptrs := make([]interface{}, nCols)
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s: %w", m.name, err)
			}
			allRows = append(allRows, vals)
		}
		rows.Close()

		// Track ID mappings for action_log rewriting
		if m.entityType != "" {
			idMap[m.entityType] = make(map[string]string)
		}

		// Build INSERT template
		placeholders := "?"
		for i := 1; i < nCols; i++ {
			placeholders += ", ?"
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s_new (%s) VALUES (%s)",
			m.name, colList(cols), placeholders)

		// Insert rows with new text IDs
		for _, vals := range allRows {
			newID, err := generateTextID(m.prefix)
			if err != nil {
				return fmt.Errorf("generate ID for %s: %w", m.name, err)
			}

			oldID := fmt.Sprintf("%v", vals[0])
			if m.entityType != "" {
				idMap[m.entityType][oldID] = newID
			}

			vals[0] = newID

			if _, err := tx.Exec(insertSQL, vals...); err != nil {
				return fmt.Errorf("insert %s_new: %w", m.name, err)
			}
		}

		// Drop old, rename new
		if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %s", m.name)); err != nil {
			return fmt.Errorf("drop %s: %w", m.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s_new RENAME TO %s", m.name, m.name)); err != nil {
			return fmt.Errorf("rename %s_new: %w", m.name, err)
		}
	}

	// Rewrite action_log.entity_id for handoff references
	for entityType, mapping := range idMap {
		for oldID, newID := range mapping {
			if _, err := tx.Exec(
				`UPDATE action_log SET entity_id = ? WHERE entity_type = ? AND entity_id = ?`,
				newID, entityType, oldID,
			); err != nil {
				return fmt.Errorf("rewrite action_log entity_id (%s %s->%s): %w", entityType, oldID, newID, err)
			}
		}
	}

	// Recreate indexes
	indexSQL := `
CREATE INDEX IF NOT EXISTS idx_logs_issue ON logs(issue_id);
CREATE INDEX IF NOT EXISTS idx_logs_work_session ON logs(work_session_id);
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_handoffs_issue ON handoffs(issue_id);
CREATE INDEX IF NOT EXISTS idx_handoffs_timestamp ON handoffs(timestamp);
CREATE INDEX IF NOT EXISTS idx_git_snapshots_issue ON git_snapshots(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_files_issue ON issue_files(issue_id);
CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id);
CREATE INDEX IF NOT EXISTS idx_comments_created_at ON comments(created_at);
CREATE INDEX IF NOT EXISTS idx_action_log_session ON action_log(session_id);
CREATE INDEX IF NOT EXISTS idx_action_log_timestamp ON action_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_action_log_entity_type ON action_log(entity_id, action_type);
`
	if _, err := tx.Exec(indexSQL); err != nil {
		return fmt.Errorf("recreate indexes: %w", err)
	}

	return nil
}

// tableHasIntegerPK checks if the given table's primary key is INTEGER type
func (db *DB) tableHasIntegerPK(table string) (bool, error) {
	exists, err := db.tableExists(table)
	if err != nil || !exists {
		return false, err
	}

	rows, err := db.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	return scanForIntegerPK(rows)
}

// tableHasIntegerPKTx is like tableHasIntegerPK but runs within a transaction
func (db *DB) tableHasIntegerPKTx(tx *sql.Tx, table string) (bool, error) {
	// Check table exists via tx
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	if err != nil || count == 0 {
		return false, err
	}

	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	return scanForIntegerPK(rows)
}

func scanForIntegerPK(rows *sql.Rows) (bool, error) {
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if pk == 1 && name == "id" {
			return ctype == "INTEGER", nil
		}
	}
	return false, nil
}

// migrateSyncState creates sync_state table and adds sync columns to action_log (idempotent).
func (db *DB) migrateSyncState() error {
	// Create sync_state table
	_, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS sync_state (
		project_id TEXT PRIMARY KEY,
		last_pushed_action_id INTEGER DEFAULT 0,
		last_pulled_server_seq INTEGER DEFAULT 0,
		last_sync_at DATETIME,
		sync_disabled INTEGER DEFAULT 0
	)`)
	if err != nil {
		return fmt.Errorf("create sync_state: %w", err)
	}

	// Add synced_at column if missing
	hasSyncedAt, err := db.columnExists("action_log", "synced_at")
	if err != nil {
		return fmt.Errorf("check synced_at: %w", err)
	}
	if !hasSyncedAt {
		if _, err := db.conn.Exec(`ALTER TABLE action_log ADD COLUMN synced_at DATETIME`); err != nil {
			return fmt.Errorf("add synced_at: %w", err)
		}
	}

	// Add server_seq column if missing
	hasServerSeq, err := db.columnExists("action_log", "server_seq")
	if err != nil {
		return fmt.Errorf("check server_seq: %w", err)
	}
	if !hasServerSeq {
		if _, err := db.conn.Exec(`ALTER TABLE action_log ADD COLUMN server_seq INTEGER`); err != nil {
			return fmt.Errorf("add server_seq: %w", err)
		}
	}

	return nil
}

// migrateDeterministicIDs adds deterministic ID primary keys to
// board_issue_positions, issue_dependencies, and issue_files.
func (db *DB) migrateDeterministicIDs() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// --- board_issue_positions: add id TEXT PRIMARY KEY ---
	exists, err := db.tableExistsTx(tx, "board_issue_positions")
	if err != nil {
		return fmt.Errorf("check board_issue_positions: %w", err)
	}
	if exists {
		hasID, err := db.columnExistsTx(tx, "board_issue_positions", "id")
		if err != nil {
			return fmt.Errorf("check bip id col: %w", err)
		}
		if !hasID {
			if _, err := tx.Exec(`DROP TABLE IF EXISTS board_issue_positions_new`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE TABLE board_issue_positions_new (
				id TEXT PRIMARY KEY,
				board_id TEXT NOT NULL,
				issue_id TEXT NOT NULL,
				position INTEGER NOT NULL,
				added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(board_id, issue_id)
			)`); err != nil {
				return fmt.Errorf("create bip_new: %w", err)
			}

			rows, err := tx.Query(`SELECT board_id, issue_id, position, added_at FROM board_issue_positions`)
			if err != nil {
				return fmt.Errorf("read bip: %w", err)
			}
			type bipRow struct {
				boardID, issueID string
				position         int
				addedAt          interface{}
			}
			var bRows []bipRow
			for rows.Next() {
				var r bipRow
				if err := rows.Scan(&r.boardID, &r.issueID, &r.position, &r.addedAt); err != nil {
					rows.Close()
					return err
				}
				bRows = append(bRows, r)
			}
			rows.Close()

			for _, r := range bRows {
				id := BoardIssuePosID(r.boardID, r.issueID)
				if _, err := tx.Exec(`INSERT INTO board_issue_positions_new (id, board_id, issue_id, position, added_at) VALUES (?, ?, ?, ?, ?)`,
					id, r.boardID, r.issueID, r.position, r.addedAt); err != nil {
					return fmt.Errorf("insert bip_new: %w", err)
				}
			}

			if _, err := tx.Exec(`DROP TABLE board_issue_positions`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE board_issue_positions_new RENAME TO board_issue_positions`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_board_positions_position ON board_issue_positions(board_id, position)`); err != nil {
				return err
			}
		}
	}

	// --- issue_dependencies: add id TEXT PRIMARY KEY ---
	exists, err = db.tableExistsTx(tx, "issue_dependencies")
	if err != nil {
		return fmt.Errorf("check issue_dependencies: %w", err)
	}
	if exists {
		hasID, err := db.columnExistsTx(tx, "issue_dependencies", "id")
		if err != nil {
			return fmt.Errorf("check dep id col: %w", err)
		}
		if !hasID {
			if _, err := tx.Exec(`DROP TABLE IF EXISTS issue_dependencies_new`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE TABLE issue_dependencies_new (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				depends_on_id TEXT NOT NULL,
				relation_type TEXT NOT NULL DEFAULT 'depends_on',
				UNIQUE(issue_id, depends_on_id, relation_type)
			)`); err != nil {
				return fmt.Errorf("create dep_new: %w", err)
			}

			rows, err := tx.Query(`SELECT issue_id, depends_on_id, relation_type FROM issue_dependencies`)
			if err != nil {
				return fmt.Errorf("read deps: %w", err)
			}
			type depRow struct {
				issueID, dependsOnID, relationType string
			}
			var dRows []depRow
			for rows.Next() {
				var r depRow
				if err := rows.Scan(&r.issueID, &r.dependsOnID, &r.relationType); err != nil {
					rows.Close()
					return err
				}
				dRows = append(dRows, r)
			}
			rows.Close()

			for _, r := range dRows {
				id := DependencyID(r.issueID, r.dependsOnID, r.relationType)
				if _, err := tx.Exec(`INSERT INTO issue_dependencies_new (id, issue_id, depends_on_id, relation_type) VALUES (?, ?, ?, ?)`,
					id, r.issueID, r.dependsOnID, r.relationType); err != nil {
					return fmt.Errorf("insert dep_new: %w", err)
				}
			}

			if _, err := tx.Exec(`DROP TABLE issue_dependencies`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE issue_dependencies_new RENAME TO issue_dependencies`); err != nil {
				return err
			}
		}
	}

	// --- issue_files: backfill deterministic IDs ---
	exists, err = db.tableExistsTx(tx, "issue_files")
	if err != nil {
		return fmt.Errorf("check issue_files: %w", err)
	}
	if exists {
		rows, err := tx.Query(`SELECT id, issue_id, file_path FROM issue_files`)
		if err != nil {
			return fmt.Errorf("read issue_files: %w", err)
		}
		type ifRow struct {
			oldID, issueID, filePath string
		}
		var fRows []ifRow
		for rows.Next() {
			var r ifRow
			if err := rows.Scan(&r.oldID, &r.issueID, &r.filePath); err != nil {
				rows.Close()
				return err
			}
			fRows = append(fRows, r)
		}
		rows.Close()

		// Deduplicate: if two rows hash to same deterministic ID, keep first
		seen := make(map[string]bool)
		for _, r := range fRows {
			newID := IssueFileID(r.issueID, r.filePath)
			if seen[newID] {
				// Duplicate — remove this row
				if _, err := tx.Exec(`DELETE FROM issue_files WHERE id = ?`, r.oldID); err != nil {
					return fmt.Errorf("delete dup issue_file: %w", err)
				}
				continue
			}
			seen[newID] = true
			if r.oldID != newID {
				if _, err := tx.Exec(`UPDATE issue_files SET id = ? WHERE id = ?`, newID, r.oldID); err != nil {
					return fmt.Errorf("update issue_file id: %w", err)
				}
			}
		}
	}

	return tx.Commit()
}

// tableExistsTx checks table existence within a transaction
func (db *DB) tableExistsTx(tx *sql.Tx, table string) (bool, error) {
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// columnExistsTx checks column existence within a transaction
func (db *DB) columnExistsTx(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateFilePathsToRelative converts absolute file paths in issue_files
// to repo-relative paths. Skips paths that are already relative.
// Best-effort: uses db.baseDir as the repo root.
func (db *DB) migrateFilePathsToRelative() error {
	exists, err := db.tableExists("issue_files")
	if err != nil || !exists {
		return err
	}

	rows, err := db.conn.Query(`SELECT id, issue_id, file_path FROM issue_files`)
	if err != nil {
		return fmt.Errorf("read issue_files: %w", err)
	}

	type fileRow struct {
		id, issueID, filePath string
	}
	var fRows []fileRow
	for rows.Next() {
		var r fileRow
		if err := rows.Scan(&r.id, &r.issueID, &r.filePath); err != nil {
			rows.Close()
			return err
		}
		fRows = append(fRows, r)
	}
	rows.Close()

	for _, r := range fRows {
		// Skip paths that are already relative
		if !IsAbsolutePath(r.filePath) {
			continue
		}

		relPath, err := ToRepoRelative(r.filePath, db.baseDir)
		if err != nil {
			// Path is outside repo — leave it as-is
			continue
		}

		newID := IssueFileID(r.issueID, relPath)

		// Check if a row with the new relative path already exists (avoid UNIQUE conflict)
		var existingCount int
		db.conn.QueryRow(`SELECT COUNT(*) FROM issue_files WHERE issue_id = ? AND file_path = ?`,
			r.issueID, relPath).Scan(&existingCount)
		if existingCount > 0 {
			// Duplicate — delete the old absolute-path row
			if _, err := db.conn.Exec(`DELETE FROM issue_files WHERE id = ?`, r.id); err != nil {
				return fmt.Errorf("delete dup file row: %w", err)
			}
			continue
		}

		// Update path and recompute deterministic ID
		if _, err := db.conn.Exec(
			`UPDATE issue_files SET file_path = ?, id = ? WHERE id = ?`,
			relPath, newID, r.id,
		); err != nil {
			return fmt.Errorf("update file path: %w", err)
		}
	}

	return nil
}

// migrateLegacyActionLogCompositeIDs normalizes unsynced action_log entries for
// board positions, dependencies, and file links after deterministic ID and
// repo-relative path migrations. It rewrites entity_type/entity_id/new_data
// and marks out-of-repo file links as synced (skipped).
func (db *DB) migrateLegacyActionLogCompositeIDs() error {
	exists, err := db.tableExists("action_log")
	if err != nil || !exists {
		return err
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, action_type, entity_type, entity_id, new_data
		FROM action_log
		WHERE synced_at IS NULL AND undone = 0
		  AND entity_type IN ('board_position', 'board_issue_positions', 'dependency', 'issue_dependencies', 'file_link', 'issue_files')
	`)
	if err != nil {
		return fmt.Errorf("read action_log: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id, actionType, entityType, entityID string
			newDataStr                           sql.NullString
		)
		if err := rows.Scan(&id, &actionType, &entityType, &entityID, &newDataStr); err != nil {
			return err
		}
		fields := map[string]any{}
		if newDataStr.Valid && newDataStr.String != "" {
			if err := json.Unmarshal([]byte(newDataStr.String), &fields); err != nil {
				return fmt.Errorf("parse action_log new_data %s: %w", id, err)
			}
		}

		canonicalType := entityType
		switch entityType {
		case "board_position":
			canonicalType = "board_issue_positions"
		case "dependency":
			canonicalType = "issue_dependencies"
		case "file_link":
			canonicalType = "issue_files"
		}

		changed := canonicalType != entityType

		switch canonicalType {
		case "board_issue_positions":
			boardID := getStringField(fields, "board_id")
			issueID := getStringField(fields, "issue_id")
			if boardID == "" || issueID == "" {
				if a, b, ok := splitLegacyEntityID(entityID); ok {
					boardID, issueID = a, b
					if setStringField(fields, "board_id", boardID) {
						changed = true
					}
					if setStringField(fields, "issue_id", issueID) {
						changed = true
					}
				}
			}
			if boardID == "" || issueID == "" {
				if strings.HasPrefix(entityID, boardIssuePosIDPrefix) {
					if setStringField(fields, "id", entityID) {
						changed = true
					}
					break
				}
				if _, err := tx.Exec(`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
					return fmt.Errorf("mark synced %s: %w", id, err)
				}
				continue
			}
			newID := BoardIssuePosID(boardID, issueID)
			if setStringField(fields, "id", newID) {
				changed = true
			}
			if entityID != newID {
				entityID = newID
				changed = true
			}
		case "issue_dependencies":
			issueID := getStringField(fields, "issue_id")
			dependsOnID := getStringField(fields, "depends_on_id")
			if issueID == "" || dependsOnID == "" {
				if a, b, ok := splitLegacyEntityID(entityID); ok {
					issueID, dependsOnID = a, b
					if setStringField(fields, "issue_id", issueID) {
						changed = true
					}
					if setStringField(fields, "depends_on_id", dependsOnID) {
						changed = true
					}
				}
			}
			if issueID == "" || dependsOnID == "" {
				if strings.HasPrefix(entityID, dependencyIDPrefix) {
					if setStringField(fields, "id", entityID) {
						changed = true
					}
					break
				}
				if _, err := tx.Exec(`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
					return fmt.Errorf("mark synced %s: %w", id, err)
				}
				continue
			}
			relationType := getStringField(fields, "relation_type")
			if relationType == "" {
				relationType = "depends_on"
				if setStringField(fields, "relation_type", relationType) {
					changed = true
				}
			}
			newID := DependencyID(issueID, dependsOnID, relationType)
			if setStringField(fields, "id", newID) {
				changed = true
			}
			if entityID != newID {
				entityID = newID
				changed = true
			}
		case "issue_files":
			issueID := getStringField(fields, "issue_id")
			filePath := getStringField(fields, "file_path")
			if issueID == "" {
				if _, err := tx.Exec(`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
					return fmt.Errorf("mark synced %s: %w", id, err)
				}
				continue
			}
			if filePath == "" {
				if looksLikePath(entityID) {
					filePath = entityID
					if setStringField(fields, "file_path", filePath) {
						changed = true
					}
				} else if strings.HasPrefix(entityID, issueFileIDPrefix) {
					if setStringField(fields, "id", entityID) {
						changed = true
					}
					break
				} else {
					if _, err := tx.Exec(`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
						return fmt.Errorf("mark synced %s: %w", id, err)
					}
					continue
				}
			}
			if IsAbsolutePath(filePath) {
				relPath, err := ToRepoRelative(filePath, db.baseDir)
				if err != nil {
					if _, err := tx.Exec(`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
						return fmt.Errorf("mark synced %s: %w", id, err)
					}
					continue
				}
				filePath = relPath
			}
			filePath = NormalizeFilePathForID(filePath)
			if setStringField(fields, "file_path", filePath) {
				changed = true
			}
			newID := IssueFileID(issueID, filePath)
			if setStringField(fields, "id", newID) {
				changed = true
			}
			if entityID != newID {
				entityID = newID
				changed = true
			}
		default:
			continue
		}

		if !changed {
			continue
		}

		if isDeleteAction(actionType) && canonicalType == "issue_files" && getStringField(fields, "file_path") == "" {
			// Avoid writing invalid payloads for delete-only entries with no file_path.
			if _, err := tx.Exec(`UPDATE action_log SET entity_type = ?, entity_id = ? WHERE id = ?`, canonicalType, entityID, id); err != nil {
				return fmt.Errorf("update action_log %s: %w", id, err)
			}
			continue
		}

		payload, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("marshal action_log new_data %s: %w", id, err)
		}
		if _, err := tx.Exec(
			`UPDATE action_log SET entity_type = ?, entity_id = ?, new_data = ? WHERE id = ?`,
			canonicalType, entityID, string(payload), id,
		); err != nil {
			return fmt.Errorf("update action_log %s: %w", id, err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	return tx.Commit()
}

// colList joins column names with commas
func colList(cols []string) string {
	result := cols[0]
	for _, c := range cols[1:] {
		result += ", " + c
	}
	return result
}

func getStringField(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		return val.String()
	default:
		return fmt.Sprint(val)
	}
}

func setStringField(fields map[string]any, key, value string) bool {
	if cur, ok := fields[key]; ok {
		if curStr, ok := cur.(string); ok && curStr == value {
			return false
		}
		if curNum, ok := cur.(json.Number); ok && curNum.String() == value {
			return false
		}
	}
	fields[key] = value
	return true
}

func splitLegacyEntityID(entityID string) (string, string, bool) {
	if parts := strings.SplitN(entityID, ":", 2); len(parts) == 2 {
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	if parts := strings.SplitN(entityID, "|", 2); len(parts) == 2 {
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func looksLikePath(value string) bool {
	if IsAbsolutePath(value) {
		return true
	}
	return strings.Contains(value, "/") || strings.Contains(value, "\\")
}

func isDeleteAction(actionType string) bool {
	switch actionType {
	case "delete", "remove_dependency", "unlink_file", "board_unposition", "board_delete", "board_remove_issue":
		return true
	default:
		return false
	}
}

// migrateWorkSessionIssueIDs adds a deterministic id TEXT PRIMARY KEY to
// work_session_issues, following the same pattern as migrateDeterministicIDs
// for board_issue_positions.
func (db *DB) migrateWorkSessionIssueIDs() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	exists, err := db.tableExistsTx(tx, "work_session_issues")
	if err != nil {
		return fmt.Errorf("check work_session_issues: %w", err)
	}
	if !exists {
		return tx.Commit()
	}

	hasID, err := db.columnExistsTx(tx, "work_session_issues", "id")
	if err != nil {
		return fmt.Errorf("check wsi id col: %w", err)
	}
	if hasID {
		return tx.Commit()
	}

	if _, err := tx.Exec(`DROP TABLE IF EXISTS work_session_issues_new`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE work_session_issues_new (
		id TEXT PRIMARY KEY,
		work_session_id TEXT NOT NULL,
		issue_id TEXT NOT NULL,
		tagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(work_session_id, issue_id)
	)`); err != nil {
		return fmt.Errorf("create wsi_new: %w", err)
	}

	rows, err := tx.Query(`SELECT work_session_id, issue_id, tagged_at FROM work_session_issues`)
	if err != nil {
		return fmt.Errorf("read wsi: %w", err)
	}
	type wsiRow struct {
		wsID, issueID string
		taggedAt      interface{}
	}
	var wRows []wsiRow
	for rows.Next() {
		var r wsiRow
		if err := rows.Scan(&r.wsID, &r.issueID, &r.taggedAt); err != nil {
			rows.Close()
			return err
		}
		wRows = append(wRows, r)
	}
	rows.Close()

	for _, r := range wRows {
		id := WsiID(r.wsID, r.issueID)
		if _, err := tx.Exec(`INSERT INTO work_session_issues_new (id, work_session_id, issue_id, tagged_at) VALUES (?, ?, ?, ?)`,
			id, r.wsID, r.issueID, r.taggedAt); err != nil {
			return fmt.Errorf("insert wsi_new: %w", err)
		}
	}

	if _, err := tx.Exec(`DROP TABLE work_session_issues`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE work_session_issues_new RENAME TO work_session_issues`); err != nil {
		return err
	}

	return tx.Commit()
}

// migrateBoardPositionSoftDelete adds deleted_at column to board_issue_positions
// if it doesn't already exist. This enables soft deletes for sync convergence.
func (db *DB) migrateBoardPositionSoftDelete() error {
	exists, err := db.columnExists("board_issue_positions", "deleted_at")
	if err != nil {
		return fmt.Errorf("check deleted_at col: %w", err)
	}
	if exists {
		return nil
	}
	_, err = db.conn.Exec(`ALTER TABLE board_issue_positions ADD COLUMN deleted_at DATETIME`)
	return err
}

// migrateActionLogNotNullID fixes NULL/empty ids in action_log and recreates
// the table with a NOT NULL constraint on the id column.
func (db *DB) migrateActionLogNotNullID() error {
	exists, err := db.tableExists("action_log")
	if err != nil || !exists {
		return err
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First fix any NULL or empty ids
	if _, err := tx.Exec(`UPDATE action_log SET id = 'al-' || lower(hex(randomblob(4))) WHERE id IS NULL OR id = ''`); err != nil {
		return fmt.Errorf("fix NULL ids: %w", err)
	}

	// Drop any leftover temp table from previous failed migrations
	if _, err := tx.Exec(`DROP TABLE IF EXISTS action_log_new`); err != nil {
		return fmt.Errorf("drop leftover action_log_new: %w", err)
	}

	// Create new table with NOT NULL constraint on id
	if _, err := tx.Exec(`CREATE TABLE action_log_new (
		id TEXT NOT NULL PRIMARY KEY,
		session_id TEXT NOT NULL,
		action_type TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		previous_data TEXT DEFAULT '',
		new_data TEXT DEFAULT '',
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		undone INTEGER DEFAULT 0,
		synced_at DATETIME,
		server_seq INTEGER
	)`); err != nil {
		return fmt.Errorf("create action_log_new: %w", err)
	}

	// Copy data
	if _, err := tx.Exec(`INSERT INTO action_log_new SELECT id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone, synced_at, server_seq FROM action_log`); err != nil {
		return fmt.Errorf("copy action_log data: %w", err)
	}

	// Drop old table and rename new
	if _, err := tx.Exec(`DROP TABLE action_log`); err != nil {
		return fmt.Errorf("drop action_log: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE action_log_new RENAME TO action_log`); err != nil {
		return fmt.Errorf("rename action_log_new: %w", err)
	}

	// Recreate indexes
	if _, err := tx.Exec(`
CREATE INDEX IF NOT EXISTS idx_action_log_session ON action_log(session_id);
CREATE INDEX IF NOT EXISTS idx_action_log_timestamp ON action_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_action_log_entity_type ON action_log(entity_id, action_type);
`); err != nil {
		return fmt.Errorf("recreate action_log indexes: %w", err)
	}

	return tx.Commit()
}
