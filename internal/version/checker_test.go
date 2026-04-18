package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCheckAsyncWithValidCache(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// Pre-populate cache with a valid entry
	now := time.Now()
	cacheEntry := &CacheEntry{
		LatestVersion:  "v1.5.0",
		CurrentVersion: "v1.0.0",
		CheckedAt:      now,
		HasUpdate:      true,
	}

	if err := SaveCache(cacheEntry); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// CheckAsync should use cache and return UpdateAvailableMsg
	cmd := CheckAsync("v1.0.0")
	msg := cmd()

	updateMsg, ok := msg.(UpdateAvailableMsg)
	if !ok {
		if msg == nil {
			t.Fatal("CheckAsync returned nil instead of UpdateAvailableMsg")
		}
		t.Fatalf("CheckAsync returned unexpected type: %T", msg)
	}

	if updateMsg.CurrentVersion != "v1.0.0" {
		t.Errorf("CurrentVersion = %q, want %q", updateMsg.CurrentVersion, "v1.0.0")
	}
	if updateMsg.LatestVersion != "v1.5.0" {
		t.Errorf("LatestVersion = %q, want %q", updateMsg.LatestVersion, "v1.5.0")
	}
	if updateMsg.UpdateCommand == "" {
		t.Error("UpdateCommand is empty for valid version")
	}
}

func TestCheckAsyncWithExpiredCache(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// Pre-populate cache with an expired entry (7 hours old, TTL is 6 hours)
	expiredTime := time.Now().Add(-7 * time.Hour)
	cacheEntry := &CacheEntry{
		LatestVersion:  "v1.5.0",
		CurrentVersion: "v1.0.0",
		CheckedAt:      expiredTime,
		HasUpdate:      true,
	}

	if err := SaveCache(cacheEntry); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// CheckAsync with expired cache should attempt to fetch from GitHub
	// (will fail since we can't mock HTTP, but it should not use cached result)
	cmd := CheckAsync("v1.0.0")
	msg := cmd()

	// Since the cache is expired and network call will fail (no mock),
	// we expect nil or an error state, not the cached message
	if msg != nil {
		if updateMsg, ok := msg.(UpdateAvailableMsg); ok {
			// If we got an UpdateAvailableMsg, it shouldn't match the expired cache
			if updateMsg.LatestVersion == "v1.5.0" {
				t.Error("CheckAsync used expired cache despite TTL expiration")
			}
		}
	}
}

func TestCheckAsyncWithVersionMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// Pre-populate cache for v1.0.0
	now := time.Now()
	cacheEntry := &CacheEntry{
		LatestVersion:  "v1.5.0",
		CurrentVersion: "v1.0.0",
		CheckedAt:      now,
		HasUpdate:      true,
	}

	if err := SaveCache(cacheEntry); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// CheckAsync with different current version should invalidate cache
	// Since cache is now invalid and network call fails (no mock),
	// we expect nil or error state
	cmd := CheckAsync("v1.1.0")
	msg := cmd()

	if msg != nil {
		if updateMsg, ok := msg.(UpdateAvailableMsg); ok {
			// If we got an UpdateAvailableMsg, it shouldn't be from old cache
			if updateMsg.LatestVersion == "v1.5.0" && updateMsg.CurrentVersion == "v1.0.0" {
				t.Error("CheckAsync used cache from different version")
			}
		}
	}
}

func TestCheckAsyncNoCacheFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// No cache file exists, CheckAsync should attempt network fetch
	// (will fail without mocking, but that's expected)
	cmd := CheckAsync("v1.0.0")
	msg := cmd()

	// Without cache and with network failure, expect nil
	if msg != nil {
		t.Errorf("Expected nil from failed network check, got: %T", msg)
	}
}

func TestUpdateAvailableMsgType(t *testing.T) {
	// Verify UpdateAvailableMsg implements tea.Msg interface
	msg := UpdateAvailableMsg{
		CurrentVersion: "v1.0.0",
		LatestVersion:  "v1.5.0",
		UpdateCommand:  "go install ...",
	}

	// If this compiles, it implements tea.Msg
	var _ tea.Msg = msg

	// Test that fields are accessible
	if msg.CurrentVersion != "v1.0.0" {
		t.Error("CurrentVersion not accessible")
	}
	if msg.LatestVersion != "v1.5.0" {
		t.Error("LatestVersion not accessible")
	}
	if msg.UpdateCommand != "go install ..." {
		t.Error("UpdateCommand not accessible")
	}
}

func TestCheckAsyncWithDevelopmentVersion(t *testing.T) {
	// Development versions should skip network check
	cmd := CheckAsync("devel")
	msg := cmd()

	// Should return nil for development versions
	if msg != nil {
		t.Errorf("Expected nil for development version, got: %T", msg)
	}
}

func TestCheckAsyncWithInvalidCache(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	path := cachePath()
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0755)

	// Write corrupted JSON to cache
	if err := os.WriteFile(path, []byte(`{corrupted}`), 0644); err != nil {
		t.Fatalf("Failed to write corrupted cache: %v", err)
	}

	// CheckAsync should handle corrupted cache gracefully
	// and attempt network fetch (which will fail without mocking)
	cmd := CheckAsync("v1.0.0")
	msg := cmd()

	// Expect nil since network fetch fails
	if msg != nil {
		t.Errorf("Expected nil after corrupted cache and failed network check, got: %T", msg)
	}
}

