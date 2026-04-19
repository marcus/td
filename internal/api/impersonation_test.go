package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// issueImpersonationToken calls the admin issuance endpoint and returns the
// decoded response. Fatals on non-200.
func issueImpersonationToken(t *testing.T, h *TestHarness, adminToken, targetUserID string) impersonationTokenResponse {
	t.Helper()
	var out impersonationTokenResponse
	resp := h.Do("POST", fmt.Sprintf("/v1/admin/users/%s/impersonation-token", targetUserID), adminToken, struct{}{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("issue impersonation token: expected 200, got %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode impersonation token: %v", err)
	}
	return out
}

func TestIssueImpersonationToken_Success(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		Done()

	adminToken := state.AdminToken("admin@test.com")
	targetID := state.UserID("target@test.com")

	tok := issueImpersonationToken(t, h, adminToken, targetID)

	if !strings.HasPrefix(tok.APIKey, "td_ipk_") {
		t.Fatalf("expected td_ipk_ prefix, got %q", tok.APIKey)
	}
	if tok.Scopes != "impersonation:read" {
		t.Fatalf("expected impersonation:read scopes, got %q", tok.Scopes)
	}
	if tok.KeyID == "" {
		t.Fatal("expected non-empty key_id")
	}
	if tok.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at")
	}
	expAt, err := time.Parse(time.RFC3339, tok.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	want := time.Now().UTC().Add(15 * time.Minute)
	if diff := want.Sub(expAt); diff > 30*time.Second || diff < -30*time.Second {
		t.Fatalf("expires_at %v not within 30s of now+15m %v", expAt, want)
	}

	// Key present in DB.
	keys, err := h.Store.ListAPIKeysForUser(targetID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	found := false
	for _, k := range keys {
		if k.ID == tok.KeyID && k.Scopes == "impersonation:read" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected impersonation key row in DB")
	}

	// auth_event recorded.
	got, err := h.Store.QueryAuthEvents(serverdb.AuthEventImpersonationIssued, "", "", "", 0, "")
	if err != nil {
		t.Fatalf("query auth events: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected 1 impersonation_issued event, got %d", len(got.Data))
	}
	if !strings.Contains(got.Data[0].Metadata, tok.KeyID) {
		t.Fatalf("expected metadata to contain key_id %q, got %s", tok.KeyID, got.Data[0].Metadata)
	}
}

func TestIssueImpersonationToken_SelfIsAllowed(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		Done()

	adminID := state.UserID("admin@test.com")
	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), adminID)

	if !strings.HasPrefix(tok.APIKey, "td_ipk_") {
		t.Fatalf("expected td_ipk_ prefix, got %q", tok.APIKey)
	}
	if tok.Scopes != "impersonation:read" {
		t.Fatalf("expected impersonation:read scopes, got %q", tok.Scopes)
	}

	// Key is bound to the admin themselves (target == caller).
	keys, err := h.Store.ListAPIKeysForUser(adminID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	found := false
	for _, k := range keys {
		if k.ID == tok.KeyID && k.Scopes == "impersonation:read" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected impersonation key row bound to admin's own user id")
	}

	// auth_event recorded with admin_user_id == target_user_id.
	got, err := h.Store.QueryAuthEvents(serverdb.AuthEventImpersonationIssued, "", "", "", 0, "")
	if err != nil {
		t.Fatalf("query auth events: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected 1 impersonation_issued event, got %d", len(got.Data))
	}
	meta := got.Data[0].Metadata
	if !strings.Contains(meta, fmt.Sprintf(`"admin_user_id":%q`, adminID)) {
		t.Fatalf("expected metadata admin_user_id == %q, got %s", adminID, meta)
	}
	if !strings.Contains(meta, fmt.Sprintf(`"target_user_id":%q`, adminID)) {
		t.Fatalf("expected metadata target_user_id == %q, got %s", adminID, meta)
	}
}

func TestIssueImpersonationToken_SelfWhenAdmin(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		Done()

	adminID := state.UserID("admin@test.com")
	resp := h.Do("POST",
		fmt.Sprintf("/v1/admin/users/%s/impersonation-token", adminID),
		state.AdminToken("admin@test.com"),
		struct{}{},
	)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for admin viewing as self, got %d", resp.StatusCode)
	}
}

func TestIssueImpersonationToken_TargetIsAdmin(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin1@test.com", "admin:read:server,sync").
		WithAdmin("admin2@test.com", "sync").
		Done()

	// admin1 targets admin2 (a DIFFERENT admin) — must be rejected with 403.
	resp := h.Do("POST",
		fmt.Sprintf("/v1/admin/users/%s/impersonation-token", state.UserID("admin2@test.com")),
		state.AdminToken("admin1@test.com"),
		struct{}{},
	)
	AssertErrorResponse(t, resp, http.StatusForbidden, "forbidden")
}

func TestIssueImpersonationToken_InsufficientScope(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:projects"). // missing admin:read:server
		WithUser("target@test.com").
		Done()

	resp := h.Do("POST",
		fmt.Sprintf("/v1/admin/users/%s/impersonation-token", state.UserID("target@test.com")),
		state.AdminToken("admin@test.com"),
		struct{}{},
	)
	AssertErrorResponse(t, resp, http.StatusForbidden, "insufficient_admin_scope")
}

