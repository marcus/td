package api

import (
	"net/http"

	"github.com/marcus/td/internal/email"
)

// devLastEmailResponse is the JSON shape returned by GET /internal/dev/last-email.
// It deliberately exposes the full email body (including the plaintext magic link)
// so local/dev/test automation can complete a login without a real mailbox.
type devLastEmailResponse struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
	Purpose string `json:"purpose"`
	TraceID string `json:"trace_id"`
}

// handleDevLastEmail returns the most recently sent login email from the
// in-memory email provider.
//
// SECURITY: this endpoint exposes a magic link in plaintext — that is its sole
// purpose. It is hard-gated by TWO independent conditions, BOTH of which must
// hold or it returns 404:
//
//  1. config.DevEmailInspect must be true (env SYNC_DEV_EMAIL_INSPECT=true|1).
//  2. The configured email provider must be *email.MemorySender.
//
// Production uses the cloudflare provider (never the memory provider), so the
// type-assertion in (2) fails in prod regardless of how (1) is set. This makes
// the endpoint impossible to enable accidentally in production. Never register
// this route without the gate, and never relax the gate.
func (s *Server) handleDevLastEmail(w http.ResponseWriter, r *http.Request) {
	// Gate 1: explicit dev flag.
	if !s.config.DevEmailInspect {
		http.NotFound(w, r)
		return
	}

	// Gate 2: in-memory provider only. Production never wires this provider, so
	// the assertion fails there even if the flag were somehow set.
	mem, ok := s.emailSender.(*email.MemorySender)
	if !ok {
		http.NotFound(w, r)
		return
	}

	last, ok := mem.Last()
	if !ok {
		// No email has been sent yet — treat as not found rather than leaking
		// an empty body, so callers can distinguish "nothing sent".
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, devLastEmailResponse{
		To:      last.To,
		Subject: last.Subject,
		Text:    last.Text,
		HTML:    last.HTML,
		Purpose: last.Purpose,
		TraceID: last.TraceID,
	})
}
