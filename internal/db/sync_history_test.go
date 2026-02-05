package db

import (
	"testing"
	"time"
)

func TestRecordSyncHistoryTx_Basic(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	entries := []SyncHistoryEntry{
		{Direction: "push", ActionType: "create", EntityType: "issues", EntityID: "td-001", ServerSeq: 10, DeviceID: "dev-a", Timestamp: now},
		{Direction: "pull", ActionType: "update", EntityType: "logs", EntityID: "log-002", ServerSeq: 11, DeviceID: "dev-b", Timestamp: now},
	}

	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entries exist
	var count int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM sync_history`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestRecordSyncHistoryTx_EmptySlice(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}

	// Empty entries should return nil without error
	if err := RecordSyncHistoryTx(tx, nil); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx with nil should not error: %v", err)
	}
	if err := RecordSyncHistoryTx(tx, []SyncHistoryEntry{}); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx with empty slice should not error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestGetSyncHistoryTail_OrderAndLimit(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	// Insert 5 entries
	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	var entries []SyncHistoryEntry
	for i := range 5 {
		entries = append(entries, SyncHistoryEntry{
			Direction:  "push",
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "td-test",
			ServerSeq:  int64(i + 1),
			Timestamp:  now,
		})
	}
	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx: %v", err)
	}
	tx.Commit()

	// Get tail with limit 3
	tail, err := db.GetSyncHistoryTail(3)
	if err != nil {
		t.Fatalf("GetSyncHistoryTail: %v", err)
	}
	if len(tail) != 3 {
		t.Fatalf("expected 3, got %d", len(tail))
	}

	// Should be in chronological order (oldest first among last 3)
	// IDs 3, 4, 5 → server_seq 3, 4, 5
	if tail[0].ServerSeq != 3 {
		t.Errorf("first entry server_seq: got %d, want 3", tail[0].ServerSeq)
	}
	if tail[2].ServerSeq != 5 {
		t.Errorf("last entry server_seq: got %d, want 5", tail[2].ServerSeq)
	}

	// Verify chronological: each ID should be less than the next
	for i := 1; i < len(tail); i++ {
		if tail[i].ID <= tail[i-1].ID {
			t.Errorf("not chronological: id[%d]=%d <= id[%d]=%d", i, tail[i].ID, i-1, tail[i-1].ID)
		}
	}
}

func TestGetSyncHistoryTail_Empty(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	tail, err := db.GetSyncHistoryTail(10)
	if err != nil {
		t.Fatalf("GetSyncHistoryTail: %v", err)
	}
	if len(tail) != 0 {
		t.Errorf("expected 0 entries, got %d", len(tail))
	}
}

func TestGetSyncHistory_AfterID(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	// Insert 5 entries
	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	var entries []SyncHistoryEntry
	for i := range 5 {
		entries = append(entries, SyncHistoryEntry{
			Direction:  "push",
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "td-test",
			ServerSeq:  int64(i + 1),
			Timestamp:  now,
		})
	}
	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx: %v", err)
	}
	tx.Commit()

	// Get all entries first to find IDs
	all, err := db.GetSyncHistoryTail(10)
	if err != nil {
		t.Fatalf("GetSyncHistoryTail: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5, got %d", len(all))
	}

	// Get entries after the 3rd one
	afterID := all[2].ID
	result, err := db.GetSyncHistory(afterID, 100)
	if err != nil {
		t.Fatalf("GetSyncHistory: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries after id %d, got %d", afterID, len(result))
	}

	// Should be in ASC order
	if result[0].ID <= afterID {
		t.Errorf("first result id %d should be > afterID %d", result[0].ID, afterID)
	}
	if result[1].ID <= result[0].ID {
		t.Errorf("results not in ASC order: %d <= %d", result[1].ID, result[0].ID)
	}
}

func TestGetSyncHistory_WithLimit(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	var entries []SyncHistoryEntry
	for i := range 10 {
		entries = append(entries, SyncHistoryEntry{
			Direction:  "pull",
			ActionType: "update",
			EntityType: "issues",
			EntityID:   "td-test",
			ServerSeq:  int64(i + 1),
			Timestamp:  now,
		})
	}
	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx: %v", err)
	}
	tx.Commit()

	// afterID=0 gets all, but limit to 3
	result, err := db.GetSyncHistory(0, 3)
	if err != nil {
		t.Fatalf("GetSyncHistory: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3, got %d", len(result))
	}
}

func TestPruneSyncHistory_KeepsNewest(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	// Insert 10 entries
	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	var entries []SyncHistoryEntry
	for i := range 10 {
		entries = append(entries, SyncHistoryEntry{
			Direction:  "push",
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "td-test",
			ServerSeq:  int64(i + 1),
			Timestamp:  now,
		})
	}
	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx: %v", err)
	}
	tx.Commit()

	// Prune to keep only 3
	tx2, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	if err := PruneSyncHistory(tx2, 3); err != nil {
		tx2.Rollback()
		t.Fatalf("PruneSyncHistory: %v", err)
	}
	tx2.Commit()

	// Should have 3 left
	var count int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM sync_history`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 remaining, got %d", count)
	}

	// The remaining should be the newest (highest server_seq)
	tail, err := db.GetSyncHistoryTail(10)
	if err != nil {
		t.Fatalf("GetSyncHistoryTail: %v", err)
	}
	if len(tail) != 3 {
		t.Fatalf("expected 3, got %d", len(tail))
	}
	// Newest entries had server_seq 8, 9, 10
	if tail[0].ServerSeq != 8 {
		t.Errorf("oldest remaining server_seq: got %d, want 8", tail[0].ServerSeq)
	}
	if tail[2].ServerSeq != 10 {
		t.Errorf("newest remaining server_seq: got %d, want 10", tail[2].ServerSeq)
	}
}

func TestPruneSyncHistory_NoOpWhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	db, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer db.Close()

	if _, err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	now := time.Now().Truncate(time.Second)

	// Insert 3 entries
	tx, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx: %v", err)
	}
	var entries []SyncHistoryEntry
	for i := range 3 {
		entries = append(entries, SyncHistoryEntry{
			Direction:  "push",
			ActionType: "create",
			EntityType: "issues",
			EntityID:   "td-test",
			ServerSeq:  int64(i + 1),
			Timestamp:  now,
		})
	}
	if err := RecordSyncHistoryTx(tx, entries); err != nil {
		tx.Rollback()
		t.Fatalf("RecordSyncHistoryTx: %v", err)
	}
	tx.Commit()

	// Prune with maxRows=10 (more than we have)
	tx2, err := db.Conn().Begin()
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	if err := PruneSyncHistory(tx2, 10); err != nil {
		tx2.Rollback()
		t.Fatalf("PruneSyncHistory: %v", err)
	}
	tx2.Commit()

	// All 3 should remain
	var count int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM sync_history`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 remaining, got %d", count)
	}
}
