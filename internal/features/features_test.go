package features

import (
	"testing"

	"github.com/marcus/td/internal/config"
)

func TestKnownFeatureDefaults(t *testing.T) {
	for _, feature := range ListAll() {
		if IsEnabledForProcess(feature.Name) != feature.Default {
			t.Fatalf("default mismatch for %s", feature.Name)
		}
	}
}

func TestIsEnabledForProcess_EnvVarOverride(t *testing.T) {
	t.Setenv("TD_FEATURE_SYNC_CLI", "true")
	if !IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("TD_FEATURE_SYNC_CLI=true should enable sync_cli")
	}

	t.Setenv("TD_FEATURE_SYNC_CLI", "false")
	if IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("TD_FEATURE_SYNC_CLI=false should disable sync_cli")
	}
}

func TestIsEnabledForProcess_EnableDisableLists(t *testing.T) {
	t.Setenv("TD_ENABLE_FEATURE", "sync_cli,sync_monitor_prompt")
	if !IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("TD_ENABLE_FEATURE should enable sync_cli")
	}
	if !IsEnabledForProcess(SyncMonitorPrompt.Name) {
		t.Fatal("TD_ENABLE_FEATURE should enable sync_monitor_prompt")
	}

	t.Setenv("TD_DISABLE_FEATURE", "sync_cli")
	if IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("TD_DISABLE_FEATURE should take precedence and disable sync_cli")
	}
}

func TestIsEnabled_ProjectConfigAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()

	if err := config.SetFeatureFlag(dir, SyncCLI.Name, true); err != nil {
		t.Fatalf("SetFeatureFlag failed: %v", err)
	}
	enabled, source := Resolve(dir, SyncCLI.Name)
	if !enabled || source != "config" {
		t.Fatalf("expected config=true, got enabled=%v source=%q", enabled, source)
	}

	t.Setenv("TD_FEATURE_SYNC_CLI", "false")
	enabled, source = Resolve(dir, SyncCLI.Name)
	if enabled || source != "env" {
		t.Fatalf("expected env=false to override config, got enabled=%v source=%q", enabled, source)
	}
}

func TestDisableExperimentalKillSwitch(t *testing.T) {
	t.Setenv("TD_ENABLE_FEATURE", "sync_cli")
	if !IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("expected sync_cli enabled before kill-switch")
	}

	t.Setenv("TD_DISABLE_EXPERIMENTAL", "1")
	if IsEnabledForProcess(SyncCLI.Name) {
		t.Fatal("kill-switch should disable sync_cli")
	}
	if IsEnabledForProcess(SyncAutosync.Name) {
		t.Fatal("kill-switch should disable sync_autosync")
	}
}

func TestSyncGateMapReferencesKnownFeatures(t *testing.T) {
	if len(SyncGateMap) == 0 {
		t.Fatal("SyncGateMap should not be empty")
	}
	for _, entry := range SyncGateMap {
		if !IsKnownFeature(entry.Feature) {
			t.Fatalf("unknown feature in SyncGateMap: %s (%s)", entry.Feature, entry.Surface)
		}
		if entry.Surface == "" {
			t.Fatalf("empty surface for feature %s", entry.Feature)
		}
	}
}
