// Package keymap provides user-configurable key bindings for the TUI monitor,
// loaded from .todos/keymap.json.
package keymap

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents user key binding configuration.
// Stored in .todos/keymap.json
type Config struct {
	// Bindings maps "context:key" to command ID
	// Example: {"main:ctrl+s": "open-stats", "modal:q": "close"}
	Bindings map[string]string `json:"bindings"`
}

// ConfigPath returns the path to the keymap config file
func ConfigPath(baseDir string) string {
	return filepath.Join(baseDir, ".todos", "keymap.json")
}

// LoadConfig loads key binding overrides from a JSON file.
// Returns an empty config if the file doesn't exist.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Bindings: make(map[string]string)}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Bindings == nil {
		cfg.Bindings = make(map[string]string)
	}

	return &cfg, nil
}

// SaveConfig saves the config to a JSON file.
func SaveConfig(path string, cfg *Config) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ApplyConfig applies user configuration overrides to the registry.
func ApplyConfig(r *Registry, cfg *Config) {
	for binding, cmdStr := range cfg.Bindings {
		// Parse "context:key" format
		ctx, key := parseBinding(binding)
		if ctx == "" || key == "" {
			continue
		}
		r.SetUserOverride(ctx, key, Command(cmdStr))
	}
}

// parseBinding parses a "context:key" string into context and key parts.
func parseBinding(s string) (Context, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return Context(s[:i]), s[i+1:]
		}
	}
	// If no colon, assume global context
	return ContextGlobal, s
}

// ExampleConfig returns an example configuration for documentation
func ExampleConfig() *Config {
	return &Config{
		Bindings: map[string]string{
			"main:ctrl+s":   "open-stats",
			"modal:q":       "close",
			"global:ctrl+q": "quit",
		},
	}
}
