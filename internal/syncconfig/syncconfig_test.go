package syncconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotThresholdDefault(t *testing.T) {
	// Clear any env var that might be set
	os.Unsetenv("TD_SYNC_SNAPSHOT_THRESHOLD")

	threshold := GetSnapshotThreshold()
	if threshold != 100 {
		t.Fatalf("default threshold: got %d, want 100", threshold)
	}
}

func TestSnapshotThresholdEnvVar(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "500")

	threshold := GetSnapshotThreshold()
	if threshold != 500 {
		t.Fatalf("env threshold: got %d, want 500", threshold)
	}
}

func TestSnapshotThresholdEnvVarInvalid(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "not-a-number")

	// Invalid env should fall through to default
	threshold := GetSnapshotThreshold()
	if threshold != 100 {
		t.Fatalf("invalid env threshold: got %d, want 100 (default)", threshold)
	}
}

func TestSnapshotThresholdEnvVarZero(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "0")

	// Zero is valid: means snapshot bootstrap is disabled
	threshold := GetSnapshotThreshold()
	if threshold != 0 {
		t.Fatalf("zero env threshold: got %d, want 0 (disabled)", threshold)
	}
}

func TestSnapshotThresholdEnvVarNegative(t *testing.T) {
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "-5")

	// Negative should fall through to default
	threshold := GetSnapshotThreshold()
	if threshold != 100 {
		t.Fatalf("negative env threshold: got %d, want 100 (default)", threshold)
	}
}

func TestSnapshotThresholdEnvOverridesConfig(t *testing.T) {
	// Even if config has a value, env should take precedence
	t.Setenv("TD_SYNC_SNAPSHOT_THRESHOLD", "42")

	threshold := GetSnapshotThreshold()
	if threshold != 42 {
		t.Fatalf("env override: got %d, want 42", threshold)
	}
}

// writeTestConfig creates a temp HOME with ~/.config/td/config.json and returns cleanup.
func writeTestConfig(t *testing.T, cfg *Config) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dir := filepath.Join(tmpDir, ".config", "td")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestAutoSyncEnabledFromConfig(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{Enabled: boolPtr(false)}}})
	t.Setenv("TD_SYNC_AUTO", "")
	if GetAutoSyncEnabled() {
		t.Error("expected auto-sync disabled from config")
	}
}

func TestAutoSyncOnStartFromConfig(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{OnStart: boolPtr(false)}}})
	t.Setenv("TD_SYNC_AUTO_START", "")
	if GetAutoSyncOnStart() {
		t.Error("expected on_start disabled from config")
	}
}

func TestAutoSyncDebounceFromConfig(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{Debounce: "10s"}}})
	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "")
	if d := GetAutoSyncDebounce(); d != 10*time.Second {
		t.Errorf("expected 10s from config, got %v", d)
	}
}

func TestAutoSyncIntervalFromConfig(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{Interval: "15m"}}})
	t.Setenv("TD_SYNC_AUTO_INTERVAL", "")
	if d := GetAutoSyncInterval(); d != 15*time.Minute {
		t.Errorf("expected 15m from config, got %v", d)
	}
}

func TestAutoSyncPullFromConfig(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{Pull: boolPtr(false)}}})
	t.Setenv("TD_SYNC_AUTO_PULL", "")
	if GetAutoSyncPull() {
		t.Error("expected pull disabled from config")
	}
}

func TestAutoSyncEnvOverridesConfig(t *testing.T) {
	// Config says disabled, env says enabled — env should win
	writeTestConfig(t, &Config{Sync: SyncConfig{Auto: AutoSyncConfig{
		Enabled:  boolPtr(false),
		OnStart:  boolPtr(false),
		Debounce: "10s",
		Interval: "15m",
		Pull:     boolPtr(false),
	}}})

	t.Setenv("TD_SYNC_AUTO", "true")
	if !GetAutoSyncEnabled() {
		t.Error("env should override config for enabled")
	}

	t.Setenv("TD_SYNC_AUTO_START", "1")
	if !GetAutoSyncOnStart() {
		t.Error("env should override config for on_start")
	}

	t.Setenv("TD_SYNC_AUTO_DEBOUNCE", "500ms")
	if d := GetAutoSyncDebounce(); d != 500*time.Millisecond {
		t.Errorf("env should override config for debounce, got %v", d)
	}

	t.Setenv("TD_SYNC_AUTO_INTERVAL", "30s")
	if d := GetAutoSyncInterval(); d != 30*time.Second {
		t.Errorf("env should override config for interval, got %v", d)
	}

	t.Setenv("TD_SYNC_AUTO_PULL", "true")
	if !GetAutoSyncPull() {
		t.Error("env should override config for pull")
	}
}

