package db

import (
	"database/sql"
	"testing"
	"time"
)

// TestSchemaVersion_At32 confirms the current schema version is 32 and that
// a freshly initialized database reports that version after migrations run.
func TestSchemaVersion_At32(t *testing.T) {
	if SchemaVersion != 32 {
		t.Fatalf("SchemaVersion: want 32, got %d", SchemaVersion)
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
