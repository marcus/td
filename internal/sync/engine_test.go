package sync

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupEngineDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := InitServerEventLog(db); err != nil {
		t.Fatalf("init event log: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeEvent(deviceID, sessionID string, actionID int64, entityID string) Event {
	return Event{
		DeviceID:        deviceID,
		SessionID:       sessionID,
		ClientActionID:  actionID,
		ActionType:      "create",
		EntityType:      "issues",
		EntityID:        entityID,
		Payload:         []byte(`{"title":"test"}`),
		ClientTimestamp: time.Now().UTC().Truncate(time.Second),
	}
}

func TestInsertServerEvents_Basic(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	events := []Event{
		makeEvent("d1", "s1", 1, "e1"),
		makeEvent("d1", "s1", 2, "e2"),
		makeEvent("d1", "s1", 3, "e3"),
	}

	result, err := InsertServerEvents(tx, events)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	if result.Accepted != 3 {
		t.Fatalf("accepted: got %d, want 3", result.Accepted)
	}
	if len(result.Acks) != 3 {
		t.Fatalf("acks: got %d, want 3", len(result.Acks))
	}
	if len(result.Rejected) != 0 {
		t.Fatalf("rejected: got %d, want 0", len(result.Rejected))
	}

	// Verify sequential server_seqs and correct client_action_ids
	for i, ack := range result.Acks {
		wantAID := int64(i + 1)
		if ack.ClientActionID != wantAID {
			t.Errorf("ack[%d] client_action_id: got %d, want %d", i, ack.ClientActionID, wantAID)
		}
		if ack.ServerSeq <= 0 {
			t.Errorf("ack[%d] server_seq should be positive, got %d", i, ack.ServerSeq)
		}
		if i > 0 && ack.ServerSeq <= result.Acks[i-1].ServerSeq {
			t.Errorf("ack[%d] server_seq %d not greater than ack[%d] %d", i, ack.ServerSeq, i-1, result.Acks[i-1].ServerSeq)
		}
	}
}

func TestInsertServerEvents_Dedup(t *testing.T) {
	db := setupEngineDB(t)

	events := []Event{
		makeEvent("d1", "s1", 1, "e1"),
		makeEvent("d1", "s1", 2, "e2"),
		makeEvent("d1", "s1", 3, "e3"),
	}

	// First insert
	tx, _ := db.Begin()
	r1, err := InsertServerEvents(tx, events)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	tx.Commit()

	if r1.Accepted != 3 {
		t.Fatalf("first: accepted=%d, want 3", r1.Accepted)
	}

	// Second insert (same events)
	tx, _ = db.Begin()
	r2, err := InsertServerEvents(tx, events)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	tx.Commit()

	if r2.Accepted != 0 {
		t.Fatalf("second: accepted=%d, want 0", r2.Accepted)
	}
	if len(r2.Rejected) != 3 {
		t.Fatalf("second: rejected=%d, want 3", len(r2.Rejected))
	}
	for i, rej := range r2.Rejected {
		if rej.Reason != "duplicate" {
			t.Errorf("rejection reason: got %q, want 'duplicate'", rej.Reason)
		}
		// Duplicate rejections should include the original server_seq
		if rej.ServerSeq != r1.Acks[i].ServerSeq {
			t.Errorf("rej[%d] ServerSeq: got %d, want %d (original)", i, rej.ServerSeq, r1.Acks[i].ServerSeq)
		}
	}

	// Verify total count in DB
	var count int
	db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if count != 3 {
		t.Fatalf("total events: got %d, want 3", count)
	}
}

func TestInsertServerEvents_ValidationReject(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	events := []Event{
		{
			DeviceID:        "",
			SessionID:       "s1",
			ClientActionID:  1,
			ActionType:      "create",
			EntityType:      "issues",
			EntityID:        "e1",
			Payload:         []byte(`{"title":"test"}`),
			ClientTimestamp: time.Now().UTC(),
		},
	}

	result, err := InsertServerEvents(tx, events)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	if result.Accepted != 0 {
		t.Fatalf("accepted: got %d, want 0", result.Accepted)
	}
	if len(result.Rejected) != 1 {
		t.Fatalf("rejected: got %d, want 1", len(result.Rejected))
	}
	if r := result.Rejected[0].Reason; r != "empty device_id" {
		t.Fatalf("reason: got %q, want contains 'empty'", r)
	}
}

func TestParseTimestamp_GoTimeStringDoubleTZ(t *testing.T) {
	ts := "2025-01-02 03:04:05 -0700 -0700"
	parsed, err := parseTimestamp(ts)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("", -7*3600))
	if !parsed.Equal(want) {
		t.Fatalf("parsed=%v, want %v", parsed, want)
	}
}

