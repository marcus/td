package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const backfillTestSchema = `
CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
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
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME,
    deleted_at DATETIME,
    minor INTEGER DEFAULT 0,
    created_branch TEXT DEFAULT '',
    creator_session TEXT DEFAULT '',
    sprint TEXT DEFAULT ''
);
CREATE TABLE comments (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE logs (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    entry TEXT NOT NULL DEFAULT '',
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_progress INTEGER DEFAULT 0,
    category TEXT DEFAULT ''
);
CREATE TABLE action_log (
    id TEXT PRIMARY KEY,
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
);
CREATE TABLE sync_state (
    project_id TEXT PRIMARY KEY,
    last_pushed_action_id INTEGER DEFAULT 0,
    last_pulled_server_seq INTEGER DEFAULT 0,
    last_sync_at DATETIME,
    sync_disabled INTEGER DEFAULT 0
);
INSERT INTO sync_state (project_id) VALUES ('test-project');
`

func setupBackfillDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(backfillTestSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBackfillOrphanEntities_DetectsOrphans(t *testing.T) {
	db := setupBackfillDB(t)

	// Insert 3 issues directly — no action_log entries
	for _, id := range []string{"td-001", "td-002", "td-003"} {
		_, err := db.Exec(`INSERT INTO issues (id, title, status) VALUES (?, ?, 'open')`, id, "Issue "+id)
		if err != nil {
			t.Fatalf("insert issue: %v", err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 backfilled, got %d", n)
	}

	// Verify action_log entries
	var count int
	tx.QueryRow(`SELECT COUNT(*) FROM action_log WHERE action_type='create' AND entity_type='issue'`).Scan(&count)
	if count != 3 {
		t.Fatalf("expected 3 action_log rows, got %d", count)
	}

	// Verify the new_data contains valid JSON with issue fields
	rows, _ := tx.Query(`SELECT entity_id, new_data FROM action_log WHERE entity_type='issue'`)
	defer rows.Close()
	for rows.Next() {
		var eid, nd string
		rows.Scan(&eid, &nd)
		var fields map[string]any
		if err := json.Unmarshal([]byte(nd), &fields); err != nil {
			t.Fatalf("invalid JSON for %s: %v", eid, err)
		}
		if fields["id"] == nil || fields["title"] == nil || fields["status"] == nil {
			t.Fatalf("missing fields in new_data for %s: %v", eid, fields)
		}
	}
}

func TestBackfillOrphanEntities_Idempotent(t *testing.T) {
	db := setupBackfillDB(t)

	_, err := db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-100', 'Test', 'open')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-101', 'Test2', 'open')`)
	if err != nil {
		t.Fatal(err)
	}

	// First backfill
	tx, _ := db.Begin()
	n1, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n1 != 2 {
		t.Fatalf("first backfill: expected 2, got %d", n1)
	}

	// Second backfill — should be no-op
	tx, _ = db.Begin()
	n2, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n2 != 0 {
		t.Fatalf("second backfill: expected 0, got %d", n2)
	}

	// Verify still only 2 entries total
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_type='issue'`).Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 total action_log rows, got %d", count)
	}
}

func TestBackfillOrphanEntities_SkipsEntitiesWithEvents(t *testing.T) {
	db := setupBackfillDB(t)

	// Issue WITH an action_log entry
	_, _ = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-200', 'Has event', 'open')`)
	_, _ = db.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
		VALUES ('al-exist', 'ses-old', 'create', 'issue', 'td-200', '{"id":"td-200"}', datetime('now'), 0)`)

	// Issue WITHOUT an action_log entry
	_, _ = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-201', 'Orphan', 'open')`)

	tx, _ := db.Begin()
	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 1 {
		t.Fatalf("expected 1 backfilled (the orphan), got %d", n)
	}

	// Verify td-200 still has exactly 1 action_log entry (not duplicated)
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_id='td-200'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 entry for td-200, got %d", count)
	}
}

func TestBackfillOrphanEntities_BackfillsWhenOnlyUpdateExists(t *testing.T) {
	db := setupBackfillDB(t)

	// Issue WITH only an update action_log entry (no create)
	_, _ = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-210', 'Updated only', 'open')`)
	_, _ = db.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
		VALUES ('al-upd', 'ses-old', 'update', 'issue', 'td-210', '{"id":"td-210","title":"Updated only"}', datetime('now'), 0)`)

	tx, _ := db.Begin()
	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 1 {
		t.Fatalf("expected 1 backfilled (missing create), got %d", n)
	}

	// Verify a create action_log entry was added
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_id='td-210' AND action_type='create'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 create entry for td-210, got %d", count)
	}
}

func TestBackfillStaleIssues_AddsUpdate(t *testing.T) {
	db := setupBackfillDB(t)

	base := time.Now().UTC()
	updatedAt := base.Add(2 * time.Second).Format("2006-01-02 15:04:05")
	_, _ = db.Exec(`INSERT INTO issues (id, title, status, updated_at) VALUES ('td-700', 'Stale', 'closed', ?)`, updatedAt)
	_, _ = db.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
		VALUES ('al-create', 'ses-old', 'create', 'issue', 'td-700', '{"id":"td-700"}', ?, 0)`, base.Format("2006-01-02 15:04:05"))

	tx, _ := db.Begin()
	n, err := BackfillStaleIssues(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 1 {
		t.Fatalf("expected 1 stale backfill, got %d", n)
	}

	var count int
	db.QueryRow(`SELECT COUNT(*) FROM action_log WHERE entity_id='td-700' AND action_type='create'`).Scan(&count)
	if count != 2 {
		t.Fatalf("expected 2 create entries for td-700 (original + backfill), got %d", count)
	}
}

