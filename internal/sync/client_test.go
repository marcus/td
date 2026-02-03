package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// farPast is a time far in the past; when used as lastSyncAt, all local rows
// appear to be modified "after last sync", so conflicts are always recorded.
var farPast = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

const clientTestSchema = `
CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    title TEXT,
    status TEXT,
    priority TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
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
`

func setupClientDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(clientTestSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertActionLog(t *testing.T, db *sql.DB, id, sessionID, actionType, entityType, entityID, newData, prevData string, undone int, syncedAt string) {
	t.Helper()
	var syncedAtVal any
	if syncedAt != "" {
		syncedAtVal = syncedAt
	}
	_, err := db.Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, actionType, entityType, entityID, newData, prevData,
		time.Now().UTC().Format("2006-01-02 15:04:05"), undone, syncedAtVal,
	)
	if err != nil {
		t.Fatalf("insert action_log: %v", err)
	}
}

func TestGetPendingEvents_Basic(t *testing.T) {
	db := setupClientDB(t)

	insertActionLog(t, db, "al-00000001", "sess1", "create", "issues", "i1",
		`{"title":"First","status":"open"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000002", "sess1", "update", "issues", "i1",
		`{"title":"Updated","status":"open"}`, `{"title":"First","status":"open"}`, 0, "")
	insertActionLog(t, db, "al-00000003", "sess1", "delete", "issues", "i2",
		`{}`, `{"title":"Gone"}`, 0, "")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "device1", "sync-sess")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	// Verify first event
	ev := events[0]
	if ev.DeviceID != "device1" {
		t.Errorf("DeviceID: got %q, want device1", ev.DeviceID)
	}
	if ev.SessionID != "sync-sess" {
		t.Errorf("SessionID: got %q, want sync-sess", ev.SessionID)
	}
	if ev.ActionType != "create" {
		t.Errorf("ActionType: got %q, want create", ev.ActionType)
	}
	if ev.EntityType != "issues" {
		t.Errorf("EntityType: got %q, want issues", ev.EntityType)
	}
	if ev.EntityID != "i1" {
		t.Errorf("EntityID: got %q, want i1", ev.EntityID)
	}
	if ev.ServerSeq != 0 {
		t.Errorf("ServerSeq: got %d, want 0", ev.ServerSeq)
	}
	if ev.ClientActionID <= 0 {
		t.Errorf("ClientActionID should be positive rowid, got %d", ev.ClientActionID)
	}

	// Verify payload structure
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["schema_version"]; !ok {
		t.Error("payload missing schema_version")
	}
	if _, ok := payload["new_data"]; !ok {
		t.Error("payload missing new_data")
	}
	if _, ok := payload["previous_data"]; !ok {
		t.Error("payload missing previous_data")
	}

	// Verify ordering (rowids ascending)
	for i := 1; i < len(events); i++ {
		if events[i].ClientActionID <= events[i-1].ClientActionID {
			t.Errorf("events not ordered: [%d].rowid=%d <= [%d].rowid=%d",
				i, events[i].ClientActionID, i-1, events[i-1].ClientActionID)
		}
	}
}

func TestGetPendingEvents_SkipsUndone(t *testing.T) {
	db := setupClientDB(t)

	insertActionLog(t, db, "al-00000001", "sess1", "create", "issues", "i1",
		`{"title":"Keep"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000002", "sess1", "create", "issues", "i2",
		`{"title":"Undone"}`, `{}`, 1, "")
	insertActionLog(t, db, "al-00000003", "sess1", "update", "issues", "i1",
		`{"title":"Also keep"}`, `{"title":"Keep"}`, 0, "")

	tx, _ := db.Begin()
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "d1", "s1")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].EntityID != "i1" || events[1].EntityID != "i1" {
		t.Errorf("expected both events for i1, got %q and %q", events[0].EntityID, events[1].EntityID)
	}
}

