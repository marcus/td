package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// Impersonation key TTL parameters. Exposed as vars for tests.
var (
	impersonationInitialTTL = 15 * time.Minute
	impersonationRenewTTL   = 5 * time.Minute
	impersonationMaxTTL     = 4 * time.Hour
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

// impersonationTokenResponse is the JSON shape for an issued impersonation token.
type impersonationTokenResponse struct {
	KeyID     string `json:"key_id"`
	APIKey    string `json:"api_key"`
	ExpiresAt string `json:"expires_at"`
	Scopes    string `json:"scopes"`
}

// handleAdminIssueImpersonationToken issues a short-lived td_ipk_ key bound
// to the target user. The caller must be an admin with admin:read:server
// scope AND must not themselves be using an impersonation key (no chained
// view-as). Admin-to-self is allowed; admin-to-other-admin is rejected.
func (s *Server) handleAdminIssueImpersonationToken(w http.ResponseWriter, r *http.Request) {
	caller := getUserFromContext(r.Context())

	// Block chained view-as: an impersonation key must not be able to issue
	// another impersonation key.
	for _, sc := range caller.Scopes {
		if sc == ImpersonationScopeRead {
			writeError(w, http.StatusForbidden, ErrCodeForbidden, "chained view-as is not allowed")
			return
		}
	}

	targetID := r.PathValue("id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing user id")
		return
	}

	// Drain any body (contract allows empty {}).
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&struct{}{})

	target, err := s.store.GetUserByID(targetID)
	if err != nil {
		logFor(r.Context()).Error("admin issue impersonation token: get user", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get user")
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "user not found")
		return
	}
	if target.IsAdmin && target.ID != caller.UserID {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "admin-to-admin view-as is not allowed")
		return
	}

	plaintext, ak, err := s.store.GenerateImpersonationKey(target.ID, impersonationInitialTTL)
	if err != nil {
		logFor(r.Context()).Error("admin issue impersonation token: generate", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to issue impersonation token")
		return
	}

	meta, _ := json.Marshal(map[string]string{
		"admin_user_id":  caller.UserID,
		"target_user_id": target.ID,
		"key_id":         ak.ID,
	})
	if err := s.store.InsertAuthEvent("", target.Email, serverdb.AuthEventImpersonationIssued, string(meta)); err != nil {
		slog.Warn("log impersonation issued", "err", err)
	}

	resp := impersonationTokenResponse{
		KeyID:     ak.ID,
		APIKey:    plaintext,
		ExpiresAt: ak.ExpiresAt.UTC().Format(time.RFC3339),
		Scopes:    ak.Scopes,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminRevokeUserKey deletes an API key by ID for a given user.
// The caller must have admin:write:users scope.
// Returns 204 on success, 404 if the user or key does not exist.
func (s *Server) handleAdminRevokeUserKey(w http.ResponseWriter, r *http.Request) {
	caller := getUserFromContext(r.Context())

	userID := r.PathValue("id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing user id")
		return
	}
	keyID := r.PathValue("keyID")
	if keyID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "missing key id")
		return
	}

	// Verify user exists.
	user, err := s.store.GetUserByID(userID)
	if err != nil {
		slog.Error("admin revoke user key: get user", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to get user")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, ErrCodeNotFound, "user not found")
		return
	}

	if err := s.store.AdminRevokeAPIKey(keyID); err != nil {
		if err == serverdb.ErrNotFound {
			writeError(w, http.StatusNotFound, ErrCodeNotFound, "key not found")
			return
		}
		slog.Error("admin revoke user key", "err", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternal, "failed to revoke api key")
		return
	}

	meta, _ := json.Marshal(map[string]string{
		"admin_user_id": caller.UserID,
		"key_id":        keyID,
	})
	if err := s.store.InsertAuthEvent("", user.Email, serverdb.AuthEventKeyRevoked, string(meta)); err != nil {
		slog.Warn("log key revoked", "err", err)
	}

	w.WriteHeader(http.StatusNoContent)
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
