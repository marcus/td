package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

// newProjectWithMembers creates a project owned by `owner`, then adds zero
// or more additional members at the requested roles. Returns the project id.
func newProjectWithMembers(t *testing.T, store *serverdb.ServerDB, owner string, members map[string]string) string {
	t.Helper()
	p, err := store.CreateProject("p", "", owner)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	for uid, role := range members {
		if _, err := store.AddMember(p.ID, uid, role, owner); err != nil {
			t.Fatalf("add member %s: %v", uid, err)
		}
	}
	return p.ID
}

// callMembership is a small helper that wires requireProjectMembership in
// front of a counter handler and runs the request through a Go 1.22 mux so
// PathValue("id") resolves correctly.
func callMembership(srv *Server, role, projectID, token string, headers map[string]string) (*httptest.ResponseRecorder, *bool) {
	called := false
	mw := srv.requireProjectMembership(role)
	mux := http.NewServeMux()
	mux.Handle("GET /v1/projects/{id}/probe", mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/v1/projects/"+projectID+"/probe", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w, &called
}

// --- requireProjectMembership ---------------------------------------------

func TestRequireProjectMembership_WriterAllowed(t *testing.T) {
	srv, store := newTestServer(t)
	// First user becomes admin automatically; create another to act as the
	// project owner so the writer-under-test isn't admin-bypassed.
	_, _ = createTestUser(t, store, "first@test.com")
	ownerID, _ := createTestUser(t, store, "owner@test.com")
	writerID, writerToken := createTestUser(t, store, "writer@test.com")
	pid := newProjectWithMembers(t, store, ownerID, map[string]string{writerID: serverdb.RoleWriter})

	w, called := callMembership(srv, serverdb.RoleWriter, pid, writerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !*called {
		t.Fatal("inner handler not invoked")
	}
}

func TestRequireProjectMembership_ReaderHittingWriterRouteForbidden(t *testing.T) {
	srv, store := newTestServer(t)
	_, _ = createTestUser(t, store, "first@test.com")
	ownerID, _ := createTestUser(t, store, "owner@test.com")
	readerID, readerToken := createTestUser(t, store, "reader@test.com")
	pid := newProjectWithMembers(t, store, ownerID, map[string]string{readerID: serverdb.RoleReader})

	w, called := callMembership(srv, serverdb.RoleWriter, pid, readerToken, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if *called {
		t.Fatal("inner handler should not be invoked when forbidden")
	}
	var resp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "forbidden" {
		t.Fatalf("expected error code forbidden, got %q", resp.Error.Code)
	}
}

func TestRequireProjectMembership_NonMemberForbidden(t *testing.T) {
	srv, store := newTestServer(t)
	_, _ = createTestUser(t, store, "first@test.com")
	ownerID, _ := createTestUser(t, store, "owner@test.com")
	_, strangerToken := createTestUser(t, store, "stranger@test.com")
	pid := newProjectWithMembers(t, store, ownerID, nil)

	w, called := callMembership(srv, serverdb.RoleReader, pid, strangerToken, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if *called {
		t.Fatal("inner handler should not be invoked")
	}
}

func TestRequireProjectMembership_AdminAllowedWithoutMembership(t *testing.T) {
	srv, store := newTestServer(t)
	// First user is auto-admin.
	_, adminToken := createTestAdminKey(t, store, "admin@test.com", "sync")
	ownerID, _ := createTestUser(t, store, "owner@test.com")
	pid := newProjectWithMembers(t, store, ownerID, nil)

	w, called := callMembership(srv, serverdb.RoleWriter, pid, adminToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin, got %d: %s", w.Code, w.Body.String())
	}
	if !*called {
		t.Fatal("inner handler not invoked for admin")
	}
}

func TestRequireProjectMembership_NoAuthReturns401(t *testing.T) {
	srv, _ := newTestServer(t)
	w, called := callMembership(srv, serverdb.RoleReader, "p_nosuch", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if *called {
		t.Fatal("inner handler should not be invoked without auth")
	}
}

// --- resolveTdWatchSession -------------------------------------------------

// runResolveSession invokes resolveTdWatchSession with the given headers and
// returns the session_id stashed on the request context.
func runResolveSession(t *testing.T, srv *Server, headers map[string]string, authUser *AuthUser) string {
	t.Helper()
	var captured string
	mw := srv.resolveTdWatchSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = TdWatchSessionFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/probe", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if authUser != nil {
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyAuthUser, authUser))
	}
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("inner status = %d", w.Code)
	}
	return captured
}

func TestResolveTdWatchSession_NormalUser(t *testing.T) {
	srv, _ := newTestServer(t)
	got := runResolveSession(t, srv, map[string]string{
		HeaderTdWatchSession: "abc123",
	}, nil)
	want := "twu_abc123"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestResolveTdWatchSession_Impersonation(t *testing.T) {
	srv, _ := newTestServer(t)
	got := runResolveSession(t, srv, map[string]string{
		HeaderTdWatchSession:     "adminSess",
		HeaderTdWatchImpersonate: "u_target",
	}, nil)
	want := "twa_adminSess_as_u_target"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, "twa_") || !strings.Contains(got, "_as_") {
		t.Fatalf("impersonation session_id missing required structure: %q", got)
	}
}

func TestResolveTdWatchSession_MissingHeadersFallsBackToUser(t *testing.T) {
	srv, _ := newTestServer(t)
	got := runResolveSession(t, srv, nil, &AuthUser{UserID: "u_bob"})
	want := "twu_unknown_u_bob"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestResolveTdWatchSession_MissingHeadersAndNoAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	got := runResolveSession(t, srv, nil, nil)
	want := "twu_unknown_anonymous"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestResolveTdWatchSession_ImpersonationOnlyTargetIgnored(t *testing.T) {
	// An impersonation header without a session header MUST NOT be used as
	// the session id (would hide the actor). It should fall through to the
	// "unknown" path.
	srv, _ := newTestServer(t)
	got := runResolveSession(t, srv, map[string]string{
		HeaderTdWatchImpersonate: "u_target",
	}, &AuthUser{UserID: "u_admin"})
	want := "twu_unknown_u_admin"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
	if strings.Contains(got, "u_target") {
		t.Fatalf("impersonation target leaked into session_id without admin actor: %q", got)
	}
}

func TestResolveTdWatchSession_SanitizesWeirdChars(t *testing.T) {
	srv, _ := newTestServer(t)
	// Slashes, spaces, control chars, and other punctuation must be stripped.
	// Underscores in the input ARE preserved because real td ids use them
	// ("u_bob"), but any literal "_as_" inside a single part is replaced with
	// "_AT_" so the rightmost "_as_" in the composed string is always the
	// actor/target separator.
	got := runResolveSession(t, srv, map[string]string{
		HeaderTdWatchSession:     "ab_c/12 3\tx_as_evil",
		HeaderTdWatchImpersonate: "u/../../target!!",
	}, nil)
	// "ab_c/12 3\tx_as_evil" -> "ab_c123x_as_evil" -> "ab_c123x_AT_evil"
	// "u/../../target!!"     -> "u....target"
	want := "twa_ab_c123x_AT_evil_as_u....target"
	if got != want {
		t.Fatalf("sanitized session_id = %q, want %q", got, want)
	}
	// Critical invariant: exactly one "_as_" separator survives.
	if strings.Count(got, "_as_") != 1 {
		t.Fatalf("sanitization allowed multiple _as_ separators: %q", got)
	}
}

func TestResolveTdWatchSession_EmptyAfterSanitizationBecomesEmptyMarker(t *testing.T) {
	srv, _ := newTestServer(t)
	// A header containing only stripped-out chars should sanitize to "empty"
	// so the format stays well-formed. (Underscores ARE allowed, so we use
	// chars that get stripped: spaces and tabs only.)
	got := runResolveSession(t, srv, map[string]string{
		HeaderTdWatchSession: "    \t  ",
	}, nil)
	want := "twu_empty"
	if got != want {
		t.Fatalf("session_id = %q, want %q", got, want)
	}
}

func TestTdWatchServerDeviceID_Constant(t *testing.T) {
	// Pinned constant — promotion code in S3 will key on this exact value.
	if TdWatchServerDeviceID != "td_watch_server" {
		t.Fatalf("TdWatchServerDeviceID drifted: %q", TdWatchServerDeviceID)
	}
}
