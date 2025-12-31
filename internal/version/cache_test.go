package version

import (
	"testing"
	"time"
)

func TestIsCacheValid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		entry          *CacheEntry
		currentVersion string
		want           bool
	}{
		{
			name:           "nil entry",
			entry:          nil,
			currentVersion: "v1.0.0",
			want:           false,
		},
		{
			name: "valid cache - same version, recent",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now,
				HasUpdate:      true,
			},
			currentVersion: "v1.0.0",
			want:           true,
		},
		{
			name: "expired cache - same version, old timestamp",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now.Add(-7 * time.Hour), // older than 6h TTL
				HasUpdate:      true,
			},
			currentVersion: "v1.0.0",
			want:           false,
		},
		{
			name: "invalid cache - version mismatch (upgrade)",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now,
				HasUpdate:      true,
			},
			currentVersion: "v1.1.0",
			want:           false,
		},
		{
			name: "invalid cache - version mismatch (downgrade)",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now,
				HasUpdate:      true,
			},
			currentVersion: "v0.9.0",
			want:           false,
		},
		{
			name: "boundary - just under TTL",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now.Add(-6*time.Hour + time.Minute),
				HasUpdate:      true,
			},
			currentVersion: "v1.0.0",
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCacheValid(tt.entry, tt.currentVersion)
			if got != tt.want {
				t.Errorf("IsCacheValid() = %v, want %v", got, tt.want)
			}
		})
	}
}
