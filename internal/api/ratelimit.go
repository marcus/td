package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// RateLimiter implements per-key fixed-window rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count    int
	windowAt time.Time
}

// NewRateLimiter creates a RateLimiter and starts background cleanup.
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{buckets: make(map[string]*bucket)}
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

// Allow checks if the key is within the rate limit (limit per 1-minute window).
func (rl *RateLimiter) Allow(key string, limit int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.windowAt) >= time.Minute {
		rl.buckets[key] = &bucket{count: 1, windowAt: now}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-2 * time.Minute)
	for k, b := range rl.buckets {
		if b.windowAt.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
}

// Default rate limits per endpoint tier (used as documentation/fallbacks).
const (
	rateLimitAuth  = 10  // /auth/* per IP
	rateLimitPush  = 60  // /sync/push per API key
	rateLimitPull  = 120 // /sync/pull per API key
	rateLimitOther = 300 // all other per API key
)

// authRateLimitMiddleware rate-limits auth endpoints by IP address.
// Applied globally; only acts on /auth/ and /v1/auth/ paths.
// When a rate limit is exceeded, the event is logged to the store.
func authRateLimitMiddleware(rl *RateLimiter, limit int, store *serverdb.ServerDB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if strings.HasPrefix(path, "/auth/") || strings.HasPrefix(path, "/v1/auth/") {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				key := "ip:" + host
				if !rl.Allow(key, limit) {
					if err := store.InsertRateLimitEvent("", host, "auth"); err != nil {
						slog.Error("log rate limit event", "err", err)
					}
					writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// withRateLimit wraps an authenticated handler with per-key rate limiting.
// The key is derived from the AuthUser's KeyID in the request context.
// When a rate limit is exceeded, the event is logged to the store.
func (s *Server) withRateLimit(handler http.HandlerFunc, limit int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if user == nil {
			handler(w, r)
			return
		}
		key := fmt.Sprintf("key:%s:%d", user.KeyID, limit)
		if !s.rateLimiter.Allow(key, limit) {
			ip := clientIP(r)
			endpointClass := classifyEndpoint(r.URL.Path)
			if err := s.store.InsertRateLimitEvent(user.KeyID, ip, endpointClass); err != nil {
				slog.Error("log rate limit event", "err", err)
			}
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		handler(w, r)
	}
}

// classifyEndpoint returns the endpoint class based on the request path.
func classifyEndpoint(path string) string {
	if strings.HasPrefix(path, "/v1/auth/") || strings.HasPrefix(path, "/auth/") {
		return "auth"
	}
	if strings.Contains(path, "/sync/push") {
		return "push"
	}
	if strings.Contains(path, "/sync/pull") {
		return "pull"
	}
	return "other"
}

// clientIP extracts the client IP from the request, checking X-Forwarded-For first.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First IP in the chain is the original client
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
