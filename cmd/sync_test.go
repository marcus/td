package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	// In TRUNCATE journal mode an idle connection leaves no -wal/-shm
	// sidecars, so it is invisible here. That is acceptable: replacement is
	// rename-based, so a stale rollback-mode connection keeps a consistent
	// private view of the old inode instead of corrupting the new file.
	if err := requireSQLiteSidecarsAbsent(dbPath); err != nil {
		t.Fatalf("idle rollback-mode connection must not block replacement: %v", err)
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

// TestAutoSyncApplyPullBatchQuarantinesPermanentFailure pins the td-8fe2bc
// contract: an event that can never apply must NOT wedge the batch behind it.
//
// This test previously asserted the opposite — that any failed event rolled the
// batch back and preserved the cursor. That was the bug: replaying the batch
// reproduced the identical failure forever, so the peer stopped converging
// permanently. Rollback is still correct for TRANSIENT failures, which
// TestAutoSyncApplyPullBatchRollsBackOnTransientFailure covers.
func TestAutoSyncApplyPullBatchQuarantinesPermanentFailure(t *testing.T) {
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
		{ServerSeq: 1, ActionType: "create", EntityType: "issues", EntityID: "td-good01", Payload: wrap(`{"id":"td-good01","title":"before the poison","status":"open","priority":"P2","type":"task"}`)},
		{ServerSeq: 2, ActionType: "create", EntityType: "not_a_table", EntityID: "bad", Payload: wrap(`{"id":"bad"}`)},
		{ServerSeq: 3, ActionType: "create", EntityType: "issues", EntityID: "td-good02", Payload: wrap(`{"id":"td-good02","title":"behind the poison","status":"open","priority":"P2","type":"task"}`)},
	}

	if err := autoSyncApplyPullBatch(database, events, "device", 3, nil); err != nil {
		t.Fatalf("permanent failure must not abort the batch: %v", err)
	}

	// Events on both sides of the poison event applied.
	for _, id := range []string{"td-good01", "td-good02"} {
		var count int
		if err := database.Conn().QueryRow(`SELECT COUNT(*) FROM issues WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("%s did not apply (count=%d) — stream still blocked by the poison event", id, count)
		}
	}

	// The cursor moved past it, which is what unblocks the stream.
	state, err := database.GetSyncState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastPulledServerSeq != 3 {
		t.Fatalf("sync cursor did not advance past the unappliable event: %d", state.LastPulledServerSeq)
	}

	// And the skip is on the record, not silently swallowed.
	counts, err := database.CountSkippedEvents()
	if err != nil {
		t.Fatal(err)
	}
	if counts[tdsync.SkipReasonQuarantined] != 1 {
		t.Fatalf("expected 1 quarantined event, got %v", counts)
	}
	skipped, err := database.GetSkippedEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped row, got %d", len(skipped))
	}
	if skipped[0].ServerSeq != 2 {
		t.Errorf("wrong server_seq recorded: %d", skipped[0].ServerSeq)
	}
	if skipped[0].Error == "" {
		t.Error("quarantined event recorded without its error")
	}
}

// TestAutoSyncApplyPullBatchRollsBackOnTransientFailure keeps the other half of
// the contract: a transient failure still rolls the batch back and preserves the
// cursor, so a valid event is never skipped just because the environment
// hiccupped.
func TestAutoSyncApplyPullBatchRollsBackOnTransientFailure(t *testing.T) {
	outcome := tdsync.ResolvePullOutcome(tdsync.ApplyResult{Failed: []tdsync.FailedEvent{
		{ServerSeq: 2, Error: errors.New("database is locked")},
	}})
	if outcome.Abort == nil {
		t.Fatal("transient failure must abort the batch so the cursor is preserved")
	}
	if len(outcome.Record) != 0 {
		t.Fatalf("transient failure must not be quarantined: %+v", outcome.Record)
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
