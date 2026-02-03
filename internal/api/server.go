package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// Server is the HTTP API server for td-sync.
type Server struct {
	config      Config
	http        *http.Server
	store       *serverdb.ServerDB
	dbPool      *ProjectDBPool
	metrics     *Metrics
	rateLimiter *RateLimiter
	cancel      context.CancelFunc
}

// NewServer creates a new Server with the given config and store.
func NewServer(cfg Config, store *serverdb.ServerDB) (*Server, error) {
	s := &Server{
		config:      cfg,
		store:       store,
		dbPool:      NewProjectDBPool(cfg.ProjectDataDir),
		metrics:     NewMetrics(),
		rateLimiter: NewRateLimiter(),
	}

	s.http = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      s.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s, nil
}

// Start begins listening for HTTP requests (non-blocking).
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("http server", "err", err)
		}
	}()

	// Periodically clean up expired auth requests
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("cleanup panic", "panic", r)
			}
		}()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := s.store.CleanupExpiredAuthRequests()
				if err != nil {
					slog.Error("cleanup expired auth requests", "err", err)
				} else if n > 0 {
					slog.Info("cleaned up expired auth requests", "count", n)
				}
			}
		}
	}()

	return nil
}

// Shutdown gracefully stops the server and closes all project databases.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	err := s.http.Shutdown(ctx)
	s.dbPool.CloseAll()
	return err
}

// routes builds the HTTP handler with all routes and middleware.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Health & metrics
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /metricz", s.handleMetrics)

	// Auth (public)
	mux.HandleFunc("POST /v1/auth/login/start", s.handleLoginStart)
	mux.HandleFunc("POST /v1/auth/login/poll", s.handleLoginPoll)
	mux.HandleFunc("GET /auth/verify", s.handleVerifyPage)
	mux.HandleFunc("POST /auth/verify", s.handleVerifySubmit)

	// Projects
	mux.HandleFunc("POST /v1/projects", s.requireAuth(s.withRateLimit(s.handleCreateProject, s.config.RateLimitOther)))
	mux.HandleFunc("GET /v1/projects", s.requireAuth(s.withRateLimit(s.handleListProjects, s.config.RateLimitOther)))
	mux.HandleFunc("GET /v1/projects/{id}", s.requireProjectAuth(serverdb.RoleReader, s.withRateLimit(s.handleGetProject, s.config.RateLimitOther)))
	mux.HandleFunc("PATCH /v1/projects/{id}", s.requireProjectAuth(serverdb.RoleWriter, s.withRateLimit(s.handleUpdateProject, s.config.RateLimitOther)))
	mux.HandleFunc("DELETE /v1/projects/{id}", s.requireProjectAuth(serverdb.RoleOwner, s.withRateLimit(s.handleDeleteProject, s.config.RateLimitOther)))

	// Members
	mux.HandleFunc("POST /v1/projects/{id}/members", s.requireProjectAuth(serverdb.RoleOwner, s.withRateLimit(s.handleAddMember, s.config.RateLimitOther)))
	mux.HandleFunc("GET /v1/projects/{id}/members", s.requireProjectAuth(serverdb.RoleReader, s.withRateLimit(s.handleListMembers, s.config.RateLimitOther)))
	mux.HandleFunc("PATCH /v1/projects/{id}/members/{userID}", s.requireProjectAuth(serverdb.RoleOwner, s.withRateLimit(s.handleUpdateMember, s.config.RateLimitOther)))
	mux.HandleFunc("DELETE /v1/projects/{id}/members/{userID}", s.requireProjectAuth(serverdb.RoleOwner, s.withRateLimit(s.handleRemoveMember, s.config.RateLimitOther)))

	// Sync
	mux.HandleFunc("POST /v1/projects/{id}/sync/push", s.requireProjectAuth(serverdb.RoleWriter, s.withRateLimit(s.handleSyncPush, s.config.RateLimitPush)))
	mux.HandleFunc("GET /v1/projects/{id}/sync/pull", s.requireProjectAuth(serverdb.RoleReader, s.withRateLimit(s.handleSyncPull, s.config.RateLimitPull)))
	mux.HandleFunc("GET /v1/projects/{id}/sync/status", s.requireProjectAuth(serverdb.RoleReader, s.withRateLimit(s.handleSyncStatus, s.config.RateLimitOther)))
	mux.HandleFunc("GET /v1/projects/{id}/sync/snapshot", s.requireProjectAuth(serverdb.RoleReader, s.withRateLimit(s.handleSyncSnapshot, s.config.RateLimitOther)))

	return chain(mux, recoveryMiddleware, requestIDMiddleware, loggerMiddleware, metricsMiddleware(s.metrics), loggingMiddleware, maxBytesMiddleware(10<<20), authRateLimitMiddleware(s.rateLimiter, s.config.RateLimitAuth))
}

// handleHealth returns a health check response, pinging the server DB.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "detail": "db unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMetrics returns a snapshot of server metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Snapshot())
}
