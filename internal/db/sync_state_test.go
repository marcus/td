package db

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	now := time.Now().UTC()
	local := time.Now()
	tests := []struct {
		name  string
		input string
	}{
		{"SQLite default", "2026-03-21 09:00:00"},
		{"RFC3339", "2026-03-21T09:00:00Z"},
		{"RFC3339Nano", now.Format(time.RFC3339Nano)},
		{"Go time.String() UTC", now.String()},
		{"Go time.String() local", local.Round(0).String()},                   // non-UTC, no monotonic
		{"Go time.String() with monotonic", local.String()},                    // includes m=+... suffix
		{"non-UTC with offset", "2026-03-21 19:00:00.123456 +1000 AEST"},      // non-UTC timezone
		{"RFC3339 with offset", "2026-03-21T09:00:00+00:00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseTimestamp(tc.input)
			if err != nil {
				t.Fatalf("parseTimestamp(%q) failed: %v", tc.input, err)
			}
			if parsed.IsZero() {
				t.Errorf("parseTimestamp(%q) returned zero time", tc.input)
			}
		})
	}
}

func TestGetRecentConflictsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	// Ensure sync_conflicts table exists (created by schema migration)
	// Insert a conflict with a Go time.Time value (as storeConflicts does)
	now := time.Now().UTC()
	_, err = database.conn.Exec(
		`INSERT INTO sync_conflicts (entity_type, entity_id, server_seq, local_data, remote_data, overwritten_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"issue", "test-123", 42, `{"title":"old"}`, `{"title":"new"}`, now,
	)
	if err != nil {
		t.Fatalf("Insert conflict failed: %v", err)
	}

	// Read it back via GetRecentConflicts
	conflicts, err := database.GetRecentConflicts(10, nil)
	if err != nil {
		t.Fatalf("GetRecentConflicts failed: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
	}

	c := conflicts[0]
	if c.EntityType != "issue" || c.EntityID != "test-123" {
		t.Errorf("Wrong conflict: got %s/%s", c.EntityType, c.EntityID)
	}
	if c.ServerSeq != 42 {
		t.Errorf("Wrong server_seq: got %d", c.ServerSeq)
	}
	// Verify timestamp is approximately correct (within 2 seconds)
	diff := now.Sub(c.OverwrittenAt)
	if diff < 0 {
		diff = -diff
	}
	if diff > 2*time.Second {
		t.Errorf("Timestamp mismatch: stored %v, got %v (diff %v)", now, c.OverwrittenAt, diff)
	}
}
