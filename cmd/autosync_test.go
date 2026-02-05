package cmd

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcus/td/internal/syncconfig"
)

func TestIsMutatingCommand(t *testing.T) {
	// Commands that should trigger auto-sync
	mutating := []string{"create", "update", "delete", "start", "close", "log", "handoff", "board", "dep", "ws", "comments", "monitor"}
	for _, name := range mutating {
		if !isMutatingCommand(name) {
			t.Errorf("expected %q to be mutating", name)
		}
	}

	// Commands that should NOT trigger auto-sync
	readOnly := []string{"list", "show", "search", "query", "sync", "auth", "status", "info", "version", "help", "doctor"}
	for _, name := range readOnly {
		if isMutatingCommand(name) {
			t.Errorf("expected %q to NOT be mutating", name)
		}
	}
}

func TestAutoSyncEnabled_Default(t *testing.T) {
	// With no env var set, auto-sync should be enabled by default
	t.Setenv("TD_SYNC_AUTO", "")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled by default")
	}
}

func TestAutoSyncEnabled_Disabled(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO", "0")
	if AutoSyncEnabled() {
		t.Error("expected auto-sync disabled when TD_SYNC_AUTO=0")
	}
}

func TestAutoSyncEnabled_Explicit(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO", "true")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled when TD_SYNC_AUTO=true")
	}

	t.Setenv("TD_SYNC_AUTO", "1")
	if !AutoSyncEnabled() {
		t.Error("expected auto-sync enabled when TD_SYNC_AUTO=1")
	}
}

func TestAutoSyncEnabled_EnvOverride(t *testing.T) {
	// TD_SYNC_AUTO=0 disables
	t.Setenv("TD_SYNC_AUTO", "0")
	if AutoSyncEnabled() {
		t.Error("expected disabled with TD_SYNC_AUTO=0")
	}
	// TD_SYNC_AUTO=false disables
	t.Setenv("TD_SYNC_AUTO", "false")
	if AutoSyncEnabled() {
		t.Error("expected disabled with TD_SYNC_AUTO=false")
	}
	// TD_SYNC_AUTO=1 enables
	t.Setenv("TD_SYNC_AUTO", "1")
	if !AutoSyncEnabled() {
		t.Error("expected enabled with TD_SYNC_AUTO=1")
	}
}

func TestAutoSyncDebounce(t *testing.T) {
	// Reset state
	autoSyncMu.Lock()
	lastAutoSyncAt = time.Time{}
	autoSyncMu.Unlock()

	// With no recent sync, debounce should allow sync
	autoSyncMu.Lock()
	elapsed := time.Since(lastAutoSyncAt)
	autoSyncMu.Unlock()

	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "1s")
	debounce := syncconfig.GetAutoSyncDebounce()
	if debounce != time.Second {
		t.Errorf("expected 1s debounce, got %v", debounce)
	}
	if elapsed < debounce {
		t.Error("expected elapsed > debounce for zero lastAutoSyncAt")
	}

	// After setting lastAutoSyncAt to now, debounce should block
	autoSyncMu.Lock()
	lastAutoSyncAt = time.Now()
	autoSyncMu.Unlock()

	autoSyncMu.Lock()
	elapsed = time.Since(lastAutoSyncAt)
	autoSyncMu.Unlock()
	if elapsed >= debounce {
		t.Error("expected elapsed < debounce immediately after setting lastAutoSyncAt")
	}
}

func TestGetAutoSyncDebounce_EnvOverride(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "10s")
	if d := syncconfig.GetAutoSyncDebounce(); d != 10*time.Second {
		t.Errorf("expected 10s, got %v", d)
	}
}

func TestGetAutoSyncDebounce_Default(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "")
	d := syncconfig.GetAutoSyncDebounce()
	if d != 3*time.Second {
		t.Errorf("expected default 3s, got %v", d)
	}
}

func TestGetAutoSyncInterval_EnvOverride(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_INTERVAL", "10m")
	if d := syncconfig.GetAutoSyncInterval(); d != 10*time.Minute {
		t.Errorf("expected 10m, got %v", d)
	}
}

func TestGetAutoSyncInterval_Default(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_INTERVAL", "")
	if d := syncconfig.GetAutoSyncInterval(); d != 5*time.Minute {
		t.Errorf("expected default 5m, got %v", d)
	}
}

func TestGetAutoSyncPull_EnvOverride(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_PULL", "false")
	if syncconfig.GetAutoSyncPull() {
		t.Error("expected pull disabled")
	}
	t.Setenv("TD_SYNC_AUTO_PULL", "true")
	if !syncconfig.GetAutoSyncPull() {
		t.Error("expected pull enabled")
	}
}

func TestGetAutoSyncPull_Default(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_PULL", "")
	if !syncconfig.GetAutoSyncPull() {
		t.Error("expected pull enabled by default")
	}
}

func TestGetAutoSyncOnStart_EnvOverride(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_START", "0")
	if syncconfig.GetAutoSyncOnStart() {
		t.Error("expected on_start disabled")
	}
	t.Setenv("TD_SYNC_AUTO_START", "1")
	if !syncconfig.GetAutoSyncOnStart() {
		t.Error("expected on_start enabled")
	}
}

func TestGetAutoSyncOnStart_Default(t *testing.T) {
	t.Setenv("TD_SYNC_AUTO_START", "")
	if !syncconfig.GetAutoSyncOnStart() {
		t.Error("expected on_start enabled by default")
	}
}

