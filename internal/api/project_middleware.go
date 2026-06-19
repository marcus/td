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

// ErrCodeInsufficientScope is the error code returned when a caller has valid
// credentials but lacks the required scope to access a project route.
const ErrCodeInsufficientScope = "insufficient_scope"

// projectScopeAllowed returns true if the authenticated user is permitted to
// access project routes. The rule is: allow if any of:
//
//	(a) caller has the "sync" scope (normal CLI/td-watch sync key)
//	(b) caller has the ImpersonationScopeRead scope (ephemeral view-as key;
//	    already constrained to GET /v1/projects/* in requireAuth)
//	(c) caller.IsAdmin (admin key + X-Td-Watch-Impersonate header path)
//
// Returns false and writes a 403 response if none of the above apply.
func projectScopeAllowed(u *AuthUser) bool {
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	return HasAnyScope(strings.Join(u.Scopes, ","), "sync", ImpersonationScopeRead)
}

// ActingUser is the effective user identity for td-watch-originated
// /v1/projects* requests. When admin view-as is active, this is the target
// user rather than the authenticated admin.
type ActingUser struct {
	UserID          string
	Email           string
	IsImpersonating bool
}

func getActingUserFromContext(ctx context.Context) *ActingUser {
	v, _ := ctx.Value(ctxKeyActingUser).(*ActingUser)
	return v
}

func (s *Server) resolveProjectActor(r *http.Request) (*ActingUser, int, string, string, error) {
	caller := getUserFromContext(r.Context())
	if caller == nil {
		return nil, http.StatusUnauthorized, "unauthorized", "missing auth user", nil
	}

	actor := &ActingUser{
		UserID: caller.UserID,
		Email:  caller.Email,
	}

	targetID := strings.TrimSpace(r.Header.Get(HeaderTdWatchImpersonate))
	if targetID == "" || !strings.HasPrefix(r.URL.Path, "/v1/projects") {
		return actor, 0, "", "", nil
	}

	if !caller.IsAdmin {
		return nil, http.StatusForbidden, ErrCodeForbidden, "impersonation header requires admin auth", nil
	}

	target, err := s.store.GetUserByID(targetID)
	if err != nil {
		return nil, http.StatusInternalServerError, ErrCodeInternal, "failed to resolve impersonated user", err
	}
	if target == nil {
		return nil, http.StatusNotFound, ErrCodeNotFound, "impersonated user not found", nil
	}
	if target.IsAdmin && target.ID != caller.UserID {
		return nil, http.StatusForbidden, ErrCodeForbidden, "admin-to-admin view-as is not allowed", nil
	}

	return &ActingUser{
		UserID:          target.ID,
		Email:           target.Email,
		IsImpersonating: true,
	}, 0, "", "", nil
}

func (s *Server) attachProjectActor(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	actor, status, code, message, err := s.resolveProjectActor(r)
	if err != nil {
		logFor(r.Context()).Error("resolve project actor", "err", err, "path", r.URL.Path)
		writeError(w, status, code, message)
		return nil, false
	}
	if status != 0 {
		writeError(w, status, code, message)
		return nil, false
	}

	ctx := context.WithValue(r.Context(), ctxKeyActingUser, actor)
	if actor.IsImpersonating {
		ctx = context.WithValue(ctx, ctxKeyLogger, logFor(ctx).With("acting_uid", actor.UserID))
	}
	return r.WithContext(ctx), true
}

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

// requireProjectScope wraps an http.HandlerFunc to enforce that the caller
// passes projectScopeAllowed before the inner handler runs. It is intended for
// the flat project routes (GET /v1/projects, POST /v1/projects) that use
// requireAuth directly and therefore do not go through requireProjectMembership.
// It must be called after requireAuth has injected the AuthUser into the context.
func (s *Server) requireProjectScope(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := getUserFromContext(r.Context())
		if !projectScopeAllowed(user) {
			writeError(w, http.StatusForbidden, ErrCodeInsufficientScope, "key does not have the sync or impersonation:read scope required for project routes")
			return
		}
		handler(w, r)
	}
}

// requireProjectMembership returns middleware that enforces the authenticated
// caller has at least the requested role on the project identified by the
// "id" path value. It composes with requireAuth so the inner handler is only
// invoked after both API-key validation and project authorization succeed.
//
// role must be one of serverdb.RoleReader or serverdb.RoleWriter. Plain admin
// requests still bypass membership checks; admin requests carrying
// X-Td-Watch-Impersonate must satisfy membership as the target user.
func (s *Server) requireProjectMembership(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.requireAuthHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, ok := s.attachProjectActor(w, r)
			if !ok {
				return
			}
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

			if !projectScopeAllowed(user) {
				writeError(w, http.StatusForbidden, ErrCodeInsufficientScope, "key does not have the sync or impersonation:read scope required for project routes")
				return
			}

			actor := getActingUserFromContext(r.Context())
			if actor == nil || actor.UserID == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing acting user")
				return
			}

			if actor.IsImpersonating {
				if err := s.store.Authorize(projectID, actor.UserID, role); err != nil {
					writeError(w, http.StatusForbidden, "forbidden", err.Error())
					return
				}
			} else if !user.IsAdmin {
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
