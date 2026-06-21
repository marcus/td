package db

import (
	"database/sql"
	"testing"
	"time"
)

// TestSchemaVersion_At35 confirms the current schema version is 35 and that
// a freshly initialized database reports that version after migrations run.
func TestSchemaVersion_At35(t *testing.T) {
	if SchemaVersion != 35 {
		t.Fatalf("SchemaVersion: want 35, got %d", SchemaVersion)
	}

	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	v, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if v != SchemaVersion {
		t.Fatalf("schema_info version: want %d, got %d", SchemaVersion, v)
	}
}

// TestMigration31_IssueColumns_Present verifies the three new review-attestation
// columns exist on the issues table after migration 31 has run.
func TestMigration31_IssueColumns_Present(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	for _, col := range []string{"reviewed_at", "review_requested_by_session", "closed_by_session"} {
		exists, err := database.columnExists("issues", col)
		if err != nil {
			t.Fatalf("columnExists(%s): %v", col, err)
		}
		if !exists {
			t.Errorf("expected issues.%s to exist after migration 31", col)
		}
	}
}

// TestMigration31_IssueReviewsTable_Shape verifies the issue_reviews table
// exists with the expected columns.
func TestMigration31_IssueReviewsTable_Shape(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	exists, err := database.tableExists("issue_reviews")
	if err != nil {
		t.Fatalf("tableExists: %v", err)
	}
	if !exists {
		t.Fatal("expected issue_reviews table to exist after migration 31")
	}

	wantCols := []string{
		"id", "issue_id", "reviewer_session", "decision", "summary",
		"requested_by_session", "created_at", "superseded_at",
	}
	for _, col := range wantCols {
		got, err := database.columnExists("issue_reviews", col)
		if err != nil {
			t.Fatalf("columnExists(issue_reviews,%s): %v", col, err)
		}
		if !got {
			t.Errorf("issue_reviews is missing column %q", col)
		}
	}
}

func TestMigration34_WorktreeIdentityColumns_Present(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	for _, table := range []string{"sessions", "work_sessions"} {
		for _, col := range []string{"worktree_id", "worktree_root", "repo_root"} {
			exists, err := database.columnExists(table, col)
			if err != nil {
				t.Fatalf("columnExists(%s,%s): %v", table, col, err)
			}
			if !exists {
				t.Errorf("expected %s.%s to exist after migration 34", table, col)
			}
		}
	}
}

func TestMigration35_SessionStateTableShapeAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	if _, err := database.conn.Exec(`DROP TABLE session_state`); err != nil {
		t.Fatalf("drop session_state: %v", err)
	}
	if err := database.setSchemaVersionInternal(34); err != nil {
		t.Fatalf("rewind schema version: %v", err)
	}
	if n, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations first: %v", err)
	} else if n != 1 {
		t.Fatalf("RunMigrations first count: got %d want 1", n)
	}
	assertSessionStateTableShape(t, database)

	if err := database.setSchemaVersionInternal(34); err != nil {
		t.Fatalf("rewind schema version second: %v", err)
	}
	if n, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations second: %v", err)
	} else if n != 1 {
		t.Fatalf("RunMigrations second count: got %d want 1", n)
	}
	assertSessionStateTableShape(t, database)
}