func TestAutoSyncOnStartup_SkipCommands(t *testing.T) {
	// These commands should be skipped - verify the skip map
	skipCmds := []string{"sync", "auth", "login", "version", "help"}
	for _, cmd := range skipCmds {
		// We can't easily test autoSyncOnStartup directly since it calls autoSyncOnce
		// which needs a real DB, but we can verify the skip logic is correct by
		// checking the function doesn't panic for these commands
		// (autoSyncOnce will return early due to no auth)
		t.Setenv("TD_SYNC_AUTO_START", "true")
		// Just verify it doesn't panic - it will return early due to no auth
		autoSyncOnStartup(cmd)
	}
}

// TestAutoSyncOnStartup_DoesNotDebouncePostMutation verifies that on_start
// sync does not set lastAutoSyncAt, so the post-mutation sync in
// PersistentPostRun still fires even if the command completes quickly.
// Regression test for: on_start debounced away post-mutation push.
func TestAutoSyncOnStartup_DoesNotDebouncePostMutation(t *testing.T) {
	// Reset state
	autoSyncMu.Lock()
	lastAutoSyncAt = time.Time{}
	autoSyncMu.Unlock()
	atomic.StoreInt32(&autoSyncInFlight, 0)

	t.Setenv("TD_SYNC_AUTO_START", "true")
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "2s")

	// Simulate PersistentPreRun: autoSyncOnStartup for a mutating command.
	// autoSyncOnce will bail early (no auth/db) but the debounce behavior
	// is what we're testing — lastAutoSyncAt must remain zero.
	autoSyncOnStartup("create")

	autoSyncMu.Lock()
	ts := lastAutoSyncAt
	autoSyncMu.Unlock()

	if !ts.IsZero() {
		t.Errorf("autoSyncOnStartup should NOT set lastAutoSyncAt, got %v", ts)
	}

	// Now simulate PersistentPostRun: autoSyncAfterMutation should NOT be
	// debounced since lastAutoSyncAt is still zero.
	debounce := syncconfig.GetAutoSyncDebounce()
	autoSyncMu.Lock()
	wouldDebounce := time.Since(lastAutoSyncAt) < debounce
	autoSyncMu.Unlock()

	if wouldDebounce {
		t.Error("post-mutation sync would be debounced — on_start leaked the timestamp")
	}
}

// TestAutoSyncAfterMutation_SetsDebounce verifies that autoSyncAfterMutation
// does set lastAutoSyncAt so that rapid sequential commands are debounced.
func TestAutoSyncAfterMutation_SetsDebounce(t *testing.T) {
	// Reset state
	autoSyncMu.Lock()
	lastAutoSyncAt = time.Time{}
	autoSyncMu.Unlock()
	atomic.StoreInt32(&autoSyncInFlight, 0)

	t.Setenv("TD_SYNC_AUTO", "true")
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "5s")

	// autoSyncAfterMutation should set lastAutoSyncAt
	autoSyncAfterMutation()

	autoSyncMu.Lock()
	ts := lastAutoSyncAt
	autoSyncMu.Unlock()

	if ts.IsZero() {
		t.Error("autoSyncAfterMutation should set lastAutoSyncAt")
	}

	// A second call immediately after should be debounced
	debounce := syncconfig.GetAutoSyncDebounce()
	autoSyncMu.Lock()
	wouldDebounce := time.Since(lastAutoSyncAt) < debounce
	autoSyncMu.Unlock()

	if !wouldDebounce {
		t.Error("second mutation should be debounced")
	}
}

// TestStartupThenMutation_FullSequence simulates the PersistentPreRun →
// command → PersistentPostRun lifecycle and verifies the post-mutation
// sync is not debounced by the startup sync.
func TestStartupThenMutation_FullSequence(t *testing.T) {
	// Reset state
	autoSyncMu.Lock()
	lastAutoSyncAt = time.Time{}
	autoSyncMu.Unlock()
	atomic.StoreInt32(&autoSyncInFlight, 0)

	t.Setenv("TD_SYNC_AUTO", "true")
	t.Setenv("TD_SYNC_AUTO_START", "true")
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "2s")

	// PersistentPreRun
	autoSyncOnStartup("create")

	// Command runs (fast — well within debounce window)

	// PersistentPostRun — must not be debounced
	autoSyncMu.Lock()
	before := lastAutoSyncAt
	autoSyncMu.Unlock()

	autoSyncAfterMutation()

	autoSyncMu.Lock()
	after := lastAutoSyncAt
	autoSyncMu.Unlock()

	// autoSyncAfterMutation should have set lastAutoSyncAt (meaning it ran,
	// not returned early from debounce)
	if after.IsZero() || after.Equal(before) {
		t.Error("post-mutation sync was debounced by startup sync — regression")
	}
}

func TestAutoSyncInFlightGuard(t *testing.T) {
	// Set the in-flight flag to simulate a sync already running
	atomic.StoreInt32(&autoSyncInFlight, 1)
	defer atomic.StoreInt32(&autoSyncInFlight, 0)

	// Enable auto-sync and auth so autoSyncOnce would proceed if not guarded
	t.Setenv("TD_SYNC_AUTO", "true")
	t.Setenv("TD_AUTH_KEY", "test-key")

	// autoSyncOnce should return immediately without doing anything
	// because the in-flight flag is already set.
	// If the guard weren't working, it would attempt DB operations and fail.
	autoSyncOnce()

	// Verify the flag is still 1 (was not cleared by the guarded return path)
	if v := atomic.LoadInt32(&autoSyncInFlight); v != 1 {
		t.Errorf("expected autoSyncInFlight=1, got %d", v)
	}
}
