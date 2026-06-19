package api

import (
	"testing"
)

func TestValidateEmailConfig_LogProviderNoWarnings(t *testing.T) {
	cfg := Config{EmailProvider: "log"}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for 'log' provider, got: %v", warnings)
	}
}

func TestValidateEmailConfig_MemoryProviderNoWarnings(t *testing.T) {
	cfg := Config{EmailProvider: "memory"}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for 'memory' provider, got: %v", warnings)
	}
}

func TestValidateEmailConfig_CloudflareAllFieldsNoWarnings(t *testing.T) {
	cfg := Config{
		EmailProvider:           "cloudflare",
		CloudflareAccountID:     "acct123",
		CloudflareEmailAPIToken: "token456",
		CloudflareEmailFrom:     "login@example.com",
		AuthEmailBaseURL:        "https://sync.example.com",
	}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for fully configured cloudflare provider, got: %v", warnings)
	}
}

func TestValidateEmailConfig_CloudflareMissingFields(t *testing.T) {
	cfg := Config{EmailProvider: "cloudflare"}
	warnings := ValidateEmailConfig(cfg)
	// Expect warnings for AccountID, APIToken, From, and AuthEmailBaseURL
	if len(warnings) != 4 {
		t.Errorf("expected 4 warnings for cloudflare with all fields missing, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateEmailConfig_CloudflareMissingAccountID(t *testing.T) {
	cfg := Config{
		EmailProvider:           "cloudflare",
		CloudflareEmailAPIToken: "token456",
		CloudflareEmailFrom:     "login@example.com",
		AuthEmailBaseURL:        "https://sync.example.com",
	}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for missing AccountID, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateEmailConfig_CloudflareMissingBaseURL(t *testing.T) {
	cfg := Config{
		EmailProvider:           "cloudflare",
		CloudflareAccountID:     "acct123",
		CloudflareEmailAPIToken: "token456",
		CloudflareEmailFrom:     "login@example.com",
		// AuthEmailBaseURL intentionally empty
	}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for missing AuthEmailBaseURL, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateEmailConfig_UnknownProvider(t *testing.T) {
	cfg := Config{EmailProvider: "sendgrid"}
	warnings := ValidateEmailConfig(cfg)
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning for unknown provider, got %d: %v", len(warnings), warnings)
	}
}

func TestLoadConfig_EmailProviderDefault(t *testing.T) {
	// Ensure SYNC_EMAIL_PROVIDER is not set in environment for this test.
	// LoadConfig reads from env, so we test the default value.
	// We call LoadConfig and check the default is "log".
	// This assumes SYNC_EMAIL_PROVIDER is unset in the test environment.
	t.Setenv("SYNC_EMAIL_PROVIDER", "")
	cfg := LoadConfig()
	if cfg.EmailProvider != "log" {
		t.Errorf("expected default EmailProvider 'log', got %q", cfg.EmailProvider)
	}
}

func TestLoadConfig_EmailProviderFromEnv(t *testing.T) {
	t.Setenv("SYNC_EMAIL_PROVIDER", "cloudflare")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct-test")
	t.Setenv("CLOUDFLARE_EMAIL_API_TOKEN", "tok-test")
	t.Setenv("CLOUDFLARE_EMAIL_FROM", "from@test.com")
	t.Setenv("CLOUDFLARE_EMAIL_FROM_NAME", "Test")
	t.Setenv("CLOUDFLARE_EMAIL_REPLY_TO", "reply@test.com")
	t.Setenv("SYNC_AUTH_WEB_CALLBACK_URL", "https://watch.test.com/home/login/complete")
	t.Setenv("SYNC_EMAIL_BASE_URL", "https://sync.test.com")

	cfg := LoadConfig()

	if cfg.EmailProvider != "cloudflare" {
		t.Errorf("expected EmailProvider 'cloudflare', got %q", cfg.EmailProvider)
	}
	if cfg.CloudflareAccountID != "acct-test" {
		t.Errorf("expected CloudflareAccountID 'acct-test', got %q", cfg.CloudflareAccountID)
	}
	if cfg.CloudflareEmailAPIToken != "tok-test" {
		t.Errorf("expected CloudflareEmailAPIToken 'tok-test', got %q", cfg.CloudflareEmailAPIToken)
	}
	if cfg.CloudflareEmailFrom != "from@test.com" {
		t.Errorf("expected CloudflareEmailFrom 'from@test.com', got %q", cfg.CloudflareEmailFrom)
	}
	if cfg.CloudflareEmailFromName != "Test" {
		t.Errorf("expected CloudflareEmailFromName 'Test', got %q", cfg.CloudflareEmailFromName)
	}
	if cfg.CloudflareEmailReplyTo != "reply@test.com" {
		t.Errorf("expected CloudflareEmailReplyTo 'reply@test.com', got %q", cfg.CloudflareEmailReplyTo)
	}
	if cfg.AuthWebCallbackURL != "https://watch.test.com/home/login/complete" {
		t.Errorf("expected AuthWebCallbackURL 'https://watch.test.com/home/login/complete', got %q", cfg.AuthWebCallbackURL)
	}
	if cfg.AuthEmailBaseURL != "https://sync.test.com" {
		t.Errorf("expected AuthEmailBaseURL 'https://sync.test.com', got %q", cfg.AuthEmailBaseURL)
	}
}

func TestLoadConfig_LegacyDeviceAuth_TrueString(t *testing.T) {
	t.Setenv("SYNC_LEGACY_DEVICE_AUTH", "true")
	cfg := LoadConfig()
	if !cfg.LegacyDeviceAuth {
		t.Error("expected LegacyDeviceAuth=true when SYNC_LEGACY_DEVICE_AUTH=true")
	}
}

func TestLoadConfig_LegacyDeviceAuth_OneString(t *testing.T) {
	t.Setenv("SYNC_LEGACY_DEVICE_AUTH", "1")
	cfg := LoadConfig()
	if !cfg.LegacyDeviceAuth {
		t.Error("expected LegacyDeviceAuth=true when SYNC_LEGACY_DEVICE_AUTH=1")
	}
}

func TestLoadConfig_LegacyDeviceAuth_UnsetDefaultsFalse(t *testing.T) {
	t.Setenv("SYNC_LEGACY_DEVICE_AUTH", "")
	cfg := LoadConfig()
	if cfg.LegacyDeviceAuth {
		t.Error("expected LegacyDeviceAuth=false when SYNC_LEGACY_DEVICE_AUTH is unset")
	}
}

func TestLoadConfig_LegacyDeviceAuth_FalseString(t *testing.T) {
	t.Setenv("SYNC_LEGACY_DEVICE_AUTH", "false")
	cfg := LoadConfig()
	if cfg.LegacyDeviceAuth {
		t.Error("expected LegacyDeviceAuth=false when SYNC_LEGACY_DEVICE_AUTH=false")
	}
}
