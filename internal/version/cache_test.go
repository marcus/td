package version

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		{
			name: "boundary - exactly at TTL expiration",
			entry: &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now.Add(-6 * time.Hour),
				HasUpdate:      true,
			},
			currentVersion: "v1.0.0",
			want:           false,
		},
		{
			name: "no update available, cache valid",
			entry: &CacheEntry{
				LatestVersion:  "v1.0.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      now,
				HasUpdate:      false,
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

func TestSaveAndLoadCache(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Set HOME to temp directory so cachePath returns our test path
	os.Setenv("HOME", tmpDir)

	tests := []struct {
		name  string
		entry *CacheEntry
	}{
		{
			name: "save and load valid cache entry",
			entry: &CacheEntry{
				LatestVersion:  "v1.2.3",
				CurrentVersion: "v1.0.0",
				CheckedAt:      time.Now().Round(time.Second), // Round for JSON serialization
				HasUpdate:      true,
			},
		},
		{
			name: "save and load no-update entry",
			entry: &CacheEntry{
				LatestVersion:  "v1.0.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      time.Now().Round(time.Second),
				HasUpdate:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save the cache entry
			err := SaveCache(tt.entry)
			if err != nil {
				t.Fatalf("SaveCache() error = %v", err)
			}

			// Verify file was created
			path := cachePath()
			if path == "" {
				t.Fatal("cachePath() returned empty string")
			}

			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Fatal("cache file not created")
			}

			// Load the cache entry
			loaded, err := LoadCache()
			if err != nil {
				t.Fatalf("LoadCache() error = %v", err)
			}

			// Verify loaded data matches saved data
			if loaded.LatestVersion != tt.entry.LatestVersion {
				t.Errorf("LatestVersion mismatch: got %q, want %q", loaded.LatestVersion, tt.entry.LatestVersion)
			}
			if loaded.CurrentVersion != tt.entry.CurrentVersion {
				t.Errorf("CurrentVersion mismatch: got %q, want %q", loaded.CurrentVersion, tt.entry.CurrentVersion)
			}
			if loaded.HasUpdate != tt.entry.HasUpdate {
				t.Errorf("HasUpdate mismatch: got %v, want %v", loaded.HasUpdate, tt.entry.HasUpdate)
			}
			if !loaded.CheckedAt.Equal(tt.entry.CheckedAt) {
				t.Errorf("CheckedAt mismatch: got %v, want %v", loaded.CheckedAt, tt.entry.CheckedAt)
			}

			// Clean up for next test
			os.Remove(path)
		})
	}
}

func TestLoadCacheErrors(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	t.Run("load nonexistent cache file", func(t *testing.T) {
		_, err := LoadCache()
		if err == nil {
			t.Error("LoadCache() should return error for nonexistent file")
		}
	})

	t.Run("load corrupted cache file", func(t *testing.T) {
		path := cachePath()
		dir := filepath.Dir(path)
		os.MkdirAll(dir, 0755)

		// Write corrupted JSON
		if err := os.WriteFile(path, []byte(`{invalid json}`), 0644); err != nil {
			t.Fatalf("Failed to write corrupted cache: %v", err)
		}

		_, err := LoadCache()
		if err == nil {
			t.Error("LoadCache() should return error for corrupted JSON")
		}
	})
}

func TestSaveCacheWithMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Set HOME to non-existent path to test directory creation
	nonExistentHome := filepath.Join(tmpDir, "nonexistent", "nested", "path")
	os.Setenv("HOME", nonExistentHome)

	entry := &CacheEntry{
		LatestVersion:  "v1.0.0",
		CurrentVersion: "v0.9.0",
		CheckedAt:      time.Now(),
		HasUpdate:      true,
	}

	// Should create missing directories
	err := SaveCache(entry)
	if err != nil {
		t.Fatalf("SaveCache() should create missing directories, got error: %v", err)
	}

	// Verify file was created
	path := cachePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("cache file not created after SaveCache")
	}
}

func TestCacheEntryJSON(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	entry := &CacheEntry{
		LatestVersion:  "v1.2.3",
		CurrentVersion: "v1.0.0",
		CheckedAt:      now,
		HasUpdate:      true,
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal back
	var loaded CacheEntry
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify round-trip
	if loaded.LatestVersion != entry.LatestVersion ||
		loaded.CurrentVersion != entry.CurrentVersion ||
		loaded.HasUpdate != entry.HasUpdate ||
		!loaded.CheckedAt.Equal(entry.CheckedAt) {
		t.Errorf("JSON round-trip failed: original=%+v, loaded=%+v", entry, loaded)
	}
}
