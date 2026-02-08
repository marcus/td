package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcus/td/internal/serverdb"
	_ "modernc.org/sqlite"
)

func TestRateLimiterAllowDeny(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}

	// Should allow up to the limit
	for i := 0; i < 5; i++ {
		if !rl.Allow("k1", 5) {
			t.Fatalf("expected allow on request %d", i+1)
		}
	}

	// Should deny at the limit
	if rl.Allow("k1", 5) {
		t.Fatal("expected deny after limit reached")
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}

	// Exhaust the limit
	for i := 0; i < 3; i++ {
		rl.Allow("k1", 3)
	}
	if rl.Allow("k1", 3) {
		t.Fatal("expected deny after limit")
	}

	// Simulate window expiry by backdating the bucket
	rl.mu.Lock()
	rl.buckets["k1"].windowAt = time.Now().Add(-2 * time.Minute)
	rl.mu.Unlock()

	// Should allow again after window reset
	if !rl.Allow("k1", 3) {
		t.Fatal("expected allow after window reset")
	}
}

func TestRateLimiterKeyIsolation(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}

	// Exhaust key1
	for i := 0; i < 2; i++ {
		rl.Allow("key1", 2)
	}
	if rl.Allow("key1", 2) {
		t.Fatal("expected key1 denied")
	}

	// key2 should still be allowed
	if !rl.Allow("key2", 2) {
		t.Fatal("expected key2 allowed")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}

	rl.Allow("stale", 10)
	rl.Allow("fresh", 10)

	// Backdate the stale entry
	rl.mu.Lock()
	rl.buckets["stale"].windowAt = time.Now().Add(-5 * time.Minute)
	rl.mu.Unlock()

	rl.cleanup()

	rl.mu.Lock()
	_, hasStale := rl.buckets["stale"]
	_, hasFresh := rl.buckets["fresh"]
	rl.mu.Unlock()

	if hasStale {
		t.Fatal("expected stale entry to be cleaned up")
	}
	if !hasFresh {
		t.Fatal("expected fresh entry to remain")
	}
}

func testStore(t *testing.T) *serverdb.ServerDB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := serverdb.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestAuthRateLimitMiddleware(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}
	store := testStore(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authRateLimitMiddleware(rl, rateLimitAuth, store)(inner)

	// Auth endpoint should be rate limited
	for i := 0; i < rateLimitAuth; i++ {
		req := httptest.NewRequest("POST", "/v1/auth/login/start", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Next request should be denied
	req := httptest.NewRequest("POST", "/v1/auth/login/start", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	// Non-auth endpoint should pass through without rate limiting
	req = httptest.NewRequest("GET", "/healthz", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz: expected 200, got %d", w.Code)
	}
}

func TestAuthRateLimitDifferentIPs(t *testing.T) {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}
	store := testStore(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authRateLimitMiddleware(rl, rateLimitAuth, store)(inner)

	// Exhaust IP 1
	for i := 0; i < rateLimitAuth; i++ {
		req := httptest.NewRequest("POST", "/v1/auth/login/start", nil)
		req.RemoteAddr = "10.0.0.1:5000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// IP 2 should still be allowed
	req := httptest.NewRequest("POST", "/v1/auth/login/start", nil)
	req.RemoteAddr = "10.0.0.2:5000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("different IP: expected 200, got %d", w.Code)
	}
}

func TestWithRateLimitIntegration(t *testing.T) {
	srv, store := newTestServerWithConfig(t, func(cfg *Config) {
		cfg.RateLimitPush = rateLimitPush
		cfg.RateLimitOther = 100000
	})
	_, token := createTestUser(t, store, "ratelimit@test.com")

	// Create a project first
	w := doRequest(srv, "POST", "/v1/projects", token, CreateProjectRequest{Name: "rl-test"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)

	// Push requests up to the limit should succeed
	for i := 0; i < rateLimitPush; i++ {
		pushBody := PushRequest{
			DeviceID:  "dev1",
			SessionID: "sess1",
			Events: []EventInput{
				{
					ClientActionID:  int64(i + 1),
					ActionType:      "create",
					EntityType:      "issues",
					EntityID:        fmt.Sprintf("i_%03d", i+1),
					Payload:         json.RawMessage(`{"title":"test"}`),
					ClientTimestamp: "2025-01-01T00:00:00Z",
				},
			},
		}
		w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
		if w.Code != http.StatusOK {
			t.Fatalf("push %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// Next push should be rate limited
	pushBody := PushRequest{
		DeviceID:  "dev1",
		SessionID: "sess1",
		Events: []EventInput{
			{
				ClientActionID:  int64(rateLimitPush + 1),
				ActionType:      "create",
				EntityType:      "issues",
				EntityID:        "i_overflow",
				Payload:         json.RawMessage(`{"title":"over"}`),
				ClientTimestamp: "2025-01-01T00:00:00Z",
			},
		},
	}
	w = doRequest(srv, "POST", fmt.Sprintf("/v1/projects/%s/sync/push", project.ID), token, pushBody)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}
