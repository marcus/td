package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/models"
)

func TestBuildPayload(t *testing.T) {
	actions := []models.ActionLog{
		{
			ID:         "al-001",
			SessionID:  "ses_abc",
			ActionType: models.ActionCreate,
			EntityType: "issues",
			EntityID:   "td-123",
			NewData:    `{"title":"test"}`,
			Timestamp:  time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:         "al-002",
			SessionID:  "ses_abc",
			ActionType: models.ActionUpdate,
			EntityType: "issues",
			EntityID:   "td-123",
			NewData:    `{"title":"updated"}`,
			Timestamp:  time.Date(2026, 2, 18, 10, 0, 1, 0, time.UTC),
		},
	}

	p := BuildPayload("/tmp/project", actions)

	if p.ProjectDir != "/tmp/project" {
		t.Errorf("ProjectDir = %q, want /tmp/project", p.ProjectDir)
	}
	if len(p.Actions) != 2 {
		t.Fatalf("len(Actions) = %d, want 2", len(p.Actions))
	}
	if p.Actions[0].ActionType != "create" {
		t.Errorf("Actions[0].ActionType = %q, want create", p.Actions[0].ActionType)
	}
	if p.Actions[1].EntityID != "td-123" {
		t.Errorf("Actions[1].EntityID = %q, want td-123", p.Actions[1].EntityID)
	}
}

func TestTempFileRoundTrip(t *testing.T) {
	tf := &TempFile{
		URL:    "https://example.com/hook",
		Secret: "s3cret",
		Payload: Payload{
			ProjectDir: "/tmp/p",
			Timestamp:  "2026-02-18T10:00:00Z",
			Actions: []ActionPayload{
				{ID: "al-001", ActionType: "create", EntityType: "issues", EntityID: "td-1"},
			},
		},
	}

	path, err := WriteTempFile(tf)
	if err != nil {
		t.Fatalf("WriteTempFile: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasPrefix(path, os.TempDir()) {
		t.Errorf("temp file not in TempDir: %s", path)
	}

	got, err := ReadTempFile(path)
	if err != nil {
		t.Fatalf("ReadTempFile: %v", err)
	}

	if got.URL != tf.URL {
		t.Errorf("URL = %q, want %q", got.URL, tf.URL)
	}
	if got.Secret != tf.Secret {
		t.Errorf("Secret = %q, want %q", got.Secret, tf.Secret)
	}
	if len(got.Payload.Actions) != 1 {
		t.Fatalf("len(Actions) = %d, want 1", len(got.Payload.Actions))
	}
	if got.Payload.Actions[0].ID != "al-001" {
		t.Errorf("Actions[0].ID = %q, want al-001", got.Payload.Actions[0].ID)
	}
}

func TestDispatch_Success(t *testing.T) {
	var gotBody []byte
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	payload := Payload{
		ProjectDir: "/tmp/p",
		Timestamp:  "2026-02-18T10:00:00Z",
		Actions: []ActionPayload{
			{ID: "al-001", ActionType: "test"},
		},
	}

	err := Dispatch(srv.URL, "", payload)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotHeaders.Get("Content-Type"))
	}
	if gotHeaders.Get("X-TD-Timestamp") == "" {
		t.Error("X-TD-Timestamp header missing")
	}
	if gotHeaders.Get("X-TD-Signature") != "" {
		t.Error("X-TD-Signature should be absent without secret")
	}

	var p Payload
	if err := json.Unmarshal(gotBody, &p); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(p.Actions) != 1 {
		t.Errorf("body actions = %d, want 1", len(p.Actions))
	}
}

func TestDispatch_WithSecret(t *testing.T) {
	secret := "test-hmac-key"
	var gotBody []byte
	var gotHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	payload := Payload{
		ProjectDir: "/tmp/p",
		Timestamp:  "2026-02-18T10:00:00Z",
		Actions:    []ActionPayload{{ID: "al-002"}},
	}

	err := Dispatch(srv.URL, secret, payload)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	sig := gotHeaders.Get("X-TD-Signature")
	if sig == "" {
		t.Fatal("X-TD-Signature header missing")
	}
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature prefix wrong: %s", sig)
	}

	ts := gotHeaders.Get("X-TD-Timestamp")

	// Verify HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(gotBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != expected {
		t.Errorf("signature mismatch:\n  got:  %s\n  want: %s", sig, expected)
	}
}

func TestDispatch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	err := Dispatch(srv.URL, "", Payload{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %q, want to contain 'status 500'", err.Error())
	}
}

func TestDispatch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// This should succeed since 200ms < 10s timeout, just verifying it works
	err := Dispatch(srv.URL, "", Payload{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPayload_Empty(t *testing.T) {
	p := BuildPayload("/tmp/empty", nil)
	if p.ProjectDir != "/tmp/empty" {
		t.Errorf("ProjectDir = %q", p.ProjectDir)
	}
	if len(p.Actions) != 0 {
		t.Errorf("len(Actions) = %d, want 0", len(p.Actions))
	}
}