func TestGetPendingEvents_SkipsSynced(t *testing.T) {
	db := setupClientDB(t)

	insertActionLog(t, db, "al-00000001", "sess1", "create", "issues", "i1",
		`{"title":"Synced"}`, `{}`, 0, "2025-01-01 00:00:00")
	insertActionLog(t, db, "al-00000002", "sess1", "create", "issues", "i2",
		`{"title":"Pending"}`, `{}`, 0, "")

	tx, _ := db.Begin()
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "d1", "s1")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].EntityID != "i2" {
		t.Errorf("expected entity i2, got %q", events[0].EntityID)
	}
}

func TestGetPendingEvents_ActionTypeMapping(t *testing.T) {
	db := setupClientDB(t)

	cases := []struct {
		id         string
		tdAction   string
		entityType string
		want       string
	}{
		{"al-00000001", "create", "issues", "create"},
		{"al-00000002", "update", "issues", "update"},
		{"al-00000003", "start", "issues", "update"},
		{"al-00000004", "delete", "issues", "soft_delete"},
		{"al-00000005", "review", "issues", "update"},
		{"al-00000006", "approve", "issues", "update"},
		{"al-00000007", "close", "issues", "update"},
		{"al-00000008", "reopen", "issues", "update"},
		{"al-00000009", "remove_dependency", "issue_dependencies", "delete"},
		{"al-00000010", "unlink_file", "issue_files", "delete"},
		{"al-00000011", "board_unposition", "board_position", "soft_delete"},
		{"al-00000012", "board_delete", "boards", "delete"},
		{"al-00000013", "board_set_position", "board_position", "create"},
		{"al-00000014", "board_remove_issue", "board_position", "soft_delete"},
		{"al-00000015", "add_dependency", "issue_dependencies", "create"},
		{"al-00000016", "link_file", "issue_files", "create"},
		{"al-00000017", "board_create", "boards", "create"},
		{"al-00000018", "board_add_issue", "board_position", "create"},
	}

	for _, tc := range cases {
		insertActionLog(t, db, tc.id, "sess1", tc.tdAction, tc.entityType, "i1",
			`{"title":"test"}`, `{}`, 0, "")
	}

	tx, _ := db.Begin()
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "d1", "s1")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	if len(events) != len(cases) {
		t.Fatalf("got %d events, want %d", len(events), len(cases))
	}

	for i, tc := range cases {
		if events[i].ActionType != tc.want {
			t.Errorf("action %q: got %q, want %q", tc.tdAction, events[i].ActionType, tc.want)
		}
	}
}