func TestGetEventsSince_All(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	var events []Event
	for i := 1; i <= 5; i++ {
		events = append(events, makeEvent("d1", "s1", int64(i), "e"+string(rune('0'+i))))
	}
	if _, err := InsertServerEvents(tx, events); err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	tx, _ = db.Begin()
	result, err := GetEventsSince(tx, 0, 100, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tx.Commit()

	if len(result.Events) != 5 {
		t.Fatalf("events: got %d, want 5", len(result.Events))
	}
	if result.HasMore {
		t.Fatal("HasMore should be false")
	}
	if result.LastServerSeq != result.Events[4].ServerSeq {
		t.Fatalf("LastServerSeq: got %d, want %d", result.LastServerSeq, result.Events[4].ServerSeq)
	}
}

func TestGetEventsSince_Partial(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	var events []Event
	for i := 1; i <= 5; i++ {
		events = append(events, makeEvent("d1", "s1", int64(i), "e"+string(rune('0'+i))))
	}
	if _, err := InsertServerEvents(tx, events); err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	tx, _ = db.Begin()
	result, err := GetEventsSince(tx, 3, 100, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tx.Commit()

	if len(result.Events) != 2 {
		t.Fatalf("events: got %d, want 2", len(result.Events))
	}
	if result.Events[0].ServerSeq != 4 {
		t.Fatalf("first event seq: got %d, want 4", result.Events[0].ServerSeq)
	}
	if result.Events[1].ServerSeq != 5 {
		t.Fatalf("second event seq: got %d, want 5", result.Events[1].ServerSeq)
	}
}

func TestGetEventsSince_Limit(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	var events []Event
	for i := 1; i <= 10; i++ {
		events = append(events, makeEvent("d1", "s1", int64(i), "e"+string(rune('0'+i))))
	}
	if _, err := InsertServerEvents(tx, events); err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	tx, _ = db.Begin()
	result, err := GetEventsSince(tx, 0, 3, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tx.Commit()

	if len(result.Events) != 3 {
		t.Fatalf("events: got %d, want 3", len(result.Events))
	}
	if !result.HasMore {
		t.Fatal("HasMore should be true")
	}
}

func TestGetEventsSince_ExcludeDevice(t *testing.T) {
	db := setupEngineDB(t)
	tx, _ := db.Begin()

	events := []Event{
		makeEvent("d1", "s1", 1, "e1"),
		makeEvent("d1", "s1", 2, "e2"),
		makeEvent("d2", "s1", 1, "e3"),
		makeEvent("d2", "s1", 2, "e4"),
	}
	if _, err := InsertServerEvents(tx, events); err != nil {
		t.Fatalf("insert: %v", err)
	}
	tx.Commit()

	tx, _ = db.Begin()
	result, err := GetEventsSince(tx, 0, 100, "d1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tx.Commit()

	if len(result.Events) != 2 {
		t.Fatalf("events: got %d, want 2", len(result.Events))
	}
	for _, ev := range result.Events {
		if ev.DeviceID != "d2" {
			t.Fatalf("expected device d2, got %q", ev.DeviceID)
		}
	}
}

func TestGetEventsSince_Empty(t *testing.T) {
	db := setupEngineDB(t)

	tx, _ := db.Begin()
	result, err := GetEventsSince(tx, 42, 100, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tx.Commit()

	if len(result.Events) != 0 {
		t.Fatalf("events: got %d, want 0", len(result.Events))
	}
	if result.LastServerSeq != 42 {
		t.Fatalf("LastServerSeq: got %d, want 42", result.LastServerSeq)
	}
	if result.HasMore {
		t.Fatal("HasMore should be false")
	}
}
