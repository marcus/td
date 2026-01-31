package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

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
	json.NewDecoder(w.Body).Decode(&startResp)

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
	json.NewDecoder(w.Body).Decode(&pollResp)
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
	json.NewDecoder(w.Body).Decode(&completeResp)
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