func TestCheckAsyncCacheSaving(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// First call should try to fetch from network (will fail)
	// but shouldn't crash
	cmd1 := CheckAsync("v1.0.0")
	_ = cmd1()

	// Manually create a cache entry as if a successful check happened
	cacheEntry := &CacheEntry{
		LatestVersion:  "v1.2.0",
		CurrentVersion: "v1.0.0",
		CheckedAt:      time.Now(),
		HasUpdate:      true,
	}
	if err := SaveCache(cacheEntry); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Second call should use the cache
	cmd2 := CheckAsync("v1.0.0")
	msg := cmd2()

	if msg == nil {
		t.Fatal("Expected UpdateAvailableMsg from cache, got nil")
	}

	updateMsg, ok := msg.(UpdateAvailableMsg)
	if !ok {
		t.Fatalf("Expected UpdateAvailableMsg, got %T", msg)
	}

	if updateMsg.LatestVersion != "v1.2.0" {
		t.Errorf("LatestVersion = %q, want %q", updateMsg.LatestVersion, "v1.2.0")
	}
}

func TestCheckAsyncUpToDate(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	defer os.Setenv("HOME", oldHome)

	os.Setenv("HOME", tmpDir)

	// Cache indicates no update available
	now := time.Now()
	cacheEntry := &CacheEntry{
		LatestVersion:  "v1.0.0",
		CurrentVersion: "v1.0.0",
		CheckedAt:      now,
		HasUpdate:      false,
	}

	if err := SaveCache(cacheEntry); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// CheckAsync should return nil when up-to-date
	cmd := CheckAsync("v1.0.0")
	msg := cmd()

	if msg != nil {
		t.Errorf("Expected nil for up-to-date version, got: %T", msg)
	}
}

// TestCheckResultTypes verifies CheckResult structure and field types
func TestCheckResultTypes(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
	}{
		{"standard-version", "v1.0.0"},
		{"prerelease-version", "v1.0.0-beta"},
		{"zero-version", "v0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Check(tt.currentVersion)

			// Verify CurrentVersion is always set
			if result.CurrentVersion != tt.currentVersion {
				t.Errorf("CurrentVersion mismatch: got %q, want %q", result.CurrentVersion, tt.currentVersion)
			}

			// LatestVersion should be a string (may be empty for dev versions)
			_ = result.LatestVersion

			// UpdateURL should be a valid URL format or empty
			if result.UpdateURL != "" && !strings.Contains(result.UpdateURL, "://") {
				t.Errorf("UpdateURL invalid format: %q", result.UpdateURL)
			}

			// HasUpdate is a boolean
			_ = result.HasUpdate

			// Error can be nil or an error
			_ = result.Error
		})
	}
}

// TestCheckWithDevelopmentVersion verifies development versions skip network checks
func TestCheckWithDevelopmentVersion(t *testing.T) {
	devVersions := []string{
		"",
		"unknown",
		"dev",
		"devel",
		"devel+abc123",
	}

	for _, version := range devVersions {
		t.Run("dev_"+version, func(t *testing.T) {
			result := Check(version)

			// Development versions should not have an error set
			if result.Error != nil {
				t.Errorf("Development version %q should not check: got error %v", version, result.Error)
			}

			// LatestVersion should be empty for dev versions
			if result.LatestVersion != "" {
				t.Errorf("Development version should have empty LatestVersion, got %q", result.LatestVersion)
			}

			// HasUpdate should be false
			if result.HasUpdate {
				t.Errorf("Development version should never have updates")
			}
		})
	}
}

// TestCheckResultStructure verifies CheckResult fields are properly populated
func TestCheckResultStructure(t *testing.T) {
	// Test with a development version that skips network check
	result := Check("devel")

	// CurrentVersion should always be set
	if result.CurrentVersion != "devel" {
		t.Errorf("CurrentVersion = %q, want %q", result.CurrentVersion, "devel")
	}

	// For development versions, other fields should be empty
	if result.LatestVersion != "" {
		t.Errorf("LatestVersion should be empty for dev version, got %q", result.LatestVersion)
	}

	// Should not have an error
	if result.Error != nil {
		t.Errorf("Dev version check should not error: %v", result.Error)
	}
}

// TestIsNewerPrereleaseComparison handles prerelease comparison edge cases
func TestIsNewerPrereleaseComparison(t *testing.T) {
	tests := []struct {
		name     string
		latest   string
		current  string
		expected bool
	}{
		// Prerelease vs final same core version
		{"v1-vs-beta", "v1.0.0", "v1.0.0-beta", false},
		{"rc1-vs-beta", "v1.0.0-rc1", "v1.0.0-beta", false},
		{"alpha-vs-alpha", "v1.0.0-alpha", "v1.0.0-alpha", false},
		// Major version jump with prerelease
		{"v2-beta-vs-v1.9.9", "v2.0.0-beta", "v1.9.9", true},
		{"v2-rc1-vs-v1.99.99", "v2.0.0-rc1", "v1.99.99", true},
		// Same prerelease versions
		{"beta-vs-beta", "v1.0.0-beta", "v1.0.0-beta", false},
		// Complex prerelease identifiers
		{"rc.1.test-vs-rc.1", "v1.0.0-rc.1.test", "v1.0.0-rc.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNewer(tt.latest, tt.current)
			if result != tt.expected {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, result, tt.expected)
			}
		})
	}
}
