package version

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		// Standard semver formats
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"v0.1.0", [3]int{0, 1, 0}},
		{"0.1.0", [3]int{0, 1, 0}},

		// Prerelease versions (prerelease removed, core version extracted)
		{"v1.0.0-beta", [3]int{1, 0, 0}},
		{"v1.0.0-alpha", [3]int{1, 0, 0}},
		{"v2.0.0-rc.1", [3]int{2, 0, 0}},
		{"1.0.0-beta.2", [3]int{1, 0, 0}},

		// Build metadata (build info removed, core version extracted)
		{"v1.0.0+build123", [3]int{1, 0, 0}},
		{"v1.0.0+20130313144700", [3]int{1, 0, 0}},
		{"1.0.0+exp.sha.5114f85", [3]int{1, 0, 0}},

		// Combined prerelease and build metadata
		{"v1.0.0-beta+build123", [3]int{1, 0, 0}},

		// Incomplete versions (defaults missing parts to 0)
		{"2.0", [3]int{2, 0, 0}},
		{"1", [3]int{1, 0, 0}},
		{"v5", [3]int{5, 0, 0}},

		// Edge cases: empty or invalid
		{"", [3]int{0, 0, 0}},
		{"invalid", [3]int{0, 0, 0}},
		{"no.numbers.here", [3]int{0, 0, 0}},

		// Large version numbers
		{"v99.99.99", [3]int{99, 99, 99}},
		{"1000.0.0", [3]int{1000, 0, 0}},

		// Weird but valid formats
		{"v0.0.0", [3]int{0, 0, 0}},
		{"0.0.1", [3]int{0, 0, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSemver(tt.input)
			if got != tt.expected {
				t.Errorf("parseSemver(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest   string
		current  string
		expected bool
	}{
		// Major version differences
		{"v1.0.0", "v0.9.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v10.0.0", "v9.9.9", true},

		// Minor version differences
		{"v0.2.0", "v0.1.0", true},
		{"v1.5.0", "v1.4.9", true},
		{"v0.10.0", "v0.9.0", true},

		// Patch version differences
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.1", "v1.0.0", true},
		{"v0.1.10", "v0.1.9", true},

		// Equal versions
		{"v0.1.0", "v0.1.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.2.3", "v1.2.3", false},

		// Current version newer (should be false)
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v0.0.1", "v0.0.2", false},

		// Prerelease handling (same core version, ignoring prerelease)
		// When core versions are the same, neither is "newer"
		{"v1.0.0-beta", "v1.0.0", false}, // prerelease vs final (same core)
		{"v1.0.0", "v1.0.0-beta", false},  // final vs prerelease (same core - not newer)
		{"v2.0.0-rc.1", "v1.9.9", true},

		// Build metadata handling (build metadata ignored)
		{"v1.0.0+build1", "v1.0.0+build2", false},
		{"v1.0.0+a", "v0.9.9", true},

		// Prefix variations with mixed formats
		{"1.0.0", "v0.9.9", true},
		{"v1.0.0", "0.9.9", true},
		{"1.0.0", "0.9.9", true},

		// Zero versions
		{"v0.0.0", "v0.0.0", false},
		{"v0.0.1", "v0.0.0", true},

		// Large numbers
		{"v99.0.0", "v98.99.99", true},
		{"v1.100.0", "v1.99.99", true},
	}

	for _, tt := range tests {
		name := tt.latest + "_vs_" + tt.current
		t.Run(name, func(t *testing.T) {
			got := isNewer(tt.latest, tt.current)
			if got != tt.expected {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.expected)
			}
		})
	}
}
