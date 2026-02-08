package api

import (
	"os"
	"strconv"
	"strings"
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
	LogFormat       string // "json" (default) or "text"
	LogLevel        string // "debug", "info" (default), "warn", "error"

	RateLimitAuth  int // /auth/* per IP per minute (default: 10)
	RateLimitPush  int // /sync/push per API key per minute (default: 60)
	RateLimitPull  int // /sync/pull per API key per minute (default: 120)
	RateLimitOther int // all other per API key per minute (default: 300)

	CORSAllowedOrigins []string // allowed origins for admin CORS; empty = disabled

	AuthEventRetention      time.Duration // retention period for auth events (default: 90 days)
	RateLimitEventRetention time.Duration // retention period for rate limit events (default: 30 days)
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
		LogFormat:       "json",
		LogLevel:        "info",

		RateLimitAuth:  10,
		RateLimitPush:  60,
		RateLimitPull:  120,
		RateLimitOther: 300,

		AuthEventRetention:      90 * 24 * time.Hour,
		RateLimitEventRetention: 30 * 24 * time.Hour,
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
	if v := os.Getenv("SYNC_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("SYNC_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("SYNC_RATE_LIMIT_AUTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateLimitAuth = n
		}
	}
	if v := os.Getenv("SYNC_RATE_LIMIT_PUSH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateLimitPush = n
		}
	}
	if v := os.Getenv("SYNC_RATE_LIMIT_PULL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateLimitPull = n
		}
	}
	if v := os.Getenv("SYNC_RATE_LIMIT_OTHER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.RateLimitOther = n
		}
	}

	if v := os.Getenv("SYNC_AUTH_EVENT_RETENTION"); v != "" {
		if d := parseDaysDuration(v); d > 0 {
			cfg.AuthEventRetention = d
		}
	}
	if v := os.Getenv("SYNC_RATE_LIMIT_EVENT_RETENTION"); v != "" {
		if d := parseDaysDuration(v); d > 0 {
			cfg.RateLimitEventRetention = d
		}
	}

	if v := os.Getenv("SYNC_CORS_ALLOWED_ORIGINS"); v != "" {
		origins := strings.Split(v, ",")
		for _, o := range origins {
			o = strings.TrimSpace(o)
			if o != "" {
				cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
			}
		}
	}

	return cfg
}

// parseDaysDuration parses a string like "90d", "30d" into a time.Duration.
// Falls back to time.ParseDuration for standard Go durations.
func parseDaysDuration(s string) time.Duration {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		if n, err := strconv.Atoi(numStr); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return 0
}
