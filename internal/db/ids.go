package db

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	idPrefix         = "td-"
	wsIDPrefix       = "ws-"
	boardIDPrefix    = "bd-"
	logIDPrefix      = "lg-"
	handoffIDPrefix  = "ho-"
	commentIDPrefix  = "cm-"
	snapshotIDPrefix = "gs-"
	noteIDPrefix     = "nt-"
	actionIDPrefix = "al-"

	// Deterministic ID prefixes for composite-key tables
	boardIssuePosIDPrefix = "bip_"
	dependencyIDPrefix    = "dep_"
	issueFileIDPrefix     = "ifl_"
	wsiIDPrefix           = "wsi_"
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

// generateLogID generates a unique log entry ID
func generateLogID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return logIDPrefix + hex.EncodeToString(bytes), nil
}

// generateHandoffID generates a unique handoff ID
func generateHandoffID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return handoffIDPrefix + hex.EncodeToString(bytes), nil
}

// generateCommentID generates a unique comment ID
func generateCommentID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return commentIDPrefix + hex.EncodeToString(bytes), nil
}

// generateSnapshotID generates a unique goal snapshot ID
func generateSnapshotID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return snapshotIDPrefix + hex.EncodeToString(bytes), nil
}

// generateNoteID generates a unique note ID
func generateNoteID() (string, error) {
	bytes := make([]byte, 3) // 6 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return noteIDPrefix + hex.EncodeToString(bytes), nil
}

// generateActionID generates a unique action log ID
func generateActionID() (string, error) {
	bytes := make([]byte, 4) // 8 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return actionIDPrefix + hex.EncodeToString(bytes), nil
}

// deterministicID computes prefix + sha256(input)[:16] for sync-stable IDs.
func deterministicID(prefix, input string) string {
	h := sha256.Sum256([]byte(input))
	return prefix + hex.EncodeToString(h[:])[:16]
}

// BoardIssuePosID returns a deterministic ID for a board_issue_positions row.
func BoardIssuePosID(boardID, issueID string) string {
	return deterministicID(boardIssuePosIDPrefix, boardID+"|"+issueID)
}

// DependencyID returns a deterministic ID for an issue_dependencies row.
func DependencyID(issueID, dependsOnID, relationType string) string {
	return deterministicID(dependencyIDPrefix, issueID+"|"+dependsOnID+"|"+relationType)
}

// IssueFileID returns a deterministic ID for an issue_files row.
// The file path is normalized to forward slashes before hashing
// so the same ID is generated regardless of OS path separators.
func IssueFileID(issueID, filePath string) string {
	return deterministicID(issueFileIDPrefix, issueID+"|"+NormalizeFilePathForID(filePath))
}

// WsiID returns a deterministic ID for a work_session_issues row.
func WsiID(workSessionID, issueID string) string {
	return deterministicID(wsiIDPrefix, workSessionID+"|"+issueID)
}
