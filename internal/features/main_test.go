package features

import (
	"os"
	"strings"
	"testing"
)

// TestMain isolates the feature-flag tests from the developer's shell
// environment. Feature resolution reads process env vars (TD_FEATURE_*,
// TD_ENABLE_FEATURE(S), TD_DISABLE_FEATURE(S), TD_DISABLE_EXPERIMENTAL); a
// developer who exports any of these (e.g. TD_FEATURE_SYNC_CLI=1) would
// otherwise see spurious failures in tests that assume a clean baseline.
// Clearing them here means the tests depend only on values they set via
// t.Setenv, regardless of the ambient environment.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if isFeatureEnvKey(key) {
			_ = os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}

func isFeatureEnvKey(key string) bool {
	switch key {
	case "TD_ENABLE_FEATURE", "TD_ENABLE_FEATURES",
		"TD_DISABLE_FEATURE", "TD_DISABLE_FEATURES",
		"TD_DISABLE_EXPERIMENTAL":
		return true
	}
	return strings.HasPrefix(key, "TD_FEATURE_")
}
