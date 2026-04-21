package db

import (
	"database/sql"
	"fmt"
	"log"
)

// migrateEnableFKEnforcement is migration 30 (td-4846e6).
//
// Preparation for flipping PRAGMA foreign_keys=ON on the CLI issues.db. It:
//
//  1. Cleans up the FK orphans identified by the Wave 1 audit (td-b8dd0d).
//     Policies (justified per relation):
//     - issues.parent_id: nullify to "" (empty-string sentinel) — preserve
//     the issue itself; parent link was dangling.
//     - handoffs/git_snapshots/issue_dependencies/issue_session_history:
//     DELETE — a row referring to a missing issue has no meaningful value.
//  2. Rewrites child tables to add ON DELETE CASCADE where td's
//     internal/sync/events.go already performs a manual cascade delete. This
//     makes the DB enforce what the app code does. internal/sync/events.go
//     still runs the manual cascade; that's fine (schema cascade is a no-op
//     after the manual path removes rows). td-0001eb will remove the
//     manual emulation once this is in.
//  3. issues.parent_id FK: the constraint is DROPPED at the schema level.
//     Rationale: td's codebase uses "" (empty string) as the "no parent"
//     sentinel throughout (INSERTs, UPDATEs, sync event payloads). SQLite
//     FK semantics treat "" as a real value, not as NULL, so a schema-level
//     FK on parent_id would reject every top-level issue insert (since no
//     issue has id = ""). Migrating every writer to use NULL is out of
//     scope for td-4846e6; keeping parent_id unconstrained matches the
//     long-standing behaviour, preserves the audit (which treats "" as not
//     a link), and leaves deleting a parent as non-cascading (children
//     become orphans with stale parent_ids until the app or a follow-up
//     cleanup rewrites them). The fk_audit keeps flagging any true orphans.
//
// The whole migration runs in a single transaction. We temporarily toggle
// PRAGMA foreign_keys=OFF around the table rewrites (standard SQLite practice
// for multi-table DDL) and rely on the final PRAGMA foreign_key_check to catch
// any violations before commit. Idempotency is guarded by the schema_version
// bump handled by runMigrationsInternal.
func (db *DB) migrateEnableFKEnforcement() error {
	// Outside the transaction: foreign_keys pragma cannot be toggled inside a
	// transaction. Record and restore it.
	var prevFK int
	if err := db.conn.QueryRow("PRAGMA foreign_keys").Scan(&prevFK); err != nil {
		return fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	if _, err := db.conn.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}
	defer func() {
		// Restore pragma to whatever it was on entry. If prevFK was 0 (off),
		// we leave it off; if 1, we turn it back on. The central opener sets
		// FK=ON by default after this migration, so future opens will be
		// correct regardless.
		if prevFK == 1 {
			db.conn.Exec("PRAGMA foreign_keys=ON")
		}
	}()

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// ------- Step 1: clean orphans -------
	var (
		nParentNullified int64
		nHandoffs        int64
		nGitSnaps        int64
		nDeps            int64
		nSessHist        int64
	)

	// issues.parent_id: nullify (empty-string sentinel). Only consider rows
	// whose parent_id is set (non-empty) and has no matching parent row.
	res, err := tx.Exec(`
		UPDATE issues SET parent_id = ''
		WHERE parent_id != '' AND parent_id NOT IN (SELECT id FROM issues)
	`)
	if err != nil {
		return fmt.Errorf("nullify orphan parent_id: %w", err)
	}
	nParentNullified, _ = res.RowsAffected()

	// handoffs: delete orphans.
	res, err = tx.Exec(`DELETE FROM handoffs WHERE issue_id NOT IN (SELECT id FROM issues)`)
	if err != nil {
		return fmt.Errorf("delete orphan handoffs: %w", err)
	}
	nHandoffs, _ = res.RowsAffected()

	// git_snapshots: delete orphans.
	res, err = tx.Exec(`DELETE FROM git_snapshots WHERE issue_id NOT IN (SELECT id FROM issues)`)
	if err != nil {
		return fmt.Errorf("delete orphan git_snapshots: %w", err)
	}
	nGitSnaps, _ = res.RowsAffected()

	// issue_dependencies: delete rows where either side references a missing issue.
	res, err = tx.Exec(`
		DELETE FROM issue_dependencies
		WHERE issue_id NOT IN (SELECT id FROM issues)
		   OR depends_on_id NOT IN (SELECT id FROM issues)
	`)
	if err != nil {
		return fmt.Errorf("delete orphan issue_dependencies: %w", err)
	}
	nDeps, _ = res.RowsAffected()

	// issue_session_history: delete orphans.
	if ok, err := db.tableExistsTx(tx, "issue_session_history"); err != nil {
		return fmt.Errorf("check issue_session_history: %w", err)
	} else if ok {
		res, err = tx.Exec(`DELETE FROM issue_session_history WHERE issue_id NOT IN (SELECT id FROM issues)`)
		if err != nil {
			return fmt.Errorf("delete orphan issue_session_history: %w", err)
		}
		nSessHist, _ = res.RowsAffected()
	}

	// Clean other audited relations too (zero in production, but makes the
	// subsequent ADD-FK safe on any drifted DB).
	if _, err := tx.Exec(`DELETE FROM issue_files WHERE issue_id NOT IN (SELECT id FROM issues)`); err != nil {
		return fmt.Errorf("delete orphan issue_files: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM comments WHERE issue_id NOT IN (SELECT id FROM issues)`); err != nil {
		return fmt.Errorf("delete orphan comments: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM work_session_issues WHERE issue_id NOT IN (SELECT id FROM issues) OR work_session_id NOT IN (SELECT id FROM work_sessions)`); err != nil {
		return fmt.Errorf("delete orphan work_session_issues: %w", err)
	}
	if ok, err := db.tableExistsTx(tx, "board_issue_positions"); err == nil && ok {
		if _, err := tx.Exec(`DELETE FROM board_issue_positions WHERE issue_id NOT IN (SELECT id FROM issues) OR board_id NOT IN (SELECT id FROM boards)`); err != nil {
			return fmt.Errorf("delete orphan board_issue_positions: %w", err)
		}
	}

	// ------- Step 2: table rewrites to add ON DELETE CASCADE -------
	//
	// For each child table that participates in a cascade path, we recreate
	// the table with schema-level ON DELETE CASCADE on its issue/work_session
	// FK columns. SQLite has no ADD CONSTRAINT so the full table-recreate
	// dance is required. We take care to preserve all columns.
	//
	// Note: internal/sync/events.go (td-0001eb) still performs a manual
	// cascade for boards -> board_issue_positions. That remains correct;
	// schema cascade is an additive safety net. Remove manual path in
	// td-0001eb.

	rewrites := []struct {
		name string
		fn   func(*sql.Tx) error
	}{
		{"issues", rewriteIssuesTable},
		{"handoffs", rewriteHandoffsTable},
		{"git_snapshots", rewriteGitSnapshotsTable},
		{"issue_files", rewriteIssueFilesTable},
		{"issue_dependencies", rewriteIssueDependenciesTable},
		{"work_session_issues", rewriteWorkSessionIssuesTable},
		{"comments", rewriteCommentsTable},
		{"issue_session_history", rewriteIssueSessionHistoryTable},
		{"board_issue_positions", rewriteBoardIssuePositionsTable},
	}
	for _, r := range rewrites {
		ok, err := db.tableExistsTx(tx, r.name)
		if err != nil {
			return fmt.Errorf("check %s: %w", r.name, err)
		}
		if !ok {
			continue
		}
		if err := r.fn(tx); err != nil {
			return fmt.Errorf("rewrite %s: %w", r.name, err)
		}
	}

	// ------- Step 3: sanity check -------
	// PRAGMA foreign_key_check must return zero rows before we commit.
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	var violations int
	for rows.Next() {
		violations++
	}
	rows.Close()
	if violations > 0 {
		return fmt.Errorf("foreign_key_check found %d violations after cleanup; aborting migration", violations)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Printf("[migration 30] fk-cleanup: parent_id_nullified=%d handoffs_deleted=%d git_snapshots_deleted=%d issue_dependencies_deleted=%d issue_session_history_deleted=%d",
		nParentNullified, nHandoffs, nGitSnaps, nDeps, nSessHist)

	return nil
}

// ---- per-table rewrites ----
//
// Each rewrite: create *_fknew table with the same columns as the live table
// plus FK constraints; copy all rows; drop old; rename; recreate indexes.
// Column lists MUST match the current live schema (base schema + all prior
// migrations) — verify each by grepping schema.go / migrations.go before
// editing.

func rewriteIssuesTable(tx *sql.Tx) error {
	// Live columns (base schema + migrations 4, 5, 6, 10, 29):
	// id, title, description, status, type, priority, points, labels,
	// parent_id, acceptance, implementer_session, reviewer_session,
	// created_at, updated_at, closed_at, deleted_at, minor, created_branch,
	// creator_session, sprint, defer_until, due_date, defer_count
	const ddl = `CREATE TABLE issues_fknew (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		type TEXT NOT NULL DEFAULT 'task',
		priority TEXT NOT NULL DEFAULT 'P2',
		points INTEGER DEFAULT 0,
		labels TEXT DEFAULT '',
		parent_id TEXT DEFAULT '',
		acceptance TEXT DEFAULT '',
		implementer_session TEXT DEFAULT '',
		reviewer_session TEXT DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		closed_at DATETIME,
		deleted_at DATETIME,
		minor INTEGER DEFAULT 0,
		created_branch TEXT DEFAULT '',
		creator_session TEXT DEFAULT '',
		sprint TEXT DEFAULT '',
		defer_until TEXT,
		due_date TEXT,
		defer_count INTEGER DEFAULT 0
		-- NOTE: no FOREIGN KEY on parent_id; see migrateEnableFKEnforcement
		-- doc comment for rationale. '' is the "no parent" sentinel in td
		-- and SQLite would treat '' as a real FK value.
	)`
	if _, err := tx.Exec(`DROP TABLE IF EXISTS issues_fknew`); err != nil {
		return err
	}
	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO issues_fknew SELECT
		id, title, description, status, type, priority, points, labels,
		parent_id, acceptance, implementer_session, reviewer_session,
		created_at, updated_at, closed_at, deleted_at, minor, created_branch,
		creator_session, sprint, defer_until, due_date, defer_count
		FROM issues`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE issues`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE issues_fknew RENAME TO issues`); err != nil {
		return err
	}
	// Recreate indexes
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
		CREATE INDEX IF NOT EXISTS idx_issues_priority ON issues(priority);
		CREATE INDEX IF NOT EXISTS idx_issues_type ON issues(type);
		CREATE INDEX IF NOT EXISTS idx_issues_parent ON issues(parent_id);
		CREATE INDEX IF NOT EXISTS idx_issues_deleted ON issues(deleted_at);
		CREATE INDEX IF NOT EXISTS idx_issues_deleted_status ON issues(deleted_at, status);
		CREATE INDEX IF NOT EXISTS idx_issues_defer_until ON issues(defer_until);
		CREATE INDEX IF NOT EXISTS idx_issues_due_date ON issues(due_date);
	`)
	return err
}

func rewriteHandoffsTable(tx *sql.Tx) error {
	// Columns after migration 15 (text-id rewrite).
	const ddl = `CREATE TABLE handoffs_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		done TEXT DEFAULT '[]',
		remaining TEXT DEFAULT '[]',
		decisions TEXT DEFAULT '[]',
		uncertain TEXT DEFAULT '[]',
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "handoffs", "handoffs_fknew", ddl,
		[]string{"id", "issue_id", "session_id", "done", "remaining", "decisions", "uncertain", "timestamp"},
		`CREATE INDEX IF NOT EXISTS idx_handoffs_issue ON handoffs(issue_id);
		 CREATE INDEX IF NOT EXISTS idx_handoffs_timestamp ON handoffs(timestamp);`)
}

func rewriteGitSnapshotsTable(tx *sql.Tx) error {
	const ddl = `CREATE TABLE git_snapshots_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		event TEXT NOT NULL,
		commit_sha TEXT NOT NULL,
		branch TEXT NOT NULL,
		dirty_files INTEGER DEFAULT 0,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "git_snapshots", "git_snapshots_fknew", ddl,
		[]string{"id", "issue_id", "event", "commit_sha", "branch", "dirty_files", "timestamp"},
		`CREATE INDEX IF NOT EXISTS idx_git_snapshots_issue ON git_snapshots(issue_id);`)
}

func rewriteIssueFilesTable(tx *sql.Tx) error {
	const ddl = `CREATE TABLE issue_files_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'implementation',
		linked_sha TEXT DEFAULT '',
		linked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(issue_id, file_path),
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "issue_files", "issue_files_fknew", ddl,
		[]string{"id", "issue_id", "file_path", "role", "linked_sha", "linked_at"},
		`CREATE INDEX IF NOT EXISTS idx_issue_files_issue ON issue_files(issue_id);`)
}

func rewriteIssueDependenciesTable(tx *sql.Tx) error {
	// Migration 18 dropped the FKs when it added the id column. Restore
	// them now with ON DELETE CASCADE on both sides.
	const ddl = `CREATE TABLE issue_dependencies_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		depends_on_id TEXT NOT NULL,
		relation_type TEXT NOT NULL DEFAULT 'depends_on',
		UNIQUE(issue_id, depends_on_id, relation_type),
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
		FOREIGN KEY (depends_on_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "issue_dependencies", "issue_dependencies_fknew", ddl,
		[]string{"id", "issue_id", "depends_on_id", "relation_type"},
		"")
}

func rewriteWorkSessionIssuesTable(tx *sql.Tx) error {
	const ddl = `CREATE TABLE work_session_issues_fknew (
		id TEXT PRIMARY KEY,
		work_session_id TEXT NOT NULL,
		issue_id TEXT NOT NULL,
		tagged_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(work_session_id, issue_id),
		FOREIGN KEY (work_session_id) REFERENCES work_sessions(id) ON DELETE CASCADE,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "work_session_issues", "work_session_issues_fknew", ddl,
		[]string{"id", "work_session_id", "issue_id", "tagged_at"},
		"")
}

func rewriteCommentsTable(tx *sql.Tx) error {
	const ddl = `CREATE TABLE comments_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		text TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "comments", "comments_fknew", ddl,
		[]string{"id", "issue_id", "session_id", "text", "created_at"},
		`CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id);
		 CREATE INDEX IF NOT EXISTS idx_comments_created_at ON comments(created_at);`)
}

