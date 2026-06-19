package email_test

import (
	"context"
	"testing"

	"github.com/marcus/td/internal/email"
)

func TestNewEmailSender_memory(t *testing.T) {
	s, err := email.NewEmailSender(email.EmailConfig{Provider: "memory"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := s.(*email.MemorySender); !ok {
		t.Fatalf("expected *email.MemorySender, got %T", s)
	}
}

func TestNewEmailSender_log(t *testing.T) {
	for _, provider := range []string{"log", ""} {
		s, err := email.NewEmailSender(email.EmailConfig{Provider: provider})
		if err != nil {
			t.Fatalf("provider %q: unexpected error: %v", provider, err)
		}
		if _, ok := s.(*email.LogSender); !ok {
			t.Fatalf("provider %q: expected *email.LogSender, got %T", provider, s)
		}
	}
}

func TestNewEmailSender_cloudflare(t *testing.T) {
	s, err := email.NewEmailSender(email.EmailConfig{
		Provider:  "cloudflare",
		AccountID: "test-account",
		APIToken:  "test-token",
		From:      "login@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil sender for cloudflare")
	}
}

func TestNewEmailSender_unknown(t *testing.T) {
	_, err := email.NewEmailSender(email.EmailConfig{Provider: "smtp"})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestMemorySender_StoresAndReturns(t *testing.T) {
	s := email.NewMemorySender()
	ctx := context.Background()

	msg := email.LoginEmail{
		To:      "alice@example.com",
		Subject: "Your login link",
		Text:    "Click here: https://example.com/login?token=abc",
		HTML:    "<a href='https://example.com/login?token=abc'>Login</a>",
		Purpose: "web_login",
		TraceID: "trace-001",
	}

	if err := s.SendLoginLink(ctx, msg); err != nil {
		t.Fatalf("SendLoginLink returned error: %v", err)
	}

	got := s.Sent()
	if len(got) != 1 {
		t.Fatalf("expected 1 message in Sent(), got %d", len(got))
	}
	if got[0].To != msg.To {
		t.Errorf("To: got %q, want %q", got[0].To, msg.To)
	}
	if got[0].Purpose != msg.Purpose {
		t.Errorf("Purpose: got %q, want %q", got[0].Purpose, msg.Purpose)
	}
	if got[0].TraceID != msg.TraceID {
		t.Errorf("TraceID: got %q, want %q", got[0].TraceID, msg.TraceID)
	}
	if got[0].HTML != msg.HTML {
		t.Errorf("HTML: got %q, want %q", got[0].HTML, msg.HTML)
	}
}

func TestMemorySender_Sent_returnsCopy(t *testing.T) {
	s := email.NewMemorySender()
	ctx := context.Background()

	_ = s.SendLoginLink(ctx, email.LoginEmail{To: "a@example.com", Purpose: "web_login"})

	first := s.Sent()
	first[0].To = "mutated"

	second := s.Sent()
	if second[0].To == "mutated" {
		t.Error("Sent() returned a slice that shares backing storage with internal state")
	}
}
