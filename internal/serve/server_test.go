package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestServer creates a Server with the given config for testing.
// The DB is nil since these tests only exercise middleware and routing.
func newTestServer(config ServeConfig) *Server {
	return NewServer(nil, "/tmp/test", "ses_test123", config)
}

// ============================================================================
// Placeholder Route Tests
// ============================================================================

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(ServeConfig{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Health endpoint works even without a DB (change_token defaults to "")
	// The handler does not panic on nil DB for GetChangeToken because it
	// uses a simple query. However, with nil DB it will panic. The recovery
	// middleware catches it. We just verify it doesn't return 404/405.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		t.Errorf("status = %d, route should be registered", resp.StatusCode)
	}
}

// ============================================================================
// Auth Middleware Tests
// ============================================================================

func TestAuthMiddleware_NoTokenConfigured(t *testing.T) {
	srv := newTestServer(ServeConfig{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Without a token configured, requests should pass through auth
	// and reach the handler. Use a placeholder route (focus) to verify.
	resp, err := http.Get(ts.URL + "/v1/issues/td-abc/start") // use a placeholder route
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should reach the handler, not be rejected by auth.
	// With nil DB, placeholder returns 501; implemented routes may panic (500).
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("status = %d, should pass through auth without token configured", resp.StatusCode)
	}
}

func TestAuthMiddleware_TokenConfigured_NoHeader(t *testing.T) {
	srv := newTestServer(ServeConfig{Token: "secret-token"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/issues")
	if err != nil {
		t.Fatalf("GET /v1/issues: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != ErrUnauthorized {
		t.Errorf("error.code = %v, want %s", env.Error, ErrUnauthorized)
	}
}

func TestAuthMiddleware_TokenConfigured_WrongToken(t *testing.T) {
	srv := newTestServer(ServeConfig{Token: "secret-token"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/issues: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_TokenConfigured_CorrectToken(t *testing.T) {
	srv := newTestServer(ServeConfig{Token: "secret-token"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("PUT", ts.URL+"/v1/focus", nil)
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /v1/focus: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should pass through auth (not 401) and reach the handler.
	// With nil DB the handler may return 400/500, but must NOT be 401.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("status = %d, should pass through auth with correct token", resp.StatusCode)
	}
}

func TestAuthMiddleware_TokenConfigured_InvalidFormat(t *testing.T) {
	srv := newTestServer(ServeConfig{Token: "secret-token"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/issues: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_HealthExempt(t *testing.T) {
	srv := newTestServer(ServeConfig{Token: "secret-token"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// GET /health should be exempt from auth even when token is configured
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should reach the handler (not 401), even though token is configured.
	// With nil DB, it may panic (500) but must NOT be 401.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("status = %d, health should skip auth", resp.StatusCode)
	}
}

// ============================================================================
// CORS Middleware Tests
// ============================================================================

func TestCORSMiddleware_NoCORSConfigured(t *testing.T) {
	srv := newTestServer(ServeConfig{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Origin", "http://example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// No CORS headers should be set
	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty", h)
	}
}

func TestCORSMiddleware_MatchingOrigin(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "http://localhost:3000"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", h, "http://localhost:3000")
	}
	if h := resp.Header.Get("Access-Control-Allow-Methods"); h == "" {
		t.Error("Access-Control-Allow-Methods should be set")
	}
	if h := resp.Header.Get("Access-Control-Max-Age"); h != "3600" {
		t.Errorf("Access-Control-Max-Age = %q, want 3600", h)
	}
}

func TestCORSMiddleware_NonMatchingOrigin(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "http://localhost:3000"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Origin", "http://evil.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for non-matching origin", h)
	}
}

func TestCORSMiddleware_WildcardOrigin(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "*"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/issues", nil)
	req.Header.Set("Origin", "http://anything.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "http://anything.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", h, "http://anything.com")
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "http://localhost:3000"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/v1/issues", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d for preflight", resp.StatusCode, http.StatusNoContent)
	}
	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", h, "http://localhost:3000")
	}
}

func TestCORSMiddleware_NoOriginHeader(t *testing.T) {
	srv := newTestServer(ServeConfig{CORSOrigin: "http://localhost:3000"})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Request without Origin header should pass through without CORS headers
	resp, err := http.Get(ts.URL + "/v1/issues")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if h := resp.Header.Get("Access-Control-Allow-Origin"); h != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty when no Origin sent", h)
	}
}

// ============================================================================
// Recovery Middleware Tests
// ============================================================================

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	// Create a server and replace a handler with one that panics
	srv := newTestServer(ServeConfig{})

	// Override the mux with a panicking handler
	panicMux := http.NewServeMux()
	panicMux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	srv.mux = panicMux

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/panic")
	if err != nil {
		t.Fatalf("GET /panic: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error == nil || env.Error.Code != ErrInternal {
		t.Errorf("error.code = %v, want %s", env.Error, ErrInternal)
	}
	if env.Error.Message != "internal server error" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "internal server error")
	}
}

// ============================================================================
// Server Struct Tests
// ============================================================================

func TestNewServer_DefaultConfig(t *testing.T) {
	config := ServeConfig{
		Port:         8080,
		Addr:         "localhost",
		PollInterval: 2 * time.Second,
	}
	srv := NewServer(nil, "/tmp/test", "ses_abc", config)

	if srv.config.Port != 8080 {
		t.Errorf("port = %d, want 8080", srv.config.Port)
	}
	if srv.config.Addr != "localhost" {
		t.Errorf("addr = %q, want localhost", srv.config.Addr)
	}
	if srv.sessionID != "ses_abc" {
		t.Errorf("sessionID = %q, want ses_abc", srv.sessionID)
	}
	if srv.mux == nil {
		t.Error("mux should not be nil")
	}
	if srv.sseHub != nil {
		t.Error("sseHub should be nil when DB is nil")
	}
}

func TestAllRoutesRegistered(t *testing.T) {
	srv := newTestServer(ServeConfig{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// All routes should be registered (no 404/405).
	// With nil DB, real handlers may return 400/500 — that's fine;
	// we just verify they are NOT 404 or 405 (unregistered).
	implementedRoutes := []struct {
		method string
		path   string
	}{
		// Read endpoints
		{"GET", "/health"},
		{"GET", "/v1/monitor"},
		{"GET", "/v1/issues"},
		{"GET", "/v1/issues/td-abc"},
		{"GET", "/v1/boards"},
		{"GET", "/v1/boards/b1"},
		{"GET", "/v1/sessions"},
		{"GET", "/v1/stats"},
		// Issue write endpoints
		{"POST", "/v1/issues"},
		{"PATCH", "/v1/issues/td-abc"},
		{"DELETE", "/v1/issues/td-abc"},
		// Workflow transitions
		{"POST", "/v1/issues/td-abc/start"},
		{"POST", "/v1/issues/td-abc/review"},
		{"POST", "/v1/issues/td-abc/approve"},
		{"POST", "/v1/issues/td-abc/reject"},
		{"POST", "/v1/issues/td-abc/block"},
		{"POST", "/v1/issues/td-abc/unblock"},
		{"POST", "/v1/issues/td-abc/close"},
		{"POST", "/v1/issues/td-abc/reopen"},
		// Comments
		{"POST", "/v1/issues/td-abc/comments"},
		{"DELETE", "/v1/issues/td-abc/comments/c1"},
		// Dependencies
		{"POST", "/v1/issues/td-abc/dependencies"},
		{"DELETE", "/v1/issues/td-abc/dependencies/d1"},
		// Focus
		{"PUT", "/v1/focus"},
		// Board write endpoints
		{"POST", "/v1/boards"},
		{"PATCH", "/v1/boards/b1"},
		{"DELETE", "/v1/boards/b1"},
		{"POST", "/v1/boards/b1/issues"},
		{"DELETE", "/v1/boards/b1/issues/td-abc"},
		// SSE events
		{"GET", "/v1/events"},
	}

	for _, r := range implementedRoutes {
		req, _ := http.NewRequest(r.method, ts.URL+r.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("%s %s: %v", r.method, r.path, err)
			continue
		}
		_ = resp.Body.Close()

		// Route must be registered (not 404 or 405)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, route should be registered", r.method, r.path, resp.StatusCode)
		}
	}
}
