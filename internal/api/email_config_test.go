package api

import "testing"

// Regression: SYNC_EMAIL_BASE_URL (AuthEmailBaseURL) must NOT flow into the
// Cloudflare REST API base. A prior wiring mapped it into EmailConfig.BaseURL,
// making the sender POST to td-sync's own host -> 404 -> login email failed.
func TestBuildEmailConfig_DoesNotLeakBaseURLIntoCloudflareAPI(t *testing.T) {
	cfg := Config{
		EmailProvider:           "cloudflare",
		CloudflareAccountID:     "acct-123",
		CloudflareEmailAPIToken: "tok-abc",
		CloudflareEmailFrom:     "login@opentangle.com",
		CloudflareEmailFromName: "td-watch",
		CloudflareEmailReplyTo:  "haplab@vorwaller.net",
		AuthEmailBaseURL:        "https://sync.haplab.com",
		AuthWebCallbackURL:      "https://watch.haplab.com/home/login/complete",
	}

	ec := buildEmailConfig(cfg)

	if ec.CloudflareBaseURL != "" {
		t.Fatalf("CloudflareBaseURL must be empty (so the sender defaults to api.cloudflare.com); got %q", ec.CloudflareBaseURL)
	}
	if ec.Provider != "cloudflare" || ec.AccountID != "acct-123" || ec.APIToken != "tok-abc" {
		t.Errorf("core cloudflare fields mismapped: %+v", ec)
	}
	if ec.From != "login@opentangle.com" || ec.ReplyTo != "haplab@vorwaller.net" || ec.FromName != "td-watch" {
		t.Errorf("from/replyto/name mismapped: %+v", ec)
	}
	if ec.CallbackURL != "https://watch.haplab.com/home/login/complete" {
		t.Errorf("CallbackURL mismapped: %q", ec.CallbackURL)
	}
}