func TestGetPendingEvents_EntityTypeNormalization(t *testing.T) {
	db := setupClientDB(t)

	insertActionLog(t, db, "al-00000001", "sess1", "create", "issue", "i1",
		`{"title":"Normalized","status":"open"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000002", "sess1", "create", "issues", "i2",
		`{"title":"AlreadyCanonical","status":"open"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000003", "sess1", "create", "dependency", "dep1",
		`{"id":"dep1","issue_id":"i1","depends_on_id":"i2","relation_type":"depends_on"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000004", "sess1", "create", "board_position", "bip1",
		`{"id":"bip1","board_id":"bd-1","issue_id":"i1","position":1,"added_at":"2025-01-01T00:00:00Z"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000005", "sess1", "create", "file_link", "ifl1",
		`{"id":"ifl1","issue_id":"i1","file_path":"main.go","role":"implementation","linked_sha":"abc","linked_at":"2025-01-01T00:00:00Z"}`, `{}`, 0, "")
	// unsupported entity type should be skipped
	insertActionLog(t, db, "al-00000006", "sess1", "create", "unknown_entity", "x1",
		`{"foo":"bar"}`, `{}`, 0, "")

	tx, _ := db.Begin()
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "d1", "s1")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	if events[0].EntityType != "issues" {
		t.Errorf("entity type normalize: got %q, want issues", events[0].EntityType)
	}
	if events[1].EntityType != "issues" {
		t.Errorf("entity type canonical: got %q, want issues", events[1].EntityType)
	}
	if events[2].EntityType != "issue_dependencies" {
		t.Errorf("dependency normalize: got %q, want issue_dependencies", events[2].EntityType)
	}
	if events[3].EntityType != "board_issue_positions" {
		t.Errorf("board_position normalize: got %q, want board_issue_positions", events[3].EntityType)
	}
	if events[4].EntityType != "issue_files" {
		t.Errorf("file_link normalize: got %q, want issue_files", events[4].EntityType)
	}
}

func TestApplyRemoteEvents_Basic(t *testing.T) {
	db := setupClientDB(t)

	events := []Event{
		{
			ServerSeq:  1,
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "i1",
			Payload:    []byte(`{"schema_version":1,"new_data":{"title":"First","status":"open"},"previous_data":{}}`),
		},
		{
			ServerSeq:  2,
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "i2",
			Payload:    []byte(`{"schema_version":1,"new_data":{"title":"Second","status":"open"},"previous_data":{}}`),
		},
		{
			ServerSeq:  3,
			ActionType: "update",
			EntityType: "issues",
			EntityID:   "i1",
			Payload:    []byte(`{"schema_version":1,"new_data":{"title":"Updated First","status":"closed"},"previous_data":{"title":"First","status":"open"}}`),
		},
	}

	tx, _ := db.Begin()
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("ApplyRemoteEvents: %v", err)
	}
	tx.Commit()

	if result.Applied != 3 {
		t.Fatalf("Applied: got %d, want 3", result.Applied)
	}
	if result.LastAppliedSeq != 3 {
		t.Fatalf("LastAppliedSeq: got %d, want 3", result.LastAppliedSeq)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("Failed: got %d, want 0", len(result.Failed))
	}

	// Verify entities in DB
	var title, status string
	if err := db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i1").Scan(&title, &status); err != nil {
		t.Fatalf("query i1: %v", err)
	}
	if title != "Updated First" || status != "closed" {
		t.Errorf("i1: title=%q status=%q", title, status)
	}

	if err := db.QueryRow("SELECT title, status FROM issues WHERE id = ?", "i2").Scan(&title, &status); err != nil {
		t.Fatalf("query i2: %v", err)
	}
	if title != "Second" || status != "open" {
		t.Errorf("i2: title=%q status=%q", title, status)
	}
}

func TestApplyRemoteEvents_PartialFailure(t *testing.T) {
	db := setupClientDB(t)

	events := []Event{
		{
			ServerSeq:  1,
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "i1",
			Payload:    []byte(`{"schema_version":1,"new_data":{"title":"Good","status":"open"},"previous_data":{}}`),
		},
		{
			ServerSeq:  2,
			ActionType: "create",
			EntityType: "nonexistent_table",
			EntityID:   "x1",
			Payload:    []byte(`{"schema_version":1,"new_data":{"name":"Bad"},"previous_data":{}}`),
		},
		{
			ServerSeq:  3,
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "i2",
			Payload:    []byte(`{"schema_version":1,"new_data":{"title":"Also Good","status":"open"},"previous_data":{}}`),
		},
	}

	tx, _ := db.Begin()
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("ApplyRemoteEvents: %v", err)
	}
	tx.Commit()

	if result.Applied != 2 {
		t.Fatalf("Applied: got %d, want 2", result.Applied)
	}
	if result.LastAppliedSeq != 3 {
		t.Fatalf("LastAppliedSeq: got %d, want 3", result.LastAppliedSeq)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("Failed: got %d, want 1", len(result.Failed))
	}
	if result.Failed[0].ServerSeq != 2 {
		t.Errorf("Failed[0].ServerSeq: got %d, want 2", result.Failed[0].ServerSeq)
	}

	// Verify good entities exist
	var count int
	db.QueryRow("SELECT COUNT(*) FROM issues").Scan(&count)
	if count != 2 {
		t.Fatalf("issues count: got %d, want 2", count)
	}
}

