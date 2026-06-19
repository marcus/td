package email

import (
	"context"
	"sync"
)

// MemorySender stores sent emails in memory. Intended for tests and local dev.
type MemorySender struct {
	mu   sync.Mutex
	sent []LoginEmail
}

// NewMemorySender returns an empty MemorySender.
func NewMemorySender() *MemorySender {
	return &MemorySender{}
}

// SendLoginLink appends msg to the in-memory store.
func (s *MemorySender) SendLoginLink(_ context.Context, msg LoginEmail) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, msg)
	return nil
}

// Sent returns a copy of all emails that have been sent so far.
func (s *MemorySender) Sent() []LoginEmail {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LoginEmail, len(s.sent))
	copy(out, s.sent)
	return out
}

// Last returns the most recently sent email and true, or a zero LoginEmail and
// false when nothing has been sent yet. It returns a copy, so the caller cannot
// mutate the buffered message.
func (s *MemorySender) Last() (LoginEmail, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return LoginEmail{}, false
	}
	return s.sent[len(s.sent)-1], true
}
