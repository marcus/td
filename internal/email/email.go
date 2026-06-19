package email

import (
	"context"
	"fmt"
)

// LoginEmail holds the data for a magic-link login email.
type LoginEmail struct {
	To      string
	Subject string
	Text    string
	HTML    string
	Purpose string // e.g. "web_login", "device_login"
	TraceID string
}

// EmailSender is the provider-neutral interface for sending login emails.
type EmailSender interface {
	SendLoginLink(ctx context.Context, msg LoginEmail) error
}

// EmailConfig holds the fields that email senders need. The mapping from
// api.Config to EmailConfig is done in the api/cmd layer to avoid import cycles.
type EmailConfig struct {
	Provider    string // "cloudflare", "memory", "log"
	AccountID   string // Cloudflare account ID
	APIToken    string // Cloudflare Email Workers API token
	From        string // e.g. login@example.com
	FromName    string // e.g. td-watch
	ReplyTo     string // e.g. support@example.com
	BaseURL     string // e.g. https://sync.example.com (for link generation)
	CallbackURL string // e.g. https://watch.example.com/home/login/complete
}

// NewEmailSender constructs the appropriate EmailSender for cfg.Provider.
// Recognised values: "cloudflare", "memory", "log", "" (empty string treated as "log").
func NewEmailSender(cfg EmailConfig) (EmailSender, error) {
	switch cfg.Provider {
	case "cloudflare":
		return NewCloudflareSender(cfg)
	case "memory":
		return NewMemorySender(), nil
	case "", "log":
		return NewLogSender(), nil
	default:
		return nil, fmt.Errorf("unknown email provider: %q", cfg.Provider)
	}
}
