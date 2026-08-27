package cmd

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/features"
	"github.com/marcus/td/internal/syncconfig"
)

// seedPendingEvents inserts n unsynced action_log rows into the DB at baseDir so
// CountPendingEvents() returns n. It opens/closes its own handle.
func seedPendingEvents(t *testing.T, baseDir string, n int) {
	t.Helper()
	database, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = database.Close() }()

	conn := database.Conn()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	now := time.Now()
	for i := 1; i <= n; i++ {
		if _, err := stmt.Exec(
			fmt.Sprintf("al-stranded-%08d", i),
			"sess-stranded",
			"create",
			"issues",
			fmt.Sprintf("i_stranded_%08d", i),
			"{}",
			fmt.Sprintf(`{"title":"Issue %d","status":"open"}`, i),
			now.Add(time.Duration(i)*time.Millisecond).Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("insert action_log %d: %v", i, err)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestStrandedSyncShouldWarn exercises the pure decision predicate for the
// "configured but autosync off" warning across the relevant gate states.
func TestStrandedSyncShouldWarn(t *testing.T) {
	t.Run("configured + kill-switch on + pending -> warn", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		// Explicit false engages the global kill-switch -> gate closed.
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		seedPendingEvents(t, dir, 3)

		warn, pending := strandedSyncShouldWarn(dir)
		if !warn {
			t.Fatal("expected warning for configured project with gate closed and pending events")
		}
		if pending != 3 {
			t.Fatalf("pending = %d, want 3", pending)
		}
	})

	t.Run("configured + explicit false via project config.json (no env) + pending -> warn", func(t *testing.T) {
		// The prior version of this case set TD_FEATURE_SYNC_AUTOSYNC=false, which
		// exercises the env path, not config.json. Here we explicitly leave the env
		// var UNSET and write sync_autosync=false into the project's config.json so
		// ResolveExplicit must return explicit-false from the config source.
		t.Setenv("TD_AUTH_KEY", "test-key")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		if err := config.SetFeatureFlag(dir, features.SyncAutosync.Name, false); err != nil {
			t.Fatalf("set feature flag in config.json: %v", err)
		}
		// Sanity: the flag must resolve explicit-false from config (not env/default).
		if v, explicit := features.ResolveExplicit(dir, features.SyncAutosync.Name); !explicit || v {
			t.Fatalf("precondition: ResolveExplicit = (%v, explicit=%v), want (false, true)", v, explicit)
		}
		seedPendingEvents(t, dir, 1)

		warn, pending := strandedSyncShouldWarn(dir)
		if !warn || pending != 1 {
			t.Fatalf("got (%v, %d), want (true, 1)", warn, pending)
		}
	})

	t.Run("configured + global kill-switch (config.json) + pending -> warn", func(t *testing.T) {
		// Engage the global autosync kill-switch via ~/.config/td/config.json (no
		// env var), exercising globalKillSwitchOff()'s no-DB path.
		t.Setenv("TD_AUTH_KEY", "test-key")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		if err := syncconfig.SetGlobalAutosyncOverride(false); err != nil {
			t.Fatalf("set global autosync override: %v", err)
		}
		seedPendingEvents(t, dir, 2)

		warn, pending := strandedSyncShouldWarn(dir)
		if !warn || pending != 2 {
			t.Fatalf("got (%v, %d), want (true, 2)", warn, pending)
		}
	})

	t.Run("flag unset (gate would be open) -> no warn, no DB work", func(t *testing.T) {
		// The hot path: sync_autosync is unset everywhere and the kill-switch is
		// not engaged, so the gate is either open-and-syncing or the project is
		// unconfigured. Either way we must NOT warn AND must short-circuit before
		// opening the DB. We prove "no DB work" by pointing at a baseDir that has
		// NO initialized DB: if strandedSyncShouldWarn touched it, db.Open would
		// fail (still returning false) but, more importantly, it must reach that
		// decision without depending on the DB at all. A non-existent dir is fine —
		// the function returns (false, 0) purely from the DB-free signals.
		t.Setenv("HOME", t.TempDir()) // unauth + clean global config
		t.Setenv("TD_AUTH_KEY", "")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		noDBDir := filepath.Join(t.TempDir(), "does-not-exist")

		warn, pending := strandedSyncShouldWarn(noDBDir)
		if warn || pending != 0 {
			t.Fatalf("got (%v, %d), want (false, 0) on the no-DB hot path", warn, pending)
		}
	})

	t.Run("unconfigured (no sync_state) -> no warn", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "", false)
		seedPendingEvents(t, dir, 5)

		if warn, _ := strandedSyncShouldWarn(dir); warn {
			t.Fatal("expected no warning for unconfigured project")
		}
	})

	t.Run("configured + gate open + pending -> no warn (avoid double-warn)", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		// Flag unset + configured -> gate open (autosync owns the warning).
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		seedPendingEvents(t, dir, 4)

		if warn, _ := strandedSyncShouldWarn(dir); warn {
			t.Fatal("expected no warning when gate is open (autosync owns it)")
		}
	})

	t.Run("configured + gate closed + zero pending -> no warn", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)

		if warn, _ := strandedSyncShouldWarn(dir); warn {
			t.Fatal("expected no warning with zero pending events")
		}
	})

	t.Run("not authenticated -> no warn", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		t.Setenv("TD_SYNC_AUTO", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		seedPendingEvents(t, dir, 2)

		if warn, _ := strandedSyncShouldWarn(dir); warn {
			t.Fatal("expected no warning when unauthenticated")
		}
	})
}

// TestWarnIfSyncStrandedThrottle verifies the wrapper debounces: two calls in
// quick succession emit at most one warning. We assert on the throttle state
// (lastStrandedWarnAt) rather than capturing stderr.
func TestWarnIfSyncStrandedThrottle(t *testing.T) {
	t.Setenv("TD_AUTH_KEY", "test-key")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
	t.Setenv("TD_SYNC_AUTO", "")
	dir := setupSyncStateDir(t, "proj-test", false)
	seedPendingEvents(t, dir, 2)

	// Reset throttle state so this test is independent of ordering.
	strandedWarnMu.Lock()
	lastStrandedWarnAt = time.Time{}
	strandedWarnMu.Unlock()

	// First call: should warn and stamp lastStrandedWarnAt.
	if warn, _ := strandedSyncShouldWarn(dir); !warn {
		t.Fatal("precondition: expected predicate to want a warning")
	}
	warnIfSyncStranded(dir)

	strandedWarnMu.Lock()
	firstStamp := lastStrandedWarnAt
	strandedWarnMu.Unlock()
	if firstStamp.IsZero() {
		t.Fatal("expected lastStrandedWarnAt to be set after first warn")
	}

	// Second immediate call: throttled, stamp must not advance.
	warnIfSyncStranded(dir)
	strandedWarnMu.Lock()
	secondStamp := lastStrandedWarnAt
	strandedWarnMu.Unlock()
	if !secondStamp.Equal(firstStamp) {
		t.Fatalf("expected throttle to suppress second warn; stamp advanced from %v to %v", firstStamp, secondStamp)
	}
}
