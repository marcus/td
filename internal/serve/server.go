package serve

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
)

// ServeConfig holds the configuration for the HTTP server.
type ServeConfig struct {
	Port         int
	Addr         string
	Token        string
	CORSOrigin   string
	PollInterval time.Duration
}

// Server is the td serve HTTP server.
type Server struct {
	db        *db.DB
	sessionID string
	baseDir   string
	config    ServeConfig
	mux       *http.ServeMux
	sseHub    *SSEHub
	http      *http.Server
}

// NewServer creates a new Server, registers all routes, and sets up the
// middleware chain. Handlers are placeholder 501s until subsequent tasks
// implement them.
func NewServer(database *db.DB, baseDir, sessionID string, config ServeConfig) *Server {
	pollInterval := config.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}

	s := &Server{
		db:        database,
		sessionID: sessionID,
		baseDir:   baseDir,
		config:    config,
		mux:       http.NewServeMux(),
	}

	// Initialize SSE hub (requires database for change_token polling)
	if database != nil {
		s.sseHub = NewSSEHub(database, pollInterval)
	}

	s.registerRoutes()
	return s
}

// Handler returns the mux wrapped in the middleware chain.
func (s *Server) Handler() http.Handler {
	h := http.Handler(s.mux)

	// Wrap order: outermost first when applied, so we apply innermost first.
	// Final order (outermost to innermost):
	//   recovery -> logging -> CORS -> auth -> handler
	h = s.authMiddleware(h)
	h = s.corsMiddleware(h)
	h = s.loggingMiddleware(h)
	h = s.recoveryMiddleware(h)

	return h
}

// ListenAndServe starts the HTTP server on the configured address and port,
// and handles graceful shutdown when the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Addr, s.config.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	// Start SSE hub polling
	if s.sseHub != nil {
		s.sseHub.Start(ctx)
	}

	s.http = &http.Server{
		Handler:      s.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		// Stop SSE hub first (closes client connections)
		if s.sseHub != nil {
			s.sseHub.Stop()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// Shutdown gracefully stops the HTTP server. If the server has not been started,
// this is a no-op.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

// StartBackground starts long-lived background processes (SSE polling loop).
func (s *Server) StartBackground(ctx context.Context) {
	if s.sseHub != nil {
		s.sseHub.Start(ctx)
	}
}

// StopBackground stops long-lived background processes.
func (s *Server) StopBackground() {
	if s.sseHub != nil {
		s.sseHub.Stop()
	}
}

// ============================================================================
// Route Registration
// ============================================================================

// registerRoutes registers all API routes. Read endpoints use real handlers;
// write/mutation endpoints use real handlers where implemented, or placeholders.
func (s *Server) registerRoutes() {
	// Health (read)
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Monitor (read)
	s.mux.HandleFunc("GET /v1/monitor", s.handleMonitor)

	// Issues CRUD
	s.mux.HandleFunc("GET /v1/issues", s.handleListIssues)
	s.mux.HandleFunc("GET /v1/issues/{id}", s.handleGetIssue)
	s.mux.HandleFunc("POST /v1/issues", s.handleCreateIssue)
	s.mux.HandleFunc("PATCH /v1/issues/{id}", s.handleUpdateIssue)
	s.mux.HandleFunc("DELETE /v1/issues/{id}", s.handleDeleteIssue)

	// Issue workflow transitions
	s.mux.HandleFunc("POST /v1/issues/{id}/start", s.handleStart)
	s.mux.HandleFunc("POST /v1/issues/{id}/review", s.handleReview)
	s.mux.HandleFunc("POST /v1/issues/{id}/approve", s.handleApprove)
	s.mux.HandleFunc("POST /v1/issues/{id}/reject", s.handleReject)
	s.mux.HandleFunc("POST /v1/issues/{id}/block", s.handleBlock)
	s.mux.HandleFunc("POST /v1/issues/{id}/unblock", s.handleUnblock)
	s.mux.HandleFunc("POST /v1/issues/{id}/close", s.handleClose)
	s.mux.HandleFunc("POST /v1/issues/{id}/reopen", s.handleReopen)

	// Comments
	s.mux.HandleFunc("POST /v1/issues/{id}/comments", s.handleAddComment)
	s.mux.HandleFunc("DELETE /v1/issues/{id}/comments/{comment_id}", s.handleDeleteComment)

	// Dependencies
	s.mux.HandleFunc("POST /v1/issues/{id}/dependencies", s.handleAddDependency)
	s.mux.HandleFunc("DELETE /v1/issues/{id}/dependencies/{dep_id}", s.handleDeleteDependency)

	// Focus
	s.mux.HandleFunc("PUT /v1/focus", s.handleSetFocus)

	// Boards (read + write)
	s.mux.HandleFunc("GET /v1/boards", s.handleListBoards)
	s.mux.HandleFunc("GET /v1/boards/{id}", s.handleGetBoard)
	s.mux.HandleFunc("POST /v1/boards", s.handleCreateBoard)
	s.mux.HandleFunc("PATCH /v1/boards/{id}", s.handleUpdateBoard)
	s.mux.HandleFunc("DELETE /v1/boards/{id}", s.handleDeleteBoard)
	s.mux.HandleFunc("POST /v1/boards/{id}/issues", s.handleSetBoardPosition)
	s.mux.HandleFunc("DELETE /v1/boards/{id}/issues/{issue_id}", s.handleRemoveBoardPosition)

	// Sessions (read)
	s.mux.HandleFunc("GET /v1/sessions", s.handleListSessions)

	// Stats (read)
	s.mux.HandleFunc("GET /v1/stats", s.handleStats)

	// SSE events
	s.mux.HandleFunc("GET /v1/events", s.handleEvents)
}

// ============================================================================
// Middleware
// ============================================================================

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.code = code
	sr.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying writer so wrappers like ResponseController can
// reach interfaces implemented by the original ResponseWriter.
func (sr *statusRecorder) Unwrap() http.ResponseWriter {
	return sr.ResponseWriter
}

// Flush forwards streaming flushes (required for SSE).
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards connection hijacking when supported by the underlying writer.
func (sr *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := sr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return hj.Hijack()
}

// recoveryMiddleware catches panics, logs the stack trace, and returns a 500
// error envelope.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				stack := debug.Stack()
				slog.Error("panic recovered",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(stack),
				)
				WriteError(w, ErrInternal, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status code, and
// duration.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sr, r)
		slog.Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sr.code,
			"dur", time.Since(start).String(),
		)
	})
}

// corsMiddleware handles CORS preflight and sets response headers when
// CORSOrigin is configured. If no CORS origin is configured, the middleware
// is a no-op pass-through.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.CORSOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if origin matches configured origin or wildcard
		if s.config.CORSOrigin != "*" && s.config.CORSOrigin != origin {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// authMiddleware validates the Bearer token when the server is configured with
// a token. GET /health is always exempt from authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No token configured - pass through
		if s.config.Token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth for health check
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			WriteError(w, ErrUnauthorized, "missing authorization header", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			WriteError(w, ErrUnauthorized, "invalid authorization format", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != s.config.Token {
			WriteError(w, ErrUnauthorized, "invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
