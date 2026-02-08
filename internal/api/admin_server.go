package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// serverOverviewResponse is the JSON response for GET /v1/admin/server/overview.
type serverOverviewResponse struct {
	UptimeSeconds float64         `json:"uptime_seconds"`
	Health        string          `json:"health"`
	Metrics       MetricsSnapshot `json:"metrics"`
	TotalProjects int             `json:"total_projects"`
	TotalUsers    int             `json:"total_users"`
	TotalMembers  int             `json:"total_members"`
}

// handleAdminServerOverview returns server overview including uptime, health, metrics, and counts.
func (s *Server) handleAdminServerOverview(w http.ResponseWriter, r *http.Request) {
	health := "ok"
	if err := s.store.Ping(); err != nil {
		health = "error"
	}

	users, err := s.store.CountUsers()
	if err != nil {
		slog.Error("count users", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to count users")
		return
	}

	projects, err := s.store.CountProjects()
	if err != nil {
		slog.Error("count projects", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to count projects")
		return
	}

	members, err := s.store.CountMembers()
	if err != nil {
		slog.Error("count members", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to count members")
		return
	}

	writeJSON(w, http.StatusOK, serverOverviewResponse{
		UptimeSeconds: time.Since(s.startTime).Seconds(),
		Health:        health,
		Metrics:       s.metrics.Snapshot(),
		TotalProjects: projects,
		TotalUsers:    users,
		TotalMembers:  members,
	})
}

// serverConfigResponse is the JSON response for GET /v1/admin/server/config.
type serverConfigResponse struct {
	ListenAddr                string              `json:"listen_addr"`
	AllowSignup               bool                `json:"allow_signup"`
	LogLevel                  string              `json:"log_level"`
	LogFormat                 string              `json:"log_format"`
	RateLimits                rateLimitsConfig     `json:"rate_limits"`
	CORSOrigins               []string            `json:"cors_origins"`
	AuthEventRetention        string              `json:"auth_event_retention"`
	RateLimitEventRetention   string              `json:"rate_limit_event_retention"`
}

type rateLimitsConfig struct {
	Auth  int `json:"auth"`
	Push  int `json:"push"`
	Pull  int `json:"pull"`
	Other int `json:"other"`
}

// handleAdminServerConfig returns non-secret config values.
func (s *Server) handleAdminServerConfig(w http.ResponseWriter, r *http.Request) {
	origins := s.config.CORSAllowedOrigins
	if origins == nil {
		origins = []string{}
	}

	writeJSON(w, http.StatusOK, serverConfigResponse{
		ListenAddr:  s.config.ListenAddr,
		AllowSignup: s.config.AllowSignup,
		LogLevel:    s.config.LogLevel,
		LogFormat:   s.config.LogFormat,
		RateLimits: rateLimitsConfig{
			Auth:  s.config.RateLimitAuth,
			Push:  s.config.RateLimitPush,
			Pull:  s.config.RateLimitPull,
			Other: s.config.RateLimitOther,
		},
		CORSOrigins:             origins,
		AuthEventRetention:      formatDaysDuration(s.config.AuthEventRetention),
		RateLimitEventRetention: formatDaysDuration(s.config.RateLimitEventRetention),
	})
}

// formatDaysDuration formats a duration as "Nd" if it's an exact number of days,
// otherwise falls back to Go's standard duration string.
func formatDaysDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	days := int(d.Hours() / 24)
	if time.Duration(days)*24*time.Hour == d {
		return fmt.Sprintf("%dd", days)
	}
	return d.String()
}

// handleAdminRateLimitViolations returns paginated rate limit violation events.
func (s *Server) handleAdminRateLimitViolations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := q.Get("from")
	to := q.Get("to")
	keyID := q.Get("key_id")
	ip := q.Get("ip")
	cursor := q.Get("cursor")

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	limit = serverdb.NormalizeLimit(limit)

	result, err := s.store.QueryRateLimitEvents(keyID, ip, from, to, limit, cursor)
	if err != nil {
		slog.Error("query rate limit events", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to query rate limit events")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
