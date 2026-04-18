package db

import (
	"database/sql"
	"fmt"
)

// OrphanCount reports the number of rows in a child table whose foreign-key
// column points at a non-existent row in the parent table.
//
// Count = number of child rows where child.col NOT NULL / NOT '' AND no
// matching parent row exists. We treat empty-string and NULL as "no link"
// to match td's mixed-sentinel convention (many FK columns default to '').
type OrphanCount struct {
	Relation     string // e.g. "handoffs.issue_id -> issues.id"
	ChildTable   string
	ChildColumn  string
	ParentTable  string
	ParentColumn string
	Count        int
}

// fkRelations enumerates every foreign-key relation declared in
// internal/db/schema.go (base schema + migrations). Keep this list in sync
// with schema.go; if a new FK is added there, append it here.
//
// Covered:
//   - issues.parent_id              -> issues.id
//   - handoffs.issue_id             -> issues.id
//   - git_snapshots.issue_id        -> issues.id
//   - issue_files.issue_id          -> issues.id
//   - issue_dependencies.issue_id   -> issues.id
//   - issue_dependencies.depends_on_id -> issues.id
//   - work_session_issues.work_session_id -> work_sessions.id
//   - work_session_issues.issue_id  -> issues.id
//   - comments.issue_id             -> issues.id
//   - issue_session_history.issue_id -> issues.id   (added in migration v7)
//   - board_issue_positions.board_id -> boards.id   (ON DELETE CASCADE, migration v9/v10)
//   - board_issue_positions.issue_id -> issues.id   (ON DELETE CASCADE, migration v9/v10)
var fkRelations = []struct {
	childTable   string
	childColumn  string
	parentTable  string
	parentColumn string
}{
	{"issues", "parent_id", "issues", "id"},
	{"handoffs", "issue_id", "issues", "id"},
	{"git_snapshots", "issue_id", "issues", "id"},
	{"issue_files", "issue_id", "issues", "id"},
	{"issue_dependencies", "issue_id", "issues", "id"},
	{"issue_dependencies", "depends_on_id", "issues", "id"},
	{"work_session_issues", "work_session_id", "work_sessions", "id"},
	{"work_session_issues", "issue_id", "issues", "id"},
	{"comments", "issue_id", "issues", "id"},
	{"issue_session_history", "issue_id", "issues", "id"},
	{"board_issue_positions", "board_id", "boards", "id"},
	{"board_issue_positions", "issue_id", "issues", "id"},
}

// AuditForeignKeys performs a read-only audit: for every FK relation declared
// in schema.go it runs a SELECT COUNT(*) that finds child rows whose column
// value is non-empty but has no matching parent row. It never mutates data.
//
// Tables that do not exist (e.g. on older DBs missing a migration) are
// skipped silently — their entry is omitted from the returned slice.
//
// This function is independent of CLI ID semantics: it works on any td-shaped
// SQLite DB and takes a raw *sql.DB so callers can point it at either the
// CLI DB or a test DB.
func AuditForeignKeys(conn *sql.DB) ([]OrphanCount, error) {
	if conn == nil {
		return nil, fmt.Errorf("AuditForeignKeys: nil db")
	}

	out := make([]OrphanCount, 0, len(fkRelations))
	for _, r := range fkRelations {
		if ok, err := tableExists(conn, r.childTable); err != nil {
			return nil, fmt.Errorf("check table %s: %w", r.childTable, err)
		} else if !ok {
			continue
		}
		if ok, err := tableExists(conn, r.parentTable); err != nil {
			return nil, fmt.Errorf("check table %s: %w", r.parentTable, err)
		} else if !ok {
			continue
		}

		// Orphan = child row whose FK column is set (non-NULL, non-empty) AND
		// no parent row has that id. LEFT JOIN + IS NULL is more portable than
		// NOT EXISTS on older SQLite builds.
		query := fmt.Sprintf(
			`SELECT COUNT(*) FROM %q c
			 LEFT JOIN %q p ON c.%q = p.%q
			 WHERE c.%q IS NOT NULL AND c.%q <> '' AND p.%q IS NULL`,
			r.childTable, r.parentTable,
			r.childColumn, r.parentColumn,
			r.childColumn, r.childColumn, r.parentColumn,
		)

		var count int
		if err := conn.QueryRow(query).Scan(&count); err != nil {
			return nil, fmt.Errorf("audit %s.%s -> %s.%s: %w",
				r.childTable, r.childColumn, r.parentTable, r.parentColumn, err)
		}

		out = append(out, OrphanCount{
			Relation: fmt.Sprintf("%s.%s -> %s.%s",
				r.childTable, r.childColumn, r.parentTable, r.parentColumn),
			ChildTable:   r.childTable,
			ChildColumn:  r.childColumn,
			ParentTable:  r.parentTable,
			ParentColumn: r.parentColumn,
			Count:        count,
		})
	}

	return out, nil
}

// tableExists returns true if a table with the given name exists in the
// current DB. Case-sensitive — matches SQLite sqlite_master semantics.
func tableExists(conn *sql.DB, name string) (bool, error) {
	var n int
	err := conn.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
