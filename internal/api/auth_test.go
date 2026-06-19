package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/email"
	"github.com/marcus/td/internal/serverdb"
)

// extractTokenFromEmail parses the magic-link token from an email text body.
// The text body format is: "Click the link to sign in to td-watch: <url>\n\n..."
// where the URL ends with ?token=selector.secret.
func extractTokenFromEmail(t *testing.T, text string) string {
	t.Helper()
	// The token appears after "?token=" in the URL on the first line.
	const marker = "?token="
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("extractTokenFromEmail: marker %q not found in email body: %q", marker, text)
	}
	rest := text[idx+len(marker):]
	// Token ends at whitespace or end of string.
	end := strings.IndexAny(rest, " \t\n\r")
	if end >= 0 {
		rest = rest[:end]
	}
	if rest == "" {
		t.Fatalf("extractTokenFromEmail: empty token after marker")
	}
	return rest
}

func TestDeviceAuthFullFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.config.AllowSignup = true
	srv.config.BaseURL = "http://localhost:8080"

	// Step 1: Start login
	w := doRequest(srv, "POST", "/v1/auth/login/start", "", map[string]string{
		"email": "newuser@example.com",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login start: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var startResp loginStartResponse
	_ = json.NewDecoder(w.Body).Decode(&startResp)

	if startResp.DeviceCode == "" || startResp.UserCode == "" {
		t.Fatal("expected non-empty device_code and user_code")
	}
	if startResp.VerificationURI != "http://localhost:8080/auth/verify" {
		t.Errorf("unexpected verification_uri: %s", startResp.VerificationURI)
	}
	if startResp.ExpiresIn != 900 {
		t.Errorf("expected expires_in=900, got %d", startResp.ExpiresIn)
	}
	if startResp.Interval != 5 {
		t.Errorf("expected interval=5, got %d", startResp.Interval)
	}

	// Step 2: Poll (should be pending)
	w = doRequest(srv, "POST", "/v1/auth/login/poll", "", map[string]string{
		"device_code": startResp.DeviceCode,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poll pending: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pollResp loginPollResponse
	_ = json.NewDecoder(w.Body).Decode(&pollResp)
	if pollResp.Status != "pending" {
		t.Fatalf("expected pending, got %s", pollResp.Status)
	}

	// Step 3: Verify via form POST
	formData := url.Values{"user_code": {startResp.UserCode}}
	httpReq := httptest.NewRequest("POST", "/auth/verify", strings.NewReader(formData.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify submit: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Device authorized") {
		t.Fatal("expected success message in verify response")
	}

	// Step 4: Poll again (should be complete)
	w = doRequest(srv, "POST", "/v1/auth/login/poll", "", map[string]string{
		"device_code": startResp.DeviceCode,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poll complete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var completeResp loginPollResponse
	_ = json.NewDecoder(w.Body).Decode(&completeResp)
	if completeResp.Status != "complete" {
		t.Fatalf("expected complete, got %s", completeResp.Status)
	}
	if completeResp.APIKey == nil || *completeResp.APIKey == "" {
		t.Fatal("expected non-empty api_key")
	}
	if completeResp.UserID == nil || *completeResp.UserID == "" {
		t.Fatal("expected non-empty user_id")
	}
	if completeResp.Email == nil || *completeResp.Email != "newuser@example.com" {
		t.Fatal("expected email newuser@example.com")
	}

	// Step 5: Use the API key to call an authenticated endpoint
	w = doRequest(srv, "GET", "/v1/projects", *completeResp.APIKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("use api key: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeviceAuthExpiredCode(t *testing.T) {
	srv, store := newTestServer(t)

	// Create auth request and expire it
	ar, err := store.CreateAuthRequest("expired@example.com")
	if err != nil {
		t.Fatalf("create auth request: %v", err)
	}

	// Force expiry by updating directly
	past := time.Now().UTC().Add(-1 * time.Hour)
	store.ForceExpireAuthRequestForTest(ar.ID, past)

	// Poll should return 410
	w := doRequest(srv, "POST", "/v1/auth/login/poll", "", map[string]string{
		"device_code": ar.DeviceCode,
	})
	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeviceAuthInvalidUserCode(t *testing.T) {
	srv, _ := newTestServer(t)

	// Submit invalid user code via form
	formData := url.Values{"user_code": {"ZZZZZZ"}}
	httpReq := httptest.NewRequest("POST", "/auth/verify", strings.NewReader(formData.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify with invalid code: expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Invalid or expired code") {
		t.Fatal("expected error message for invalid code")
	}
}

func TestLoginStartInvalidEmail(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/login/start", "", map[string]string{
		"email": "notanemail",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginStartSignupDisabled(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.config.AllowSignup = false

	w := doRequest(srv, "POST", "/v1/auth/login/start", "", map[string]string{
		"email": "nobody@example.com",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLoginPollNotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/login/poll", "", map[string]string{
		"device_code": "nonexistent",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyPageGET(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/auth/verify", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Authorize Device") {
		t.Fatal("expected page title in response")
	}
}

// --- POST /v1/auth/web/start tests ---

// webStartBody is a helper to build the request body for /v1/auth/web/start.
func webStartBody(email, redirectURI, state string) map[string]string {
	return map[string]string{
		"email":        email,
		"redirect_uri": redirectURI,
		"state":        state,
	}
}

// assertWebStartGeneric200 asserts the generic 200 response shape.
func assertWebStartGeneric200(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp webStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "email_sent_if_allowed" {
		t.Errorf("status: got %q, want %q", resp.Status, "email_sent_if_allowed")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}
	if resp.RetryAfter != 60 {
		t.Errorf("retry_after: got %d, want 60", resp.RetryAfter)
	}
}

// TestWebStart_ExistingUser verifies that an existing user gets the generic 200 and one email.
func TestWebStart_ExistingUser(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("existing@example.com")

	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("existing@example.com", "", "csrf-state"))
	assertWebStartGeneric200(t, w)

	sent := ms.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(sent))
	}
	if sent[0].To != "existing@example.com" {
		t.Errorf("email To: got %q, want %q", sent[0].To, "existing@example.com")
	}
	if sent[0].Purpose != "web_login" {
		t.Errorf("email Purpose: got %q, want %q", sent[0].Purpose, "web_login")
	}
}

// TestWebStart_UnknownUser verifies that an unknown user gets the generic 200 with no email sent
// and that an AuthEventEmailSuppressed event is recorded.
func TestWebStart_UnknownUser(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("nobody@example.com", "", "csrf-state"))
	assertWebStartGeneric200(t, w)

	if len(ms.Sent()) != 0 {
		t.Fatalf("expected 0 emails sent for unknown user, got %d", len(ms.Sent()))
	}

	// Assert AuthEventEmailSuppressed was recorded.
	result, err := store.QueryAuthEvents(serverdb.AuthEventEmailSuppressed, "nobody@example.com", "", "", 10, "")
	if err != nil {
		t.Fatalf("query auth events: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("expected AuthEventEmailSuppressed to be recorded for unknown user")
	}
}

// TestWebStart_InvalidEmail verifies that a syntactically-invalid email returns 400.
func TestWebStart_InvalidEmail(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("notanemail", "", "state"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebStart_MismatchedRedirectURI verifies that a redirect_uri that doesn't match
// the configured AuthWebCallbackURL returns 400.
func TestWebStart_MismatchedRedirectURI(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.config.AuthWebCallbackURL = "https://watch.example.com/home/login/complete"

	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("user@example.com", "https://evil.example.com/callback", "state"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebStart_ResendRateLimit verifies that a second request within 60s for the same email
// gets the generic 200 but does NOT send a second email.
func TestWebStart_ResendRateLimit(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("resend@example.com")

	// First request — should send one email.
	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("resend@example.com", "", "state1"))
	assertWebStartGeneric200(t, w)
	if len(ms.Sent()) != 1 {
		t.Fatalf("first request: expected 1 email, got %d", len(ms.Sent()))
	}

	// Second request immediately after — rate-limited, no new email.
	w = doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("resend@example.com", "", "state2"))
	assertWebStartGeneric200(t, w)
	if len(ms.Sent()) != 1 {
		t.Fatalf("second request: expected still 1 email (rate limited), got %d", len(ms.Sent()))
	}
}

// TestWebStart_AllowSignupFalseUnknownUser verifies that AllowSignup=false + unknown email
// still returns generic 200 (suppressed, non-enumeration).
func TestWebStart_AllowSignupFalseUnknownUser(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.config.AllowSignup = false
	ms := email.NewMemorySender()
	srv.emailSender = ms

	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody("unknown@example.com", "", "state"))
	assertWebStartGeneric200(t, w)

	if len(ms.Sent()) != 0 {
		t.Fatalf("expected 0 emails sent (suppressed), got %d", len(ms.Sent()))
	}
}

// --- POST /v1/auth/web/exchange tests ---

// webExchangeBody is a helper to build the request body for /v1/auth/web/exchange.
func webExchangeBody(token, state string) map[string]string {
	return map[string]string{
		"token": token,
		"state": state,
	}
}

// doWebStart is a helper that calls web/start for an existing user and returns
// the plaintext token parsed from the sent email.
func doWebStart(t *testing.T, srv *Server, ms *email.MemorySender, userEmail, state string) string {
	t.Helper()
	before := len(ms.Sent())
	w := doRequest(srv, "POST", "/v1/auth/web/start", "", webStartBody(userEmail, "", state))
	if w.Code != http.StatusOK {
		t.Fatalf("web/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	sent := ms.Sent()
	if len(sent) <= before {
		t.Fatal("doWebStart: expected one new email, none sent")
	}
	return extractTokenFromEmail(t, sent[len(sent)-1].Text)
}

// TestWebExchange_FullFlow verifies the complete web login flow:
// start -> capture token -> exchange -> 200 with api_key/user_id/email/expires_at;
// that the returned key authenticates a real request; and that expires_at is ~30 days out.
func TestWebExchange_FullFlow(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	const userEmail = "webflow@example.com"
	_, _ = store.CreateUser(userEmail)

	const state = "csrf-state-abc123"
	token := doWebStart(t, srv, ms, userEmail, state)

	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(token, state))
	if w.Code != http.StatusOK {
		t.Fatalf("exchange: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp webExchangeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}

	if resp.Status != "complete" {
		t.Errorf("status: got %q, want %q", resp.Status, "complete")
	}
	if resp.APIKey == "" {
		t.Error("expected non-empty api_key")
	}
	if resp.UserID == "" {
		t.Error("expected non-empty user_id")
	}
	if resp.Email != userEmail {
		t.Errorf("email: got %q, want %q", resp.Email, userEmail)
	}
	if resp.ExpiresAt == "" {
		t.Error("expected non-empty expires_at")
	}

	// Verify the returned key authenticates a real request.
	w2 := doRequest(srv, "GET", "/v1/projects", resp.APIKey, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("use api key: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Verify expires_at is approximately 30 days out (±5 minutes tolerance).
	expiresAt, err := time.Parse(time.RFC3339, resp.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	expected := time.Now().UTC().Add(30 * 24 * time.Hour)
	diff := expiresAt.Sub(expected)
	if diff < -5*time.Minute || diff > 5*time.Minute {
		t.Errorf("expires_at %v is not ~30 days from now (diff=%v)", expiresAt, diff)
	}
}

// TestWebExchange_Replay verifies that replaying the same token returns 401 token_replayed.
func TestWebExchange_Replay(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("replay@example.com")

	const state = "replay-state"
	token := doWebStart(t, srv, ms, "replay@example.com", state)

	// First exchange — should succeed.
	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(token, state))
	if w.Code != http.StatusOK {
		t.Fatalf("first exchange: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Second exchange — should fail with token_replayed.
	w = doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(token, state))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("replay: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "token_replayed" {
		t.Errorf("replay: error code: got %q, want %q", errResp.Error.Code, "token_replayed")
	}
}

// TestWebExchange_Expired verifies that an expired token returns 401 token_expired.
func TestWebExchange_Expired(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("expired@example.com")

	const state = "expire-state"
	token := doWebStart(t, srv, ms, "expired@example.com", state)

	// Parse selector from token to force expiry.
	dotIdx := strings.Index(token, ".")
	if dotIdx < 0 {
		t.Fatal("token has no dot separator")
	}
	selector := token[:dotIdx]
	store.ForceExpireChallengeForTest(selector, time.Now().UTC().Add(-1*time.Hour))

	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(token, state))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "token_expired" {
		t.Errorf("expired: error code: got %q, want %q", errResp.Error.Code, "token_expired")
	}
}

// TestWebExchange_WrongSecret verifies that a wrong secret returns 401 invalid_token.
func TestWebExchange_WrongSecret(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("wrongsec@example.com")

	const state = "wrongsec-state"
	token := doWebStart(t, srv, ms, "wrongsec@example.com", state)

	// Corrupt the secret portion by replacing the part after the dot.
	dotIdx := strings.Index(token, ".")
	if dotIdx < 0 {
		t.Fatal("token has no dot separator")
	}
	badToken := token[:dotIdx+1] + strings.Repeat("0", 64)

	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(badToken, state))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "invalid_token" {
		t.Errorf("wrong secret: error code: got %q, want %q", errResp.Error.Code, "invalid_token")
	}
}

// TestWebExchange_WrongState verifies that a wrong state returns 401 invalid_state.
func TestWebExchange_WrongState(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	_, _ = store.CreateUser("wrongstate@example.com")

	const state = "correct-state"
	token := doWebStart(t, srv, ms, "wrongstate@example.com", state)

	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(token, "wrong-state"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong state: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "invalid_state" {
		t.Errorf("wrong state: error code: got %q, want %q", errResp.Error.Code, "invalid_state")
	}
}

// TestWebExchange_MalformedToken verifies that a token with no dot returns 400.
func TestWebExchange_MalformedToken(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody("nodottoken", "some-state"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed token: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebExchange_NonExistentSelector verifies that a non-existent selector returns 401 invalid_token.
func TestWebExchange_NonExistentSelector(t *testing.T) {
	srv, _ := newTestServer(t)

	fakeToken := "deadbeefdeadbeefdeadbeefdeadbeef.000000000000000000000000000000000000000000000000000000000000dead"
	w := doRequest(srv, "POST", "/v1/auth/web/exchange", "", webExchangeBody(fakeToken, "some-state"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("nonexistent selector: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "invalid_token" {
		t.Errorf("nonexistent selector: error code: got %q, want %q", errResp.Error.Code, "invalid_token")
	}
}
