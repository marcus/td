package api

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

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
		"ip":         clientIP(r),
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
		"ip":         clientIP(r),
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
	verifyTmpl.Execute(w, verifyPageData{})
}

// handleVerifySubmit handles POST /auth/verify.
func (s *Server) handleVerifySubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := r.ParseForm(); err != nil {
		verifyTmpl.Execute(w, verifyPageData{Error: "Invalid form data."})
		return
	}

	userCode := r.FormValue("user_code")
	userCode = strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(userCode, "-", "")))

	if userCode == "" {
		verifyTmpl.Execute(w, verifyPageData{Error: "Please enter a code."})
		return
	}

	if len(userCode) != 6 || !isValidUserCode(userCode) {
		verifyTmpl.Execute(w, verifyPageData{Error: "Invalid or expired code."})
		return
	}

	ar, err := s.store.GetAuthRequestByUserCode(userCode)
	if err != nil {
		logFor(r.Context()).Error("get auth request by user code", "err", err)
		verifyTmpl.Execute(w, verifyPageData{Error: "Something went wrong. Please try again."})
		return
	}
	if ar == nil {
		slog.Warn("verify failed", "reason", "invalid_or_expired")
		s.logAuthEvent("", "", serverdb.AuthEventFailed, map[string]string{
			"failure_reason": "invalid_code",
			"ip":             clientIP(r),
			"user_agent":     r.Header.Get("User-Agent"),
		})
		verifyTmpl.Execute(w, verifyPageData{Error: "Invalid or expired code."})
		return
	}

	// Look up or create user
	user, err := s.store.GetUserByEmail(ar.Email)
	if err != nil {
		logFor(r.Context()).Error("get user by email", "err", err)
		verifyTmpl.Execute(w, verifyPageData{Error: "Something went wrong. Please try again."})
		return
	}

	if user == nil {
		if !s.config.AllowSignup {
			slog.Warn("signup denied", "email", ar.Email)
			verifyTmpl.Execute(w, verifyPageData{Error: "Signups are disabled."})
			return
		}
		user, err = s.store.CreateUser(ar.Email)
		if err != nil {
			logFor(r.Context()).Error("create user during verify", "err", err)
			verifyTmpl.Execute(w, verifyPageData{Error: "Failed to create account. Please try again."})
			return
		}
	}

	if err := s.store.VerifyAuthRequest(userCode, user.ID); err != nil {
		logFor(r.Context()).Error("verify auth request", "err", err)
		verifyTmpl.Execute(w, verifyPageData{Error: "Failed to authorize device. Code may have expired."})
		return
	}

	s.logAuthEvent(ar.ID, ar.Email, serverdb.AuthEventCodeVerified, map[string]string{
		"ip":         clientIP(r),
		"user_agent": r.Header.Get("User-Agent"),
	})

	logFor(r.Context()).Info("device verified", "email", ar.Email)
	verifyTmpl.Execute(w, verifyPageData{Success: true})
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
