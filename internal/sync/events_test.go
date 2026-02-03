package sync

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const testSchema = `CREATE TABLE issues (
	id         TEXT PRIMARY KEY,
	title      TEXT,
	status     TEXT,
	priority   TEXT,
	labels     TEXT DEFAULT '',
	created_at DATETIME,
	updated_at DATETIME,
	deleted_at DATETIME
);
CREATE TABLE handoffs (
	id          TEXT PRIMARY KEY,
	issue_id    TEXT,
	done        TEXT DEFAULT '[]',
	remaining   TEXT DEFAULT '[]',
	decisions   TEXT DEFAULT '[]',
	uncertain   TEXT DEFAULT '[]',
	created_at  DATETIME,
	deleted_at  DATETIME
);`

var testValidator EntityValidator = func(t string) bool { return t == "issues" || t == "handoffs" }

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(testSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func beginTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return tx
}

func TestUpsertEntity_Create(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	payload, _ := json.Marshal(map[string]any{
		"title":  "first issue",
		"status": "open",
	})
	_, err := upsertEntity(tx, "issues", "i1", payload)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tx.Commit()

	var title, status string
	err = db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i1").Scan(&title, &status)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if title != "first issue" || status != "open" {
		t.Fatalf("got title=%q status=%q", title, status)
	}
}

func TestUpsertEntity_Update(t *testing.T) {
	db := setupDB(t)

	// Insert
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "old", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p1); err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	// Upsert with new title
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "new", "status": "closed"})
	if _, err := upsertEntity(tx, "issues", "i1", p2); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tx.Commit()

	var title, status string
	db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i1").Scan(&title, &status)
	if title != "new" || status != "closed" {
		t.Fatalf("got title=%q status=%q", title, status)
	}
}

func TestUpsertExistingEntity(t *testing.T) {
	db := setupDB(t)

	// Create with title+status
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "orig", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p1); err != nil {
		t.Fatalf("create: %v", err)
	}
	tx.Commit()

	// Upsert with completely different data
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "replaced", "priority": "high"})
	if _, err := upsertEntity(tx, "issues", "i1", p2); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tx.Commit()

	var title string
	var priority sql.NullString
	var status sql.NullString
	db.QueryRow("SELECT title, status, priority FROM issues WHERE id = ?", "i1").Scan(&title, &status, &priority)
	if title != "replaced" {
		t.Fatalf("title should be replaced, got %q", title)
	}
	if priority.Valid && priority.String != "high" {
		t.Fatalf("priority should be high, got %q", priority.String)
	}
	// status should be NULL since the new payload didn't include it (INSERT OR REPLACE replaces full row)
	if status.Valid {
		t.Fatalf("status should be NULL after full row replace, got %q", status.String)
	}
}

func TestPartialPayloadDropsColumns(t *testing.T) {
	db := setupDB(t)

	// Create with title+status+priority
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "full", "status": "open", "priority": "high"})
	if _, err := upsertEntity(tx, "issues", "i1", p1); err != nil {
		t.Fatalf("create: %v", err)
	}
	tx.Commit()

	// Upsert with only title
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "partial"})
	if _, err := upsertEntity(tx, "issues", "i1", p2); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tx.Commit()

	var title string
	var status, priority sql.NullString
	db.QueryRow("SELECT title, status, priority FROM issues WHERE id = ?", "i1").Scan(&title, &status, &priority)
	if title != "partial" {
		t.Fatalf("title should be partial, got %q", title)
	}
	if status.Valid {
		t.Fatalf("status should be NULL, got %q", status.String)
	}
	if priority.Valid {
		t.Fatalf("priority should be NULL, got %q", priority.String)
	}
}

func TestNilPayload(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    nil,
	}, testValidator)
	if err == nil {
		t.Fatal("expected error for nil payload")
	}
}

func TestEmptyEntityID(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "",
		Payload:    []byte(`{"title":"test"}`),
	}, testValidator)
	if err == nil {
		t.Fatal("expected error for empty entity ID")
	}
}

func TestMalformedJSON(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    []byte("not json"),
	}, testValidator)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestUpdateDoesNotRecreateAfterDelete(t *testing.T) {
	db := setupDB(t)

	// Create
	tx := beginTx(t, db)
	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    []byte(`{"title":"old","status":"open"}`),
	}, testValidator)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	tx.Commit()

	// Delete
	tx = beginTx(t, db)
	_, err = ApplyEvent(tx, Event{
		ActionType: "delete",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    []byte(`{}`),
	}, testValidator)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	tx.Commit()

	// Update after delete should be ignored
	tx = beginTx(t, db)
	_, err = ApplyEvent(tx, Event{
		ActionType: "update",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    []byte(`{"title":"resurrected","status":"closed"}`),
	}, testValidator)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	tx.Commit()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM issues WHERE id = ?", "i1").Scan(&count)
	if count != 0 {
		t.Fatalf("expected issue to remain deleted, got count=%d", count)
	}
}

