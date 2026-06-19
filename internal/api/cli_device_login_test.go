package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/email"
	"github.com/marcus/td/internal/syncclient"
	"github.com/marcus/td/internal/syncconfig"
)

// approvalTokenFromEmail extracts the selector.secret token from the device
// approval link in an email text body. This mirrors what a real user does when
// they click the emailed link — here we read it directly from the in-process
// MemorySender instead.
func approvalTokenFromEmail(t *testing.T, text string) string {
	t.Helper()
	const marker = "/auth/device/approve?token="
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("approval marker %q not found in email body: %q", marker, text)
	}
	rest := text[idx+len(marker):]
	if end := strings.IndexAny(rest, " \t\n\r"); end >= 0 {
		rest = rest[:end]
	}
	if rest == "" {
		t.Fatal("empty approval token")
	}
	return rest
}

// startCLIDeviceLogin spins up the real api routes on an httptest.Server, wires
// in a MemorySender, creates the user, and runs the real syncclient PKCE flow
// up to (but not including) the final poll. It returns the live HTTP server,
// the mem sender, the sync client, the generated PKCE pair, and the device_code.
func startCLIDeviceLogin(t *testing.T, userEmail string) (*httptest.Server, *email.MemorySender, *syncclient.Client, *syncclient.PKCE, string) {
	t.Helper()

	srv, store := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	if _, err := store.CreateUser(userEmail); err != nil {
		t.Fatalf("create user: %v", err)
	}

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := syncclient.New(ts.URL, "", "")

	// Real CLI step: generate the local PKCE pair.
	pkce, err := syncclient.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}

	// Real CLI step: DeviceStart with only the S256 challenge.
	startResp, err := client.DeviceStart(userEmail, pkce.Challenge, pkce.Method, "td-cli@test")
	if err != nil {
		t.Fatalf("DeviceStart: %v", err)
	}
	if startResp.DeviceCode == "" {
		t.Fatal("DeviceStart returned empty device_code")
	}
	if !startResp.EmailSent {
		t.Fatal("DeviceStart returned email_sent=false")
	}

	return ts, ms, client, pkce, startResp.DeviceCode
}

// clickApprovalLink performs the GET the user's browser would do when they click
// the emailed approval link.
func clickApprovalLink(t *testing.T, baseURL, token string) {
	t.Helper()
	resp, err := http.Get(baseURL + "/auth/device/approve?token=" + token)
	if err != nil {
		t.Fatalf("approve GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve GET: status %d", resp.StatusCode)
	}
}

// TestCLIDeviceLogin_FullFlow drives the entire CLI login through the real
// syncclient against a live in-process server: GeneratePKCE -> DeviceStart ->
// read magic link from MemorySender -> approve -> DevicePoll -> 365-day key.
// It also proves the issued key authenticates and that auth.json roundtrips
// through syncconfig in the unchanged format.
func TestCLIDeviceLogin_FullFlow(t *testing.T) {
	const userEmail = "cli-flow@example.com"
	ts, ms, client, pkce, deviceCode := startCLIDeviceLogin(t, userEmail)

	// Before the link is clicked the poll must stay pending — no key.
	pending, err := client.DevicePoll(deviceCode, pkce.Verifier)
	if err != nil {
		t.Fatalf("DevicePoll (pre-approval): %v", err)
	}
	if pending.Status != "pending" {
		t.Fatalf("pre-approval status: got %q, want pending", pending.Status)
	}
	if pending.APIKey != nil {
		t.Fatal("pre-approval poll returned an api_key — login completed without email click")
	}

	// Read the magic link straight from the in-process sender (stands in for
	// the user opening their inbox) and click it.
	sent := ms.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sent))
	}
	if sent[0].Purpose != "device_login" {
		t.Errorf("email purpose: got %q, want device_login", sent[0].Purpose)
	}
	token := approvalTokenFromEmail(t, sent[0].Text)
	clickApprovalLink(t, ts.URL, token)

	// Now the poll completes with a key.
	complete, err := client.DevicePoll(deviceCode, pkce.Verifier)
	if err != nil {
		t.Fatalf("DevicePoll (post-approval): %v", err)
	}
	if complete.Status != "complete" {
		t.Fatalf("post-approval status: got %q, want complete", complete.Status)
	}
	if complete.APIKey == nil || *complete.APIKey == "" {
		t.Fatal("post-approval poll returned no api_key")
	}
	if complete.Email == nil || *complete.Email != userEmail {
		t.Fatalf("post-approval email: got %v, want %q", complete.Email, userEmail)
	}

	// Key must be a ~365-day key.
	if complete.ExpiresAt == nil {
		t.Fatal("post-approval poll returned no expires_at")
	}
	exp, err := time.Parse(time.RFC3339, *complete.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", *complete.ExpiresAt, err)
	}
	days := time.Until(exp).Hours() / 24
	if days < 364 || days > 366 {
		t.Errorf("key lifetime: got ~%.0f days, want ~365", days)
	}

	// Save via syncconfig with a temp HOME and reload — proves the auth.json
	// format the CLI writes still roundtrips unchanged.
	t.Setenv("HOME", t.TempDir())
	creds := &syncconfig.AuthCredentials{
		ServerURL: ts.URL,
		Email:     userEmail,
		DeviceID:  "test-device-id",
	}
	creds.APIKey = *complete.APIKey
	if complete.UserID != nil {
		creds.UserID = *complete.UserID
	}
	creds.ExpiresAt = *complete.ExpiresAt
	if err := syncconfig.SaveAuth(creds); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}
	loaded, err := syncconfig.LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadAuth returned nil after save")
	}
	if loaded.APIKey != creds.APIKey || loaded.Email != userEmail || loaded.UserID != creds.UserID {
		t.Errorf("auth.json roundtrip mismatch: got %+v", loaded)
	}

	// The issued key must authenticate a real request (e.g. list projects).
	authedClient := syncclient.New(ts.URL, *complete.APIKey, "test-device-id")
	if _, err := authedClient.ListProjects(); err != nil {
		t.Errorf("issued key failed to authenticate ListProjects: %v", err)
	}
}

