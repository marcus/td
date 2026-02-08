package serverdb

import (
	"testing"
	"time"
)

func TestInsertRateLimitEvent_WithKeyID(t *testing.T) {
	db := newTestDB(t)
	err := db.InsertRateLimitEvent("ak_123", "192.168.1.1", "push")
	if err != nil {
		t.Fatalf("insert rate limit event: %v", err)
	}

	result, err := db.QueryRateLimitEvents("ak_123", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Data))
	}
	e := result.Data[0]
	if e.KeyID != "ak_123" {
		t.Errorf("expected key_id ak_123, got %s", e.KeyID)
	}
	if e.IP != "192.168.1.1" {
		t.Errorf("expected ip 192.168.1.1, got %s", e.IP)
	}
	if e.EndpointClass != "push" {
		t.Errorf("expected endpoint_class push, got %s", e.EndpointClass)
	}
}

func TestInsertRateLimitEvent_WithoutKeyID(t *testing.T) {
	db := newTestDB(t)
	err := db.InsertRateLimitEvent("", "10.0.0.1", "auth")
	if err != nil {
		t.Fatalf("insert rate limit event: %v", err)
	}

	result, err := db.QueryRateLimitEvents("", "10.0.0.1", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 event, got %d", len(result.Data))
	}
	e := result.Data[0]
	if e.KeyID != "" {
		t.Errorf("expected empty key_id, got %s", e.KeyID)
	}
	if e.IP != "10.0.0.1" {
		t.Errorf("expected ip 10.0.0.1, got %s", e.IP)
	}
	if e.EndpointClass != "auth" {
		t.Errorf("expected endpoint_class auth, got %s", e.EndpointClass)
	}
}

func TestQueryRateLimitEvents_FilterByKeyID(t *testing.T) {
	db := newTestDB(t)
	db.InsertRateLimitEvent("ak_1", "1.1.1.1", "push")
	db.InsertRateLimitEvent("ak_2", "2.2.2.2", "pull")
	db.InsertRateLimitEvent("ak_1", "3.3.3.3", "other")

	result, err := db.QueryRateLimitEvents("ak_1", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events for ak_1, got %d", len(result.Data))
	}
}

func TestQueryRateLimitEvents_FilterByIP(t *testing.T) {
	db := newTestDB(t)
	db.InsertRateLimitEvent("ak_1", "1.1.1.1", "push")
	db.InsertRateLimitEvent("ak_2", "2.2.2.2", "pull")
	db.InsertRateLimitEvent("ak_3", "1.1.1.1", "other")

	result, err := db.QueryRateLimitEvents("", "1.1.1.1", "", "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 events for ip 1.1.1.1, got %d", len(result.Data))
	}
}

func TestQueryRateLimitEvents_FilterByDateRange(t *testing.T) {
	db := newTestDB(t)

	// Use consistent datetime format matching SQLite's CURRENT_TIMESTAMP
	const dtFmt = "2006-01-02 15:04:05"
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(dtFmt)
	recent := now.Add(-1 * time.Hour).Format(dtFmt)
	nowStr := now.Format(dtFmt)

	db.conn.Exec(`INSERT INTO rate_limit_events (key_id, ip, endpoint_class, created_at) VALUES (?, ?, ?, ?)`,
		"ak_1", "1.1.1.1", "push", old)
	db.conn.Exec(`INSERT INTO rate_limit_events (key_id, ip, endpoint_class, created_at) VALUES (?, ?, ?, ?)`,
		"ak_2", "2.2.2.2", "pull", recent)
	db.conn.Exec(`INSERT INTO rate_limit_events (key_id, ip, endpoint_class, created_at) VALUES (?, ?, ?, ?)`,
		"ak_3", "3.3.3.3", "other", nowStr)

	// Query for events from 2 hours ago onwards
	from := now.Add(-2 * time.Hour).Format(dtFmt)
	result, err := db.QueryRateLimitEvents("", "", from, "", 10, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(result.Data))
	}
}

func TestQueryRateLimitEvents_Pagination(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 5; i++ {
		db.InsertRateLimitEvent("ak_1", "1.1.1.1", "push")
	}

	// First page
	result, err := db.QueryRateLimitEvents("", "", "", "", 2, "")
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
		t.Fatal("expected non-empty cursor")
	}

	// Second page
	result2, err := db.QueryRateLimitEvents("", "", "", "", 2, result.NextCursor)
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
	result3, err := db.QueryRateLimitEvents("", "", "", "", 2, result2.NextCursor)
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

func TestCleanupRateLimitEvents(t *testing.T) {
	db := newTestDB(t)

	const dtFmt = "2006-01-02 15:04:05"
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour).Format(dtFmt)
	recent := now.Add(-1 * time.Hour).Format(dtFmt)

	// Insert old event
	db.conn.Exec(`INSERT INTO rate_limit_events (key_id, ip, endpoint_class, created_at) VALUES (?, ?, ?, ?)`,
		"ak_1", "1.1.1.1", "push", old)
	// Insert recent event
	db.conn.Exec(`INSERT INTO rate_limit_events (key_id, ip, endpoint_class, created_at) VALUES (?, ?, ?, ?)`,
		"ak_2", "2.2.2.2", "pull", recent)
	// Insert current event
	db.InsertRateLimitEvent("ak_3", "3.3.3.3", "other")

	// Cleanup events older than 24 hours
	deleted, err := db.CleanupRateLimitEvents(24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	// Verify remaining events
	result, err := db.QueryRateLimitEvents("", "", "", "", 10, "")
	if err != nil {
		t.Fatalf("query after cleanup: %v", err)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 remaining events, got %d", len(result.Data))
	}
}

func TestCleanupRateLimitEvents_NothingToDelete(t *testing.T) {
	db := newTestDB(t)
	db.InsertRateLimitEvent("ak_1", "1.1.1.1", "push")

	deleted, err := db.CleanupRateLimitEvents(24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted, got %d", deleted)
	}
}
