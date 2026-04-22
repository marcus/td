package features

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/reviewpolicy"
)

// Feature describes a named feature flag.
type Feature struct {
	Name        string
	Default     bool
	Description string
}

var (
	// BalancedReviewPolicy allows creator-only approval for issues implemented by
	// a different session, while still blocking implementer/self approval.
	//
	// DEPRECATED (Step 5): prefer review_policy_mode=balanced. The flag remains
	// registered so existing configs do not error at load time. When explicitly
	// set, it still maps to balanced mode (Default=false means an untouched
	// config does NOT trip the deprecation warning). The flag will be removed
	// in a future release.
	BalancedReviewPolicy = Feature{
		Name:        "balanced_review_policy",
		Default:     false,
		Description: "DEPRECATED: use review_policy_mode=balanced. Allows creator-only approvals for externally implemented issues.",
	}

	// SyncCLI gates user-facing sync/auth commands.
	SyncCLI = Feature{
		Name:        "sync_cli",
		Default:     false,
		Description: "Enable sync/auth CLI commands for end users",
	}

	// SyncAutosync gates startup/post-mutation/monitor autosync behavior.
	SyncAutosync = Feature{
		Name:        "sync_autosync",
		Default:     false,
		Description: "Enable background autosync hooks",
	}

	// SyncMonitorPrompt gates the monitor sync prompt UX.
	SyncMonitorPrompt = Feature{
		Name:        "sync_monitor_prompt",
		Default:     false,
		Description: "Enable monitor sync setup prompt",
	}

	// SyncNotes gates notes entity sync for sidecar notes plugin rollout.
	SyncNotes = Feature{
		Name:        "sync_notes",
		Default:     true,
		Description: "Enable sync transport for notes entities",
	}
)

// ReviewPolicyMode is the string-valued feature name that selects between
// strict | balanced | delegated review policies.
//
// Default: delegated (Step 5). Prior to Step 5 the effective default was
// balanced via the legacy BalancedReviewPolicy=true feature flag. Step 5
// flipped BalancedReviewPolicy.Default to false AND the resolver default to
// delegated — a fresh install with no explicit configuration now runs the
// review-attestation flow, where reviewer-independence is the core rule and
// creator/implementer/reviewer/review-requester roles may all close once an
// independent review has been recorded.
//
// Implementation note (Option B): the existing Feature struct is boolean-
// typed, and retrofitting it with a union type would ripple through
// ListAll / IsKnownFeature / defaultValues / env-override logic. The
// less invasive path is to keep Feature unchanged and add a parallel
// string-feature resolver. This keeps the bool-feature surface simple
// and opts string features in one-by-one. If a second string feature ever
// shows up, this should be generalized.
const (
	ReviewPolicyMode        = "review_policy_mode"
	ReviewPolicyModeDefault = string(reviewpolicy.ModeDelegated)
)

// balancedReviewPolicyDeprecatedLogged fires the legacy-mapping deprecation
// warning exactly once per process. Tests can reset it via
// ResetDeprecationWarningsForTests to exercise the warning path repeatedly.
var balancedReviewPolicyDeprecatedLogged sync.Once

// ResetDeprecationWarningsForTests clears the once-only deprecation
// warning latches. Intended for use from tests in this package only.
func ResetDeprecationWarningsForTests() {
	balancedReviewPolicyDeprecatedLogged = sync.Once{}
}

var allFeatures = []Feature{
	BalancedReviewPolicy,
	SyncAutosync,
	SyncCLI,
	SyncMonitorPrompt,
	SyncNotes,
}

var defaultValues = buildDefaultMap()

func buildDefaultMap() map[string]bool {
	values := make(map[string]bool, len(allFeatures))
	for _, feature := range allFeatures {
		values[feature.Name] = feature.Default
	}
	return values
}

