package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubHandler is a simple handler that returns 200 OK.
var stubHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func newCORSServer(origins []string) *Server {
	return &Server{
		config: Config{
			CORSAllowedOrigins: origins,
		},
	}
}

func TestCORS_NoOriginsConfigured(t *testing.T) {
	s := newCORSServer(nil)
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS headers when no origins configured")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	s := newCORSServer([]string{"https://example.com"})
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	// No Origin header set
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS headers when no Origin header")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORS_AllowedOrigin(t *testing.T) {
	s := newCORSServer([]string{"https://admin.example.com"})
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected Access-Control-Allow-Origin=https://admin.example.com, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("expected Allow-Headers, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, OPTIONS" {
		t.Fatalf("expected Allow-Methods, got %q", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	s := newCORSServer([]string{"https://admin.example.com"})
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS headers for disallowed origin")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORS_PreflightAllowed(t *testing.T) {
	s := newCORSServer([]string{"https://admin.example.com"})
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("OPTIONS", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected CORS origin header on preflight, got %q", got)
	}
}

func TestCORS_WildcardOrigin(t *testing.T) {
	s := newCORSServer([]string{"*"})
	handler := s.CORSMiddleware(stubHandler)

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://anything.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.example.com" {
		t.Fatalf("expected wildcard to allow any origin, got %q", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORS_MultipleAllowedOrigins(t *testing.T) {
	s := newCORSServer([]string{"https://one.example.com", "https://two.example.com"})
	handler := s.CORSMiddleware(stubHandler)

	// First origin should be allowed
	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://one.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://one.example.com" {
		t.Fatalf("expected first origin allowed, got %q", got)
	}

	// Second origin should be allowed
	req = httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://two.example.com")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://two.example.com" {
		t.Fatalf("expected second origin allowed, got %q", got)
	}

	// Third origin should NOT be allowed
	req = httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Origin", "https://three.example.com")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected third origin to be disallowed")
	}
}