func rewriteIssueSessionHistoryTable(tx *sql.Tx) error {
	const ddl = `CREATE TABLE issue_session_history_fknew (
		id TEXT PRIMARY KEY,
		issue_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		action TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	return recreateTable(tx, "issue_session_history", "issue_session_history_fknew", ddl,
		[]string{"id", "issue_id", "session_id", "action", "created_at"},
		`CREATE INDEX IF NOT EXISTS idx_ish_issue ON issue_session_history(issue_id);
		 CREATE INDEX IF NOT EXISTS idx_ish_session ON issue_session_history(session_id);`)
}

func rewriteBoardIssuePositionsTable(tx *sql.Tx) error {
	// Migration 18's board_issue_positions_new DROPPED the FK constraints.
	// Migration 25 added deleted_at. Restore cascade FKs here.
	const ddl = `CREATE TABLE board_issue_positions_fknew (
		id TEXT PRIMARY KEY,
		board_id TEXT NOT NULL,
		issue_id TEXT NOT NULL,
		position INTEGER NOT NULL,
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		deleted_at DATETIME,
		UNIQUE(board_id, issue_id),
		FOREIGN KEY (board_id) REFERENCES boards(id) ON DELETE CASCADE,
		FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
	)`
	// deleted_at may or may not exist depending on migration ordering; we
	// guard by checking columns via PRAGMA and building the SELECT list
	// accordingly.
	hasDeleted, err := columnExistsTx(tx, "board_issue_positions", "deleted_at")
	if err != nil {
		return err
	}
	cols := []string{"id", "board_id", "issue_id", "position", "added_at"}
	if hasDeleted {
		cols = append(cols, "deleted_at")
	}
	if err := recreateTable(tx, "board_issue_positions", "board_issue_positions_fknew", ddl, cols, ""); err != nil {
		return err
	}
	// Note: migration 22 intentionally dropped the UNIQUE index on
	// (board_id, position) to support sparse positioning — don't recreate it.
	return nil
}

// recreateTable is a small helper for the pattern: create newTable with ddl,
// copy rows from oldTable selecting cols, drop oldTable, rename newTable to
// oldTable, then execute extraSQL for indexes.
func recreateTable(tx *sql.Tx, oldTable, newTable, ddl string, cols []string, extraSQL string) error {
	if _, err := tx.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, newTable)); err != nil {
		return err
	}
	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	colList := joinCols(cols)
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", newTable, colList, colList, oldTable)); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %s", oldTable)); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s RENAME TO %s", newTable, oldTable)); err != nil {
		return err
	}
	if extraSQL != "" {
		if _, err := tx.Exec(extraSQL); err != nil {
			return err
		}
	}
	return nil
}

func joinCols(cols []string) string {
	out := cols[0]
	for _, c := range cols[1:] {
		out += ", " + c
	}
	return out
}

// columnExistsTx is a package-scoped helper for checking columns inside a tx.
// Mirrors the (db *DB) method but doesn't need a DB receiver.
func columnExistsTx(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
