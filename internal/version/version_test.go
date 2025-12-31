package version

import "testing"

func TestIsDevelopmentVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", true},
		{"unknown", true},
		{"dev", true},
		{"devel", true},
		{"devel+abc123", true},
		{"devel+abc+dirty", true},
		{"v0.1.0", false},
		{"0.1.0", false},
		{"1.0.0-beta", false},
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
		{"v1.2.3", `go install -ldflags "-X main.Version=v1.2.3" github.com/marcus/td@v1.2.3`},
		{"1.2.3", `go install -ldflags "-X main.Version=1.2.3" github.com/marcus/td@1.2.3`},
		{"v0.3.0-beta", `go install -ldflags "-X main.Version=v0.3.0-beta" github.com/marcus/td@v0.3.0-beta`},
		{"v1.0.0-rc.1", `go install -ldflags "-X main.Version=v1.0.0-rc.1" github.com/marcus/td@v1.0.0-rc.1`},
		// Invalid versions should return empty string (shell injection protection)
		{"", ""},
		{"invalid", ""},
		{`"; rm -rf /`, ""},
		{"v1.2.3; echo pwned", ""},
		{"v1.2.3$(whoami)", ""},
		{"../../../etc/passwd", ""},
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
