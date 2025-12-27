package keymap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigNonExistent(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/keymap.json")
	if err != nil {
		t.Errorf("LoadConfig should not error on nonexistent file: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig should return non-nil config")
	}
	if cfg.Bindings == nil {
		t.Error("Bindings map should be initialized")
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".todos", "keymap.json")

	// Create config
	cfg := &Config{
		Bindings: map[string]string{
			"main:ctrl+s": "open-stats",
			"modal:q":     "close",
		},
	}

	// Save config
	err := SaveConfig(configPath, cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load config back
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify contents
	if loaded.Bindings["main:ctrl+s"] != "open-stats" {
		t.Errorf("expected 'open-stats', got '%s'", loaded.Bindings["main:ctrl+s"])
	}
	if loaded.Bindings["modal:q"] != "close" {
		t.Errorf("expected 'close', got '%s'", loaded.Bindings["modal:q"])
	}
}

func TestApplyConfig(t *testing.T) {
	r := NewRegistry()
	RegisterDefaults(r)

	cfg := &Config{
		Bindings: map[string]string{
			"main:x": "quit", // Override delete to quit
		},
	}

	ApplyConfig(r, cfg)

	// Create a key event for 'x'
	// The override should now make 'x' quit instead of delete
	// We can verify by checking that the user override was set
	r.mu.RLock()
	defer r.mu.RUnlock()

	if cmd, ok := r.userOverrides["main:x"]; !ok {
		t.Error("user override was not set")
	} else if cmd != "quit" {
		t.Errorf("expected 'quit', got '%s'", cmd)
	}
}

func TestConfigPath(t *testing.T) {
	path := ConfigPath("/home/user/myproject")
	expected := "/home/user/myproject/.todos/keymap.json"
	if path != expected {
		t.Errorf("ConfigPath() = %s, want %s", path, expected)
	}
}

func TestParseBinding(t *testing.T) {
	tests := []struct {
		input   string
		context Context
		key     string
	}{
		{"main:ctrl+d", ContextMain, "ctrl+d"},
		{"modal:esc", ContextModal, "esc"},
		{"global:q", ContextGlobal, "q"},
		{"j", ContextGlobal, "j"}, // No colon, assume global
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ctx, key := parseBinding(tt.input)
			if ctx != tt.context {
				t.Errorf("parseBinding(%s) context = %s, want %s", tt.input, ctx, tt.context)
			}
			if key != tt.key {
				t.Errorf("parseBinding(%s) key = %s, want %s", tt.input, key, tt.key)
			}
		})
	}
}

func TestExampleConfig(t *testing.T) {
	cfg := ExampleConfig()
	if cfg == nil {
		t.Fatal("ExampleConfig returned nil")
	}
	if len(cfg.Bindings) == 0 {
		t.Error("ExampleConfig should have some bindings")
	}
}
