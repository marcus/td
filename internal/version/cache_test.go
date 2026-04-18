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
		_ = os.MkdirAll(dir, 0755)

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

// TestCachePathEmptyHome tests cachePath behavior with empty HOME directory
func TestCachePathEmptyHome(t *testing.T) {
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Unset HOME to simulate environment without it
	os.Setenv("HOME", "")

	path := cachePath()
	if path != "" {
		t.Errorf("cachePath() should return empty string when HOME is not set, got %q", path)
	}
}

// TestIsCacheValidBoundaryConditions tests cache validity at boundary times
func TestIsCacheValidBoundaryConditions(t *testing.T) {
	now := time.Now()
	cacheTTL := 6 * time.Hour

	tests := []struct {
		name          string
		checkedAt     time.Time
		expectedValid bool
	}{
		{"just created", now, true},
		{"1 second old", now.Add(-1 * time.Second), true},
		{"almost expired", now.Add(-cacheTTL + time.Second), true},
		{"exactly at TTL", now.Add(-cacheTTL), false},
		{"just past TTL", now.Add(-cacheTTL - time.Millisecond), false},
		{"way past TTL", now.Add(-24 * time.Hour), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &CacheEntry{
				LatestVersion:  "v1.1.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      tt.checkedAt,
				HasUpdate:      true,
			}

			valid := IsCacheValid(entry, "v1.0.0")
			if valid != tt.expectedValid {
				t.Errorf("IsCacheValid() = %v, want %v", valid, tt.expectedValid)
			}
		})
	}
}

// TestSaveCacheCreatesDirs tests that SaveCache creates necessary directories
func TestSaveCacheCreatesDirs(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	// Set HOME to temp directory to control cache location
	os.Setenv("HOME", tmpDir)

	entry := &CacheEntry{
		LatestVersion:  "v1.0.0",
		CurrentVersion: "v0.9.0",
		CheckedAt:      time.Now(),
		HasUpdate:      true,
	}

	err := SaveCache(entry)
	if err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}

	// Verify config directory was created
	configDir := filepath.Join(tmpDir, ".config", "td")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("SaveCache() should create config directory")
	}

	// Verify cache file exists
	cacheFile := filepath.Join(configDir, "version_cache.json")
	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		t.Errorf("SaveCache() should create cache file")
	}
}

// TestLoadCachePermissions tests that loaded cache data is accessible
func TestLoadCachePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	originalEntry := &CacheEntry{
		LatestVersion:  "v2.0.0",
		CurrentVersion: "v1.5.0",
		CheckedAt:      time.Now().Round(time.Second),
		HasUpdate:      true,
	}

	// Save cache
	if err := SaveCache(originalEntry); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}

	// Load cache
	loaded, err := LoadCache()
	if err != nil {
		t.Fatalf("LoadCache() error = %v", err)
	}

	// Verify all fields are accessible
	if loaded == nil {
		t.Fatal("LoadCache() returned nil")
	}

	if loaded.LatestVersion == "" {
		t.Error("LatestVersion not loaded")
	}
	if loaded.CurrentVersion == "" {
		t.Error("CurrentVersion not loaded")
	}
	if loaded.CheckedAt.IsZero() {
		t.Error("CheckedAt not loaded properly")
	}
}

// TestCacheVsVersionCheck tests that cache invalidation on version change works correctly
func TestCacheVersionChange(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		cachedVersion  string
		currentVersion string
		shouldBeValid  bool
	}{
		{"same version", "v1.0.0", "v1.0.0", true},
		{"upgraded", "v1.0.0", "v1.1.0", false},
		{"downgraded", "v1.1.0", "v1.0.0", false},
		{"patch upgrade", "v1.0.0", "v1.0.1", false},
		{"major upgrade", "v1.0.0", "v2.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &CacheEntry{
				LatestVersion:  "v1.5.0",
				CurrentVersion: tt.cachedVersion,
				CheckedAt:      now,
				HasUpdate:      true,
			}

			valid := IsCacheValid(entry, tt.currentVersion)
			if valid != tt.shouldBeValid {
				t.Errorf("IsCacheValid() = %v, want %v for cache %q vs current %q",
					valid, tt.shouldBeValid, tt.cachedVersion, tt.currentVersion)
			}
		})
	}
}

// TestCacheEntryEdgeCases tests various edge case CacheEntry values
func TestCacheEntryEdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	tests := []struct {
		name  string
		entry *CacheEntry
	}{
		{
			name: "with prerelease versions",
			entry: &CacheEntry{
				LatestVersion:  "v1.0.0-beta",
				CurrentVersion: "v1.0.0-alpha",
				CheckedAt:      time.Now().Round(time.Second),
				HasUpdate:      true,
			},
		},
		{
			name: "with zero time",
			entry: &CacheEntry{
				LatestVersion:  "v1.0.0",
				CurrentVersion: "v0.9.0",
				CheckedAt:      time.Time{},
				HasUpdate:      false,
			},
		},
		{
			name: "very old timestamp",
			entry: &CacheEntry{
				LatestVersion:  "v1.0.0",
				CurrentVersion: "v1.0.0",
				CheckedAt:      time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
				HasUpdate:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save
			if err := SaveCache(tt.entry); err != nil {
				t.Fatalf("SaveCache() error = %v", err)
			}

			// Load
			loaded, err := LoadCache()
			if err != nil {
				t.Fatalf("LoadCache() error = %v", err)
			}

			// Verify structure is preserved
			if loaded == nil {
				t.Fatal("Loaded entry is nil")
			}

			// Clean up for next test
			os.Remove(cachePath())
		})
	}
}
