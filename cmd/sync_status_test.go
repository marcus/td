package cmd

import (
	"encoding/json"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/features"
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

// wireSyncCommandsForTest runs the real wiring logic over throwaway stand-ins
// for the two parents and the three always-reachable subcommands, so a test can
// exercise either branch without re-parenting the package-level command tree
// that the rest of the suite (and Execute) depends on.
func wireSyncCommandsForTest(syncCLIEnabled bool) (chosen, full, alwaysOn *cobra.Command) {
	full = &cobra.Command{Use: "sync"}
	alwaysOn = &cobra.Command{Use: "sync"}
	chosen = wireSyncCommands(syncCLIEnabled, full, alwaysOn,
		&cobra.Command{Use: "enable"},
		&cobra.Command{Use: "disable"},
		&cobra.Command{Use: "status"},
	)
	return chosen, full, alwaysOn
}

// alwaysReachableSyncSubcommands are the subcommands that must stay reachable
// no matter how the SyncCLI gate resolves: the kill-switch pair (a user who
// disabled sync must be able to re-enable it) and the read-only diagnostic.
var alwaysReachableSyncSubcommands = []string{"status", "enable", "disable"}

// TestSyncStatusReachableWhenSyncCLIOff asserts that when SyncCLI is OFF the
// always-on parent carries status/enable/disable and the full parent carries
// none of them.
//
// This drives wireSyncCommands directly instead of inspecting the package
// command tree. The tree is built by init() (cmd/sync.go) from the *ambient
// process* env before any test runs, so t.Setenv cannot move it and an
// observational test silently changes meaning with the developer's shell: with
// TD_FEATURE_SYNC_CLI=1 exported — which CLAUDE.md recommends putting in
// ~/.zshenv, so it is set in every agent subshell — it would assert the
// SyncCLI-ON path and never check the case it is named for. Passing the gate as
// a parameter makes the off-branch verifiable in every environment. (td-6fda71)
func TestSyncStatusReachableWhenSyncCLIOff(t *testing.T) {
	chosen, full, alwaysOn := wireSyncCommandsForTest(false)

	if chosen != alwaysOn {
		t.Fatal("SyncCLI off must select the always-on sync parent")
	}
	for _, name := range alwaysReachableSyncSubcommands {
		if !hasSubcommand(alwaysOn, name) {
			t.Errorf("expected `%s` on the always-on sync parent (reachable when SyncCLI is off)", name)
		}
		if hasSubcommand(full, name) {
			t.Errorf("`%s` must not also land on the full sync parent when SyncCLI is off", name)
		}
	}
}

// TestSyncStatusReachableWhenSyncCLIOn is the mirror case: with the gate on,
// the same subcommands live under the full sync parent and the always-on parent
// stays empty (it is not registered on root at all in that configuration).
func TestSyncStatusReachableWhenSyncCLIOn(t *testing.T) {
	chosen, full, alwaysOn := wireSyncCommandsForTest(true)

	if chosen != full {
		t.Fatal("SyncCLI on must select the full sync parent")
	}
	for _, name := range alwaysReachableSyncSubcommands {
		if !hasSubcommand(full, name) {
			t.Errorf("expected `%s` on the full sync parent (SyncCLI on)", name)
		}
		if hasSubcommand(alwaysOn, name) {
			t.Errorf("`%s` must not also land on the always-on sync parent when SyncCLI is on", name)
		}
	}
}

// TestSyncStatusRegistrationMutuallyExclusive guards the tree this process
// actually built: `status` must live under exactly one of the two parents,
// never both (double-registration would leave it with the wrong parent) and
// never neither (unreachable). This holds whichever way the ambient gate
// resolved, so it needs no env control.
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

// TestSyncCommandsWiredFromProcessGate ties the pure wiring function back to
// the real command tree: init() must have wired the live parents through
// wireSyncCommands with the process gate, so the parent selected by the ambient
// gate is the one carrying the real subcommands.
func TestSyncCommandsWiredFromProcessGate(t *testing.T) {
	parent, label := syncCmd, "full sync parent (SyncCLI on)"
	if !features.IsEnabledForProcess(features.SyncCLI.Name) {
		parent, label = syncAlwaysOnCmd, "always-on sync parent (SyncCLI off)"
	}
	for _, name := range alwaysReachableSyncSubcommands {
		if !hasSubcommand(parent, name) {
			t.Errorf("expected `%s` on the %s", name, label)
		}
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