func TestColumnNameInjection_DroppedSilently(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Injection column name is not a valid table column, so it gets silently dropped.
	// With no known fields remaining, the upsert returns an error — no injection occurs.
	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    []byte(`{"bad; DROP TABLE issues": "hacked"}`),
	}, testValidator)
	if err == nil {
		t.Fatal("expected error when all fields are unknown (injection fields dropped)")
	}

	// Verify the table wasn't dropped
	var count int
	db.QueryRow("SELECT COUNT(*) FROM issues").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestDeleteEntity(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	p, _ := json.Marshal(map[string]any{"title": "bye"})
	_, _ = upsertEntity(tx, "issues", "i1", p)
	tx.Commit()

	tx = beginTx(t, db)
	if err := deleteEntity(tx, "issues", "i1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tx.Commit()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM issues WHERE id = ?", "i1").Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 rows, got %d", count)
	}
}

func TestDeleteEntity_Missing(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	if err := deleteEntity(tx, "issues", "nonexistent"); err != nil {
		t.Fatalf("delete missing should not error: %v", err)
	}
	tx.Commit()
}

func TestSoftDeleteEntity(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	p, _ := json.Marshal(map[string]any{"title": "soft"})
	_, _ = upsertEntity(tx, "issues", "i1", p)
	tx.Commit()

	now := time.Now().UTC()
	tx = beginTx(t, db)
	if err := softDeleteEntity(tx, "issues", "i1", now); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	tx.Commit()

	var deletedAt sql.NullTime
	db.QueryRow("SELECT deleted_at FROM issues WHERE id = ?", "i1").Scan(&deletedAt)
	if !deletedAt.Valid {
		t.Fatal("deleted_at should be set")
	}
}

func TestSoftDeleteEntity_Missing(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	if err := softDeleteEntity(tx, "issues", "nonexistent", time.Now()); err != nil {
		t.Fatalf("soft delete missing should not error: %v", err)
	}
	tx.Commit()
}

func TestApplyEvent_UnknownAction(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	_, err := ApplyEvent(tx, Event{
		ActionType: "bogus",
		EntityType: "issues",
		EntityID:   "i1",
	}, testValidator)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestApplyEvent_InvalidEntityType(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "users",
		EntityID:   "u1",
		Payload:    []byte(`{"name":"bad"}`),
	}, testValidator)
	if err == nil {
		t.Fatal("expected error for invalid entity type")
	}
}

func TestApplyEvent_Create(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	payload, _ := json.Marshal(map[string]any{"title": "via apply", "status": "open"})
	_, err := ApplyEvent(tx, Event{
		ActionType: "create",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    payload,
	}, testValidator)
	if err != nil {
		t.Fatalf("apply create: %v", err)
	}
	tx.Commit()

	var title string
	db.QueryRow("SELECT title FROM issues WHERE id = ?", "i1").Scan(&title)
	if title != "via apply" {
		t.Fatalf("got title=%q", title)
	}
}

func TestApplyEvent_Update(t *testing.T) {
	db := setupDB(t)

	// Create first
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "orig", "status": "open"})
	_, _ = ApplyEvent(tx, Event{ActionType: "create", EntityType: "issues", EntityID: "i1", Payload: p1}, testValidator)
	tx.Commit()

	// Update
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "updated", "status": "closed"})
	_, err := ApplyEvent(tx, Event{ActionType: "update", EntityType: "issues", EntityID: "i1", Payload: p2}, testValidator)
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	tx.Commit()

	var title, status string
	db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i1").Scan(&title, &status)
	if title != "updated" || status != "closed" {
		t.Fatalf("got title=%q status=%q", title, status)
	}
}

func TestUpsertEntity_OverwriteDetection(t *testing.T) {
	db := setupDB(t)

	// First insert should not be an overwrite
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "first"})
	res, err := upsertEntity(tx, "issues", "i1", p1)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if res.Overwritten {
		t.Fatal("first insert should not be an overwrite")
	}
	if res.OldData != nil {
		t.Fatal("first insert should have nil OldData")
	}
	tx.Commit()

	// Second insert to same ID should be an overwrite
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "second"})
	res, err = upsertEntity(tx, "issues", "i1", p2)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !res.Overwritten {
		t.Fatal("second insert to same ID should be an overwrite")
	}
	if res.OldData == nil {
		t.Fatal("overwrite should capture OldData")
	}
	// Verify OldData contains the previous title
	var old map[string]any
	if err := json.Unmarshal(res.OldData, &old); err != nil {
		t.Fatalf("unmarshal OldData: %v", err)
	}
	if old["title"] != "first" {
		t.Fatalf("OldData title=%v, want 'first'", old["title"])
	}
	tx.Commit()

	// Insert to different ID should not be an overwrite
	tx = beginTx(t, db)
	p3, _ := json.Marshal(map[string]any{"title": "other"})
	res, err = upsertEntity(tx, "issues", "i2", p3)
	if err != nil {
		t.Fatalf("insert i2: %v", err)
	}
	if res.Overwritten {
		t.Fatal("insert to new ID should not be an overwrite")
	}
	tx.Commit()
}

