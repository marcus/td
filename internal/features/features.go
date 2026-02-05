package features

import (
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/marcus/td/internal/config"
)

// Feature describes a named feature flag.
type Feature struct {
	Name        string
	Default     bool
	Description string
}

var (
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
		Default:     false,
		Description: "Enable sync transport for notes entities",
	}
)

var allFeatures = []Feature{
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
