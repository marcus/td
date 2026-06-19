package syncclient

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// PKCEChallengeMethod is the only PKCE method td-sync supports.
const PKCEChallengeMethod = "S256"

// pkceVerifierBytes is the number of random bytes used to build the verifier.
// 32 bytes -> 43-char base64url string, comfortably within RFC 7636's 43..128
// length window for code verifiers.
const pkceVerifierBytes = 32

// PKCE holds a generated PKCE verifier/challenge pair for the device-login flow.
// Verifier is kept locally and only sent to the server at poll time; Challenge
// (the S256 hash) is the only part disclosed in DeviceStart.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE creates a fresh PKCE pair using crypto/rand. The relationship is
// Challenge = base64.RawURLEncoding(SHA-256(Verifier)), which is exactly what
// the td-sync device/poll handler recomputes and compares against the stored
// code_challenge.
func GeneratePKCE() (*PKCE, error) {
	b := make([]byte, pkceVerifierBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	return &PKCE{
		Verifier:  verifier,
		Challenge: deriveChallenge(verifier),
		Method:    PKCEChallengeMethod,
	}, nil
}

// deriveChallenge computes the S256 challenge for a verifier.
func deriveChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