func TestApplyEvent_OverwriteTracking(t *testing.T) {
	db := setupDB(t)

	// Create
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "orig"})
	overwritten, err := ApplyEvent(tx, Event{ActionType: "create", EntityType: "issues", EntityID: "i1", Payload: p1}, testValidator)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if overwritten {
		t.Fatal("create should not report overwrite")
	}
	tx.Commit()

	// Update same entity
	tx = beginTx(t, db)
	p2, _ := json.Marshal(map[string]any{"title": "changed"})
	overwritten, err = ApplyEvent(tx, Event{ActionType: "update", EntityType: "issues", EntityID: "i1", Payload: p2}, testValidator)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !overwritten {
		t.Fatal("update to existing entity should report overwrite")
	}
	tx.Commit()
}

func TestUpsertEntity_LabelsArrayNormalized(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	// Payload with labels as JSON array (what sync payloads send)
	payload := []byte(`{"title":"labeled","labels":["bug","urgent"]}`)
	_, err := upsertEntity(tx, "issues", "i1", payload)
	if err != nil {
		t.Fatalf("upsert with labels array: %v", err)
	}
	tx.Commit()

	var labels string
	db.QueryRow("SELECT labels FROM issues WHERE id = ?", "i1").Scan(&labels)
	if labels != "bug,urgent" {
		t.Fatalf("labels: got %q, want 'bug,urgent'", labels)
	}
}

func TestUpsertEntity_HandoffArraysNormalized(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	payload := []byte(`{"issue_id":"i1","done":["task A"],"remaining":["task B","task C"],"decisions":[],"uncertain":["maybe"]}`)
	_, err := upsertEntity(tx, "handoffs", "h1", payload)
	if err != nil {
		t.Fatalf("upsert handoff with arrays: %v", err)
	}
	tx.Commit()

	var done, remaining, decisions, uncertain string
	db.QueryRow("SELECT done, remaining, decisions, uncertain FROM handoffs WHERE id = ?", "h1").
		Scan(&done, &remaining, &decisions, &uncertain)

	if done != `["task A"]` {
		t.Fatalf("done: got %q, want '[\"task A\"]'", done)
	}
	if remaining != `["task B","task C"]` {
		t.Fatalf("remaining: got %q", remaining)
	}
	if decisions != `[]` {
		t.Fatalf("decisions: got %q, want '[]'", decisions)
	}
	if uncertain != `["maybe"]` {
		t.Fatalf("uncertain: got %q", uncertain)
	}
}

func TestUpsertEntity_NestedObjectNormalized(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	// Test that a map value gets serialized to JSON string
	payload := []byte(`{"title":"with meta","priority":{"level":"high","score":5}}`)
	_, err := upsertEntity(tx, "issues", "i1", payload)
	if err != nil {
		t.Fatalf("upsert with nested object: %v", err)
	}
	tx.Commit()

	var priority string
	db.QueryRow("SELECT priority FROM issues WHERE id = ?", "i1").Scan(&priority)
	if priority != `{"level":"high","score":5}` {
		t.Fatalf("priority: got %q", priority)
	}
}

func TestGetTableColumns(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	cols, err := getTableColumns(tx, "issues")
	if err != nil {
		t.Fatalf("getTableColumns: %v", err)
	}

	expected := []string{"id", "title", "status", "priority", "labels", "created_at", "updated_at", "deleted_at"}
	for _, col := range expected {
		if !cols[col] {
			t.Errorf("expected column %q not found", col)
		}
	}
	if cols["nonexistent"] {
		t.Error("nonexistent column should not be in set")
	}
}

func TestUpsertEntity_UnknownFieldsIgnored(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)

	payload := []byte(`{"title":"Alien","status":"open","custom_xyz":"should be ignored","another_fake":"also ignored"}`)
	_, err := upsertEntity(tx, "issues", "i1", payload)
	if err != nil {
		t.Fatalf("upsert with unknown fields: %v", err)
	}
	tx.Commit()

	var title, status string
	err = db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i1").Scan(&title, &status)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if title != "Alien" || status != "open" {
		t.Fatalf("got title=%q status=%q, want Alien/open", title, status)
	}
}

