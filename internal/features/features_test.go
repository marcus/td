package features

import (
	"testing"

	"github.com/marcus/td/internal/config"
)

func resetFeatureEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"TD_DISABLE_EXPERIMENTAL",
		"TD_ENABLE_FEATURE",
		"TD_ENABLE_FEATURES",
		"TD_DISABLE_FEATURE",
		"TD_DISABLE_FEATURES",
		"TD_FEATURE_BALANCED_REVIEW_POLICY",
		"TD_FEATURE_SYNC_AUTOSYNC",
		"TD_FEATURE_SYNC_CLI",
		"TD_FEATURE_SYNC_MONITOR_PROMPT",
		"TD_FEATURE_SYNC_NOTES",
	} {
		t.Setenv(key, "")
	}
}

func TestKnownFeatureDefaults(t *testing.T) {
	resetFeatureEnv(t)

	for _, feature := range ListAll() {
		if IsEnabledForProcess(feature.Name) != feature.Default {
			t.Fatalf("default mismatch for %s", feature.Name)
		}
	}
}

func TestIsEnabledForProcess_EnvVarOverride(t *testing.T) {
	resetFeatureEnv(t)

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
	resetFeatureEnv(t)

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
	resetFeatureEnv(t)

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
	resetFeatureEnv(t)

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
	resetFeatureEnv(t)

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
