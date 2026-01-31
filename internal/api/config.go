package api

import (
	"os"
	"time"
)

// Config holds the server configuration, loaded from environment variables.
type Config struct {
	ListenAddr      string
	ServerDBPath    string
	ProjectDataDir  string
	ShutdownTimeout time.Duration
	AllowSignup     bool
	BaseURL         string
}

// LoadConfig reads configuration from environment variables with sensible defaults.
func LoadConfig() Config {
	cfg := Config{
		ListenAddr:      ":8080",
		ServerDBPath:    "./data/server.db",
		ProjectDataDir:  "./data/projects",
		ShutdownTimeout: 30 * time.Second,
		AllowSignup:     true,
		BaseURL:         "http://localhost:8080",
	}

	if v := os.Getenv("SYNC_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("SYNC_SERVER_DB_PATH"); v != "" {
		cfg.ServerDBPath = v
	}
	if v := os.Getenv("SYNC_PROJECT_DATA_DIR"); v != "" {
		cfg.ProjectDataDir = v
	}
	if v := os.Getenv("SYNC_SHUTDOWN_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ShutdownTimeout = d
		}
	}
	if v := os.Getenv("SYNC_ALLOW_SIGNUP"); v == "false" || v == "0" {
		cfg.AllowSignup = false
	}
	if v := os.Getenv("SYNC_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}

	return cfg
}
