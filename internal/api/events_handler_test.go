package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/serverdb"
)

// newTestServerWithPing creates a test server with a custom ping interval
// (injected via the sseHubs / pingInterval fields) and returns the server
// together with its in-process httptest.Server.
func newSSETestServer(t *testing.T, ping time.Duration) (*Server, *serverdb.ServerDB, *httptest.Server) {
	t.Helper()
	srv, store := newTestServer(t)
	srv.pingInterval = ping
	httpSrv := httptest.NewServer(srv.routes())
	t.Cleanup(func() { httpSrv.Close() })
	return srv, store, httpSrv
}

// readSSELines reads lines from an SSE response body until it finds one
// matching the predicate or the deadline fires.  Returns the matched line
// and true on success.
func readSSELine(t *testing.T, resp *http.Response, deadline time.Duration, match func(string) bool) (string, bool) {
	t.Helper()
	done := time.After(deadline)
	scanner := bufio.NewScanner(resp.Body)
	lineCh := make(chan string, 32)
	go func() {
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()
	for {
		select {
		case <-done:
			return "", false
		case line, open := <-lineCh:
			if !open {
				return "", false
			}
			if match(line) {
				return line, true
			}
		}
	}
}

// --- helper to create a project and owner token ---

func createProjectAndOwner(t *testing.T, store *serverdb.ServerDB) (projectID, ownerToken string) {
	t.Helper()
	// First user auto-becomes admin; create a throw-away to avoid that making
	// the test skip membership checks.
	_, _ = createTestUser(t, store, "first@example.com")
	ownerID, tok := createTestUser(t, store, "owner@example.com")
	p, err := store.CreateProject("myproject", "", ownerID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p.ID, tok
}

// --- Tests ---

// TestSSE_HeadersOn200 checks Content-Type and other SSE headers are present.
func TestSSE_HeadersOn200(t *testing.T) {
	srv, store, httpSrv := newSSETestServer(t, 50*time.Millisecond)
	_ = srv

	pid, tok := createProjectAndOwner(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", httpSrv.URL+"/v1/projects/"+pid+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", xab)
	}
}

// TestSSE_PingReceived verifies a ping comment arrives within the test deadline.
func TestSSE_PingReceived(t *testing.T) {
	srv, store, httpSrv := newSSETestServer(t, 50*time.Millisecond)
	_ = srv

	pid, tok := createProjectAndOwner(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", httpSrv.URL+"/v1/projects/"+pid+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	_, found := readSSELine(t, resp, 2*time.Second, func(line string) bool {
		return line == ": ping"
	})
	if !found {
		t.Fatal("no ping comment received within deadline")
	}
}

// TestSSE_MissingAuth returns 401.
func TestSSE_MissingAuth(t *testing.T) {
	_, _, httpSrv := newSSETestServer(t, 50*time.Millisecond)

	resp, err := http.Get(httpSrv.URL + "/v1/projects/nosuchproject/events")
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestSSE_WrongProject returns 403 for a non-member.
func TestSSE_WrongProject(t *testing.T) {
	_, store, httpSrv := newSSETestServer(t, 50*time.Millisecond)

	pid, _ := createProjectAndOwner(t, store)
	_, strangerTok := createTestUser(t, store, "stranger@example.com")

	req, _ := http.NewRequest("GET", httpSrv.URL+"/v1/projects/"+pid+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+strangerTok)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// TestSSE_LastEventIDTriggersRefresh checks that sending Last-Event-ID causes
// an immediate "refresh" event before any pings.
func TestSSE_LastEventIDTriggersRefresh(t *testing.T) {
	srv, store, httpSrv := newSSETestServer(t, 200*time.Millisecond)
	_ = srv

	pid, tok := createProjectAndOwner(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", httpSrv.URL+"/v1/projects/"+pid+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Last-Event-ID", "stale-token-xyz")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	// The very first non-empty line after headers should be the event type.
	_, found := readSSELine(t, resp, 3*time.Second, func(line string) bool {
		return strings.HasPrefix(line, "event: ") && strings.Contains(line, string(EventRefresh))
	})
	if !found {
		t.Fatal("no refresh event received after Last-Event-ID header")
	}
}

// TestSSE_ContextCancelExitsHandler verifies the handler shuts down within 1s
// when the client cancels its context.
func TestSSE_ContextCancelExitsHandler(t *testing.T) {
	srv, store, httpSrv := newSSETestServer(t, 500*time.Millisecond)
	_ = srv

	pid, tok := createProjectAndOwner(t, store)

	ctx, cancel := context.WithCancel(context.Background())

	req, _ := http.NewRequestWithContext(ctx, "GET", httpSrv.URL+"/v1/projects/"+pid+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	// Ensure connection is up by waiting for first ping.
	done := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == ": ping" {
				close(done)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no ping received before cancel test")
	}

	// Cancel the client context. The server-side handler should exit cleanly.
	cancelStart := time.Now()
	cancel()

	// Give the server side 1s to observe the disconnect.
	// We verify indirectly: the hub should have 0 clients soon after cancel.
	hub := srv.sseHubs.GetOrCreate(pid)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("handler did not unregister client within 1s of context cancel (elapsed: %s)", time.Since(cancelStart))
}