func TestUpsertEntity_AllFieldsUnknown(t *testing.T) {
	db := setupDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	payload := []byte(`{"custom_xyz":"ignored","another_fake":"also ignored"}`)
	_, err := upsertEntity(tx, "issues", "i1", payload)
	if err == nil {
		t.Fatal("expected error when all fields are unknown")
	}
}

// depSchema adds the issue_dependencies table for cycle detection tests
const depSchema = `CREATE TABLE issue_dependencies (
	id           TEXT PRIMARY KEY,
	issue_id     TEXT NOT NULL,
	depends_on_id TEXT NOT NULL,
	relation_type TEXT NOT NULL DEFAULT 'depends_on',
	created_at   DATETIME
);`

// depValidator allows issue_dependencies entity type
var depValidator EntityValidator = func(t string) bool {
	return t == "issues" || t == "issue_dependencies"
}

func setupDepDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(testSchema + depSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestWouldCreateCycleTx_NoCycle(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Add A->B
	_, err := tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d1', 'A', 'B', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert A->B: %v", err)
	}

	// B->C should not create cycle
	if wouldCreateCycleTx(tx, "B", "C") {
		t.Fatal("B->C should not create cycle")
	}
}

func TestWouldCreateCycleTx_DirectCycle(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Add A->B
	_, err := tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d1', 'A', 'B', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert A->B: %v", err)
	}

	// B->A would create cycle
	if !wouldCreateCycleTx(tx, "B", "A") {
		t.Fatal("B->A should create cycle with existing A->B")
	}
}

func TestWouldCreateCycleTx_TransitiveCycle(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Add A->B, B->C
	_, err := tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d1', 'A', 'B', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert A->B: %v", err)
	}
	_, err = tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d2', 'B', 'C', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert B->C: %v", err)
	}

	// C->A would create cycle (C->A->B->C)
	if !wouldCreateCycleTx(tx, "C", "A") {
		t.Fatal("C->A should create transitive cycle")
	}
}

func TestCheckAndResolveCyclicDependency_NoConflict(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	event := Event{
		EntityType: "issue_dependencies",
		EntityID:   "d1",
		Payload:    []byte(`{"issue_id":"A","depends_on_id":"B","relation_type":"depends_on"}`),
	}

	if checkAndResolveCyclicDependency(tx, event) {
		t.Fatal("should not skip when no cycle exists")
	}
}

func TestCheckAndResolveCyclicDependency_SkipsLargerKey(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Add B->A first (larger key)
	_, err := tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d1', 'B', 'A', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert B->A: %v", err)
	}

	// Try to add A->B (smaller key) - should win, remove B->A
	event := Event{
		EntityType: "issue_dependencies",
		EntityID:   "d2",
		Payload:    []byte(`{"issue_id":"A","depends_on_id":"B","relation_type":"depends_on"}`),
	}

	// A|B < B|A, so incoming A->B wins and B->A is removed
	if checkAndResolveCyclicDependency(tx, event) {
		t.Fatal("A->B (smaller key) should win over B->A (larger key)")
	}

	// Verify B->A was removed
	var count int
	tx.QueryRow("SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='B' AND depends_on_id='A'").Scan(&count)
	if count != 0 {
		t.Fatalf("B->A should have been removed, got count=%d", count)
	}
}

func TestCheckAndResolveCyclicDependency_KeepsSmallerKey(t *testing.T) {
	db := setupDepDB(t)
	tx := beginTx(t, db)
	defer tx.Rollback()

	// Add A->B first (smaller key)
	_, err := tx.Exec(`INSERT INTO issue_dependencies (id, issue_id, depends_on_id, relation_type) VALUES ('d1', 'A', 'B', 'depends_on')`)
	if err != nil {
		t.Fatalf("insert A->B: %v", err)
	}

	// Try to add B->A (larger key) - should be skipped, A->B stays
	event := Event{
		EntityType: "issue_dependencies",
		EntityID:   "d2",
		Payload:    []byte(`{"issue_id":"B","depends_on_id":"A","relation_type":"depends_on"}`),
	}

	// B|A > A|B, so incoming B->A is skipped
	if !checkAndResolveCyclicDependency(tx, event) {
		t.Fatal("B->A (larger key) should be skipped in favor of existing A->B (smaller key)")
	}

	// Verify A->B still exists
	var count int
	tx.QueryRow("SELECT COUNT(*) FROM issue_dependencies WHERE issue_id='A' AND depends_on_id='B'").Scan(&count)
	if count != 1 {
		t.Fatalf("A->B should still exist, got count=%d", count)
	}
}
