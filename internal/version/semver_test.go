package version

import "testing"

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2.3", [3]int{1, 2, 3}},
		{"v0.1.0", [3]int{0, 1, 0}},
		{"v1.0.0-beta", [3]int{1, 0, 0}},
		{"2.0", [3]int{2, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"v1.0.0+build123", [3]int{1, 0, 0}},
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
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.0.1", "v0.0.0", true},
		{"v2.0.0", "v1.9.9", true},
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
