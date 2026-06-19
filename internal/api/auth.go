package api

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/marcus/td/internal/email"
	"github.com/marcus/td/internal/serverdb"
)

//go:embed templates/verify.html
var verifyFS embed.FS

var verifyTmpl = template.Must(template.ParseFS(verifyFS, "templates/verify.html"))

// verifyPageData holds template data for the verify page.
type verifyPageData struct {
	Error   string
	Success bool
}

// loginStartRequest is the JSON body for POST /v1/auth/login/start.
type loginStartRequest struct {
	Email string `json:"email"`
}

// loginStartResponse is the JSON response for POST /v1/auth/login/start.
type loginStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// loginPollRequest is the JSON body for POST /v1/auth/login/poll.
type loginPollRequest struct {
	DeviceCode string `json:"device_code"`
}

// loginPollResponse is the JSON response for POST /v1/auth/login/poll.
type loginPollResponse struct {
	Status    string  `json:"status"`
	APIKey    *string `json:"api_key,omitempty"`
	UserID    *string `json:"user_id,omitempty"`
	Email     *string `json:"email,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// handleLoginStart handles POST /v1/auth/login/start.
func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	var req loginStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "valid email is required")
		return
	}

	if !s.config.AllowSignup {
		user, err := s.store.GetUserByEmail(req.Email)
		if err != nil {
			logFor(r.Context()).Error("check user for login", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to check user")
			return
		}
		if user == nil {
			writeError(w, http.StatusForbidden, "signup_disabled", "signups are disabled")
			return
		}
	}

	ar, err := s.store.CreateAuthRequest(req.Email)
	if err != nil {
		logFor(r.Context()).Error("create auth request", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create auth request")
		return
	}

	s.logAuthEvent(ar.ID, req.Email, serverdb.AuthEventStarted, map[string]string{
		"ip":         clientIP(r, s.config.TrustedProxies),
		"user_agent": r.Header.Get("User-Agent"),
	})

	writeJSON(w, http.StatusOK, loginStartResponse{
		DeviceCode:      ar.DeviceCode,
		UserCode:        ar.UserCode,
		VerificationURI: s.config.BaseURL + "/auth/verify",
		ExpiresIn:       int(serverdb.AuthRequestTTL.Seconds()),
		Interval:        serverdb.PollInterval,
	})
}

// handleLoginPoll handles POST /v1/auth/login/poll.
func (s *Server) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
	var req loginPollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_code is required")
		return
	}

	ar, err := s.store.GetAuthRequestByDeviceCode(req.DeviceCode)
	if err != nil {
		logFor(r.Context()).Error("get auth request", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to get auth request")
		return
	}
	if ar == nil {
		writeError(w, http.StatusNotFound, "not_found", "auth request not found")
		return
	}

	// Check expiry
	if ar.Status == serverdb.AuthStatusExpired || ar.ExpiresAt.Before(time.Now().UTC()) {
		writeError(w, http.StatusGone, "expired", "auth request has expired")
		return
	}

	if ar.Status == serverdb.AuthStatusUsed {
		writeError(w, http.StatusGone, "already_used", "auth request already used")
		return
	}

	if ar.Status == serverdb.AuthStatusPending {
		writeJSON(w, http.StatusOK, loginPollResponse{Status: "pending"})
		return
	}

	// Status is verified — complete the flow
	completed, err := s.store.CompleteAuthRequest(ar.DeviceCode)
	if err != nil {
		logFor(r.Context()).Error("complete auth request", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to complete auth request")
		return
	}
	if completed == nil || completed.UserID == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "auth request not in expected state")
		return
	}

	// Generate API key with 1-year expiry
	expiry := time.Now().UTC().Add(365 * 24 * time.Hour)
	plaintext, ak, err := s.store.GenerateAPIKey(*completed.UserID, "device-auth", "sync", &expiry)
	if err != nil {
		logFor(r.Context()).Error("generate api key for device auth", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate api key")
		return
	}

	logFor(r.Context()).Info("device auth complete", "user_id", *completed.UserID)

	if err := s.store.SetAuthRequestAPIKey(completed.ID, ak.ID); err != nil {
		slog.Warn("set auth request api key", "err", err)
	}

	s.logAuthEvent(completed.ID, completed.Email, serverdb.AuthEventKeyIssued, map[string]string{
		"ip":         clientIP(r, s.config.TrustedProxies),
		"user_agent": r.Header.Get("User-Agent"),
	})

	expiresAtStr := expiry.Format(time.RFC3339)
	writeJSON(w, http.StatusOK, loginPollResponse{
		Status:    "complete",
		APIKey:    &plaintext,
		UserID:    completed.UserID,
		Email:     &completed.Email,
		ExpiresAt: &expiresAtStr,
	})
}

// handleVerifyPage handles GET /auth/verify.
func (s *Server) handleVerifyPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = verifyTmpl.Execute(w, verifyPageData{})
}

// handleVerifySubmit handles POST /auth/verify.
func (s *Server) handleVerifySubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := r.ParseForm(); err != nil {
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Invalid form data."})
		return
	}

	userCode := r.FormValue("user_code")
	userCode = strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(userCode, "-", "")))

	if userCode == "" {
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Please enter a code."})
		return
	}

	if len(userCode) != 6 || !isValidUserCode(userCode) {
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Invalid or expired code."})
		return
	}

	ar, err := s.store.GetAuthRequestByUserCode(userCode)
	if err != nil {
		logFor(r.Context()).Error("get auth request by user code", "err", err)
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Something went wrong. Please try again."})
		return
	}
	if ar == nil {
		slog.Warn("verify failed", "reason", "invalid_or_expired")
		s.logAuthEvent("", "", serverdb.AuthEventFailed, map[string]string{
			"failure_reason": "invalid_code",
			"ip":             clientIP(r, s.config.TrustedProxies),
			"user_agent":     r.Header.Get("User-Agent"),
		})
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Invalid or expired code."})
		return
	}

	// Look up or create user
	user, err := s.store.GetUserByEmail(ar.Email)
	if err != nil {
		logFor(r.Context()).Error("get user by email", "err", err)
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Something went wrong. Please try again."})
		return
	}

	if user == nil {
		if !s.config.AllowSignup {
			slog.Warn("signup denied", "email", ar.Email)
			_ = verifyTmpl.Execute(w, verifyPageData{Error: "Signups are disabled."})
			return
		}
		user, err = s.store.CreateUser(ar.Email)
		if err != nil {
			logFor(r.Context()).Error("create user during verify", "err", err)
			_ = verifyTmpl.Execute(w, verifyPageData{Error: "Failed to create account. Please try again."})
			return
		}
	}

	if err := s.store.VerifyAuthRequest(userCode, user.ID); err != nil {
		logFor(r.Context()).Error("verify auth request", "err", err)
		_ = verifyTmpl.Execute(w, verifyPageData{Error: "Failed to authorize device. Code may have expired."})
		return
	}

	s.logAuthEvent(ar.ID, ar.Email, serverdb.AuthEventCodeVerified, map[string]string{
		"ip":         clientIP(r, s.config.TrustedProxies),
		"user_agent": r.Header.Get("User-Agent"),
	})

	logFor(r.Context()).Info("device verified", "email", ar.Email)
	_ = verifyTmpl.Execute(w, verifyPageData{Success: true})
}

// webStartRequest is the JSON body for POST /v1/auth/web/start.
type webStartRequest struct {
	Email       string `json:"email"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
}