func TestIssueImpersonationToken_ChainedViewAsBlocked(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		WithUser("victim@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))

	// Attempt to issue another impersonation token using the impersonation key
	resp := h.Do("POST",
		fmt.Sprintf("/v1/admin/users/%s/impersonation-token", state.UserID("victim@test.com")),
		tok.APIKey,
		struct{}{},
	)
	// Request hits requireAuth first — impersonation key on /v1/admin/* is
	// rejected by scope check with ErrCodeForbidden before reaching the
	// admin middleware.
	AssertErrorResponse(t, resp, http.StatusForbidden, "forbidden")
}

func TestImpersonationKey_AllowsProjectGet(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		WithProject("proj1", "target@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))

	pid := state.ProjectID("proj1")
	resp := h.Do("GET", fmt.Sprintf("/v1/projects/%s", pid), tok.APIKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET project, got %d", resp.StatusCode)
	}

	// /v1/projects (list) is also allowed.
	resp2 := h.Do("GET", "/v1/projects", tok.APIKey, nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on GET /v1/projects, got %d", resp2.StatusCode)
	}
}

func TestImpersonationKey_RejectsProjectPost(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		WithProject("proj1", "target@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))
	pid := state.ProjectID("proj1")

	resp := h.Do("POST", fmt.Sprintf("/v1/projects/%s/sync/push", pid), tok.APIKey, PushRequest{
		DeviceID:  "d1",
		SessionID: "s1",
		Events:    []EventInput{},
	})
	AssertErrorResponse(t, resp, http.StatusForbidden, "method_not_allowed_view_as")
}

func TestImpersonationKey_RejectsWrongProject(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		WithUser("other@test.com").
		WithProject("other_proj", "other@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))
	otherPid := state.ProjectID("other_proj")

	resp := h.Do("GET", fmt.Sprintf("/v1/projects/%s", otherPid), tok.APIKey, nil)
	AssertErrorResponse(t, resp, http.StatusForbidden, "forbidden")
}

func TestImpersonationKey_RejectsAdminEndpoints(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))

	resp := h.Do("GET", "/v1/admin/users", tok.APIKey, nil)
	AssertErrorResponse(t, resp, http.StatusForbidden, "forbidden")
}

func TestImpersonationKey_SlidingTTL(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		WithProject("proj1", "target@test.com").
		Done()

	tok := issueImpersonationToken(t, h, state.AdminToken("admin@test.com"), state.UserID("target@test.com"))

	// Capture the initial expires_at from DB.
	keys, err := h.Store.ListAPIKeysForUser(state.UserID("target@test.com"))
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	var before time.Time
	for _, k := range keys {
		if k.ID == tok.KeyID && k.ExpiresAt != nil {
			before = *k.ExpiresAt
		}
	}
	if before.IsZero() {
		t.Fatal("initial expires_at not set")
	}

	// Sleep briefly so the new expires_at differs from the initial one.
	time.Sleep(30 * time.Millisecond)

	// Successful GET on a /v1/projects/* path bumps the TTL.
	resp := h.Do("GET", fmt.Sprintf("/v1/projects/%s", state.ProjectID("proj1")), tok.APIKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	keys2, err := h.Store.ListAPIKeysForUser(state.UserID("target@test.com"))
	if err != nil {
		t.Fatalf("list keys after: %v", err)
	}
	var after time.Time
	for _, k := range keys2 {
		if k.ID == tok.KeyID && k.ExpiresAt != nil {
			after = *k.ExpiresAt
		}
	}
	if after.IsZero() {
		t.Fatal("post-request expires_at not set")
	}

	// After a successful request, expires_at should be ~ now + 5m, which is
	// LESS than the initial now + 15m. This is by design: renewTTL=5m,
	// initialTTL=15m.
	delta := before.Sub(after)
	if delta < 9*time.Minute || delta > 11*time.Minute {
		t.Fatalf("expected before-after delta ~10m, got %v (before=%v after=%v)", delta, before, after)
	}
}

func TestImpersonationKey_RevokesPriorKeys(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	state := h.Build().
		WithAdmin("admin@test.com", "admin:read:server,sync").
		WithUser("target@test.com").
		Done()

	adminToken := state.AdminToken("admin@test.com")
	targetID := state.UserID("target@test.com")

	first := issueImpersonationToken(t, h, adminToken, targetID)
	second := issueImpersonationToken(t, h, adminToken, targetID)

	if first.KeyID == second.KeyID {
		t.Fatal("second issuance returned same key_id as first")
	}

	// The first key should no longer verify.
	ak, _, err := h.Store.VerifyAPIKey(first.APIKey)
	if err != nil {
		t.Fatalf("verify first key: %v", err)
	}
	if ak != nil {
		t.Fatal("first impersonation key should be revoked after re-issuance")
	}

	// The second key should verify.
	ak2, _, err := h.Store.VerifyAPIKey(second.APIKey)
	if err != nil {
		t.Fatalf("verify second key: %v", err)
	}
	if ak2 == nil {
		t.Fatal("second impersonation key should be valid")
	}
}
