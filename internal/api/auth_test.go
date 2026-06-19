package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

// --- POST /v1/auth/device/start tests ---

// deviceStartBody builds a request body for POST /v1/auth/device/start.
func deviceStartBody(email, codeChallenge, codeChallengeMethod, deviceName string) map[string]string {
	return map[string]string{
		"email":                 email,
		"code_challenge":        codeChallenge,
		"code_challenge_method": codeChallengeMethod,
		"device_name":           deviceName,
	}
}

// TestDeviceStart_KnownUser verifies that a known user receives 200 with
// device_code, expires_in=900, interval=5, email_sent=true, and exactly one
// email whose text body contains the approval link with the selector.
func TestDeviceStart_KnownUser(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	_, _ = store.CreateUser("cli@example.com")

	w := doRequest(srv, "POST", "/v1/auth/device/start", "", deviceStartBody(
		"cli@example.com",
		"abc123-code-challenge",
		"S256",
		"marcus-macbook",
	))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp deviceStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DeviceCode == "" {
		t.Error("expected non-empty device_code")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}
	if resp.Interval != 5 {
		t.Errorf("interval: got %d, want 5", resp.Interval)
	}
	if !resp.EmailSent {
		t.Error("expected email_sent=true")
	}

	sent := ms.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(sent))
	}
	if sent[0].To != "cli@example.com" {
		t.Errorf("email To: got %q, want %q", sent[0].To, "cli@example.com")
	}
	if sent[0].Purpose != "device_login" {
		t.Errorf("email Purpose: got %q, want %q", sent[0].Purpose, "device_login")
	}
	// Approval link must contain the selector portion of the token.
	const approvalMarker = "/auth/device/approve?token="
	if !strings.Contains(sent[0].Text, approvalMarker) {
		t.Errorf("email body missing approval link marker %q; body: %s", approvalMarker, sent[0].Text)
	}
	// Extract selector from link and verify it is non-empty.
	idx := strings.Index(sent[0].Text, approvalMarker)
	tokenPart := sent[0].Text[idx+len(approvalMarker):]
	end := strings.IndexAny(tokenPart, " \t\n\r")
	if end >= 0 {
		tokenPart = tokenPart[:end]
	}
	dotIdx := strings.Index(tokenPart, ".")
	if dotIdx < 0 {
		t.Fatalf("approval token has no dot separator: %q", tokenPart)
	}
	selector := tokenPart[:dotIdx]
	if selector == "" {
		t.Error("expected non-empty selector in approval link")
	}
}