// webStartResponse is the JSON response for POST /v1/auth/web/start.
type webStartResponse struct {
	Status     string `json:"status"`
	ExpiresIn  int    `json:"expires_in"`
	RetryAfter int    `json:"retry_after"`
}

// handleWebStart handles POST /v1/auth/web/start.
// It always returns the generic 200 response for any syntactically-valid email
// to avoid user enumeration. Only malformed JSON or invalid email returns 400.
func (s *Server) handleWebStart(w http.ResponseWriter, r *http.Request) {
	var req webStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "valid email is required")
		return
	}

	// Step 2: Validate redirect_uri when AuthWebCallbackURL is configured.
	if s.config.AuthWebCallbackURL != "" && req.RedirectURI != s.config.AuthWebCallbackURL {
		writeError(w, http.StatusBadRequest, "bad_request", "redirect_uri not allowed")
		return
	}

	ip := clientIP(r, s.config.TrustedProxies)
	ua := r.Header.Get("User-Agent")
	meta := map[string]string{"ip": ip, "user_agent": ua}

	genericOK := func() {
		writeJSON(w, http.StatusOK, webStartResponse{
			Status:     "email_sent_if_allowed",
			ExpiresIn:  900,
			RetryAfter: 60,
		})
	}

	// Step 3: Check resend rate limit (1 per minute per email).
	rateLimitKey := "web-start:" + strings.ToLower(req.Email)
	if !s.rateLimiter.Allow(rateLimitKey, 1) {
		if err := s.store.InsertRateLimitEvent("", ip, "auth"); err != nil {
			slog.Warn("log rate limit event for web-start", "err", err)
		}
		// Return generic 200 — do not reveal rate limiting.
		genericOK()
		return
	}

	// Step 4: Look up user; unknown email => suppressed path.
	user, err := s.store.GetUserByEmail(req.Email)
	if err != nil {
		logFor(r.Context()).Error("get user by email for web start", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to look up user")
		return
	}
	if user == nil {
		// Unknown user — suppressed regardless of AllowSignup.
		s.logAuthEvent("", req.Email, serverdb.AuthEventEmailSuppressed, meta)
		genericOK()
		return
	}

	// Step 6: Compute state_hash = sha256(state) hex.
	stateSum := sha256.Sum256([]byte(req.State))
	stateHash := hex.EncodeToString(stateSum[:])

	// Step 7: Create email challenge.
	redirectURI := req.RedirectURI
	selector, plaintextSecret, err := s.store.CreateEmailChallenge(
		req.Email,
		"web_login",
		user.ID,
		serverdb.ChallengeOptions{
			RedirectURI: &redirectURI,
			StateHash:   &stateHash,
			IP:          &ip,
			UserAgent:   &ua,
		},
	)
	if err != nil {
		logFor(r.Context()).Error("create email challenge for web start", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create auth challenge")
		return
	}

	// Step 8: Build magic link. NEVER log the link or the secret.
	linkURL := s.config.AuthWebCallbackURL + "?token=" + selector + "." + plaintextSecret

	// Step 9: Build email body.
	htmlBody := fmt.Sprintf(
		`<p>Click the link below to sign in to td-watch.</p>`+
			`<p><a href="%s" style="display:inline-block;padding:12px 24px;background:#0070f3;color:#fff;text-decoration:none;border-radius:4px;">Sign in to td-watch</a></p>`+
			`<p>Or copy this link into your browser (do not share it):</p>`+
			`<p style="word-break:break-all;">%s</p>`+
			`<p>This link expires in 15 minutes.</p>`,
		linkURL, linkURL,
	)
	textBody := "Click the link to sign in to td-watch: " + linkURL + "\n\nThis link expires in 15 minutes."

	traceID := getRequestID(r.Context())
	msg := email.LoginEmail{
		To:      req.Email,
		Subject: "Sign in to td-watch",
		Text:    textBody,
		HTML:    htmlBody,
		Purpose: "web_login",
		TraceID: traceID,
	}

	// Step 10: Send email.
	if err := s.emailSender.SendLoginLink(r.Context(), msg); err != nil {
		// Step 11: Send failure — log at Warn (NO token in message), record failure, return 500.
		logFor(r.Context()).Warn("send login link failed for web start", "err", err)
		s.logAuthEvent(selector, req.Email, serverdb.AuthEventLoginFailed, meta)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to send login email")
		return
	}

	// Step 12: Record success events; auth_request_id = selector (the challenge selector).
	s.logAuthEvent(selector, req.Email, serverdb.AuthEventChallengeStarted, meta)
	s.logAuthEvent(selector, req.Email, serverdb.AuthEventEmailSent, meta)

	// Step 13: Generic 200.
	genericOK()
}

// webExchangeRequest is the JSON body for POST /v1/auth/web/exchange.
type webExchangeRequest struct {
	Token string `json:"token"`
	State string `json:"state"`
}

// webExchangeResponse is the JSON response for POST /v1/auth/web/exchange.
type webExchangeResponse struct {
	Status    string `json:"status"`
	APIKey    string `json:"api_key"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	ExpiresAt string `json:"expires_at"`
}

// handleWebExchange handles POST /v1/auth/web/exchange.
// It consumes a one-time magic-link token, verifies the CSRF state, and issues
// a 30-day scoped API key.
func (s *Server) handleWebExchange(w http.ResponseWriter, r *http.Request) {
	var req webExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	// Step 2: Parse token as selector + "." + secret; split on first dot only.
	dotIdx := strings.IndexByte(req.Token, '.')
	if dotIdx < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed token")
		return
	}
	selector := req.Token[:dotIdx]
	plaintextSecret := req.Token[dotIdx+1:]
	if selector == "" || plaintextSecret == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed token")
		return
	}

	ip := clientIP(r, s.config.TrustedProxies)
	ua := r.Header.Get("User-Agent")
	meta := map[string]string{"ip": ip, "user_agent": ua}

	// Step 3: Consume the challenge atomically.
	challenge, err := s.store.ConsumeChallenge(selector, plaintextSecret)
	if err != nil {
		switch err {
		case serverdb.ErrChallengeNotFound, serverdb.ErrChallengeInvalidToken:
			s.logAuthEvent(selector, "", serverdb.AuthEventLoginFailed, meta)
			writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		case serverdb.ErrChallengeAlreadyConsumed:
			s.logAuthEvent(selector, "", serverdb.AuthEventLoginFailed, meta)
			writeError(w, http.StatusUnauthorized, "token_replayed", "token has already been used")
		case serverdb.ErrChallengeExpired:
			s.logAuthEvent(selector, "", serverdb.AuthEventLoginFailed, meta)
			writeError(w, http.StatusUnauthorized, "token_expired", "token has expired")
		default:
			logFor(r.Context()).Error("consume challenge for web exchange", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to consume token")
		}
		return
	}

	// Step 4: Verify purpose == "web_login".
	if challenge.Purpose != "web_login" {
		s.logAuthEvent(selector, challenge.Email, serverdb.AuthEventLoginFailed, meta)
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return
	}

	// Step 5: Verify state hash — sha256(req.State) hex must match challenge.StateHash.
	stateSum := sha256.Sum256([]byte(req.State))
	stateHex := hex.EncodeToString(stateSum[:])
	if challenge.StateHash == nil || stateHex != *challenge.StateHash {
		s.logAuthEvent(selector, challenge.Email, serverdb.AuthEventLoginFailed, meta)
		writeError(w, http.StatusUnauthorized, "invalid_state", "state mismatch")
		return
	}

	// Step 6: Verify UserID is non-nil (suppressed challenges have no user).
	if challenge.UserID == nil {
		s.logAuthEvent(selector, challenge.Email, serverdb.AuthEventLoginFailed, meta)
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid or expired token")
		return
	}

	// Step 7: Issue 30-day API key with scope "sync", name "td-watch-web".
	expiry := time.Now().UTC().Add(30 * 24 * time.Hour)
	plaintext, ak, err := s.store.GenerateAPIKey(*challenge.UserID, "td-watch-web", "sync", &expiry)
	if err != nil {
		logFor(r.Context()).Error("generate api key for web exchange", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate api key")
		return
	}

	// Step 8: Log success event with key_id only — never log the plaintext key.
	s.logAuthEvent(selector, challenge.Email, serverdb.AuthEventWebExchanged, map[string]string{
		"ip":         ip,
		"user_agent": ua,
		"key_id":     ak.ID,
	})

	// Step 9: Return the key.
	writeJSON(w, http.StatusOK, webExchangeResponse{
		Status:    "complete",
		APIKey:    plaintext,
		UserID:    *challenge.UserID,
		Email:     challenge.Email,
		ExpiresAt: expiry.Format(time.RFC3339),
	})
}

// logAuthEvent logs an auth event, silently ignoring errors.
func (s *Server) logAuthEvent(authRequestID, email, eventType string, meta map[string]string) {
	metadata := "{}"
	if len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			metadata = string(b)
		}
	}
	if err := s.store.InsertAuthEvent(authRequestID, email, eventType, metadata); err != nil {
		slog.Warn("log auth event", "type", eventType, "err", err)
	}
}

// isValidUserCode checks that every character is in the valid charset.
func isValidUserCode(code string) bool {
	const validChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	for _, c := range code {
		if !strings.ContainsRune(validChars, c) {
			return false
		}
	}
	return true
}

// deviceStartRequest is the JSON body for POST /v1/auth/device/start.
type deviceStartRequest struct {
	Email               string `json:"email"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	DeviceName          string `json:"device_name"`
}

// deviceStartResponse is the JSON response for POST /v1/auth/device/start.
type deviceStartResponse struct {
	DeviceCode string `json:"device_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
	EmailSent  bool   `json:"email_sent"`
}

// generateDeviceCode generates a 32-byte random hex device code (64 chars).
func generateDeviceCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleDeviceStart handles POST /v1/auth/device/start.
// It is non-enumerating: any syntactically-valid email always receives 200 with
// device_code/expires_in/interval/email_sent=true. Unknown emails get
// AuthEventEmailSuppressed but no challenge is created and no email is sent.
func (s *Server) handleDeviceStart(w http.ResponseWriter, r *http.Request) {
	var req deviceStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(req.Email); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "valid email is required")
		return
	}

	if req.CodeChallenge == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "code_challenge is required")
		return
	}

	if req.CodeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "bad_request", "code_challenge_method must be S256")
		return
	}

	ip := clientIP(r, s.config.TrustedProxies)
	ua := r.Header.Get("User-Agent")
	meta := map[string]string{"ip": ip, "user_agent": ua}

	// Generate the device_code that is returned to the CLI.
	// Only its SHA-256 hash is stored; the plaintext is never persisted.
	deviceCode, err := generateDeviceCode()
	if err != nil {
		logFor(r.Context()).Error("generate device code for device start", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to generate device code")
		return
	}

	genericOK := func() {
		writeJSON(w, http.StatusOK, deviceStartResponse{
			DeviceCode: deviceCode,
			ExpiresIn:  int(serverdb.ChallengeTTL.Seconds()),
			Interval:   5,
			EmailSent:  true,
		})
	}

	// Resend rate limit: 1 per minute per email (same window as web/start).
	rateLimitKey := "device-start:" + strings.ToLower(req.Email)
	if !s.rateLimiter.Allow(rateLimitKey, 1) {
		if err := s.store.InsertRateLimitEvent("", ip, "auth"); err != nil {
			slog.Warn("log rate limit event for device-start", "err", err)
		}
		// Return generic 200 — do not reveal rate limiting.
		genericOK()
		return
	}

	// Look up user. Unknown email -> suppressed path (non-enumeration).
	user, err := s.store.GetUserByEmail(req.Email)
	if err != nil {
		logFor(r.Context()).Error("get user by email for device start", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to look up user")
		return
	}
	if user == nil {
		// Unknown user — suppress silently; D4 poll will never transition this device_code.
		s.logAuthEvent("", req.Email, serverdb.AuthEventEmailSuppressed, meta)
		genericOK()
		return
	}

	// Known user path: create challenge, build approval link, send email.
	deviceCodeHashSum := sha256.Sum256([]byte(deviceCode))
	deviceCodeHash := hex.EncodeToString(deviceCodeHashSum[:])
	codeChallenge := req.CodeChallenge
	codeChallengeMethod := req.CodeChallengeMethod

	selector, plaintextSecret, err := s.store.CreateEmailChallenge(
		req.Email,
		"device_login",
		user.ID,
		serverdb.ChallengeOptions{
			DeviceCodeHash:      &deviceCodeHash,
			CodeChallenge:       &codeChallenge,
			CodeChallengeMethod: &codeChallengeMethod,
			IP:                  &ip,
			UserAgent:           &ua,
		},
	)
	if err != nil {
		logFor(r.Context()).Error("create email challenge for device start", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create auth challenge")
		return
	}

	// Build approval link. NEVER log the link, selector, or secret.
	linkURL := s.config.AuthEmailBaseURL + "/auth/device/approve?token=" + selector + "." + plaintextSecret

	htmlBody := fmt.Sprintf(
		`<p>A CLI login was requested for your td account.</p>`+
			`<p>Click the link below to approve the login from your terminal.</p>`+
			`<p><a href="%s" style="display:inline-block;padding:12px 24px;background:#0070f3;color:#fff;text-decoration:none;border-radius:4px;">Approve td CLI login</a></p>`+
			`<p>Or copy this link into your browser (do not share it):</p>`+
			`<p style="word-break:break-all;">%s</p>`+
			`<p>This link expires in 15 minutes. If you did not request this, you can ignore this email.</p>`,
		linkURL, linkURL,
	)
	textBody := "A CLI login was requested for your td account.\n\nClick the link to approve: " + linkURL + "\n\nThis link expires in 15 minutes. If you did not request this, you can ignore this email."

	traceID := getRequestID(r.Context())
	msg := email.LoginEmail{
		To:      req.Email,
		Subject: "Approve td CLI login",
		Text:    textBody,
		HTML:    htmlBody,
		Purpose: "device_login",
		TraceID: traceID,
	}

	if err := s.emailSender.SendLoginLink(r.Context(), msg); err != nil {
		// Log at Warn — never include the link/token.
		logFor(r.Context()).Warn("send login link failed for device start", "err", err)
		s.logAuthEvent(selector, req.Email, serverdb.AuthEventLoginFailed, meta)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to send login email")
		return
	}

	s.logAuthEvent(selector, req.Email, serverdb.AuthEventChallengeStarted, meta)
	s.logAuthEvent(selector, req.Email, serverdb.AuthEventEmailSent, meta)

	genericOK()
}
