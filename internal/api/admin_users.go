package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/marcus/td/internal/serverdb"
)

// handleAdminListUsers returns a paginated list of users with aggregate info.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := q.Get("q")
	cursor := q.Get("cursor")

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	limit = serverdb.NormalizeLimit(limit)

	result, err := s.store.AdminListUsers(query, limit, cursor)
	if err != nil {
		slog.Error("admin list users", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to list users")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleAdminGetUser returns a single user with detail and project memberships.
func (s *Server) handleAdminGetUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing user id")
		return
	}

	detail, err := s.store.AdminGetUser(userID)
	if err != nil {
		slog.Error("admin get user", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get user")
		return
	}
	if detail == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// apiKeyInfoResponse is the JSON shape for API key info (no key_hash).
type apiKeyInfoResponse struct {
	ID         string  `json:"id"`
	KeyPrefix  string  `json:"key_prefix"`
	Name       string  `json:"name"`
	Scopes     string  `json:"scopes"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

// handleAdminUserKeys returns the API keys for a given user.
func (s *Server) handleAdminUserKeys(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing user id")
		return
	}

	// Verify user exists
	user, err := s.store.GetUserByID(userID)
	if err != nil {
		slog.Error("admin user keys: get user", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get user")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "user not found")
		return
	}

	keys, err := s.store.ListAPIKeysForUser(userID)
	if err != nil {
		slog.Error("admin user keys", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to list api keys")
		return
	}

	resp := make([]apiKeyInfoResponse, 0, len(keys))
	for _, k := range keys {
		info := apiKeyInfoResponse{
			ID:        k.ID,
			KeyPrefix: k.KeyPrefix,
			Name:      k.Name,
			Scopes:    k.Scopes,
			CreatedAt: k.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if k.LastUsedAt != nil {
			s := k.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
			info.LastUsedAt = &s
		}
		if k.ExpiresAt != nil {
			s := k.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
			info.ExpiresAt = &s
		}
		resp = append(resp, info)
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": resp})
}

// handleAdminAuthEvents returns paginated auth events with optional filters.
func (s *Server) handleAdminAuthEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	eventType := q.Get("status")
	email := q.Get("email")
	from := q.Get("from")
	to := q.Get("to")
	cursor := q.Get("cursor")

	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	limit = serverdb.NormalizeLimit(limit)

	result, err := s.store.QueryAuthEvents(eventType, email, from, to, limit, cursor)
	if err != nil {
		slog.Error("admin auth events", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to query auth events")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
