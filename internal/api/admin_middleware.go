package api

import (
	"fmt"
	"net/http"
	"strings"
)

// requireAdmin returns an http.HandlerFunc that checks the caller is an
// authenticated admin with the required scope before invoking handler.
func (s *Server) requireAdmin(scope string, handler http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if !user.IsAdmin {
			writeError(w, http.StatusForbidden, ErrCodeInsufficientAdminScope, "admin access required")
			return
		}
		if !HasScope(strings.Join(user.Scopes, ","), scope) {
			writeError(w, http.StatusForbidden, ErrCodeInsufficientAdminScope, fmt.Sprintf("missing required scope: %s", scope))
			return
		}
		handler(w, r)
	})
}
