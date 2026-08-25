package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/syncconfig"
)

// setupSyncStateDir creates an initialized td DB in a temp dir with an optional
// sync_state row, and returns the base dir. When projectID is empty, no
// sync_state row is written.
func setupSyncStateDir(t *testing.T, projectID string, disabled bool) string {
	t.Helper()
	// Isolate the global config/auth dir (~/.config/td via os.UserHomeDir) so an
	// empty TD_AUTH_KEY actually means "not authenticated" and does not fall
	// through to the developer's real on-disk auth.json.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer database.Close()

	if projectID != "" {
		if err := database.SetSyncState(projectID); err != nil {
			t.Fatalf("set sync state: %v", err)
		}
		if disabled {
			if _, err := database.Conn().Exec(`UPDATE sync_state SET sync_disabled = 1`); err != nil {
				t.Fatalf("disable sync: %v", err)
			}
		}
	}

	return dir
}

func TestProjectSyncConfigured(t *testing.T) {
	t.Run("configured and authenticated", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", false)
		if !projectSyncConfigured(dir) {
			t.Fatal("expected configured project to report true")
		}
	})

	t.Run("no sync state", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "", false)
		if projectSyncConfigured(dir) {
			t.Fatal("expected project without sync_state to report false")
		}
	})

	t.Run("sync disabled", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", true)
		if projectSyncConfigured(dir) {
			t.Fatal("expected sync-disabled project to report false")
		}
	})

	t.Run("not authenticated", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "")
		dir := setupSyncStateDir(t, "proj-test", false)
		if projectSyncConfigured(dir) {
			t.Fatal("expected unauthenticated project to report false")
		}
	})

	t.Run("empty base dir", func(t *testing.T) {
		t.Setenv("TD_AUTH_KEY", "test-key")
		if projectSyncConfigured("") {
			t.Fatal("expected empty base dir to report false")
		}
	})
}

// TestGlobalKillSwitchOff exercises the real global kill-switch (td-735875): it
// is engaged ONLY when the global autosync override resolves to an explicit
// false. Absent, or an explicit true, must NOT engage it.
func TestGlobalKillSwitchOff(t *testing.T) {
	t.Run("absent -> not engaged", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		if globalKillSwitchOff() {
			t.Fatal("absent override must not engage kill-switch")
		}
	})

	t.Run("explicit true -> not engaged", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("TD_SYNC_AUTO", "")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "true")
		if globalKillSwitchOff() {
			t.Fatal("explicit true must not engage kill-switch")
		}
	})

	t.Run("explicit false -> engaged", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("TD_SYNC_AUTO", "")
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		if !globalKillSwitchOff() {
			t.Fatal("explicit false must engage kill-switch")
		}
	})

	t.Run("legacy enabled:false does not engage", func(t *testing.T) {
		// Migration guard: a config carrying only the legacy sync.enabled=false
		// (new sync.autosync absent) must NOT silently kill autosync.
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		writeGlobalConfigJSON(t, home, `{"sync":{"enabled":false}}`)
		if globalKillSwitchOff() {
			t.Fatal("legacy enabled:false must not engage the kill-switch")
		}
	})

	t.Run("config false (no env) -> engaged", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
		t.Setenv("TD_SYNC_AUTO", "")
		writeGlobalConfigJSON(t, home, `{"sync":{"autosync":false}}`)
		if !globalKillSwitchOff() {
			t.Fatal("config sync.autosync=false must engage the kill-switch")
		}
	})
}

// writeGlobalConfigJSON writes raw JSON to <home>/.config/td/config.json.
func writeGlobalConfigJSON(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "td")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestSyncEnableDisableRoundTrip drives the td sync enable/disable subcommands
// and verifies they round-trip the sync.autosync bit in config.json.
func TestSyncEnableDisableRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Ensure env does not mask the config value we are asserting.
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO", "")

	if err := syncDisableCmd.RunE(syncDisableCmd, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if v := syncconfig.GetGlobalAutosyncOverride(); v == nil || *v != false {
		t.Fatalf("after disable: got %v, want false", v)
	}

	if err := syncEnableCmd.RunE(syncEnableCmd, nil); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if v := syncconfig.GetGlobalAutosyncOverride(); v == nil || *v != true {
		t.Fatalf("after enable: got %v, want true", v)
	}
}

// TestAutosyncGateOpen exercises the gate decision matrix: global kill-switch,
// explicit sync_autosync override (env), and per-project config fallback.
func TestAutosyncGateOpen(t *testing.T) {
	tests := []struct {
		name         string
		authKey      string
		projectID    string
		disabled     bool
		flagEnv      string // value for TD_FEATURE_SYNC_AUTOSYNC; "" = unset
		wantGateOpen bool
	}{
		{
			name:         "configured + flag unset -> open (new default)",
			authKey:      "k",
			projectID:    "proj",
			flagEnv:      "",
			wantGateOpen: true,
		},
		{
			name:         "unconfigured + flag unset -> closed",
			authKey:      "k",
			projectID:    "",
			flagEnv:      "",
			wantGateOpen: false,
		},
		{
			name:         "configured + explicit false -> closed (override wins)",
			authKey:      "k",
			projectID:    "proj",
			flagEnv:      "false",
			wantGateOpen: false,
		},
		{
			name:         "unconfigured + explicit true -> closed (linkage still required)",
			authKey:      "k",
			projectID:    "",
			flagEnv:      "true",
			wantGateOpen: false,
		},
		{
			name:         "sync disabled + flag unset -> closed",
			authKey:      "k",
			projectID:    "proj",
			disabled:     true,
			flagEnv:      "",
			wantGateOpen: false,
		},
		{
			name:         "configured but unauthenticated + flag unset -> closed",
			authKey:      "",
			projectID:    "proj",
			flagEnv:      "",
			wantGateOpen: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TD_AUTH_KEY", tc.authKey)
			if tc.flagEnv == "" {
				t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
			} else {
				t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", tc.flagEnv)
			}
			dir := setupSyncStateDir(t, tc.projectID, tc.disabled)

			if got := autosyncGateOpen(dir); got != tc.wantGateOpen {
				t.Fatalf("autosyncGateOpen = %v, want %v", got, tc.wantGateOpen)
			}
		})
	}
}
