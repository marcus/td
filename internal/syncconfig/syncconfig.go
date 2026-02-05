package syncconfig

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AutoSyncConfig holds auto-sync settings.
type AutoSyncConfig struct {
	Enabled  *bool  `json:"enabled,omitempty"`  // nil = default true
	OnStart  *bool  `json:"on_start,omitempty"` // nil = default true
	Debounce string `json:"debounce,omitempty"` // duration string, default "3s"
	Interval string `json:"interval,omitempty"` // duration string, default "5m"
	Pull     *bool  `json:"pull,omitempty"`     // nil = default true
}

// SyncConfig holds sync-related settings.
type SyncConfig struct {
	URL               string         `json:"url"`
	Enabled           bool           `json:"enabled"`
	SnapshotThreshold *int           `json:"snapshot_threshold,omitempty"`
	Auto              AutoSyncConfig `json:"auto"`
}

// Config is the global td config stored at ~/.config/td/config.json.
type Config struct {
	Sync SyncConfig `json:"sync"`
}

// AuthCredentials stores authentication state at ~/.config/td/auth.json.
type AuthCredentials struct {
	APIKey    string `json:"api_key"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	ServerURL string `json:"server_url"`
	DeviceID  string `json:"device_id"`
	ExpiresAt string `json:"expires_at"`
}

const defaultServerURL = "http://localhost:8080"

// ConfigDir returns ~/.config/td, creating it if necessary.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "td")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// LoadConfig reads the global config from ~/.config/td/config.json.
func LoadConfig() (*Config, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes the global config to ~/.config/td/config.json.
func SaveConfig(cfg *Config) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}

// LoadAuth reads auth credentials from ~/.config/td/auth.json.
func LoadAuth() (*AuthCredentials, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var creds AuthCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// SaveAuth writes auth credentials to ~/.config/td/auth.json (0600 perms).
func SaveAuth(creds *AuthCredentials) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "auth.json"), data, 0600)
}

// ClearAuth removes the auth.json file.
func ClearAuth() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(dir, "auth.json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// GetServerURL returns the sync server URL.
// Priority: TD_SYNC_URL env > config.json > default.
func GetServerURL() string {
	if v := os.Getenv("TD_SYNC_URL"); v != "" {
		return v
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.URL != "" {
		return cfg.Sync.URL
	}
	return defaultServerURL
}

// GetSnapshotThreshold returns the snapshot bootstrap threshold (min server events).
// Priority: TD_SYNC_SNAPSHOT_THRESHOLD env > config.json > default (100).
func GetSnapshotThreshold() int {
	if v := os.Getenv("TD_SYNC_SNAPSHOT_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.SnapshotThreshold != nil && *cfg.Sync.SnapshotThreshold >= 0 {
		return *cfg.Sync.SnapshotThreshold
	}
	return 100
}

// GetAPIKey returns the API key.
// Priority: TD_AUTH_KEY env > auth.json.
func GetAPIKey() string {
	if v := os.Getenv("TD_AUTH_KEY"); v != "" {
		return v
	}
	creds, err := LoadAuth()
	if err == nil && creds != nil {
		return creds.APIKey
	}
	return ""
}

// IsAuthenticated returns true if an API key is available.
func IsAuthenticated() bool {
	return GetAPIKey() != ""
}

// GetDeviceID returns the device ID from auth.json, generating one if needed.
func GetDeviceID() (string, error) {
	creds, err := LoadAuth()
	if err != nil {
		return "", err
	}
	if creds != nil && creds.DeviceID != "" {
		return creds.DeviceID, nil
	}
	return GenerateDeviceID()
}

// GenerateDeviceID creates a new random device ID (16 bytes hex).
func GenerateDeviceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// parseBoolEnv returns nil if env not set, pointer to bool if set.
func parseBoolEnv(envKey string) *bool {
	v := os.Getenv(envKey)
	if v == "" {
		return nil
	}
	v = strings.ToLower(v)
	if v == "1" || v == "true" {
		b := true
		return &b
	}
	if v == "0" || v == "false" {
		b := false
		return &b
	}
	return nil
}

// GetAutoSyncEnabled returns whether auto-sync is enabled.
// Priority: TD_SYNC_AUTO env > config.json sync.auto.enabled > true
func GetAutoSyncEnabled() bool {
	if v := parseBoolEnv("TD_SYNC_AUTO"); v != nil {
		return *v
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.Auto.Enabled != nil {
		return *cfg.Sync.Auto.Enabled
	}
	return true
}

// GetAutoSyncOnStart returns whether to sync on startup.
// Priority: TD_SYNC_AUTO_START env > config.json sync.auto.on_start > true
func GetAutoSyncOnStart() bool {
	if v := parseBoolEnv("TD_SYNC_AUTO_START"); v != nil {
		return *v
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.Auto.OnStart != nil {
		return *cfg.Sync.Auto.OnStart
	}
	return true
}

// GetAutoSyncDebounce returns the debounce duration for post-mutation sync.
// Priority: TD_SYNC_AUTO_DEBOUNCE env > config.json sync.auto.debounce > 3s
func GetAutoSyncDebounce() time.Duration {
	if v := os.Getenv("TD_SYNC_AUTO_DEBOUNCE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.Auto.Debounce != "" {
		if d, err := time.ParseDuration(cfg.Sync.Auto.Debounce); err == nil {
			return d
		}
	}
	return 3 * time.Second
}

// GetAutoSyncInterval returns the periodic sync interval.
// Priority: TD_SYNC_AUTO_INTERVAL env > config.json sync.auto.interval > 5m
func GetAutoSyncInterval() time.Duration {
	if v := os.Getenv("TD_SYNC_AUTO_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.Auto.Interval != "" {
		if d, err := time.ParseDuration(cfg.Sync.Auto.Interval); err == nil {
			return d
		}
	}
	return 5 * time.Minute
}

// GetAutoSyncPull returns whether auto-sync should include pull.
// Priority: TD_SYNC_AUTO_PULL env > config.json sync.auto.pull > true
func GetAutoSyncPull() bool {
	if v := parseBoolEnv("TD_SYNC_AUTO_PULL"); v != nil {
		return *v
	}
	cfg, err := LoadConfig()
	if err == nil && cfg.Sync.Auto.Pull != nil {
		return *cfg.Sync.Auto.Pull
	}
	return true
}
