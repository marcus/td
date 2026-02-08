package serverdb

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInsertAuthEvent(t *testing.T) {
	db := newTestDB(t)

	err := db.InsertAuthEvent("ar_test1", "user@example.com", AuthEventStarted, `{"ip":"127.0.0.1"}`)
	if err != nil {
		t.Fatalf("insert auth event: %v", err)
	}

	// Verify row exists
	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Data))
	}

	e := result.Data[0]
	if e.AuthRequestID != "ar_test1" {
		t.Errorf("expected auth_request_id ar_test1, got %s", e.AuthRequestID)
	}
	if e.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", e.Email)
	}
	if e.EventType != AuthEventStarted {
		t.Errorf("expected event_type started, got %s", e.EventType)
	}
	if e.ID <= 0 {
		t.Errorf("expected positive id, got %d", e.ID)
	}
}

func TestInsertAuthEventDefaultMetadata(t *testing.T) {
	db := newTestDB(t)

	err := db.InsertAuthEvent("ar_test2", "user@example.com", AuthEventStarted, "")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Data[0].Metadata != "{}" {
		t.Errorf("expected default metadata '{}', got %q", result.Data[0].Metadata)
	}
}

func TestInsertAuthEventMetadataJSON(t *testing.T) {
	db := newTestDB(t)

	meta := map[string]string{"ip": "10.0.0.1", "user_agent": "Mozilla/5.0"}
	metaJSON, _ := json.Marshal(meta)

	err := db.InsertAuthEvent("ar_json", "json@test.com", AuthEventStarted, string(metaJSON))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal([]byte(result.Data[0].Metadata), &decoded); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if decoded["ip"] != "10.0.0.1" {
		t.Errorf("expected ip 10.0.0.1, got %s", decoded["ip"])
	}
	if decoded["user_agent"] != "Mozilla/5.0" {
		t.Errorf("expected user_agent Mozilla/5.0, got %s", decoded["user_agent"])
	}
}

func TestQueryAuthEventsFilterByEventType(t *testing.T) {
	db := newTestDB(t)

	db.InsertAuthEvent("ar_1", "a@test.com", AuthEventStarted, "{}")
	db.InsertAuthEvent("ar_2", "b@test.com", AuthEventCodeVerified, "{}")
	db.InsertAuthEvent("ar_3", "c@test.com", AuthEventFailed, "{}")
	db.InsertAuthEvent("ar_4", "d@test.com", AuthEventStarted, "{}")

	result, err := db.QueryAuthEvents(AuthEventStarted, "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 started events, got %d", len(result.Data))
	}
	for _, e := range result.Data {
		if e.EventType != AuthEventStarted {
			t.Errorf("expected started, got %s", e.EventType)
		}
	}
}

func TestQueryAuthEventsFilterByEmail(t *testing.T) {
	db := newTestDB(t)

	db.InsertAuthEvent("ar_1", "alice@example.com", AuthEventStarted, "{}")
	db.InsertAuthEvent("ar_2", "bob@example.com", AuthEventStarted, "{}")
	db.InsertAuthEvent("ar_3", "alice@other.com", AuthEventCodeVerified, "{}")

	result, err := db.QueryAuthEvents("", "alice", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events for alice, got %d", len(result.Data))
	}
}

func TestQueryAuthEventsFilterByDateRange(t *testing.T) {
	db := newTestDB(t)

	// Insert events with forced timestamps
	db.conn.Exec(`INSERT INTO auth_events (auth_request_id, email, event_type, metadata, created_at) VALUES (?, ?, ?, ?, ?)`,
		"ar_old", "old@test.com", AuthEventStarted, "{}", "2024-01-01 00:00:00")
	db.conn.Exec(`INSERT INTO auth_events (auth_request_id, email, event_type, metadata, created_at) VALUES (?, ?, ?, ?, ?)`,
		"ar_mid", "mid@test.com", AuthEventStarted, "{}", "2024-06-15 12:00:00")
	db.conn.Exec(`INSERT INTO auth_events (auth_request_id, email, event_type, metadata, created_at) VALUES (?, ?, ?, ?, ?)`,
		"ar_new", "new@test.com", AuthEventStarted, "{}", "2024-12-31 23:59:59")

	// Query from June onward
	result, err := db.QueryAuthEvents("", "", "2024-06-01 00:00:00", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events from June onward, got %d", len(result.Data))
	}

	// Query up to June
	result, err = db.QueryAuthEvents("", "", "", "2024-06-30 23:59:59", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events up to June, got %d", len(result.Data))
	}

	// Query exact range
	result, err = db.QueryAuthEvents("", "", "2024-06-01 00:00:00", "2024-06-30 23:59:59", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 event in June range, got %d", len(result.Data))
	}
}

