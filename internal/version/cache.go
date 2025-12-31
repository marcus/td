package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	cacheFile = "version_cache.json"
	cacheTTL  = 6 * time.Hour
)

// CacheEntry stores cached version check result.
type CacheEntry struct {
	LatestVersion  string    `json:"latestVersion"`
	CurrentVersion string    `json:"currentVersion"`
	CheckedAt      time.Time `json:"checkedAt"`
	HasUpdate      bool      `json:"hasUpdate"`
}

// cachePath returns the full path to the cache file.
func cachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "td", cacheFile)
}

// LoadCache reads cached version check result from disk.
func LoadCache() (*CacheEntry, error) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, err
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// SaveCache writes version check result to disk.
func SaveCache(entry *CacheEntry) error {
	path := cachePath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// IsCacheValid checks if cache exists and is not expired.
// Also invalidates if user version changed (upgrade or downgrade).
func IsCacheValid(entry *CacheEntry, currentVersion string) bool {
	if entry == nil {
		return false
	}
	// Invalidate if current version changed (handles upgrade or downgrade)
	if entry.CurrentVersion != currentVersion {
		return false
	}
	if time.Since(entry.CheckedAt) >= cacheTTL {
		return false
	}
	return true
}
