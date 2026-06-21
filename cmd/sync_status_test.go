package cmd

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/spf13/cobra"
)

// setProjectFeature persists a feature flag into the project config at baseDir.
func setProjectFeature(t *testing.T, baseDir, name string, enabled bool) {
	t.Helper()
	if err := config.SetFeatureFlag(baseDir, name, enabled); err != nil {
		t.Fatalf("set feature %s: %v", name, err)
	}
}

// clearSyncEnv neutralizes env vars that influence the gate decision so each
// subtest starts from a known baseline (no global override, no explicit
// sync_autosync). HOME is isolated by setupSyncStateDir.
func clearSyncEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO", "")
}

func TestGatherSyncStatus(t *testing.T) {
	t.Run("configured project, gate derived-on", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", false)

		r := gatherSyncStatus(dir)
		if !r.Configured {
			t.Error("expected configured=true")
		}
		if r.ProjectID != "proj-test" {
			t.Errorf("project_id: got %q want proj-test", r.ProjectID)
		}
		if !r.Authenticated {
			t.Error("expected authenticated=true")
		}
		if r.Gate != "ON" {
			t.Errorf("gate: got %q want ON", r.Gate)
		}
		if r.GateSource != "derived-per-project" {
			t.Errorf("gate_source: got %q want derived-per-project", r.GateSource)
		}
		if r.PendingEvents != 0 {
			t.Errorf("pending: got %d want 0", r.PendingEvents)
		}
	})

	t.Run("unconfigured project, gate derived-off", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "", false)

		r := gatherSyncStatus(dir)
		if r.Configured {
			t.Error("expected configured=false")
		}
		if r.ProjectID != "" {
			t.Errorf("project_id: got %q want empty", r.ProjectID)
		}
		if r.Gate != "OFF" {
			t.Errorf("gate: got %q want OFF", r.Gate)
		}
		if r.GateSource != "derived-per-project" {
			t.Errorf("gate_source: got %q want derived-per-project", r.GateSource)
		}
	})

	t.Run("gate killed by global kill-switch", func(t *testing.T) {
		clearSyncEnv(t)
		// Explicit global false engages the kill-switch regardless of config.
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", false)

		r := gatherSyncStatus(dir)
		if r.Gate != "KILLED" {
			t.Errorf("gate: got %q want KILLED", r.Gate)
		}
		if r.GateSource != "global-kill-switch" {
			t.Errorf("gate_source: got %q want global-kill-switch", r.GateSource)
		}
		// A killed gate must still report configuration correctly.
		if !r.Configured {
			t.Error("expected configured=true even when killed")
		}
	})

	t.Run("gate explicit-off via project config", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", false)
		// Write sync_autosync=false into project config. With no global override
		// engaged, this is an explicit OFF (not the global kill-switch).
		setProjectFeature(t, dir, "sync_autosync", false)

		r := gatherSyncStatus(dir)
		if r.Gate != "OFF" {
			t.Errorf("gate: got %q want OFF", r.Gate)
		}
		if r.GateSource != "explicit-config" {
			t.Errorf("gate_source: got %q want explicit-config", r.GateSource)
		}
	})

	t.Run("gate explicit-on via env", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "true")
		t.Setenv("TD_AUTH_KEY", "test-key")
		// Even an unconfigured project reports ON when explicitly forced on.
		dir := setupSyncStateDir(t, "", false)

		r := gatherSyncStatus(dir)
		if r.Gate != "ON" {
			t.Errorf("gate: got %q want ON", r.Gate)
		}
		if r.GateSource != "explicit-env" {
			t.Errorf("gate_source: got %q want explicit-env", r.GateSource)
		}
	})

	t.Run("degrades gracefully with no database", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("TD_AUTH_KEY", "test-key")

		// A directory with no td DB must not hard-error.
		r := gatherSyncStatus(t.TempDir())
		if r.Configured {
			t.Error("expected configured=false with no DB")
		}
		if r.PendingEvents != -1 {
			t.Errorf("pending: got %d want -1 (unknown)", r.PendingEvents)
		}
		if len(r.Notes) == 0 {
			t.Error("expected a degradation note when DB is absent")
		}
	})

	t.Run("empty base dir does not panic", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("HOME", t.TempDir())
		r := gatherSyncStatus("")
		if r.Configured {
			t.Error("expected configured=false for empty base dir")
		}
		if len(r.Notes) == 0 {
			t.Error("expected a note for empty base dir")
		}
	})

	t.Run("report marshals to valid JSON", func(t *testing.T) {
		clearSyncEnv(t)
		t.Setenv("TD_AUTH_KEY", "test-key")
		dir := setupSyncStateDir(t, "proj-test", false)

		r := gatherSyncStatus(dir)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var round map[string]any
		if err := json.Unmarshal(data, &round); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, key := range []string{"gate", "gate_source", "configured", "authenticated", "server_url", "pending_events"} {
			if _, ok := round[key]; !ok {
				t.Errorf("JSON missing key %q", key)
			}
		}
	})
}

// hasSubcommand reports whether parent has a direct subcommand named name.
func hasSubcommand(parent *cobra.Command, name string) bool {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return true
		}
	}
	return false
}

// TestSyncStatusReachableWhenSyncCLIOff asserts the read-only `status`
// subcommand is registered on the always-on sync parent so it is reachable even
// when SyncCLI is off. The test binary's init() runs with SyncCLI at its default
// (off), so the always-on parent is the one that gets wired up. Registration on
// the two parents is mutually exclusive by design (avoids double-registration),
// so we only assert the active parent here; the SyncCLI=on path is covered by
// the manual `TD_FEATURE_SYNC_CLI=1 sync status --help` verification and by
// TestSyncStatusRegistrationMutuallyExclusive below.
func TestSyncStatusReachableWhenSyncCLIOff(t *testing.T) {
	if !hasSubcommand(syncAlwaysOnCmd, "status") {
		t.Error("expected `status` on the always-on sync parent (reachable when SyncCLI is off)")
	}
	if !hasSubcommand(syncAlwaysOnCmd, "enable") || !hasSubcommand(syncAlwaysOnCmd, "disable") {
		t.Error("expected enable/disable to remain on the always-on sync parent")
	}
}

// TestSyncStatusRegistrationMutuallyExclusive guards against double-registration:
// `status` must live under exactly one of the two parents, never both (cobra
// panics on AddCommand of an already-parented command).
func TestSyncStatusRegistrationMutuallyExclusive(t *testing.T) {
	onFull := hasSubcommand(syncCmd, "status")
	onAlwaysOn := hasSubcommand(syncAlwaysOnCmd, "status")
	if onFull && onAlwaysOn {
		t.Error("`status` registered on BOTH parents — would panic on double AddCommand")
	}
	if !onFull && !onAlwaysOn {
		t.Error("`status` registered on NEITHER parent — not reachable")
	}
}

// TestDoctorRegisteredUngated asserts `td doctor` is on the root command so it
// runs even when SyncCLI is off.
func TestDoctorRegisteredUngated(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "doctor" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected `doctor` to be registered ungated on rootCmd")
	}
}
