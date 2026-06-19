package syncclient

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// TestGeneratePKCE_S256Relationship asserts the generated challenge is exactly
// what the server recomputes: base64.RawURLEncoding(SHA-256(verifier)).
func TestGeneratePKCE_S256Relationship(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}

	if p.Method != "S256" {
		t.Errorf("Method: got %q, want S256", p.Method)
	}
	if p.Verifier == "" {
		t.Fatal("expected non-empty verifier")
	}
	if p.Challenge == "" {
		t.Fatal("expected non-empty challenge")
	}

	// Recompute the way handleDevicePoll does in internal/api/auth.go.
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge mismatch:\n got  %q\n want %q", p.Challenge, want)
	}

	// base64url no-pad: must not contain '+', '/', or '='.
	for _, c := range p.Verifier + p.Challenge {
		if c == '+' || c == '/' || c == '=' {
			t.Errorf("unexpected base64url-unsafe char %q", c)
		}
	}
}

// TestGeneratePKCE_Unique ensures successive calls produce distinct verifiers
// (i.e. randomness is actually being used).
func TestGeneratePKCE_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		p, err := GeneratePKCE()
		if err != nil {
			t.Fatalf("GeneratePKCE: %v", err)
		}
		if _, dup := seen[p.Verifier]; dup {
			t.Fatalf("duplicate verifier generated: %q", p.Verifier)
		}
		seen[p.Verifier] = struct{}{}
	}
}

// TestPKCEVerifierLength checks the verifier falls inside RFC 7636's 43..128
// character window.
func TestPKCEVerifierLength(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if n := len(p.Verifier); n < 43 || n > 128 {
		t.Errorf("verifier length %d outside RFC 7636 range 43..128", n)
	}
}
