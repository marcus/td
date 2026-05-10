package db

import (
	"os"
	"testing"
)

// TestMigrationChain_InitializeThenOpen verifies that a freshly initialized DB
// can be re-opened without error, and that all migrations are already applied.
func TestMigrationChain_InitializeThenOpen(t *testing.T) {
	dir := t.TempDir()
	db1, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	db1.Close()

	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after Initialize: %v", err)
	}
	defer db2.Close()

	ver, err := db2.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if ver != SchemaVersion {
		t.Fatalf("expected version %d, got %d", SchemaVersion, ver)
	}
}

// TestMigrationChain_Idempotent verifies that running migrations multiple times
// produces the same result.
func TestMigrationChain_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db1, err := Initialize(dir)
	if err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	db1.Close()

	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()

	n, err := db2.RunMigrations()
	if err != nil {
		t.Fatalf("repeat RunMigrations: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 migrations on re-run, got %d", n)
	}
}

// TestMigrationChain_VersionsContinuous verifies migration versions form
// a continuous sequence from 2 through SchemaVersion with no gaps or duplicates.
func TestMigrationChain_VersionsContinuous(t *testing.T) {
	seen := make(map[int]bool)
	for _, m := range Migrations {
		if seen[m.Version] {
			t.Errorf("duplicate migration version %d", m.Version)
		}
		seen[m.Version] = true
	}
	for v := 2; v <= SchemaVersion; v++ {
		if !seen[v] {
			t.Errorf("missing migration for version %d", v)
		}
	}
}

// TestMigrationChain_AllExpectedTablesExist verifies that after Initialize(),
// all expected tables are present in the database.
func TestMigrationChain_AllExpectedTablesExist(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer db.Close()

	expectedTables := []string{
		"issues", "logs", "handoffs", "git_snapshots",
		"issue_files", "issue_dependencies", "work_sessions",
		"work_session_issues", "comments", "sessions", "schema_info",
		"boards", "board_issue_positions", "notes",
		"action_log", "sync_state", "sync_conflicts", "sync_history",
		"issue_session_history",
	}

	for _, table := range expectedTables {
		exists, err := db.tableExists(table)
		if err != nil {
			t.Errorf("tableExists(%q): %v", table, err)
		}
		if !exists {
			t.Errorf("table %q missing after Initialize()", table)
		}
	}
}

// TestMigrationChain_AllExpectedColumnsExist verifies that key columns
// added across the migration chain exist after Initialize().
func TestMigrationChain_AllExpectedColumnsExist(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer db.Close()

	// Columns added by various migrations
	issueColumns := []string{
		"id", "title", "description", "status", "type", "priority",
		"points", "labels", "parent_id", "acceptance",
		"implementer_session", "reviewer_session",
		"created_at", "updated_at", "closed_at", "deleted_at",
		// Added by migrations:
		"minor",           // v4
		"created_branch",  // v5
		"creator_session", // v6
		"sprint",          // v10
		"defer_until",     // v29
		"due_date",        // v29
		"defer_count",     // v29
	}
	for _, col := range issueColumns {
		exists, err := db.columnExists("issues", col)
		if err != nil {
			t.Errorf("columnExists(issues, %q): %v", col, err)
		}
		if !exists {
			t.Errorf("column issues.%s missing", col)
		}
	}

	// Board columns
	boardColumns := []string{"id", "name", "query", "is_builtin", "view_mode"}
	for _, col := range boardColumns {
		exists, err := db.columnExists("boards", col)
		if err != nil {
			t.Errorf("columnExists(boards, %q): %v", col, err)
		}
		if !exists {
			t.Errorf("column boards.%s missing", col)
		}
	}

	// Notes columns (v28)
	noteColumns := []string{"id", "title", "content", "pinned", "archived", "deleted_at"}
	for _, col := range noteColumns {
		exists, err := db.columnExists("notes", col)
		if err != nil {
			t.Errorf("columnExists(notes, %q): %v", col, err)
		}
		if !exists {
			t.Errorf("column notes.%s missing", col)
		}
	}
}

// TestMigrationChain_FromIntermediateVersion verifies that a DB frozen at a
// specific intermediate schema version can be migrated forward by Open().
// We simulate this by creating a DB, rolling back its recorded version, and
// verifying that Open() re-applies the missing migrations.
func TestMigrationChain_FromIntermediateVersion(t *testing.T) {
	dir := t.TempDir()
	db1, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Set schema version back to an intermediate value.
	// The DDL tables already have all columns, so idempotent checks in
	// RunMigrations should handle skipping/re-applying gracefully.
	intermediateVersion := SchemaVersion - 1
	if err := db1.setSchemaVersionInternal(intermediateVersion); err != nil {
		t.Fatalf("set intermediate version: %v", err)
	}
	db1.Close()

	// Re-open — should run the remaining migration(s)
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open from version %d: %v", intermediateVersion, err)
	}
	defer db2.Close()

	ver, err := db2.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if ver != SchemaVersion {
		t.Fatalf("expected version %d after migration, got %d", SchemaVersion, ver)
	}
}

// TestMigrationChain_BuiltInBoardExists verifies the "All Issues" built-in board
// is created during initialization (migration v10).
func TestMigrationChain_BuiltInBoardExists(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer db.Close()

	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM boards WHERE id = 'bd-all-issues'").Scan(&count)
	if err != nil {
		t.Fatalf("query built-in board: %v", err)
	}
	if count == 0 {
		t.Error("built-in 'All Issues' board (bd-all-issues) not found after Initialize()")
	}
}

// setupV0Database creates a database with the original v0 schema (no migrations applied).
// Note: the real migration chain expects the base DDL to be applied first, so this
// is used only by the schema_compat_test for DDL-vs-migration drift checks on
// tables that exist in both the base DDL and the migration chain.
func setupV0Database(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	todosDir := dir + "/.todos"
	if err := os.MkdirAll(todosDir, 0755); err != nil {
		t.Fatalf("mkdir .todos: %v", err)
	}

	conn, err := openConn(todosDir + "/issues.db")
	if err != nil {
		t.Fatalf("open v0 db: %v", err)
	}

	// Use the current base DDL — this matches what Initialize() does
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		t.Fatalf("create base schema: %v", err)
	}

	// Don't set any schema version — starts at 0
	return &DB{conn: conn, baseDir: dir}
}
