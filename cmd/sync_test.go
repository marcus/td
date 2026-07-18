package cmd

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	tdsync "github.com/marcus/td/internal/sync"
	_ "modernc.org/sqlite"
)

func TestCopyFileProducesIdenticalCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.txt")
	dst := filepath.Join(dir, "dest.txt")

	content := []byte("hello world — test data with UTF-8: é€\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestValidateSQLiteFileRejectsNonDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00not really a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLiteFile(path); err == nil {
		t.Fatal("expected invalid SQLite file to be rejected")
	}
}

func TestValidateSQLiteFileAcceptsHealthyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE example (id INTEGER PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLiteFile(path); err != nil {
		t.Fatalf("validate healthy DB: %v", err)
	}
}

func TestRequireSQLiteSidecarsAbsentFailsClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "issues.db")
	if err := requireSQLiteSidecarsAbsent(dbPath); err != nil {
		t.Fatalf("no sidecars should be accepted: %v", err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("possibly live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireSQLiteSidecarsAbsent(dbPath); err == nil {
		t.Fatal("existing sidecar must abort replacement")
	}
}

func TestRequireSQLiteSidecarsAbsentDetectsOtherOpenConnection(t *testing.T) {
	baseDir := t.TempDir()
	first, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.Open(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if err := checkpointSQLiteForReplacement(first); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(baseDir, ".todos", "issues.db")
	if err := requireSQLiteSidecarsAbsent(dbPath); err == nil {
		t.Fatal("other open SQLite connection must keep a sidecar and abort replacement")
	}
}

func TestCheckpointSQLiteForReplacementFlushesWAL(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()
	if _, err := database.Conn().Exec(`INSERT INTO schema_info (key, value) VALUES ('checkpoint-test', 'written')`); err != nil {
		t.Fatal(err)
	}
	if err := checkpointSQLiteForReplacement(database); err != nil {
		t.Fatalf("checkpointSQLiteForReplacement: %v", err)
	}

	// Copy only the main file (not -wal/-shm) and prove the committed value is
	// present there. This is the invariant replacement relies on before cleanup.
	standalone := filepath.Join(t.TempDir(), "standalone.db")
	if err := copyFile(filepath.Join(baseDir, ".todos", "issues.db"), standalone); err != nil {
		t.Fatal(err)
	}
	conn, err := sql.Open("sqlite", standalone+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var value string
	if err := conn.QueryRow(`SELECT value FROM schema_info WHERE key = 'checkpoint-test'`).Scan(&value); err != nil {
		t.Fatalf("standalone main file lacks checkpointed row: %v", err)
	}
	if value != "written" {
		t.Fatalf("checkpointed value=%q, want written", value)
	}
}

func TestAutoSyncApplyPullBatchRollsBackOnFailedEvent(t *testing.T) {
	database, err := db.Initialize(t.TempDir())
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()
	if err := database.SetSyncState("project-test"); err != nil {
		t.Fatalf("SetSyncState: %v", err)
	}

	wrap := func(data string) json.RawMessage {
		return json.RawMessage(`{"schema_version":1,"new_data":` + data + `,"previous_data":{}}`)
	}
	events := []tdsync.Event{
		{ServerSeq: 1, ActionType: "create", EntityType: "issues", EntityID: "td-good01", Payload: wrap(`{"id":"td-good01","title":"must roll back","status":"open","priority":"P2","type":"task"}`)},
		{ServerSeq: 2, ActionType: "create", EntityType: "not_a_table", EntityID: "bad", Payload: wrap(`{"id":"bad"}`)},
	}

	if err := autoSyncApplyPullBatch(database, events, "device", 2, nil); err == nil {
		t.Fatal("expected failed remote event to abort batch")
	}

	var count int
	if err := database.Conn().QueryRow(`SELECT COUNT(*) FROM issues WHERE id = 'td-good01'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("successful event was committed despite later failure: count=%d", count)
	}
	state, err := database.GetSyncState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastPulledServerSeq != 0 {
		t.Fatalf("sync cursor advanced past failed event: %d", state.LastPulledServerSeq)
	}
}

func TestFailedRemoteEventsErrorIncludesRetrySafety(t *testing.T) {
	err := failedRemoteEventsError(tdsync.ApplyResult{Failed: []tdsync.FailedEvent{{
		ServerSeq: 42,
		Error:     sql.ErrNoRows,
	}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "seq=42", "rolled back", "cursor preserved") {
		t.Fatalf("error lacks actionable context: %q", got)
	}

	if err := failedRemoteEventsError(tdsync.ApplyResult{}); err != nil {
		t.Fatalf("unexpected error for successful result: %v", err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}

func TestSyncEntityValidatorAcceptsAllEntities(t *testing.T) {
	// Every entity type that the sync engine can produce must be accepted
	entities := []string{
		"issues", "logs", "comments", "handoffs", "boards",
		"work_sessions", "board_issue_positions",
		"issue_dependencies", "issue_files",
		"work_session_issues", // must not be missing
	}
	for _, entity := range entities {
		if !syncEntityValidator(entity) {
			t.Errorf("syncEntityValidator rejected %q — entity will never sync", entity)
		}
	}
}

func TestSyncEntityValidatorRejectsSessionState(t *testing.T) {
	if syncEntityValidator("session_state") {
		t.Fatal("session_state must remain local-only and out of base syncable entities")
	}
}

func TestCopyFileNonexistentSourceReturnsNil(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "dest"))
	if err != nil {
		t.Errorf("expected nil for nonexistent source, got: %v", err)
	}
}