func assertSessionStateTableShape(t *testing.T, database *DB) {
	t.Helper()
	exists, err := database.tableExists("session_state")
	if err != nil {
		t.Fatalf("tableExists(session_state): %v", err)
	}
	if !exists {
		t.Fatal("expected session_state table to exist")
	}

	type columnInfo struct {
		name       string
		columnType string
		notNull    int
		defaultVal sql.NullString
		pk         int
	}
	rows, err := database.conn.Query(`PRAGMA table_info(session_state)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(session_state): %v", err)
	}
	defer rows.Close()

	columns := map[string]columnInfo{}
	for rows.Next() {
		var cid int
		var c columnInfo
		if err := rows.Scan(&cid, &c.name, &c.columnType, &c.notNull, &c.defaultVal, &c.pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[c.name] = c
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	want := map[string]struct {
		columnType string
		pk         int
	}{
		"session_id":             {"TEXT", 1},
		"worktree_id":            {"TEXT", 2},
		"focused_issue_id":       {"TEXT", 0},
		"active_work_session_id": {"TEXT", 0},
		"updated_at":             {"DATETIME", 0},
	}
	for name, w := range want {
		got, ok := columns[name]
		if !ok {
			t.Errorf("session_state missing column %q", name)
			continue
		}
		if got.columnType != w.columnType {
			t.Errorf("%s type: got %q want %q", name, got.columnType, w.columnType)
		}
		if got.pk != w.pk {
			t.Errorf("%s pk position: got %d want %d", name, got.pk, w.pk)
		}
	}
	if columns["session_id"].notNull != 1 {
		t.Errorf("session_id should be NOT NULL")
	}
	if columns["updated_at"].notNull != 1 {
		t.Errorf("updated_at should be NOT NULL")
	}
	if !columns["updated_at"].defaultVal.Valid || columns["updated_at"].defaultVal.String != "CURRENT_TIMESTAMP" {
		t.Errorf("updated_at default: got %q want CURRENT_TIMESTAMP", columns["updated_at"].defaultVal.String)
	}
}

// TestMigration31_ClosedBySessionBackfill verifies the migration backfills
// closed_by_session = reviewer_session on historical closed rows without
// inventing a reviewed_at timestamp.
func TestMigration31_ClosedBySessionBackfill(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Simulate a schema-30-era row: reviewer_session set, closed status,
	// closed_by_session still empty, reviewed_at NULL. We rewind the schema
	// version to force migration 31 to run again via RunMigrations.
	now := time.Now()
	_, err = database.conn.Exec(`
		INSERT INTO issues (id, title, status, type, priority, reviewer_session, closed_by_session, created_at, updated_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "td-historic", "historic issue", "closed", "task", "P2", "ses-reviewer", "", now, now, now)
	if err != nil {
		t.Fatalf("insert historical row: %v", err)
	}

	// Also insert a row that should NOT be touched (no reviewer_session).
	_, err = database.conn.Exec(`
		INSERT INTO issues (id, title, status, type, priority, reviewer_session, closed_by_session, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "td-untouched", "open issue", "open", "task", "P2", "", "", now, now)
	if err != nil {
		t.Fatalf("insert untouched row: %v", err)
	}

	// Force re-run of migration 31 by rewinding schema_info version.
	if err := database.setSchemaVersionInternal(30); err != nil {
		t.Fatalf("rewind schema version: %v", err)
	}
	if _, err := database.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var closedBy, reviewer string
	var reviewedAt sql.NullTime
	err = database.conn.QueryRow(`SELECT reviewer_session, closed_by_session, reviewed_at FROM issues WHERE id = ?`, "td-historic").
		Scan(&reviewer, &closedBy, &reviewedAt)
	if err != nil {
		t.Fatalf("scan historic row: %v", err)
	}

	if closedBy != "ses-reviewer" {
		t.Errorf("closed_by_session: want ses-reviewer, got %q", closedBy)
	}
	if reviewer != "ses-reviewer" {
		t.Errorf("reviewer_session: want ses-reviewer, got %q", reviewer)
	}
	if reviewedAt.Valid {
		t.Errorf("reviewed_at should remain NULL on historical rows, got %v", reviewedAt.Time)
	}

	// Non-closed row should retain empty closed_by_session.
	var untouchedClosedBy string
	err = database.conn.QueryRow(`SELECT closed_by_session FROM issues WHERE id = ?`, "td-untouched").Scan(&untouchedClosedBy)
	if err != nil {
		t.Fatalf("scan untouched row: %v", err)
	}
	if untouchedClosedBy != "" {
		t.Errorf("untouched row closed_by_session: want empty, got %q", untouchedClosedBy)
	}
}
