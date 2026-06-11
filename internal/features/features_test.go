package features

import (
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/reviewpolicy"
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

func TestResolveReviewPolicyMode_DefaultsToTrusted(t *testing.T) {
	// trusted-review-mode final stage: with no explicit config (neither
	// review_policy_mode nor balanced_review_policy set), the resolver
	// returns ModeTrusted. BalancedReviewPolicy.Default stays false and the
	// legacy flag is only honored when explicitly opted into.
	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "")
	ResetDeprecationWarningsForTests()
	dir := t.TempDir()
	mode, err := ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeTrusted {
		t.Fatalf("default mode: got %q, want %q", mode, reviewpolicy.ModeTrusted)
	}
}

func TestResolveReviewPolicyMode_EnvOverride(t *testing.T) {
	ResetDeprecationWarningsForTests()
	dir := t.TempDir()

	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "delegated")
	mode, err := ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeDelegated {
		t.Fatalf("env mode: got %q, want %q", mode, reviewpolicy.ModeDelegated)
	}

	t.Setenv("TD_FEATURE_REVIEW_POLICY_MODE", "bogus")
	if _, err := ResolveReviewPolicyMode(dir); err == nil {
		t.Fatal("invalid env value should error")
	}
}

func TestResolveReviewPolicyMode_ConfigValue(t *testing.T) {
	ResetDeprecationWarningsForTests()
	dir := t.TempDir()

	if err := config.SetFeatureStringFlag(dir, ReviewPolicyMode, "balanced"); err != nil {
		t.Fatalf("SetFeatureStringFlag: %v", err)
	}

	mode, err := ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeBalanced {
		t.Fatalf("config mode: got %q, want %q", mode, reviewpolicy.ModeBalanced)
	}
}

func TestResolveReviewPolicyMode_LegacyBalancedMapping(t *testing.T) {
	ResetDeprecationWarningsForTests()
	dir := t.TempDir()

	// balanced_review_policy explicitly set to true, no review_policy_mode set.
	if err := config.SetFeatureFlag(dir, BalancedReviewPolicy.Name, true); err != nil {
		t.Fatalf("SetFeatureFlag: %v", err)
	}

	mode, err := ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeBalanced {
		t.Fatalf("legacy true mapping: got %q, want %q", mode, reviewpolicy.ModeBalanced)
	}

	// Now flip it false — should map to strict.
	if err := config.SetFeatureFlag(dir, BalancedReviewPolicy.Name, false); err != nil {
		t.Fatalf("SetFeatureFlag: %v", err)
	}
	mode, err = ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("ResolveReviewPolicyMode: %v", err)
	}
	if mode != reviewpolicy.ModeStrict {
		t.Fatalf("legacy false mapping: got %q, want %q", mode, reviewpolicy.ModeStrict)
	}
}

func TestResolveReviewPolicyMode_ConflictingFlags(t *testing.T) {
	ResetDeprecationWarningsForTests()
	dir := t.TempDir()

	// Explicit conflict: review_policy_mode=delegated but balanced_review_policy=true.
	if err := config.SetFeatureStringFlag(dir, ReviewPolicyMode, "delegated"); err != nil {
		t.Fatalf("SetFeatureStringFlag: %v", err)
	}
	if err := config.SetFeatureFlag(dir, BalancedReviewPolicy.Name, true); err != nil {
		t.Fatalf("SetFeatureFlag: %v", err)
	}

	if _, err := ResolveReviewPolicyMode(dir); err == nil {
		t.Fatal("conflicting review_policy_mode=delegated vs balanced_review_policy=true should error")
	}

	// Non-conflict: balanced_review_policy=true and review_policy_mode=balanced.
	// Both point at balanced so resolution should succeed.
	if err := config.SetFeatureStringFlag(dir, ReviewPolicyMode, "balanced"); err != nil {
		t.Fatalf("SetFeatureStringFlag: %v", err)
	}
	mode, err := ResolveReviewPolicyMode(dir)
	if err != nil {
		t.Fatalf("agreeing flags should not error: %v", err)
	}
	if mode != reviewpolicy.ModeBalanced {
		t.Fatalf("agreeing flags: got %q, want %q", mode, reviewpolicy.ModeBalanced)
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
