package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/syncclient"
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

func TestRunBootstrapSuccessfulReplacement(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "1")

	localDir := t.TempDir()
	localDB, err := db.Initialize(localDir)
	if err != nil {
		t.Fatalf("init local DB: %v", err)
	}
	if err := localDB.SetSyncState("proj-test"); err != nil {
		t.Fatalf("set sync state: %v", err)
	}
	state, err := localDB.GetSyncState()
	if err != nil {
		t.Fatal(err)
	}

	snapshotDir := t.TempDir()
	snapshotDB, err := db.Initialize(snapshotDir)
	if err != nil {
		t.Fatalf("init snapshot DB: %v", err)
	}
	if _, err := snapshotDB.Conn().Exec(`INSERT INTO schema_info (key, value) VALUES ('snapshot-marker', 'present')`); err != nil {
		t.Fatal(err)
	}
	if err := checkpointSQLiteForReplacement(snapshotDB); err != nil {
		t.Fatal(err)
	}
	if err := snapshotDB.Close(); err != nil {
		t.Fatal(err)
	}
	snapshotBytes, err := os.ReadFile(filepath.Join(snapshotDir, ".todos", "issues.db"))
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/projects/proj-test/sync/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"event_count":99,"last_server_seq":99}`))
		case "/v1/projects/proj-test/sync/snapshot":
			w.Header().Set("X-Snapshot-Seq", "99")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(snapshotBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := syncclient.New(server.URL, "test-key", "test-device")
	replaced, err := runBootstrap(localDB, client, state)
	if err != nil {
		t.Fatalf("runBootstrap: %v", err)
	}
	if replaced == nil {
		t.Fatal("runBootstrap returned nil replacement DB")
	}
	defer replaced.Close()

	var marker string
	if err := replaced.Conn().QueryRow(`SELECT value FROM schema_info WHERE key = 'snapshot-marker'`).Scan(&marker); err != nil {
		t.Fatalf("read snapshot marker: %v", err)
	}
	if marker != "present" {
		t.Fatalf("snapshot marker=%q, want present", marker)
	}
	newState, err := replaced.GetSyncState()
	if err != nil {
		t.Fatal(err)
	}
	if newState == nil || newState.ProjectID != "proj-test" || newState.LastPulledServerSeq != 99 {
		t.Fatalf("unexpected sync state after bootstrap: %+v", newState)
	}
	if err := replaced.QuickCheck(); err != nil {
		t.Fatalf("replacement integrity: %v", err)
	}
}
