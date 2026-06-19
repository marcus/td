package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcus/td/internal/email"
)

// devEmailServer builds a test server with the given DevEmailInspect flag and
// email provider, returning a live httptest server and (when memory) the sender.
func devEmailServer(t *testing.T, devInspect bool, provider string) (*httptest.Server, *email.MemorySender) {
	t.Helper()
	srv, _ := newTestServerWithConfig(t, func(c *Config) {
		c.DevEmailInspect = devInspect
		c.EmailProvider = provider
	})
	var ms *email.MemorySender
	if m, ok := srv.emailSender.(*email.MemorySender); ok {
		ms = m
	}
	ts := httptest.NewServer(srv.routes())
	t.Cleanup(ts.Close)
	return ts, ms
}

func getLastEmail(t *testing.T, baseURL string) *http.Response {
	t.Helper()
	resp, err := http.Get(baseURL + "/internal/dev/last-email")
	if err != nil {
		t.Fatalf("GET last-email: %v", err)
	}
	return resp
}

// TestDevLastEmail_FlagOff404 proves the endpoint is 404 when the flag is off,
// even though the memory provider is active.
func TestDevLastEmail_FlagOff404(t *testing.T) {
	ts, ms := devEmailServer(t, false, "memory")
	if ms == nil {
		t.Fatal("expected memory sender")
	}
	// Even with a sent email present, flag-off must 404.
	_ = ms.SendLoginLink(context.Background(), email.LoginEmail{To: "a@b.c", Text: "link"})

	resp := getLastEmail(t, ts.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("flag off: got status %d, want 404", resp.StatusCode)
	}
}

// TestDevLastEmail_NonMemoryProvider404 proves that with the flag ON but a
// non-memory provider (log), the endpoint is 404. This is the prod-shaped case
// (cloudflare behaves identically to log here: not *MemorySender).
func TestDevLastEmail_NonMemoryProvider404(t *testing.T) {
	ts, ms := devEmailServer(t, true, "log")
	if ms != nil {
		t.Fatal("log provider should not be a MemorySender")
	}
	resp := getLastEmail(t, ts.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-memory provider: got status %d, want 404", resp.StatusCode)
	}
}

// TestDevLastEmail_NoEmailsYet404 proves a 404 when both gates pass but nothing
// has been sent.
func TestDevLastEmail_NoEmailsYet404(t *testing.T) {
	ts, ms := devEmailServer(t, true, "memory")
	if ms == nil {
		t.Fatal("expected memory sender")
	}
	resp := getLastEmail(t, ts.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("no emails: got status %d, want 404", resp.StatusCode)
	}
}

// TestDevLastEmail_ReturnsLastEmail proves a 200 with the most recently sent
// email body when both gates pass.
func TestDevLastEmail_ReturnsLastEmail(t *testing.T) {
	ts, ms := devEmailServer(t, true, "memory")
	if ms == nil {
		t.Fatal("expected memory sender")
	}

	_ = ms.SendLoginLink(context.Background(), email.LoginEmail{
		To: "first@example.com", Subject: "First", Text: "link-1", Purpose: "web_login", TraceID: "t1",
	})
	_ = ms.SendLoginLink(context.Background(), email.LoginEmail{
		To: "second@example.com", Subject: "Second", Text: "https://sync/auth/device/approve?token=sel.secret",
		HTML: "<a>x</a>", Purpose: "device_login", TraceID: "t2",
	})

	resp := getLastEmail(t, ts.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}

	var got devLastEmailResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.To != "second@example.com" {
		t.Errorf("To: got %q, want second@example.com (most recent)", got.To)
	}
	if got.Subject != "Second" {
		t.Errorf("Subject: got %q, want Second", got.Subject)
	}
	if got.Text != "https://sync/auth/device/approve?token=sel.secret" {
		t.Errorf("Text: got %q", got.Text)
	}
	if got.HTML != "<a>x</a>" {
		t.Errorf("HTML: got %q", got.HTML)
	}
	if got.Purpose != "device_login" {
		t.Errorf("Purpose: got %q, want device_login", got.Purpose)
	}
	if got.TraceID != "t2" {
		t.Errorf("TraceID: got %q, want t2", got.TraceID)
	}
}

// TestDevLastEmail_ReflectsMostRecent proves the endpoint tracks subsequent
// sends (the "last" semantics).
func TestDevLastEmail_ReflectsMostRecent(t *testing.T) {
	ts, ms := devEmailServer(t, true, "memory")
	if ms == nil {
		t.Fatal("expected memory sender")
	}

	_ = ms.SendLoginLink(context.Background(), email.LoginEmail{To: "one@x.com", Subject: "One"})
	resp1 := getLastEmail(t, ts.URL)
	var got1 devLastEmailResponse
	_ = json.NewDecoder(resp1.Body).Decode(&got1)
	resp1.Body.Close()
	if got1.To != "one@x.com" {
		t.Fatalf("first read: got %q", got1.To)
	}

	_ = ms.SendLoginLink(context.Background(), email.LoginEmail{To: "two@x.com", Subject: "Two"})
	resp2 := getLastEmail(t, ts.URL)
	var got2 devLastEmailResponse
	_ = json.NewDecoder(resp2.Body).Decode(&got2)
	resp2.Body.Close()
	if got2.To != "two@x.com" {
		t.Fatalf("second read: got %q, want two@x.com", got2.To)
	}
}