// TestDeviceStart_UnknownUser verifies that an unknown email returns 200 with
// email_sent=true and a device_code (identical shape to known-user response),
// but sends 0 emails and records AuthEventEmailSuppressed.
func TestDeviceStart_UnknownUser(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms

	w := doRequest(srv, "POST", "/v1/auth/device/start", "", deviceStartBody(
		"nobody@example.com",
		"abc123-code-challenge",
		"S256",
		"some-machine",
	))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp deviceStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Response shape must be identical to the known-user case (non-enumeration).
	if resp.DeviceCode == "" {
		t.Error("expected non-empty device_code for unknown user (non-enumeration)")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}
	if resp.Interval != 5 {
		t.Errorf("interval: got %d, want 5", resp.Interval)
	}
	if !resp.EmailSent {
		t.Error("expected email_sent=true for unknown user (non-enumeration)")
	}

	if len(ms.Sent()) != 0 {
		t.Fatalf("expected 0 emails for unknown user, got %d", len(ms.Sent()))
	}

	// AuthEventEmailSuppressed must be recorded.
	result, err := store.QueryAuthEvents(serverdb.AuthEventEmailSuppressed, "nobody@example.com", "", "", 10, "")
	if err != nil {
		t.Fatalf("query auth events: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("expected AuthEventEmailSuppressed to be recorded for unknown user")
	}
}

// TestDeviceStart_MissingCodeChallenge verifies that a missing code_challenge returns 400.
func TestDeviceStart_MissingCodeChallenge(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/device/start", "", map[string]string{
		"email":                 "user@example.com",
		"code_challenge_method": "S256",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeviceStart_BadChallengeMethod verifies that code_challenge_method != "S256" returns 400.
func TestDeviceStart_BadChallengeMethod(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/device/start", "", map[string]string{
		"email":                 "user@example.com",
		"code_challenge":        "some-challenge",
		"code_challenge_method": "plain",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeviceStart_InvalidEmail verifies that a syntactically-invalid email returns 400.
func TestDeviceStart_InvalidEmail(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/device/start", "", map[string]string{
		"email":                 "notanemail",
		"code_challenge":        "some-challenge",
		"code_challenge_method": "S256",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
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

// --- GET /auth/device/approve + POST /v1/auth/device/poll tests (D4) ---

// makePKCEPair generates a random code_verifier and its S256 code_challenge.
func makePKCEPair(t *testing.T) (verifier, challenge string) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand read: %v", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

// extractDeviceApprovalToken parses the ?token=selector.secret from the approval
// link in the email text body.
func extractDeviceApprovalToken(t *testing.T, text string) string {
	t.Helper()
	const marker = "/auth/device/approve?token="
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("extractDeviceApprovalToken: marker %q not found in: %q", marker, text)
	}
	rest := text[idx+len(marker):]
	end := strings.IndexAny(rest, " \t\n\r")
	if end >= 0 {
		rest = rest[:end]
	}
	if rest == "" {
		t.Fatal("extractDeviceApprovalToken: empty token")
	}
	return rest
}

// doDeviceStart is a helper that calls device/start for an existing user,
// returning the device_code from the response and the raw approval token from
// the email. The caller must create the user before calling this.
func doDeviceStart(t *testing.T, srv *Server, ms *email.MemorySender, userEmail, codeChallenge string) (deviceCode, approvalToken string) {
	t.Helper()
	before := len(ms.Sent())
	w := doRequest(srv, "POST", "/v1/auth/device/start", "", deviceStartBody(
		userEmail, codeChallenge, "S256", "test-device",
	))
	if w.Code != http.StatusOK {
		t.Fatalf("device/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp deviceStartResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode device/start response: %v", err)
	}
	deviceCode = resp.DeviceCode
	sent := ms.Sent()
	if len(sent) <= before {
		t.Fatal("doDeviceStart: expected one new email, none sent")
	}
	approvalToken = extractDeviceApprovalToken(t, sent[len(sent)-1].Text)
	return
}

// doDeviceApprove GETs /auth/device/approve?token=<token> and returns the response.
func doDeviceApprove(srv *Server, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/auth/device/approve?token="+url.QueryEscape(token), nil)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	return w
}

// TestDevicePoll_FullFlow tests the complete device login flow:
// start -> poll(pending) -> approve(email link) -> poll(complete) with api_key;
// returned key authenticates a real request; expires_at ~365d.
func TestDevicePoll_FullFlow(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	const userEmail = "cliuser@example.com"
	_, _ = store.CreateUser(userEmail)

	verifier, challenge := makePKCEPair(t)
	deviceCode, approvalToken := doDeviceStart(t, srv, ms, userEmail, challenge)

	// Poll before approve — should be pending.
	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poll before approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var pendingResp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&pendingResp)
	if pendingResp.Status != "pending" {
		t.Fatalf("poll before approve: expected pending, got %q", pendingResp.Status)
	}

	// Approve via email link.
	wa := doDeviceApprove(srv, approvalToken)
	if wa.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d: %s", wa.Code, wa.Body.String())
	}
	if !strings.Contains(wa.Body.String(), "Device approved") {
		t.Fatalf("approve: expected success page, got: %s", wa.Body.String())
	}
	if wa.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("approve: missing Referrer-Policy: no-referrer header")
	}

	// Poll after approve — should be complete.
	w = doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poll after approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var completeResp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&completeResp)
	if completeResp.Status != "complete" {
		t.Fatalf("poll after approve: expected complete, got %q", completeResp.Status)
	}
	if completeResp.APIKey == nil || *completeResp.APIKey == "" {
		t.Fatal("poll after approve: expected non-empty api_key")
	}
	if completeResp.UserID == nil || *completeResp.UserID == "" {
		t.Fatal("poll after approve: expected non-empty user_id")
	}
	if completeResp.Email == nil || *completeResp.Email != userEmail {
		t.Fatalf("poll after approve: expected email %q, got %v", userEmail, completeResp.Email)
	}
	if completeResp.ExpiresAt == nil || *completeResp.ExpiresAt == "" {
		t.Fatal("poll after approve: expected non-empty expires_at")
	}

	// Returned key must authenticate a real request.
	w2 := doRequest(srv, "GET", "/v1/projects", *completeResp.APIKey, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("use api key: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// expires_at should be ~365 days from now (±5 minutes tolerance).
	expiresAt, err := time.Parse(time.RFC3339, *completeResp.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	expected := time.Now().UTC().Add(365 * 24 * time.Hour)
	diff := expiresAt.Sub(expected)
	if diff < -5*time.Minute || diff > 5*time.Minute {
		t.Errorf("expires_at %v is not ~365 days from now (diff=%v)", expiresAt, diff)
	}
}

// TestDevicePoll_BeforeApprove verifies poll returns pending before the email link is clicked.
func TestDevicePoll_BeforeApprove(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	_, _ = store.CreateUser("beforeapprove@example.com")
	verifier, challenge := makePKCEPair(t)
	deviceCode, _ := doDeviceStart(t, srv, ms, "beforeapprove@example.com", challenge)

	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "pending" {
		t.Fatalf("expected pending, got %q", resp.Status)
	}
}

// TestDevicePoll_WrongSecret verifies that approving with a wrong secret leaves
// the challenge pending and subsequent poll still returns pending (not complete).
func TestDevicePoll_WrongSecret(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	_, _ = store.CreateUser("wrongsecret@example.com")
	verifier, challenge := makePKCEPair(t)
	deviceCode, approvalToken := doDeviceStart(t, srv, ms, "wrongsecret@example.com", challenge)

	// Corrupt the secret portion of the approval token.
	dotIdx := strings.Index(approvalToken, ".")
	if dotIdx < 0 {
		t.Fatal("approval token has no dot separator")
	}
	badToken := approvalToken[:dotIdx+1] + strings.Repeat("0", 64)

	wa := doDeviceApprove(srv, badToken)
	if wa.Code != http.StatusOK {
		t.Fatalf("approve with wrong secret: expected 200 HTML, got %d", wa.Code)
	}
	body := wa.Body.String()
	if strings.Contains(body, "Device approved") {
		t.Fatal("approve with wrong secret: should NOT show success page")
	}

	// Subsequent poll should still return pending (challenge was not verified).
	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("poll after bad approve: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "pending" {
		t.Fatalf("poll after bad approve: expected pending, got %q", resp.Status)
	}
}

// TestDevicePoll_WrongVerifier verifies that poll after approve with wrong
// code_verifier returns 401 invalid_verifier.
func TestDevicePoll_WrongVerifier(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	_, _ = store.CreateUser("wrongverifier@example.com")
	_, challenge := makePKCEPair(t)
	deviceCode, approvalToken := doDeviceStart(t, srv, ms, "wrongverifier@example.com", challenge)

	// Approve the link (correct token).
	wa := doDeviceApprove(srv, approvalToken)
	if wa.Code != http.StatusOK || !strings.Contains(wa.Body.String(), "Device approved") {
		t.Fatalf("approve: expected success, got %d: %s", wa.Code, wa.Body.String())
	}

	// Poll with a WRONG verifier.
	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": "wrong-verifier-that-does-not-match",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("poll wrong verifier: expected 401, got %d: %s", w.Code, w.Body.String())
	}
	var errResp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Error.Code != "invalid_verifier" {
		t.Errorf("poll wrong verifier: error code: got %q, want %q", errResp.Error.Code, "invalid_verifier")
	}
}

// TestDevicePoll_DoubleIssuePrevented verifies that re-polling after a successful
// complete returns 410 and that only ONE device-auth key exists for the user.
func TestDevicePoll_DoubleIssuePrevented(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	const userEmail = "doubleissue@example.com"
	_, _ = store.CreateUser(userEmail)
	user, _ := store.GetUserByEmail(userEmail)

	verifier, challenge := makePKCEPair(t)
	deviceCode, approvalToken := doDeviceStart(t, srv, ms, userEmail, challenge)

	// Approve.
	doDeviceApprove(srv, approvalToken)

	// First poll — should succeed.
	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("first poll: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "complete" {
		t.Fatalf("first poll: expected complete, got %q", resp.Status)
	}

	// Second poll — should return 410 (no second key).
	w2 := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   deviceCode,
		"code_verifier": verifier,
	})
	if w2.Code != http.StatusGone {
		t.Fatalf("second poll: expected 410, got %d: %s", w2.Code, w2.Body.String())
	}

	// Assert exactly ONE device-auth key exists for the user.
	keys, err := store.ListAPIKeysForUser(user.ID)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	deviceAuthKeys := 0
	for _, k := range keys {
		if k.Name == "device-auth" {
			deviceAuthKeys++
		}
	}
	if deviceAuthKeys != 1 {
		t.Fatalf("expected exactly 1 device-auth key, got %d", deviceAuthKeys)
	}
}