func TestQueryAuthEventsPagination(t *testing.T) {
	db := newTestDB(t)

	for i := 0; i < 5; i++ {
		db.InsertAuthEvent("ar_pg", "pg@test.com", AuthEventStarted, "{}")
	}

	// First page
	result, err := db.QueryAuthEvents("", "", "", "", 2, "")
	if err != nil {
		t.Fatalf("query page 1: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events on page 1, got %d", len(result.Data))
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true")
	}
	if result.NextCursor == "" {
		t.Fatal("expected non-empty next_cursor")
	}

	// Second page
	result2, err := db.QueryAuthEvents("", "", "", "", 2, result.NextCursor)
	if err != nil {
		t.Fatalf("query page 2: %v", err)
	}
	if len(result2.Data) != 2 {
		t.Fatalf("expected 2 events on page 2, got %d", len(result2.Data))
	}
	if !result2.HasMore {
		t.Fatal("expected has_more=true on page 2")
	}

	// Third page (last)
	result3, err := db.QueryAuthEvents("", "", "", "", 2, result2.NextCursor)
	if err != nil {
		t.Fatalf("query page 3: %v", err)
	}
	if len(result3.Data) != 1 {
		t.Fatalf("expected 1 event on page 3, got %d", len(result3.Data))
	}
	if result3.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

func TestCleanupAuthEvents(t *testing.T) {
	db := newTestDB(t)

	// Insert old event
	db.conn.Exec(`INSERT INTO auth_events (auth_request_id, email, event_type, metadata, created_at) VALUES (?, ?, ?, ?, ?)`,
		"ar_old", "old@test.com", AuthEventStarted, "{}", time.Now().UTC().Add(-100*24*time.Hour).Format("2006-01-02 15:04:05"))

	// Insert recent event
	db.InsertAuthEvent("ar_new", "new@test.com", AuthEventStarted, "{}")

	// Cleanup events older than 90 days
	n, err := db.CleanupAuthEvents(90 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 deleted, got %d", n)
	}

	// Verify only the new event remains
	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(result.Data))
	}
	if result.Data[0].AuthRequestID != "ar_new" {
		t.Errorf("expected ar_new to remain, got %s", result.Data[0].AuthRequestID)
	}
}

func TestCleanupAuthEventsKeepsRecent(t *testing.T) {
	db := newTestDB(t)

	// Insert only recent events
	db.InsertAuthEvent("ar_1", "a@test.com", AuthEventStarted, "{}")
	db.InsertAuthEvent("ar_2", "b@test.com", AuthEventCodeVerified, "{}")

	n, err := db.CleanupAuthEvents(90 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deleted, got %d", n)
	}

	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 remaining events, got %d", len(result.Data))
	}
}

func TestGetPendingExpiredAuthRequests(t *testing.T) {
	db := newTestDB(t)

	ar1, _ := db.CreateAuthRequest("expired@test.com")
	db.CreateAuthRequest("fresh@test.com")

	// Force ar1 to be expired
	db.conn.Exec(`UPDATE auth_requests SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-1*time.Hour), ar1.ID)

	expired, err := db.GetPendingExpiredAuthRequests()
	if err != nil {
		t.Fatalf("get pending expired: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired, got %d", len(expired))
	}
	if expired[0].ID != ar1.ID {
		t.Errorf("expected expired request %s, got %s", ar1.ID, expired[0].ID)
	}
}

func TestAllAuthEventTypes(t *testing.T) {
	db := newTestDB(t)

	types := []string{AuthEventStarted, AuthEventCodeVerified, AuthEventKeyIssued, AuthEventExpired, AuthEventFailed}
	for _, et := range types {
		if err := db.InsertAuthEvent("ar_"+et, et+"@test.com", et, "{}"); err != nil {
			t.Fatalf("insert %s: %v", et, err)
		}
	}

	result, err := db.QueryAuthEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 5 {
		t.Fatalf("expected 5 events, got %d", len(result.Data))
	}

	for i, e := range result.Data {
		if e.EventType != types[i] {
			t.Errorf("event %d: expected type %s, got %s", i, types[i], e.EventType)
		}
	}
}