func TestApplyRemoteEvents_ConflictTracking(t *testing.T) {
	db := setupClientDB(t)

	// Create initial row
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "local", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// Apply remote event that overwrites
	remotePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       map[string]any{"title": "remote", "status": "closed"},
	})
	events := []Event{{
		ServerSeq:  42,
		DeviceID:   "other-device",
		ActionType: "update",
		EntityType: "issues",
		EntityID:   "i1",
		Payload:    remotePayload,
	}}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Overwrites != 1 {
		t.Fatalf("expected 1 overwrite, got %d", result.Overwrites)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	c := result.Conflicts[0]
	if c.ServerSeq != 42 {
		t.Errorf("conflict ServerSeq=%d, want 42", c.ServerSeq)
	}
	if c.EntityType != "issues" || c.EntityID != "i1" {
		t.Errorf("conflict entity=%s/%s, want issues/i1", c.EntityType, c.EntityID)
	}

	var local map[string]any
	if err := json.Unmarshal(c.LocalData, &local); err != nil {
		t.Fatalf("unmarshal LocalData: %v", err)
	}
	if local["title"] != "local" {
		t.Errorf("LocalData title=%v, want 'local'", local["title"])
	}

	var remote map[string]any
	if err := json.Unmarshal(c.RemoteData, &remote); err != nil {
		t.Fatalf("unmarshal RemoteData: %v", err)
	}
	if remote["title"] != "remote" {
		t.Errorf("RemoteData title=%v, want 'remote'", remote["title"])
	}
}

func TestApplyRemoteEvents_MultipleOverwritesProduceConflicts(t *testing.T) {
	db := setupClientDB(t)

	// Seed two local rows
	tx := beginTx(t, db)
	p1, _ := json.Marshal(map[string]any{"title": "local-A", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p1); err != nil {
		t.Fatalf("seed i1: %v", err)
	}
	p2, _ := json.Marshal(map[string]any{"title": "local-B", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i2", p2); err != nil {
		t.Fatalf("seed i2: %v", err)
	}
	tx.Commit()

	// Apply batch of remote events that overwrite both
	makePayload := func(title, status string) []byte {
		b, _ := json.Marshal(map[string]any{
			"schema_version": 1,
			"new_data":       map[string]any{"title": title, "status": status},
		})
		return b
	}

	events := []Event{
		{ServerSeq: 10, DeviceID: "other", ActionType: "update", EntityType: "issues", EntityID: "i1", Payload: makePayload("remote-A", "closed")},
		{ServerSeq: 11, DeviceID: "other", ActionType: "update", EntityType: "issues", EntityID: "i2", Payload: makePayload("remote-B", "closed")},
	}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Applied != 2 {
		t.Fatalf("Applied=%d, want 2", result.Applied)
	}
	if result.Overwrites != 2 {
		t.Fatalf("Overwrites=%d, want 2", result.Overwrites)
	}
	if len(result.Conflicts) != 2 {
		t.Fatalf("Conflicts=%d, want 2", len(result.Conflicts))
	}

	// Verify each conflict has correct server seq and entity
	for i, c := range result.Conflicts {
		expectedSeq := int64(10 + i)
		expectedID := fmt.Sprintf("i%d", i+1)
		if c.ServerSeq != expectedSeq {
			t.Errorf("conflict[%d] ServerSeq=%d, want %d", i, c.ServerSeq, expectedSeq)
		}
		if c.EntityID != expectedID {
			t.Errorf("conflict[%d] EntityID=%s, want %s", i, c.EntityID, expectedID)
		}
	}
}

func TestApplyRemoteEvents_DeleteDoesNotProduceConflict(t *testing.T) {
	db := setupClientDB(t)

	// Seed a local row
	tx := beginTx(t, db)
	p, _ := json.Marshal(map[string]any{"title": "local", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// Apply a delete event from remote
	deletePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       map[string]any{},
	})
	events := []Event{
		{ServerSeq: 50, DeviceID: "other", ActionType: "delete", EntityType: "issues", EntityID: "i1", Payload: deletePayload},
	}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Applied != 1 {
		t.Fatalf("Applied=%d, want 1", result.Applied)
	}
	if result.Overwrites != 0 {
		t.Fatalf("Overwrites=%d, want 0 (delete should not count)", result.Overwrites)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("Conflicts=%d, want 0 (delete should not produce conflict)", len(result.Conflicts))
	}

	// Verify row is actually deleted
	var count int
	db.QueryRow("SELECT COUNT(*) FROM issues WHERE id = ?", "i1").Scan(&count)
	if count != 0 {
		t.Fatal("row should be deleted")
	}
}

func TestApplyRemoteEvents_ConflictDataCorrectness(t *testing.T) {
	db := setupClientDB(t)

	// Seed with specific local data
	tx := beginTx(t, db)
	localFields, _ := json.Marshal(map[string]any{
		"title":    "my-local-title",
		"status":   "in_progress",
		"priority": "high",
	})
	if _, err := upsertEntity(tx, "issues", "i1", localFields); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// Remote overwrites with different data
	remoteFields := map[string]any{
		"title":    "remote-title",
		"status":   "closed",
		"priority": "low",
	}
	remotePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       remoteFields,
	})
	events := []Event{{
		ServerSeq: 99, DeviceID: "other", ActionType: "update",
		EntityType: "issues", EntityID: "i1", Payload: remotePayload,
	}}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &farPast)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if len(result.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(result.Conflicts))
	}

	c := result.Conflicts[0]

	// Verify LocalData has the original values
	var local map[string]any
	if err := json.Unmarshal(c.LocalData, &local); err != nil {
		t.Fatalf("unmarshal LocalData: %v", err)
	}
	if local["title"] != "my-local-title" {
		t.Errorf("LocalData title=%v, want 'my-local-title'", local["title"])
	}
	if local["status"] != "in_progress" {
		t.Errorf("LocalData status=%v, want 'in_progress'", local["status"])
	}

	// Verify RemoteData has the new values
	var remote map[string]any
	if err := json.Unmarshal(c.RemoteData, &remote); err != nil {
		t.Fatalf("unmarshal RemoteData: %v", err)
	}
	if remote["title"] != "remote-title" {
		t.Errorf("RemoteData title=%v, want 'remote-title'", remote["title"])
	}
	if remote["status"] != "closed" {
		t.Errorf("RemoteData status=%v, want 'closed'", remote["status"])
	}
	if remote["priority"] != "low" {
		t.Errorf("RemoteData priority=%v, want 'low'", remote["priority"])
	}

	// Verify OverwrittenAt is recent
	if c.OverwrittenAt.IsZero() {
		t.Error("OverwrittenAt should not be zero")
	}
	if time.Since(c.OverwrittenAt) > 5*time.Second {
		t.Error("OverwrittenAt should be recent")
	}
}

