package db

import (
	"testing"
	"time"
)

// TestWritesAreCanonical verifies the root-cause fix: a time.Time bound through
// the normal write path is now stored in the canonical layout (via the DSN's
// _time_format=sqlite) rather than the fragile Go time.Time.String() format.
func TestWritesAreCanonical(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// time.Now() carries a monotonic reading; the buggy writer would emit
	// "... -0700 PDT m=+...". Bind it the normal way.
	now := time.Now()
	if err := database.UpsertSession(&SessionRow{
		ID: "ses_write01", Branch: "main", AgentType: "terminal",
		StartedAt: now, LastActivity: now,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	var raw string
	if err := database.conn.QueryRow(
		`SELECT CAST(started_at AS TEXT) FROM sessions WHERE id='ses_write01'`).Scan(&raw); err != nil {
		t.Fatalf("read started_at: %v", err)
	}
	if LooksLikeGoTimeString(raw) {
		t.Errorf("timestamp written in legacy Go format: %q", raw)
	}
	// And it must round-trip through a strict scan.
	sess, err := database.GetSessionByID("ses_write01")
	if err != nil {
		t.Fatalf("GetSessionByID: %v", err)
	}
	if sess == nil || sess.StartedAt.IsZero() {
		t.Fatal("expected non-zero started_at after round trip")
	}
}

// TestMigrateNormalizeTimestamps verifies that timestamps stored in the legacy
// Go time.Time.String() format are rewritten to the canonical layout, that
// canonical / CURRENT_TIMESTAMP values are left untouched, and that the
// migration is idempotent.
func TestMigrateNormalizeTimestamps(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Insert a session row with corrupted (Go String()) timestamps, bypassing
	// the normal write path by writing the raw text directly.
	badStarted := "2026-07-18 07:29:04.73272 -0700 PDT m=+0.100833084"
	badActivity := "2026-01-09 11:28:00.067291 -0700 -0700" // nameless zone
	if _, err := database.conn.Exec(`INSERT INTO sessions
		(id, name, branch, agent_type, agent_pid, context_id, started_at, last_activity)
		VALUES ('ses_bad001','', 'main', 'terminal', 0, '', ?, ?)`,
		badStarted, badActivity); err != nil {
		t.Fatalf("insert corrupted session: %v", err)
	}

	// A canonical value that must NOT be altered.
	goodCanonical := "2026-07-18 07:29:04.73272-07:00"
	if _, err := database.conn.Exec(`INSERT INTO sessions
		(id, name, branch, agent_type, agent_pid, context_id, started_at, last_activity)
		VALUES ('ses_good001','', 'main', 'terminal', 0, '', ?, ?)`,
		goodCanonical, goodCanonical); err != nil {
		t.Fatalf("insert canonical session: %v", err)
	}

	// Run the migration.
	if err := database.migrateNormalizeTimestamps(); err != nil {
		t.Fatalf("migrateNormalizeTimestamps: %v", err)
	}

	// The corrupted row must now be parseable via a strict scan.
	sess, err := database.GetSessionByID("ses_bad001")
	if err != nil {
		t.Fatalf("GetSessionByID after repair: %v", err)
	}
	if sess == nil {
		t.Fatal("expected repaired session to be found")
	}
	if sess.StartedAt.IsZero() {
		t.Error("expected repaired started_at to be non-zero")
	}
	if sess.LastActivity.IsZero() {
		t.Error("expected repaired last_activity to be non-zero")
	}

	// Verify raw stored value is now canonical (no monotonic / zone-name markers).
	var rawStarted string
	if err := database.conn.QueryRow(
		`SELECT CAST(started_at AS TEXT) FROM sessions WHERE id='ses_bad001'`).Scan(&rawStarted); err != nil {
		t.Fatalf("read repaired started_at: %v", err)
	}
	if LooksLikeGoTimeString(rawStarted) {
		t.Errorf("started_at still in Go format after repair: %q", rawStarted)
	}

	// The canonical row must be byte-for-byte unchanged.
	var rawGood string
	if err := database.conn.QueryRow(
		`SELECT CAST(started_at AS TEXT) FROM sessions WHERE id='ses_good001'`).Scan(&rawGood); err != nil {
		t.Fatalf("read canonical started_at: %v", err)
	}
	if rawGood != goodCanonical {
		t.Errorf("canonical value was modified: got %q want %q", rawGood, goodCanonical)
	}

	// Idempotency: a second run must change nothing further.
	before := rawStarted
	if err := database.migrateNormalizeTimestamps(); err != nil {
		t.Fatalf("second migrateNormalizeTimestamps: %v", err)
	}
	var after string
	if err := database.conn.QueryRow(
		`SELECT CAST(started_at AS TEXT) FROM sessions WHERE id='ses_bad001'`).Scan(&after); err != nil {
		t.Fatalf("read after second run: %v", err)
	}
	if after != before {
		t.Errorf("second run mutated value: before %q after %q", before, after)
	}
}

// TestSessionLookupToleratesCorruptTimestamp verifies that a session row whose
// timestamps are in an unparseable legacy format can still be looked up (the
// scan degrades gracefully) rather than failing the whole query.
func TestSessionLookupToleratesCorruptTimestamp(t *testing.T) {
	dir := t.TempDir()
	database, err := Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer database.Close()

	// Deliberately unparseable last_activity.
	if _, err := database.conn.Exec(`INSERT INTO sessions
		(id, name, branch, agent_type, agent_pid, context_id, started_at, last_activity)
		VALUES ('ses_corrupt','', 'main', 'terminal', 0, '', ?, ?)`,
		"2026-07-18 07:29:04-07:00", "utterly unparseable"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	sess, err := database.GetSessionByID("ses_corrupt")
	if err != nil {
		t.Fatalf("GetSessionByID should not fail on corrupt timestamp: %v", err)
	}
	if sess == nil {
		t.Fatal("expected session to be found despite corrupt last_activity")
	}
	// last_activity falls back to started_at when unparseable.
	if sess.LastActivity.IsZero() {
		t.Error("expected last_activity to fall back to started_at, got zero")
	}
}
