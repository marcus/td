package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
)

func TestRunBootstrapSkipsWhenPendingEvents(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "1")

	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	if err := database.SetSyncState("proj-test"); err != nil {
		t.Fatalf("set sync state: %v", err)
	}

	// Insert a pending action_log row (synced_at NULL, undone=0).
	_, err = database.Conn().Exec(
		`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		"al-test", "sess1", "create", "issues", "i_001", "{}", "{}", time.Now().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert action_log: %v", err)
	}

	state, err := database.GetSyncState()
	if err != nil {
		t.Fatalf("get sync state: %v", err)
	}

	newDB, err := runBootstrap(database, nil, state)
	if !errors.Is(err, errBootstrapNotNeeded) {
		t.Fatalf("expected errBootstrapNotNeeded, got %v", err)
	}
	if newDB != nil {
		t.Fatalf("expected nil db, got %v", newDB)
	}

	// Ensure db connection still usable (bootstrap should not close it on skip).
	if _, err := database.Conn().Exec("SELECT 1"); err != nil {
		t.Fatalf("db unusable after bootstrap skip: %v", err)
	}
}

func TestReopenAfterBootstrapFailureMarksRecoveryErrors(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Initialize(tmpDir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := reopenAfterBootstrapFailure(tmpDir, "write snapshot", errors.New("write failed"), errors.New("rename failed"))
	if reopened == nil {
		t.Fatal("expected reopened db handle")
	}
	defer func() { _ = reopened.Close() }()

	if !errors.Is(err, errBootstrapRecoveryFailed) {
		t.Fatalf("expected bootstrap recovery error, got %v", err)
	}
	if _, queryErr := reopened.Conn().Exec("SELECT 1"); queryErr != nil {
		t.Fatalf("reopened db unusable: %v", queryErr)
	}
}