// --- Global autosync override (td-735875) ---

// clearGlobalOverrideEnv unsets the env vars consulted by
// GetGlobalAutosyncOverride so config-only / default cases are not perturbed by
// ambient TD_* values in the test environment.
func clearGlobalOverrideEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO", "")
}

func TestGlobalAutosyncOverrideAbsent(t *testing.T) {
	writeTestConfig(t, &Config{})
	clearGlobalOverrideEnv(t)
	if v := GetGlobalAutosyncOverride(); v != nil {
		t.Fatalf("absent override: got %v, want nil", *v)
	}
}

func TestGlobalAutosyncOverrideConfigTrue(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Autosync: boolPtr(true)}})
	clearGlobalOverrideEnv(t)
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != true {
		t.Fatalf("config true: got %v, want true", v)
	}
}

func TestGlobalAutosyncOverrideConfigFalse(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Autosync: boolPtr(false)}})
	clearGlobalOverrideEnv(t)
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != false {
		t.Fatalf("config false: got %v, want false", v)
	}
}

func TestGlobalAutosyncOverrideEnvTrue(t *testing.T) {
	writeTestConfig(t, &Config{})
	t.Setenv("TD_SYNC_AUTO", "")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "true")
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != true {
		t.Fatalf("env true: got %v, want true", v)
	}
}

func TestGlobalAutosyncOverrideEnvFalse(t *testing.T) {
	writeTestConfig(t, &Config{})
	t.Setenv("TD_SYNC_AUTO", "")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "false")
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != false {
		t.Fatalf("env false: got %v, want false", v)
	}
}

func TestGlobalAutosyncOverrideEnvOverridesConfig(t *testing.T) {
	// config says false, env says true -> env wins
	writeTestConfig(t, &Config{Sync: SyncConfig{Autosync: boolPtr(false)}})
	t.Setenv("TD_SYNC_AUTO", "")
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "true")
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != true {
		t.Fatalf("env-over-config: got %v, want true", v)
	}
}

func TestGlobalAutosyncOverrideTDSyncAutoEnv(t *testing.T) {
	// TD_SYNC_AUTO also feeds the override (secondary precedence).
	writeTestConfig(t, &Config{})
	t.Setenv("TD_FEATURE_SYNC_AUTOSYNC", "")
	t.Setenv("TD_SYNC_AUTO", "false")
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != false {
		t.Fatalf("TD_SYNC_AUTO env: got %v, want false", v)
	}
}

// TestGlobalAutosyncOverrideLegacyEnabledNoSilentKill is the critical migration
// guard: a config carrying the LEGACY sync.enabled=false (with the new
// sync.autosync field absent) must NOT produce a global kill — the override
// must resolve to nil so per-project gating decides.
func TestGlobalAutosyncOverrideLegacyEnabledNoSilentKill(t *testing.T) {
	writeTestConfig(t, &Config{Sync: SyncConfig{Enabled: false}})
	clearGlobalOverrideEnv(t)
	if v := GetGlobalAutosyncOverride(); v != nil {
		t.Fatalf("legacy enabled:false must not set override: got %v, want nil", *v)
	}
}

func TestSetGlobalAutosyncOverrideRoundTrip(t *testing.T) {
	writeTestConfig(t, &Config{})
	clearGlobalOverrideEnv(t)

	if err := SetGlobalAutosyncOverride(false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	v := GetGlobalAutosyncOverride()
	if v == nil || *v != false {
		t.Fatalf("after set false: got %v, want false", v)
	}

	if err := SetGlobalAutosyncOverride(true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	v = GetGlobalAutosyncOverride()
	if v == nil || *v != true {
		t.Fatalf("after set true: got %v, want true", v)
	}

	// Setting the override must not have touched the legacy enabled field.
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Sync.Autosync == nil || *cfg.Sync.Autosync != true {
		t.Fatalf("persisted autosync: got %v, want true", cfg.Sync.Autosync)
	}
}
