package db

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	idPrefix      = "td-"
	wsIDPrefix    = "ws-"
	boardIDPrefix = "bd-"
)

// NormalizeIssueID ensures an issue ID has the td- prefix
// Accepts bare hex IDs like "abc123" and returns "td-abc123"
func NormalizeIssueID(id string) string {
	if id == "" {
		return id
	}
	if !strings.HasPrefix(id, idPrefix) {
		return idPrefix + id
	}
	return id
}

// idGenerator is the function used to generate issue IDs.
// It can be replaced in tests to control ID generation.
var idGenerator = defaultGenerateID

// defaultGenerateID generates a unique issue ID using crypto/rand
func defaultGenerateID() (string, error) {
	bytes := make([]byte, 3) // 6 hex characters - balances brevity with collision resistance
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(bytes), nil
}

// generateID generates a unique issue ID using the configured generator
func generateID() (string, error) {
	return idGenerator()
}

// generateWSID generates a unique work session ID
func generateWSID() (string, error) {
	bytes := make([]byte, 2) // 4 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return wsIDPrefix + hex.EncodeToString(bytes), nil
}

// generateBoardID generates a unique board ID
func generateBoardID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return boardIDPrefix + hex.EncodeToString(bytes), nil
}