// TestCLIDeviceLogin_WrongVerifier proves a poller that does NOT hold the
// matching code_verifier cannot complete the login even after the email link is
// clicked. This is the PKCE guarantee: a different process that observes the
// device_code but lacks the verifier gets nothing.
func TestCLIDeviceLogin_WrongVerifier(t *testing.T) {
	const userEmail = "cli-wrong@example.com"
	ts, ms, client, _, deviceCode := startCLIDeviceLogin(t, userEmail)

	// Approve via the emailed link.
	sent := ms.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 email, got %d", len(sent))
	}
	token := approvalTokenFromEmail(t, sent[0].Text)
	clickApprovalLink(t, ts.URL, token)

	// Poll with a DIFFERENT verifier than the one whose challenge was sent.
	attacker, err := syncclient.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE (attacker): %v", err)
	}
	_, err = client.DevicePoll(deviceCode, attacker.Verifier)
	if err == nil {
		t.Fatal("DevicePoll with wrong verifier succeeded — PKCE not enforced")
	}
	if !strings.Contains(err.Error(), "invalid_verifier") {
		t.Errorf("wrong-verifier error: got %v, want invalid_verifier", err)
	}
}

// TestCLIDeviceLogin_NoEmailClick proves the login cannot complete if the email
// link is never clicked: every poll stays pending and no key is ever issued.
func TestCLIDeviceLogin_NoEmailClick(t *testing.T) {
	const userEmail = "cli-noemail@example.com"
	_, _, client, pkce, deviceCode := startCLIDeviceLogin(t, userEmail)

	for i := 0; i < 3; i++ {
		poll, err := client.DevicePoll(deviceCode, pkce.Verifier)
		if err != nil {
			t.Fatalf("DevicePoll: %v", err)
		}
		if poll.Status != "pending" {
			t.Fatalf("poll %d without email click: got status %q, want pending", i, poll.Status)
		}
		if poll.APIKey != nil {
			t.Fatalf("poll %d without email click returned an api_key — no-email login completed", i)
		}
	}
}

// TestCLIDeviceLogin_UnknownEmailNoKey proves the non-enumerating path: an
// unknown email yields a normal-looking DeviceStart response but the poll never
// completes (no challenge was created, no email sent).
func TestCLIDeviceLogin_UnknownEmailNoKey(t *testing.T) {
	srv, _ := newTestServer(t)
	ms := email.NewMemorySender()
	srv.emailSender = ms
	srv.config.AuthEmailBaseURL = "https://sync.example.com"

	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)

	client := syncclient.New(ts.URL, "", "")
	pkce, err := syncclient.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}

	startResp, err := client.DeviceStart("nobody@example.com", pkce.Challenge, pkce.Method, "td-cli@test")
	if err != nil {
		t.Fatalf("DeviceStart: %v", err)
	}
	if !startResp.EmailSent || startResp.DeviceCode == "" {
		t.Fatal("unknown-email DeviceStart should look identical to known (non-enumeration)")
	}
	if len(ms.Sent()) != 0 {
		t.Fatalf("expected 0 emails for unknown user, got %d", len(ms.Sent()))
	}

	poll, err := client.DevicePoll(startResp.DeviceCode, pkce.Verifier)
	if err != nil {
		t.Fatalf("DevicePoll: %v", err)
	}
	if poll.Status != "pending" || poll.APIKey != nil {
		t.Fatalf("unknown-email poll: got status %q apikey=%v, want pending/no-key", poll.Status, poll.APIKey)
	}
}