func TestApplyRemoteEvents_NoConflictWhenUnchangedSinceSync(t *testing.T) {
	db := setupClientDB(t)

	// Create a local row with a known updated_at
	tx := beginTx(t, db)
	oldTime := "2025-01-01T00:00:00Z"
	_, err := tx.Exec(`INSERT INTO issues (id, title, status, updated_at) VALUES (?, ?, ?, ?)`,
		"i1", "local", "open", oldTime)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// lastSyncAt is AFTER the local row's updated_at → no conflict expected
	syncTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	remotePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       map[string]any{"title": "remote", "status": "closed"},
	})
	events := []Event{{
		ServerSeq: 1, DeviceID: "other", ActionType: "update",
		EntityType: "issues", EntityID: "i1", Payload: remotePayload,
	}}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &syncTime)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Applied != 1 {
		t.Fatalf("Applied=%d, want 1", result.Applied)
	}
	if result.Overwrites != 0 {
		t.Fatalf("Overwrites=%d, want 0 (local unchanged since sync)", result.Overwrites)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("Conflicts=%d, want 0", len(result.Conflicts))
	}
}

func TestApplyRemoteEvents_ConflictWhenModifiedAfterSync(t *testing.T) {
	db := setupClientDB(t)

	// Create a local row with updated_at AFTER lastSyncAt
	tx := beginTx(t, db)
	recentTime := "2025-07-01T00:00:00Z"
	_, err := tx.Exec(`INSERT INTO issues (id, title, status, updated_at) VALUES (?, ?, ?, ?)`,
		"i1", "modified-locally", "open", recentTime)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// lastSyncAt is BEFORE the local row's updated_at → conflict expected
	syncTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	remotePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       map[string]any{"title": "remote", "status": "closed"},
	})
	events := []Event{{
		ServerSeq: 1, DeviceID: "other", ActionType: "update",
		EntityType: "issues", EntityID: "i1", Payload: remotePayload,
	}}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, &syncTime)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Overwrites != 1 {
		t.Fatalf("Overwrites=%d, want 1 (local was modified after sync)", result.Overwrites)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("Conflicts=%d, want 1", len(result.Conflicts))
	}
}

