// Package webhook handles webhook configuration and HTTP dispatch.
package webhook

import (
	"os"

	"github.com/marcus/td/internal/config"
)

// GetURL returns the webhook URL for the project.
// Priority: TD_WEBHOOK_URL env > config.json webhook.url.
func GetURL(baseDir string) string {
	if v := os.Getenv("TD_WEBHOOK_URL"); v != "" {
		return v
	}
	cfg, err := config.Load(baseDir)
	if err != nil {
		return ""
	}
	if cfg.Webhook != nil {
		return cfg.Webhook.URL
	}
	return ""
}

// GetSecret returns the webhook HMAC secret.
// Priority: TD_WEBHOOK_SECRET env > config.json webhook.secret.
func GetSecret(baseDir string) string {
	if v := os.Getenv("TD_WEBHOOK_SECRET"); v != "" {
		return v
	}
	cfg, err := config.Load(baseDir)
	if err != nil {
		return ""
	}
	if cfg.Webhook != nil {
		return cfg.Webhook.Secret
	}
	return ""
}

// IsEnabled returns true if a webhook URL is configured.
func IsEnabled(baseDir string) bool {
	return GetURL(baseDir) != ""
}