// ListAll returns all known features.
func ListAll() []Feature {
	items := make([]Feature, len(allFeatures))
	copy(items, allFeatures)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

// IsKnownFeature returns true when the feature exists in the registry.
func IsKnownFeature(name string) bool {
	_, ok := defaultValues[normalizeName(name)]
	return ok
}

// IsEnabled resolves a feature using env overrides, then project config, then defaults.
func IsEnabled(baseDir, name string) bool {
	enabled, _ := Resolve(baseDir, name)
	return enabled
}

// IsEnabledForProcess resolves a feature using env overrides then defaults.
// Useful during command registration when project config may not be available.
func IsEnabledForProcess(name string) bool {
	canonical := normalizeName(name)
	if enabled, ok := resolveEnvOverride(canonical); ok {
		return enabled
	}
	return getDefault(canonical)
}

// Resolve returns the resolved feature state and the source ("env", "config", "default").
func Resolve(baseDir, name string) (bool, string) {
	canonical := normalizeName(name)

	if enabled, ok := resolveEnvOverride(canonical); ok {
		return enabled, "env"
	}

	if baseDir != "" {
		cfg, err := config.Load(baseDir)
		if err == nil && cfg.FeatureFlags != nil {
			if enabled, ok := cfg.FeatureFlags[canonical]; ok {
				return enabled, "config"
			}
		}
	}

	return getDefault(canonical), "default"
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func getDefault(name string) bool {
	if enabled, ok := defaultValues[name]; ok {
		return enabled
	}
	return false
}

func resolveEnvOverride(name string) (bool, bool) {
	// Emergency kill-switch for all experimental features.
	if disabled, ok := parseBoolEnv("TD_DISABLE_EXPERIMENTAL"); ok && disabled {
		return false, true
	}

	featureVar := "TD_FEATURE_" + normalizeForEnvKey(name)
	if enabled, ok := parseBoolEnv(featureVar); ok {
		return enabled, true
	}

	if containsFeatureName(os.Getenv("TD_DISABLE_FEATURE"), name) ||
		containsFeatureName(os.Getenv("TD_DISABLE_FEATURES"), name) {
		return false, true
	}
	if containsFeatureName(os.Getenv("TD_ENABLE_FEATURE"), name) ||
		containsFeatureName(os.Getenv("TD_ENABLE_FEATURES"), name) {
		return true, true
	}

	return false, false
}

func normalizeForEnvKey(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range upper {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func parseBoolEnv(key string) (bool, bool) {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	default:
		return false, false
	}
}

// ResolveReviewPolicyMode returns the effective review-policy mode for the
// given project baseDir. Resolution order (Step 5):
//  1. TD_FEATURE_REVIEW_POLICY_MODE env var (explicit) -> use it
//  2. config.FeatureStringFlags["review_policy_mode"] (explicit) -> use it
//  3. legacy balanced_review_policy EXPLICITLY set (env or config, NOT
//     default) -> map true->balanced / false->strict AND emit a one-time
//     deprecation warning.
//  4. default: delegated.
//
// Key difference from Step 1-4 behavior: the legacy flag's *default* value
// no longer feeds into mode resolution. Only an explicit set triggers the
// compat mapping. This is the "flip" — users who never touched either flag
// now get delegated mode instead of balanced.
//
// Conflict detection: if BOTH review_policy_mode AND balanced_review_policy
// are set EXPLICITLY (env or config, not default) to values that disagree,
// refuse to resolve rather than pick a silent winner.
func ResolveReviewPolicyMode(baseDir string) (reviewpolicy.Mode, error) {
	envKey := "TD_FEATURE_" + normalizeForEnvKey(ReviewPolicyMode)
	envRaw := strings.TrimSpace(os.Getenv(envKey))

	var (
		configMode    string
		configModeSet bool
	)
	if baseDir != "" {
		v, ok, err := config.GetFeatureStringFlag(baseDir, ReviewPolicyMode)
		if err == nil && ok {
			configMode = v
			configModeSet = true
		}
	}

	// Conflict detection requires knowing whether the LEGACY flag was set
	// explicitly — a default legacy value should not fight with an
	// explicit review_policy_mode.
	legacyExplicit, legacyExplicitSet := explicitBalancedReviewPolicyValue(baseDir)

	explicitMode := ""
	if envRaw != "" {
		explicitMode = envRaw
	} else if configModeSet {
		explicitMode = configMode
	}
	if legacyExplicitSet && explicitMode != "" {
		legacyMapped := legacyBalancedToMode(legacyExplicit)
		if legacyMapped != reviewpolicy.Mode(explicitMode) {
			return "", fmt.Errorf(
				"conflicting review policy settings: review_policy_mode=%q vs balanced_review_policy=%t "+
					"(balanced_review_policy is deprecated; unset one of the two)",
				explicitMode, legacyExplicit)
		}
	}

	// 1. env wins.
	if envRaw != "" {
		mode, err := reviewpolicy.ParseMode(envRaw)
		if err != nil {
			return "", fmt.Errorf("%s=%s invalid: %w", envKey, envRaw, err)
		}
		return mode, nil
	}

	// 2. config explicit.
	if configModeSet {
		mode, err := reviewpolicy.ParseMode(configMode)
		if err != nil {
			return "", fmt.Errorf("config review_policy_mode=%q invalid: %w", configMode, err)
		}
		return mode, nil
	}

	// 3. Legacy compat (Step 5): only honor the legacy flag when it was set
	// explicitly. Default-valued legacy does NOT feed mode resolution anymore,
	// so an untouched config falls through to step 4 (delegated).
	if legacyExplicitSet {
		balancedReviewPolicyDeprecatedLogged.Do(func() {
			slog.Warn("balanced_review_policy is deprecated; use review_policy_mode="+
				string(legacyBalancedToMode(legacyExplicit))+" instead. This flag will be removed in a future release.",
				"mapped_to", string(legacyBalancedToMode(legacyExplicit)))
		})
		return legacyBalancedToMode(legacyExplicit), nil
	}

	// 4. Default: delegated.
	return reviewpolicy.ModeDelegated, nil
}

// legacyBalancedToMode maps the deprecated balanced_review_policy boolean.
func legacyBalancedToMode(enabled bool) reviewpolicy.Mode {
	if enabled {
		return reviewpolicy.ModeBalanced
	}
	return reviewpolicy.ModeStrict
}

// explicitBalancedReviewPolicyValue reports the balanced_review_policy flag
// if (and only if) it was explicitly set by env or project config. The
// feature's default value does not count as "explicit" — see plan under
// "Feature Flag and Compatibility Plan".
func explicitBalancedReviewPolicyValue(baseDir string) (bool, bool) {
	canonical := normalizeName(BalancedReviewPolicy.Name)
	if v, ok := resolveEnvOverride(canonical); ok {
		return v, true
	}
	if baseDir == "" {
		return false, false
	}
	cfg, err := config.Load(baseDir)
	if err != nil || cfg.FeatureFlags == nil {
		return false, false
	}
	v, ok := cfg.FeatureFlags[canonical]
	return v, ok
}

func containsFeatureName(raw, target string) bool {
	if raw == "" {
		return false
	}
	target = normalizeName(target)
	for _, item := range strings.Split(raw, ",") {
		if normalizeName(item) == target {
			return true
		}
	}
	return false
}