func TestApplyRemoteEvents_NilLastSyncAtSkipsConflicts(t *testing.T) {
	db := setupClientDB(t)

	// Seed a local row
	tx := beginTx(t, db)
	p, _ := json.Marshal(map[string]any{"title": "local", "status": "open"})
	if _, err := upsertEntity(tx, "issues", "i1", p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tx.Commit()

	// Apply remote overwrite with nil lastSyncAt (bootstrap scenario)
	remotePayload, _ := json.Marshal(map[string]any{
		"schema_version": 1,
		"new_data":       map[string]any{"title": "remote", "status": "closed"},
	})
	events := []Event{{
		ServerSeq: 1, DeviceID: "other", ActionType: "update",
		EntityType: "issues", EntityID: "i1", Payload: remotePayload,
	}}

	tx = beginTx(t, db)
	result, err := ApplyRemoteEvents(tx, events, "my-device", testValidator, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	tx.Commit()

	if result.Overwrites != 0 {
		t.Fatalf("Overwrites=%d, want 0 (nil lastSyncAt = no conflicts)", result.Overwrites)
	}
}

func TestMarkEventsSynced(t *testing.T) {
	db := setupClientDB(t)

	insertActionLog(t, db, "al-00000001", "sess1", "create", "issues", "i1",
		`{"title":"One"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000002", "sess1", "create", "issues", "i2",
		`{"title":"Two"}`, `{}`, 0, "")
	insertActionLog(t, db, "al-00000003", "sess1", "update", "issues", "i1",
		`{"title":"Three"}`, `{"title":"One"}`, 0, "")

	// Get rowids for first two rows
	var rowid1, rowid2 int64
	db.QueryRow("SELECT rowid FROM action_log WHERE id = ?", "al-00000001").Scan(&rowid1)
	db.QueryRow("SELECT rowid FROM action_log WHERE id = ?", "al-00000002").Scan(&rowid2)

	acks := []Ack{
		{ClientActionID: rowid1, ServerSeq: 100},
		{ClientActionID: rowid2, ServerSeq: 101},
	}

	tx, _ := db.Begin()
	if err := MarkEventsSynced(tx, acks); err != nil {
		t.Fatalf("MarkEventsSynced: %v", err)
	}
	tx.Commit()

	// Verify synced rows
	var syncedAt sql.NullString
	var serverSeq sql.NullInt64

	db.QueryRow("SELECT synced_at, server_seq FROM action_log WHERE id = ?", "al-00000001").Scan(&syncedAt, &serverSeq)
	if !syncedAt.Valid {
		t.Error("al-00000001: synced_at should be set")
	}
	if !serverSeq.Valid || serverSeq.Int64 != 100 {
		t.Errorf("al-00000001: server_seq got %v, want 100", serverSeq)
	}

	db.QueryRow("SELECT synced_at, server_seq FROM action_log WHERE id = ?", "al-00000002").Scan(&syncedAt, &serverSeq)
	if !syncedAt.Valid {
		t.Error("al-00000002: synced_at should be set")
	}
	if !serverSeq.Valid || serverSeq.Int64 != 101 {
		t.Errorf("al-00000002: server_seq got %v, want 101", serverSeq)
	}

	// Verify unsynced row
	db.QueryRow("SELECT synced_at, server_seq FROM action_log WHERE id = ?", "al-00000003").Scan(&syncedAt, &serverSeq)
	if syncedAt.Valid {
		t.Error("al-00000003: synced_at should NOT be set")
	}
	if serverSeq.Valid {
		t.Error("al-00000003: server_seq should NOT be set")
	}

	// Verify GetPendingEvents now only returns the unsynced one
	tx, _ = db.Begin()
	defer tx.Rollback()
	events, err := GetPendingEvents(tx, "d1", "s1")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("pending events: got %d, want 1", len(events))
	}
}

// TestGetPendingEvents_NullID verifies that action_log rows with NULL id are
// skipped without error or panic, while valid rows are still processed.
func TestGetPendingEvents_NullID(t *testing.T) {
	db := setupClientDB(t)

	// Insert a row with NULL id directly via SQL (helper requires non-null id)
	_, err := db.Exec(`
		INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone, synced_at)
		VALUES (NULL, 'sess1', 'create', 'issues', 'i-null', '{"title":"Null ID"}', '{}', datetime('now'), 0, NULL)`)
	if err != nil {
		t.Fatalf("insert NULL id row: %v", err)
	}

	// Insert a valid row
	insertActionLog(t, db, "al-valid-001", "sess1", "create", "issues", "i-valid",
		`{"title":"Valid ID"}`, `{}`, 0, "")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "device1", "sync-sess")
	if err != nil {
		t.Fatalf("GetPendingEvents should not error on NULL id: %v", err)
	}

	// Should return only the valid row
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (NULL id row should be skipped)", len(events))
	}
	if events[0].EntityID != "i-valid" {
		t.Errorf("expected entity i-valid, got %q", events[0].EntityID)
	}
}

// TestGetPendingEvents_RealActionTypesIntegration inserts action_log rows with
// real td action types (not pre-mapped "delete") and verifies they produce the
// correct sync event action types end-to-end through GetPendingEvents.
func TestGetPendingEvents_RealActionTypesIntegration(t *testing.T) {
	db := setupClientDB(t)

	// Insert rows using real action types that td's action_log would contain.
	// These must map to "delete" after flowing through mapActionType.
	insertActionLog(t, db, "al-int-001", "sess1", "remove_dependency", "dependency", "dep-1",
		`{}`, `{"id":"dep-1","issue_id":"i1","depends_on_id":"i2"}`, 0, "")
	insertActionLog(t, db, "al-int-002", "sess1", "unlink_file", "file_link", "fl-1",
		`{}`, `{"id":"fl-1","issue_id":"i1","file_path":"main.go"}`, 0, "")
	insertActionLog(t, db, "al-int-003", "sess1", "board_unposition", "board_position", "bp-1",
		`{}`, `{"id":"bp-1","board_id":"bd-1","issue_id":"i1","position":1}`, 0, "")
	insertActionLog(t, db, "al-int-004", "sess1", "board_remove_issue", "board_position", "bp-2",
		`{}`, `{"id":"bp-2","board_id":"bd-1","issue_id":"i2","position":2}`, 0, "")

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	events, err := GetPendingEvents(tx, "device-int", "sess-int")
	if err != nil {
		t.Fatalf("GetPendingEvents: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}

	// Real action types should map to delete or soft_delete as appropriate
	expected := []struct {
		entityType string
		actionType string
	}{
		{"issue_dependencies", "delete"},
		{"issue_files", "delete"},
		{"board_issue_positions", "soft_delete"},
		{"board_issue_positions", "soft_delete"},
	}

	for i, exp := range expected {
		if events[i].ActionType != exp.actionType {
			t.Errorf("events[%d] ActionType: got %q, want %q", i, events[i].ActionType, exp.actionType)
		}
		if events[i].EntityType != exp.entityType {
			t.Errorf("events[%d] EntityType: got %q, want %q", i, events[i].EntityType, exp.entityType)
		}
	}
}