// TestDevicePoll_ApproveLinkUsedTwice verifies that clicking the approval link
// a second time shows the "already used" page.
func TestDevicePoll_ApproveLinkUsedTwice(t *testing.T) {
	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	_, _ = store.CreateUser("reapprove@example.com")
	_, challenge := makePKCEPair(t)
	_, approvalToken := doDeviceStart(t, srv, ms, "reapprove@example.com", challenge)

	// First approval — should succeed.
	wa1 := doDeviceApprove(srv, approvalToken)
	if wa1.Code != http.StatusOK || !strings.Contains(wa1.Body.String(), "Device approved") {
		t.Fatalf("first approve: expected success, got %d: %s", wa1.Code, wa1.Body.String())
	}

	// Second approval — should show already-used page.
	wa2 := doDeviceApprove(srv, approvalToken)
	if wa2.Code != http.StatusOK {
		t.Fatalf("second approve: expected 200 HTML, got %d", wa2.Code)
	}
	if !strings.Contains(wa2.Body.String(), "already been used") {
		t.Fatalf("second approve: expected already-used message, got: %s", wa2.Body.String())
	}
}

// TestDevicePoll_UnknownDeviceCodePending verifies that an unknown/garbage
// device_code returns 200 pending (non-enumeration), NOT 410.
func TestDevicePoll_UnknownDeviceCodePending(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, "POST", "/v1/auth/device/poll", "", map[string]string{
		"device_code":   "totally-bogus-device-code-that-does-not-exist",
		"code_verifier": "some-verifier",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("unknown device_code: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp devicePollResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "pending" {
		t.Fatalf("unknown device_code: expected pending, got %q", resp.Status)
	}
}
