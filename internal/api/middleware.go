package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

)

type contextKey int

const (
	ctxKeyAuthUser contextKey = iota
	ctxKeyRequestID
	_              // reserved
	ctxKeyLogger
)

// AuthUser holds the authenticated user information extracted from the API key.
type AuthUser struct {
	UserID string
	Email  string
	KeyID  string
	Scopes []string
}

// getUserFromContext returns the authenticated user from the request context, or nil.
func getUserFromContext(ctx context.Context) *AuthUser {
	u, _ := ctx.Value(ctxKeyAuthUser).(*AuthUser)
	return u
}

// getRequestID returns the request ID from the context.
func getRequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// logFor returns the context-scoped logger, falling back to the default logger.
func logFor(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// loggerMiddleware creates a per-request logger with the request ID and stores it in the context.
func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := slog.Default().With("rid", getRequestID(r.Context()))
		ctx := context.WithValue(r.Context(), ctxKeyLogger, l)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// metricsMiddleware records request counts and categorizes response status codes.
func metricsMiddleware(m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.RecordRequest()
			sc := &statusCapture{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(sc, r)
			switch {
			case sc.code >= 500:
				m.RecordError()
			case sc.code >= 400:
				m.RecordClientError()
			}
		})
	}
}

// recoveryMiddleware catches panics and returns a 500 response.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logFor(r.Context()).Error("panic recovered", "panic", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// generateRequestID creates a random hex string for request tracing.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// requestIDMiddleware generates a unique request ID and adds it to the context and response headers.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusCapture wraps ResponseWriter to capture the status code.
type statusCapture struct {
	http.ResponseWriter
	code int
}

func (sc *statusCapture) WriteHeader(code int) {
	sc.code = code
	sc.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware logs each request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sc := &statusCapture{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sc, r)
		logFor(r.Context()).Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sc.code,
			"dur", time.Since(start).String(),
		)
	})
}

// requireAuth returns an http.HandlerFunc that verifies the Bearer token
// and injects AuthUser into the context before calling the inner handler.
func (s *Server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing authorization header")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid authorization format")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		ak, user, err := s.store.VerifyAPIKey(token)
		if err != nil {
			logFor(r.Context()).Error("verify api key", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to verify key")
			return
		}
		if ak == nil || user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired api key")
			return
		}

		scopes := parseScopes(ak.Scopes)
		authUser := &AuthUser{
			UserID: user.ID,
			Email:  user.Email,
			KeyID:  ak.ID,
			Scopes: scopes,
		}

		ctx := context.WithValue(r.Context(), ctxKeyAuthUser, authUser)
		// Enrich logger with user ID
		ctx = context.WithValue(ctx, ctxKeyLogger, logFor(ctx).With("uid", user.ID))
		handler(w, r.WithContext(ctx))
	}
}

// requireProjectAuth is a helper that validates auth and checks the user has
// the required role for the project identified by the "id" path value.
func (s *Server) requireProjectAuth(requiredRole string, handler http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing project id")
			return
		}

		user := getUserFromContext(r.Context())
		if err := s.store.Authorize(projectID, user.UserID, requiredRole); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}

		// Enrich logger with project ID
		ctx := context.WithValue(r.Context(), ctxKeyLogger, logFor(r.Context()).With("pid", projectID))
		handler(w, r.WithContext(ctx))
	})
}

func parseScopes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// maxBytesMiddleware limits request body size to prevent abuse.
func maxBytesMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// chain applies middleware in order (first applied is outermost).
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

