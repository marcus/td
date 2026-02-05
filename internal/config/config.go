package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"

	"github.com/marcus/td/internal/models"
)

const configFile = ".todos/config.json"
const lockFile = ".todos/config.json.lock"

// Title validation defaults
const (
	DefaultTitleMinLength = 15
	DefaultTitleMaxLength = 100
)

// Load reads the config from disk
func Load(baseDir string) (*models.Config, error) {
	configPath := filepath.Join(baseDir, configFile)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.Config{}, nil
		}
		return nil, err
	}

	var cfg models.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Save writes the config to disk using atomic write (temp file + rename)
func Save(baseDir string, cfg *models.Config) error {
	configPath := filepath.Join(baseDir, configFile)

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: temp file in same dir, then rename
	tmp, err := os.CreateTemp(dir, "config-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, configPath)
}

// withConfigLock serializes access to config.json using flock
func withConfigLock(baseDir string, fn func() error) error {
	lockPath := filepath.Join(baseDir, lockFile)

	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

// SetFocus sets the focused issue ID
func SetFocus(baseDir string, issueID string) error {
	return withConfigLock(baseDir, func() error {
		cfg, err := Load(baseDir)
		if err != nil {
			return err
		}
		cfg.FocusedIssueID = issueID
		return Save(baseDir, cfg)
	})
}

// ClearFocus clears the focused issue
func ClearFocus(baseDir string) error {
	return SetFocus(baseDir, "")
}

// GetFocus returns the focused issue ID
func GetFocus(baseDir string) (string, error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return "", err
	}
	return cfg.FocusedIssueID, nil
}

// SetActiveWorkSession sets the active work session ID
func SetActiveWorkSession(baseDir string, wsID string) error {
	return withConfigLock(baseDir, func() error {
		cfg, err := Load(baseDir)
		if err != nil {
			return err
		}
		cfg.ActiveWorkSession = wsID
		return Save(baseDir, cfg)
	})
}

// GetActiveWorkSession returns the active work session ID
func GetActiveWorkSession(baseDir string) (string, error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return "", err
	}
	return cfg.ActiveWorkSession, nil
}

// ClearActiveWorkSession clears the active work session
func ClearActiveWorkSession(baseDir string) error {
	return SetActiveWorkSession(baseDir, "")
}

// DefaultPaneHeights returns the default pane height ratios (equal thirds)
func DefaultPaneHeights() [3]float64 {
	return [3]float64{1.0 / 3.0, 1.0 / 3.0, 1.0 / 3.0}
}

// GetPaneHeights returns the configured pane heights, or defaults if not set
func GetPaneHeights(baseDir string) ([3]float64, error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return DefaultPaneHeights(), err
	}

	// Return defaults if not configured or invalid
	if cfg.PaneHeights[0] == 0 && cfg.PaneHeights[1] == 0 && cfg.PaneHeights[2] == 0 {
		return DefaultPaneHeights(), nil
	}

	// Validate: each pane must be at least 10% and sum must be ~1.0
	sum := cfg.PaneHeights[0] + cfg.PaneHeights[1] + cfg.PaneHeights[2]
	if sum < 0.99 || sum > 1.01 {
		return DefaultPaneHeights(), nil
	}
	for _, h := range cfg.PaneHeights {
		if h < 0.1 {
			return DefaultPaneHeights(), nil
		}
	}

	return cfg.PaneHeights, nil
}

// SetPaneHeights saves the pane height ratios to config
func SetPaneHeights(baseDir string, heights [3]float64) error {
	return withConfigLock(baseDir, func() error {
		cfg, err := Load(baseDir)
		if err != nil {
			return err
		}
		cfg.PaneHeights = heights
		return Save(baseDir, cfg)
	})
}

// FilterState holds the current filter/search state for the monitor
type FilterState struct {
	SearchQuery   string
	SortMode      string // "priority", "created", "updated"
	TypeFilter    string // "", "epic", "task", "bug", "feature", "chore"
	IncludeClosed bool
}

// GetFilterState returns the saved filter state
func GetFilterState(baseDir string) (*FilterState, error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return nil, err
	}
	return &FilterState{
		SearchQuery:   cfg.SearchQuery,
		SortMode:      cfg.SortMode,
		TypeFilter:    cfg.TypeFilter,
		IncludeClosed: cfg.IncludeClosed,
	}, nil
}

// SetFilterState saves the filter state to config
func SetFilterState(baseDir string, state *FilterState) error {
	return withConfigLock(baseDir, func() error {
		cfg, err := Load(baseDir)
		if err != nil {
			return err
		}
		cfg.SearchQuery = state.SearchQuery
		cfg.SortMode = state.SortMode
		cfg.TypeFilter = state.TypeFilter
		cfg.IncludeClosed = state.IncludeClosed
		return Save(baseDir, cfg)
	})
}

// GetTitleLengthLimits returns min/max title length limits from config (with defaults)
func GetTitleLengthLimits(baseDir string) (min, max int, err error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return DefaultTitleMinLength, DefaultTitleMaxLength, err
	}

	min = cfg.TitleMinLength
	if min <= 0 {
		min = DefaultTitleMinLength
	}

	max = cfg.TitleMaxLength
	if max <= 0 {
		max = DefaultTitleMaxLength
	}

	return min, max, nil
}

// GetFeatureFlag returns a feature flag from local config.
// The second return value indicates whether the flag is explicitly set.
func GetFeatureFlag(baseDir, name string) (bool, bool, error) {
	cfg, err := Load(baseDir)
	if err != nil {
		return false, false, err
	}
	if cfg.FeatureFlags == nil {
		return false, false, nil
	}
	value, ok := cfg.FeatureFlags[name]
	return value, ok, nil
}

// SetFeatureFlag persists a feature flag in local config.
func SetFeatureFlag(baseDir, name string, enabled bool) error {
	cfg, err := Load(baseDir)
	if err != nil {
		return err
	}
	if cfg.FeatureFlags == nil {
		cfg.FeatureFlags = make(map[string]bool)
	}
	cfg.FeatureFlags[name] = enabled
	return Save(baseDir, cfg)
}

// UnsetFeatureFlag removes an explicitly-set feature flag from local config.
func UnsetFeatureFlag(baseDir, name string) error {
	cfg, err := Load(baseDir)
	if err != nil {
		return err
	}
	if cfg.FeatureFlags == nil {
		return nil
	}
	delete(cfg.FeatureFlags, name)
	if len(cfg.FeatureFlags) == 0 {
		cfg.FeatureFlags = nil
	}
	return Save(baseDir, cfg)
}