func TestBackfillStaleIssues_SkipsWhenUpToDate(t *testing.T) {
	db := setupBackfillDB(t)

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, _ = db.Exec(`INSERT INTO issues (id, title, status, updated_at) VALUES ('td-701', 'Fresh', 'open', ?)`, now)
	_, _ = db.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
		VALUES ('al-create2', 'ses-old', 'create', 'issue', 'td-701', '{"id":"td-701","status":"open"}', ?, 0)`, now)

	tx, _ := db.Begin()
	n, err := BackfillStaleIssues(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 0 {
		t.Fatalf("expected 0 stale updates, got %d", n)
	}
}

func TestBackfillStaleIssues_BackfillsInvalidJSON(t *testing.T) {
	db := setupBackfillDB(t)

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, _ = db.Exec(`INSERT INTO issues (id, title, status, updated_at) VALUES ('td-702', 'Bad JSON', 'open', ?)`, now)
	_, _ = db.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, timestamp, undone)
		VALUES ('al-badjson', 'ses-old', 'update', 'issue', 'td-702', 'not-json', ?, 0)`, now)

	tx, _ := db.Begin()
	n, err := BackfillStaleIssues(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 1 {
		t.Fatalf("expected 1 stale update for invalid JSON, got %d", n)
	}
}

func TestBackfillOrphanEntities_MultipleEntityTypes(t *testing.T) {
	db := setupBackfillDB(t)

	_, _ = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-300', 'Issue', 'open')`)
	_, _ = db.Exec(`INSERT INTO comments (id, issue_id, session_id, body) VALUES ('cm-300', 'td-300', 'ses-x', 'A comment')`)
	_, _ = db.Exec(`INSERT INTO logs (id, issue_id, session_id, entry) VALUES ('lg-300', 'td-300', 'ses-x', 'A log')`)

	tx, _ := db.Begin()
	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 3 {
		t.Fatalf("expected 3 backfilled, got %d", n)
	}

	// Check entity types
	types := map[string]int{}
	rows, _ := db.Query(`SELECT entity_type FROM action_log WHERE session_id='ses-test'`)
	defer rows.Close()
	for rows.Next() {
		var et string
		rows.Scan(&et)
		types[et]++
	}
	if types["issue"] != 1 {
		t.Errorf("expected 1 issue backfill, got %d", types["issue"])
	}
	if types["comments"] != 1 {
		t.Errorf("expected 1 comments backfill, got %d", types["comments"])
	}
	if types["logs"] != 1 {
		t.Errorf("expected 1 logs backfill, got %d", types["logs"])
	}
}

func TestBackfillOrphanEntities_FullRoundTrip(t *testing.T) {
	db := setupBackfillDB(t)

	_, _ = db.Exec(`INSERT INTO issues (id, title, status, priority) VALUES ('td-400', 'Roundtrip', 'open', 'P1')`)

	tx, _ := db.Begin()
	events, err := GetPendingEvents(tx, "dev-test", "ses-test")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	tx.Rollback()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.ActionType != "create" {
		t.Errorf("expected action_type 'create', got %q", ev.ActionType)
	}
	if ev.EntityType != "issues" { // normalizeEntityType maps "issue" → "issues"
		t.Errorf("expected entity_type 'issues', got %q", ev.EntityType)
	}
	if ev.EntityID != "td-400" {
		t.Errorf("expected entity_id 'td-400', got %q", ev.EntityID)
	}

	// Verify payload structure (should be wrapped with schema_version/new_data/previous_data)
	var wrapper struct {
		SchemaVersion int             `json:"schema_version"`
		NewData       json.RawMessage `json:"new_data"`
		PreviousData  json.RawMessage `json:"previous_data"`
	}
	if err := json.Unmarshal(ev.Payload, &wrapper); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if wrapper.SchemaVersion != 1 {
		t.Errorf("expected schema_version 1, got %d", wrapper.SchemaVersion)
	}

	var fields map[string]any
	if err := json.Unmarshal(wrapper.NewData, &fields); err != nil {
		t.Fatalf("unmarshal new_data: %v", err)
	}
	if fmt.Sprint(fields["title"]) != "Roundtrip" {
		t.Errorf("expected title 'Roundtrip', got %v", fields["title"])
	}
}

func TestBackfillOrphanEntities_IncludesSoftDeleted(t *testing.T) {
	db := setupBackfillDB(t)

	now := time.Now().Format("2006-01-02 15:04:05")
	_, _ = db.Exec(`INSERT INTO issues (id, title, status, deleted_at) VALUES ('td-500', 'Deleted', 'closed', ?)`, now)

	tx, _ := db.Begin()
	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 1 {
		t.Fatalf("expected 1 backfilled (soft-deleted), got %d", n)
	}

	// Verify the new_data includes deleted_at
	var nd string
	db.QueryRow(`SELECT new_data FROM action_log WHERE entity_id='td-500'`).Scan(&nd)
	var fields map[string]any
	json.Unmarshal([]byte(nd), &fields)
	if fields["deleted_at"] == nil {
		t.Error("expected deleted_at in new_data for soft-deleted entity")
	}
}

func TestBackfillOrphanEntities_SkipsAfterPull(t *testing.T) {
	db := setupBackfillDB(t)

	// Insert orphan issue
	_, _ = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('td-600', 'Orphan', 'open')`)

	// Simulate that a pull has already happened
	_, _ = db.Exec(`UPDATE sync_state SET last_pulled_server_seq = 42`)

	tx, _ := db.Begin()
	n, err := BackfillOrphanEntities(tx, "ses-test")
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	if n != 0 {
		t.Fatalf("expected 0 backfilled after pull, got %d", n)
	}

	// Verify no action_log entries were created
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM action_log`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 action_log rows, got %d", count)
	}
}
