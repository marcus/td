package cmd

import (
	"testing"

	"github.com/marcus/td/internal/db"
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

func TestGlobalKillSwitchOff_StubReturnsFalse(t *testing.T) {
	// td-735875 will implement the real kill-switch. Until then it must be a
	// no-op (false) so the gate is never globally suppressed.
	if globalKillSwitchOff() {
		t.Fatal("globalKillSwitchOff stub should return false")
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
			name:         "unconfigured + explicit true -> open (override wins)",
			authKey:      "k",
			projectID:    "",
			flagEnv:      "true",
			wantGateOpen: true,
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
