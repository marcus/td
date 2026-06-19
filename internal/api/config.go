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

	TrustedProxies     []string // trusted proxy IPs; when empty, X-Forwarded-For is ignored
	CORSAllowedOrigins []string // allowed origins for admin CORS; empty = disabled

	AuthEventRetention      time.Duration // retention period for auth events (default: 90 days)
	RateLimitEventRetention time.Duration // retention period for rate limit events (default: 30 days)

	EmailProvider           string // "cloudflare", "memory", "log"; default "log" for dev
	CloudflareAccountID     string
	CloudflareEmailAPIToken string
	CloudflareEmailFrom     string // e.g. login@opentangle.com
	CloudflareEmailFromName string // e.g. td-watch
	CloudflareEmailReplyTo  string // e.g. haplab@vorwaller.net
	AuthWebCallbackURL      string // e.g. https://watch.haplab.com/home/login/complete
	AuthEmailBaseURL        string // e.g. https://sync.haplab.com (for link generation)

	LegacyDeviceAuth bool // When true, enables /v1/auth/login/start, /v1/auth/login/poll, GET/POST /auth/verify

	// DevEmailInspect, when true, allows GET /internal/dev/last-email to return
	// the most recently sent magic-link email in plaintext. This is a DEV/TEST
	// ONLY affordance: the endpoint additionally requires the in-memory email
	// provider (*email.MemorySender), which production never uses. Both gates
	// must hold, so this flag is inert in prod even if accidentally set.
	DevEmailInspect bool
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

	if v := os.Getenv("SYNC_TRUSTED_PROXIES"); v != "" {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.TrustedProxies = append(cfg.TrustedProxies, p)
			}
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

	cfg.EmailProvider = "log"
	if v := os.Getenv("SYNC_EMAIL_PROVIDER"); v != "" {
		cfg.EmailProvider = v
	}
	if v := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); v != "" {
		cfg.CloudflareAccountID = v
	}
	if v := os.Getenv("CLOUDFLARE_EMAIL_API_TOKEN"); v != "" {
		cfg.CloudflareEmailAPIToken = v
	}
	if v := os.Getenv("CLOUDFLARE_EMAIL_FROM"); v != "" {
		cfg.CloudflareEmailFrom = v
	}
	if v := os.Getenv("CLOUDFLARE_EMAIL_FROM_NAME"); v != "" {
		cfg.CloudflareEmailFromName = v
	}
	if v := os.Getenv("CLOUDFLARE_EMAIL_REPLY_TO"); v != "" {
		cfg.CloudflareEmailReplyTo = v
	}
	if v := os.Getenv("SYNC_AUTH_WEB_CALLBACK_URL"); v != "" {
		cfg.AuthWebCallbackURL = v
	}
	if v := os.Getenv("SYNC_EMAIL_BASE_URL"); v != "" {
		cfg.AuthEmailBaseURL = v
	}

	if v := os.Getenv("SYNC_LEGACY_DEVICE_AUTH"); v == "true" || v == "1" {
		cfg.LegacyDeviceAuth = true
	}

	if v := os.Getenv("SYNC_DEV_EMAIL_INSPECT"); v == "true" || v == "1" {
		cfg.DevEmailInspect = true
	}

	return cfg
}

// ValidateEmailConfig checks the email provider configuration and returns a list
// of warning strings for any missing or unrecognized settings.
func ValidateEmailConfig(cfg Config) []string {
	var warnings []string
	switch cfg.EmailProvider {
	case "log", "memory":
		// no warnings for development providers
	case "cloudflare":
		if cfg.CloudflareAccountID == "" {
			warnings = append(warnings, "CLOUDFLARE_ACCOUNT_ID is not set")
		}
		if cfg.CloudflareEmailAPIToken == "" {
			warnings = append(warnings, "CLOUDFLARE_EMAIL_API_TOKEN is not set")
		}
		if cfg.CloudflareEmailFrom == "" {
			warnings = append(warnings, "CLOUDFLARE_EMAIL_FROM is not set")
		}
		if cfg.AuthEmailBaseURL == "" {
			warnings = append(warnings, "SYNC_EMAIL_BASE_URL is not set (required for cloudflare email link generation)")
		}
	default:
		warnings = append(warnings, "unrecognized SYNC_EMAIL_PROVIDER: "+cfg.EmailProvider)
	}
	return warnings
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
