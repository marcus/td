package db

import (
	"testing"
)

// TestSchemaCompat_DDLMatchesMigrations verifies that Initialize() (which runs
// the base DDL + migrations) produces a consistent schema — every table created
// by the base DDL still has all its columns after migrations run.
// This catches drift where a migration accidentally drops/renames a column.
func TestSchemaCompat_DDLMatchesMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer db.Close()

	columns := getAllColumns(t, db)

	// Verify tables from the base DDL have their expected columns
	baseDDLTables := map[string][]string{
		"issues": {
			"id", "title", "description", "status", "type", "priority",
			"points", "labels", "parent_id", "acceptance",
			"implementer_session", "reviewer_session",
			"created_at", "updated_at", "closed_at", "deleted_at",
			"minor", "created_branch",
		},
		"logs": {
			"id", "issue_id", "session_id", "work_session_id",
			"message", "type", "timestamp",
		},
		"handoffs": {
			"id", "issue_id", "session_id",
			"done", "remaining", "decisions", "uncertain", "timestamp",
		},
		"git_snapshots": {
			"id", "issue_id", "event", "commit_sha", "branch",
			"dirty_files", "timestamp",
		},
		"issue_files": {
			"id", "issue_id", "file_path", "role", "linked_sha", "linked_at",
		},
		"issue_dependencies": {
			"id", "issue_id", "depends_on_id", "relation_type",
		},
		"work_sessions": {
			"id", "name", "session_id", "started_at", "ended_at",
			"start_sha", "end_sha",
		},
		"work_session_issues": {
			"id", "work_session_id", "issue_id", "tagged_at",
		},
		"comments": {
			"id", "issue_id", "session_id", "text", "created_at",
		},
		"sessions": {
			"id", "name", "branch", "agent_type", "agent_pid",
			"context_id", "previous_session_id",
			"started_at", "ended_at", "last_activity",
		},
		"schema_info": {"key", "value"},
	}

	for table, expectedCols := range baseDDLTables {
		tableCols, ok := columns[table]
		if !ok {
			t.Errorf("table %q expected from base DDL but not found", table)
			continue
		}
		for _, col := range expectedCols {
			if !tableCols[col] {
				t.Errorf("column %s.%s expected from base DDL but not found after migrations", table, col)
			}
		}
	}

	// Verify tables added by migrations also exist with expected columns
	migrationTables := map[string][]string{
		"action_log":             {"id", "session_id", "action_type", "entity_type", "entity_id"},
		"boards":                 {"id", "name", "query", "is_builtin", "view_mode"},
		"board_issue_positions":  {"board_id", "issue_id", "position"},
		"issue_session_history":  {"id", "issue_id", "session_id", "action"},
		"sync_state":             {"project_id", "last_pushed_action_id", "last_pulled_server_seq"},
		"sync_conflicts":         {"id", "entity_type", "entity_id", "server_seq"},
		"sync_history":           {"id", "direction", "action_type", "entity_type", "entity_id"},
		"notes":                  {"id", "title", "content", "pinned", "archived"},
	}

	for table, expectedCols := range migrationTables {
		tableCols, ok := columns[table]
		if !ok {
			t.Errorf("table %q expected from migrations but not found", table)
			continue
		}
		for _, col := range expectedCols {
			if !tableCols[col] {
				t.Errorf("column %s.%s expected from migrations but not found", table, col)
			}
		}
	}
}

// TestSchemaCompat_AllTablesHavePK ensures no table is missing a primary key.
func TestSchemaCompat_AllTablesHavePK(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer db.Close()

	tables := getUserTables(t, db)
	for _, table := range tables {
		hasPK := false
		rows, err := db.conn.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Errorf("table_info(%s): %v", table, err)
			continue
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt interface{}
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Errorf("scan %s: %v", table, err)
				break
			}
			if pk > 0 {
				hasPK = true
			}
		}
		rows.Close()
		if !hasPK {
			t.Errorf("table %q has no primary key", table)
		}
	}
}

// TestSchemaCompat_SchemaVersionConstMatchesMigrations ensures the SchemaVersion
// constant matches the highest migration version.
func TestSchemaCompat_SchemaVersionConstMatchesMigrations(t *testing.T) {
	maxMigration := 0
	for _, m := range Migrations {
		if m.Version > maxMigration {
			maxMigration = m.Version
		}
	}
	if maxMigration != SchemaVersion {
		t.Errorf("SchemaVersion constant (%d) does not match highest migration version (%d)",
			SchemaVersion, maxMigration)
	}
}

// getAllColumns returns map[table]map[column]true for all user tables.
func getAllColumns(t *testing.T, db *DB) map[string]map[string]bool {
	t.Helper()
	result := make(map[string]map[string]bool)
	tables := getUserTables(t, db)
	for _, table := range tables {
		cols := make(map[string]bool)
		rows, err := db.conn.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt interface{}
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scan %s: %v", table, err)
			}
			cols[name] = true
		}
		rows.Close()
		result[table] = cols
	}
	return result
}

// getUserTables returns all user table names (excludes sqlite internal tables).
func getUserTables(t *testing.T, db *DB) []string {
	t.Helper()
	rows, err := db.conn.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	return tables
}
