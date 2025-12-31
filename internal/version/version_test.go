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
	cmd := UpdateCommand("v1.2.3")
	expected := `go install -ldflags "-X main.Version=v1.2.3" github.com/marcus/td@v1.2.3`
	if cmd != expected {
		t.Errorf("UpdateCommand = %q, want %q", cmd, expected)
	}
}
