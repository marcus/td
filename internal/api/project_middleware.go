// Package api: project-scoped middleware for the td-watch BFF surface.
//
// These middlewares are intended for the new Perch-shape /v1/projects/{id}/*
// routes (see plan §6.3). They are deliberately separate from the existing
// /v1/sync/* path so the legacy CLI sync flow is unaffected.
//
// requireProjectMembership composes the existing API-key auth with a
// per-project role check. resolveTdWatchSession derives the session_id that
// will eventually be stamped onto action_log rows so `td monitor` and audit
// surfaces preserve actor provenance for td-watch-originated writes.
package api

import (
	"context"
	"net/http"
	"strings"
)

// HeaderTdWatchSession carries the td-watch app session id (NOT a Claude/td
// session). td-watch's BFF forwards this for every project-scoped request so
// td-sync can derive a stable, attributable action_log session_id.
const HeaderTdWatchSession = "X-Td-Watch-Session"

// HeaderTdWatchImpersonate carries the target user id when an admin uses
// td-watch's "view as user" feature. The td-watch BFF is the only writer of
// this header; td-sync still authenticates the request against the admin's own
// API key, so the header alone is never sufficient to act as someone else.
const HeaderTdWatchImpersonate = "X-Td-Watch-Impersonate"

// TdWatchServerDeviceID is the constant device_id for events promoted from
// td-watch-originated writes. Per plan §10 Q2, td-watch is not a /sync/pull
// client, so a single server-originated device id keeps dedupe semantics
// (device_id, session_id, client_action_id) intact without requiring a
// per-browser device registration.
const TdWatchServerDeviceID = "td_watch_server"

// requireProjectMembership returns middleware that enforces the authenticated
// caller has at least the requested role on the project identified by the
// "id" path value. It composes with requireAuth so the inner handler is only
// invoked after both API-key validation and project authorization succeed.
//
// role must be one of serverdb.RoleReader or serverdb.RoleWriter. Admin users
// bypass the membership check (admins routinely operate cross-project, and
// the td-watch BFF relies on this for "view as user" reads).
func (s *Server) requireProjectMembership(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.requireAuthHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID := r.PathValue("id")
			if projectID == "" {
				writeError(w, http.StatusBadRequest, "bad_request", "missing project id")
				return
			}

			user := getUserFromContext(r.Context())
			if user == nil {
				// Defense-in-depth: requireAuthHandler should always populate this.
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing auth user")
				return
			}

			if !user.IsAdmin {
				if err := s.store.Authorize(projectID, user.UserID, role); err != nil {
					writeError(w, http.StatusForbidden, "forbidden", err.Error())
					return
				}
			}

			ctx := context.WithValue(r.Context(), ctxKeyLogger, logFor(r.Context()).With("pid", projectID))
			next.ServeHTTP(w, r.WithContext(ctx))
		}))
	}
}

// resolveTdWatchSession derives the action_log session_id from the
// X-Td-Watch-Session and (optional) X-Td-Watch-Impersonate headers and stashes
// it on the request context under ctxKeyTdWatchSessionID. It MUST run after
// requireAuth so the AuthUser is available as a fallback.
//
// Format (per plan §10 Q1):
//   - normal user write:        "twu_" + sanitize(session header)
//   - admin "view as" write:    "twa_" + sanitize(admin session header) + "_as_" + sanitize(target user id)
//   - missing td-watch session: "twu_unknown_" + sanitize(auth user id)
//     (the request still proceeds — auth still attributes to a user — but the
//     unknown_ prefix flags the missing provenance for later audit triage)
//
// SECURITY: the impersonation target id alone is never used as the session
// identifier. The actor (admin) is always part of the session_id so that
// `td monitor` and any audit replay can answer "who actually clicked this?".
// Likewise, the API key, the bearer token, and the X-Td-Watch-Impersonate
// header value alone are never trusted as session identity.
func (s *Server) resolveTdWatchSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(HeaderTdWatchSession)
		impersonate := r.Header.Get(HeaderTdWatchImpersonate)

		var sessionID string
		switch {
		case raw != "" && impersonate != "":
			sessionID = "twa_" + sanitizeSessionPart(raw) + "_as_" + sanitizeSessionPart(impersonate)
		case raw != "":
			sessionID = "twu_" + sanitizeSessionPart(raw)
		default:
			fallback := "anonymous"
			if user := getUserFromContext(r.Context()); user != nil && user.UserID != "" {
				fallback = sanitizeSessionPart(user.UserID)
			}
			sessionID = "twu_unknown_" + fallback
		}

		ctx := context.WithValue(r.Context(), ctxKeyTdWatchSessionID, sessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TdWatchSessionFromCtx returns the resolved td-watch session_id stashed by
// resolveTdWatchSession, or "" if the middleware did not run.
func TdWatchSessionFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTdWatchSessionID).(string)
	return v
}

// sanitizeSessionPart restricts each component to a safe character set
// ([A-Za-z0-9._-]) and caps length so a malicious client cannot blow up
// downstream log lines. Underscores are allowed because real td user/session
// ids contain them (e.g. "u_bob"); to keep the impersonation separator
// "_as_" unambiguous we ALSO neutralize any literal "_as_" substring inside a
// part by replacing it with "_AT_". This means the rightmost "_as_" in a
// composed twa_* session_id is always the actor/target separator regardless
// of what the client sent.
func sanitizeSessionPart(s string) string {
	const maxLen = 128
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			// Skip spaces, slashes, control chars, etc.
		}
	}
	out := b.String()
	if out == "" {
		return "empty"
	}
	// Neutralize the reserved separator inside any single part.
	out = strings.ReplaceAll(out, "_as_", "_AT_")
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

// requireAuthHandler is an http.Handler-shaped adapter over the existing
// requireAuth (which speaks http.HandlerFunc). It exists so requireProjectMembership
// can compose cleanly into the standard `func(http.Handler) http.Handler`
// middleware signature used by the new Perch-shape route table (S2.3).
func (s *Server) requireAuthHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(s.requireAuth(next.ServeHTTP))
}
