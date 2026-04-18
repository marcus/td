package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcus/td/internal/serverdb"
)

// createTestAdminKey creates an admin user with specific scopes, returning userID and bearer token.
func createTestAdminKey(t *testing.T, store *serverdb.ServerDB, email string, scopes string) (string, string) {
	t.Helper()
	user, err := store.CreateUser(email)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if !user.IsAdmin {
		if err := store.SetUserAdmin(email, true); err != nil {
			t.Fatalf("set admin: %v", err)
		}
	}
	token, _, err := store.GenerateAPIKey(user.ID, "admin-test", scopes, nil)
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	return user.ID, token
}

func TestAdminMiddleware_ValidAdminCorrectScope(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	called := false
	handler := srv.requireAdmin("admin:read:server", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("inner handler was not called")
	}
}

func TestAdminMiddleware_NonAdminReturns403(t *testing.T) {
	srv, store := newTestServer(t)
	// First user is auto-admin; create a second non-admin user
	_, _ = store.CreateUser("first@test.com") // consume auto-admin slot
	userID, token := createTestUser(t, store, "nonadmin@test.com")
	_ = userID

	handler := srv.requireAdmin("admin:read:server", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for non-admin")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "insufficient_admin_scope" {
		t.Fatalf("expected error code insufficient_admin_scope, got %q", resp.Error.Code)
	}
}

func TestAdminMiddleware_AdminWrongScope(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:projects")

	handler := srv.requireAdmin("admin:read:server", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with wrong scope")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "insufficient_admin_scope" {
		t.Fatalf("expected error code insufficient_admin_scope, got %q", resp.Error.Code)
	}
}

func TestAdminMiddleware_NoKeyReturns401(t *testing.T) {
	srv, _ := newTestServer(t)

	handler := srv.requireAdmin("admin:read:server", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without auth")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminMiddleware_MultipleScopesCorrectPasses(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,admin:read:projects,sync")

	called := false
	handler := srv.requireAdmin("admin:read:server", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("inner handler was not called")
	}
}

func TestAdminMiddleware_MultipleScopesWrongFails(t *testing.T) {
	srv, store := newTestServer(t)
	_, token := createTestAdminKey(t, store, "admin@test.com", "admin:read:server,sync")

	handler := srv.requireAdmin("admin:read:projects", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called with wrong scope")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "insufficient_admin_scope" {
		t.Fatalf("expected error code insufficient_admin_scope, got %q", resp.Error.Code)
	}
}
