package email_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcus/td/internal/email"
)

// newTestSender constructs a CloudflareSender pointed at srv with the given config.
// It always supplies the required accountID, apiToken, and fromAddress unless the
// test is specifically testing missing-field validation.
func newTestCloudflareSender(t *testing.T, srv *httptest.Server, cfg email.EmailConfig) *email.CloudflareSender {
	t.Helper()
	cfg.AccountID = "test-account"
	cfg.APIToken = "test-token"
	cfg.From = "login@example.com"
	// Override BaseURL to point at the test server, using the AccountID-based
	// subpath. The CloudflareSender builds: {baseURL}/accounts/{accountID}/email/...
	// so we strip that prefix from the server URL.
	cfg.BaseURL = srv.URL
	s, err := email.NewCloudflareSender(cfg)
	if err != nil {
		t.Fatalf("NewCloudflareSender: unexpected error: %v", err)
	}
	return s
}

// cfSuccessBody is a minimal Cloudflare success envelope.
const cfSuccessBody = `{"success":true,"errors":[],"messages":[],"result":{}}`

// cfErrorBody returns a Cloudflare error envelope with the given message.
func cfErrorBody(code int, msg string) string {
	return `{"success":false,"errors":[{"code":` + strings.ReplaceAll(
		strings.ReplaceAll(
			`{"code":CODE,"message":"MSG"}`,
			"CODE", strings.TrimSpace(string(rune('0'+code))),
		),
		"MSG", msg,
	) + `],"messages":[],"result":null}`
}

// buildCFErrorBody builds a proper Cloudflare error envelope JSON.
func buildCFErrorBody(code int, msg string) string {
	type cfErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type envelope struct {
		Success bool    `json:"success"`
		Errors  []cfErr `json:"errors"`
	}
	b, _ := json.Marshal(envelope{
		Success: false,
		Errors:  []cfErr{{Code: code, Message: msg}},
	})
	return string(b)
}

func TestCloudflareSender_Success(t *testing.T) {
	var capturedReq struct {
		body   map[string]any
		bearer string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq.bearer = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfSuccessBody))
	}))
	defer srv.Close()

	s := newTestCloudflareSender(t, srv, email.EmailConfig{
		FromName: "td-watch",
		ReplyTo:  "support@example.com",
	})

	msg := email.LoginEmail{
		To:      "alice@example.com",
		Subject: "Your login link",
		HTML:    "<a href='https://example.com/login?token=secret'>Login</a>",
		Text:    "Visit: https://example.com/login?token=secret",
	}

	err := s.SendLoginLink(context.Background(), msg)
	if err != nil {
		t.Fatalf("SendLoginLink returned unexpected error: %v", err)
	}

	// Verify Bearer auth header.
	if capturedReq.bearer != "Bearer test-token" {
		t.Errorf("Authorization header: got %q, want %q", capturedReq.bearer, "Bearer test-token")
	}

	// Verify request body shape.
	fromRaw, ok := capturedReq.body["from"].(map[string]any)
	if !ok {
		t.Fatalf("from field missing or wrong type: %v", capturedReq.body["from"])
	}
	if fromRaw["address"] != "login@example.com" {
		t.Errorf("from.address: got %v, want %q", fromRaw["address"], "login@example.com")
	}
	if fromRaw["name"] != "td-watch" {
		t.Errorf("from.name: got %v, want %q", fromRaw["name"], "td-watch")
	}

	toRaw, ok := capturedReq.body["to"].([]any)
	if !ok || len(toRaw) != 1 || toRaw[0] != "alice@example.com" {
		t.Errorf("to field: got %v, want [alice@example.com]", capturedReq.body["to"])
	}

	if capturedReq.body["subject"] != "Your login link" {
		t.Errorf("subject: got %v, want %q", capturedReq.body["subject"], "Your login link")
	}

	// Cloudflare requires reply_to as a plain string, not an object.
	replyToRaw, ok := capturedReq.body["reply_to"].(string)
	if !ok {
		t.Fatalf("reply_to field missing or not a string: %v", capturedReq.body["reply_to"])
	}
	if replyToRaw != "support@example.com" {
		t.Errorf("reply_to: got %v, want %q", replyToRaw, "support@example.com")
	}
}

func TestCloudflareSender_OmitsReplyToWhenEmpty(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfSuccessBody))
	}))
	defer srv.Close()

	// No ReplyTo set.
	s := newTestCloudflareSender(t, srv, email.EmailConfig{})

	err := s.SendLoginLink(context.Background(), email.LoginEmail{
		To:      "bob@example.com",
		Subject: "Login",
	})
	if err != nil {
		t.Fatalf("SendLoginLink returned unexpected error: %v", err)
	}

	if _, present := capturedBody["reply_to"]; present {
		t.Error("reply_to should be omitted when replyTo is empty")
	}
}

func TestCloudflareSender_OmitsFromNameWhenEmpty(t *testing.T) {
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfSuccessBody))
	}))
	defer srv.Close()

	// No FromName set.
	s := newTestCloudflareSender(t, srv, email.EmailConfig{})

	err := s.SendLoginLink(context.Background(), email.LoginEmail{
		To:      "carol@example.com",
		Subject: "Login",
	})
	if err != nil {
		t.Fatalf("SendLoginLink returned unexpected error: %v", err)
	}

	fromRaw, ok := capturedBody["from"].(map[string]any)
	if !ok {
		t.Fatalf("from field missing or wrong type")
	}
	if _, present := fromRaw["name"]; present {
		t.Error("from.name should be omitted when fromName is empty")
	}
}

func TestCloudflareSender_CFErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(buildCFErrorBody(1001, "Invalid sender address")))
	}))
	defer srv.Close()

	s := newTestCloudflareSender(t, srv, email.EmailConfig{})

	err := s.SendLoginLink(context.Background(), email.LoginEmail{
		To:      "dave@example.com",
		Subject: "Login",
	})
	if err == nil {
		t.Fatal("expected error from CF error envelope, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid sender address") {
		t.Errorf("error should contain CF message %q, got: %v", "Invalid sender address", err)
	}
	// API token must never appear in the error.
	if strings.Contains(err.Error(), "test-token") {
		t.Errorf("error message must not contain the API token; got: %v", err)
	}
}

func TestCloudflareSender_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":500,"message":"internal error"}]}`))
	}))
	defer srv.Close()

	s := newTestCloudflareSender(t, srv, email.EmailConfig{})

	err := s.SendLoginLink(context.Background(), email.LoginEmail{
		To:      "eve@example.com",
		Subject: "Login",
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestNewCloudflareSender_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  email.EmailConfig
	}{
		{
			name: "missing accountID",
			cfg: email.EmailConfig{
				APIToken: "tok",
				From:     "from@example.com",
			},
		},
		{
			name: "missing apiToken",
			cfg: email.EmailConfig{
				AccountID: "acct",
				From:      "from@example.com",
			},
		},
		{
			name: "missing fromAddress",
			cfg: email.EmailConfig{
				AccountID: "acct",
				APIToken:  "tok",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := email.NewCloudflareSender(tc.cfg)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
