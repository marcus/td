package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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

// Rate limits per endpoint tier.
const (
	rateLimitAuth  = 10  // /auth/* per IP
	rateLimitPush  = 60  // /sync/push per API key
	rateLimitPull  = 120 // /sync/pull per API key
	rateLimitOther = 300 // all other per API key
)

// authRateLimitMiddleware rate-limits auth endpoints by IP address.
// Applied globally; only acts on /auth/ and /v1/auth/ paths.
func authRateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if strings.HasPrefix(path, "/auth/") || strings.HasPrefix(path, "/v1/auth/") {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				key := "ip:" + host
				if !rl.Allow(key, rateLimitAuth) {
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
func (s *Server) withRateLimit(handler http.HandlerFunc, limit int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if user == nil {
			handler(w, r)
			return
		}
		key := fmt.Sprintf("key:%s:%d", user.KeyID, limit)
		if !s.rateLimiter.Allow(key, limit) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		handler(w, r)
	}
}
