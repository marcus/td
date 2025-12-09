package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sessionFile   = ".todos/session"
	sessionPrefix = "ses_"
)

// Session represents the current terminal session
type Session struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	ContextID         string    `json:"context_id"`
	PreviousSessionID string    `json:"previous_session_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	IsNew             bool      `json:"-"` // True if session was just created (not persisted)
}

// generateID creates a new random session ID
func generateID() (string, error) {
	bytes := make([]byte, 3) // 6 hex characters
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return sessionPrefix + hex.EncodeToString(bytes), nil
}

// getContextID generates a unique identifier for the current execution context.
// This detects when a new terminal/AI session has started.
func getContextID() string {
	// Priority 1: Explicit AI agent session IDs
	// Set TD_SESSION_ID to force a specific session context (most reliable)
	if val := os.Getenv("TD_SESSION_ID"); val != "" {
		return "explicit:" + val
	}

	// Priority 2: AI agent session IDs (Claude Code, etc.)
	for _, envVar := range []string{
		"CLAUDE_CODE_SSE_PORT", // Claude Code SSE port (unique per session)
		"CLAUDE_SESSION_ID",    // Claude Code session (if set)
		"ANTHROPIC_SESSION_ID", // Generic Anthropic
		"AI_SESSION_ID",        // Generic AI
		"CURSOR_SESSION_ID",    // Cursor IDE
		"COPILOT_SESSION_ID",   // GitHub Copilot
	} {
		if val := os.Getenv(envVar); val != "" {
			return "ai:" + val
		}
	}

	// Priority 3: Terminal session IDs (stable across command runs)
	for _, envVar := range []string{
		"TERM_SESSION_ID", // iTerm2
		"WINDOWID",        // X11 window ID
		"TMUX_PANE",       // tmux pane
		"STY",             // screen session
		"KONSOLE_DBUS_SESSION", // KDE Konsole
		"GNOME_TERMINAL_SCREEN", // GNOME Terminal
	} {
		if val := os.Getenv(envVar); val != "" {
			return "term:" + envVar + "=" + val
		}
	}

	// Priority 4: Shell PID (stable within a shell session)
	// SHLVL stays constant within same shell, $$ is the shell's PID
	if shlvl := os.Getenv("SHLVL"); shlvl != "" {
		// Use a combination that's stable within a shell session
		// but changes when a new shell starts
		return "shell:shlvl=" + shlvl
	}

	// Fallback: Use a stable default that doesn't rotate on every command
	// This means manual rotation via `td session --new` or env var is required
	return "default"
}

// GetOrCreate returns the current session, creating a new one if:
// 1. No session file exists
// 2. The context has changed (new terminal/AI session)
func GetOrCreate(baseDir string) (*Session, error) {
	sessionPath := filepath.Join(baseDir, sessionFile)
	currentContextID := getContextID()

	// Check if session file exists
	data, err := os.ReadFile(sessionPath)
	if err == nil {
		// Parse existing session
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) >= 3 {
			storedContextID := strings.TrimSpace(lines[2])

			// If context matches, reuse existing session
			if storedContextID == currentContextID {
				sess := &Session{
					ID:        strings.TrimSpace(lines[0]),
					ContextID: storedContextID,
					IsNew:     false,
				}
				if t, err := time.Parse(time.RFC3339, strings.TrimSpace(lines[1])); err == nil {
					sess.StartedAt = t
				}
				if len(lines) >= 4 {
					sess.Name = strings.TrimSpace(lines[3])
				}
				if len(lines) >= 5 {
					sess.PreviousSessionID = strings.TrimSpace(lines[4])
				}
				return sess, nil
			}

			// Context changed - create new session, track previous
			previousID := strings.TrimSpace(lines[0])
			return createNewSession(baseDir, sessionPath, currentContextID, previousID)
		}
	}

	// No valid session file - create fresh
	return createNewSession(baseDir, sessionPath, currentContextID, "")
}

// createNewSession generates and saves a new session
func createNewSession(baseDir, sessionPath, contextID, previousID string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	sess := &Session{
		ID:                id,
		ContextID:         contextID,
		PreviousSessionID: previousID,
		StartedAt:         time.Now(),
		IsNew:             true,
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	// Write session file
	if err := Save(baseDir, sess); err != nil {
		return nil, err
	}

	return sess, nil
}

// Save writes the session to disk
// Format: ID\nStartedAt\nContextID\nName\nPreviousSessionID
func Save(baseDir string, sess *Session) error {
	sessionPath := filepath.Join(baseDir, sessionFile)

	content := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n",
		sess.ID,
		sess.StartedAt.Format(time.RFC3339),
		sess.ContextID,
		sess.Name,
		sess.PreviousSessionID,
	)
	if err := os.WriteFile(sessionPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	return nil
}

// SetName sets the session name
func SetName(baseDir string, name string) (*Session, error) {
	sess, err := GetOrCreate(baseDir)
	if err != nil {
		return nil, err
	}

	sess.Name = name
	if err := Save(baseDir, sess); err != nil {
		return nil, err
	}

	return sess, nil
}

// Get returns the current session without creating one
func Get(baseDir string) (*Session, error) {
	sessionPath := filepath.Join(baseDir, sessionFile)

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("session not found: run 'td init' first")
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid session file")
	}

	sess := &Session{
		ID: strings.TrimSpace(lines[0]),
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(lines[1])); err == nil {
		sess.StartedAt = t
	}
	if len(lines) >= 3 {
		sess.ContextID = strings.TrimSpace(lines[2])
	}
	if len(lines) >= 4 {
		sess.Name = strings.TrimSpace(lines[3])
	}
	if len(lines) >= 5 {
		sess.PreviousSessionID = strings.TrimSpace(lines[4])
	}

	return sess, nil
}

// GetWithContextCheck returns the current session and checks if context changed.
// If context changed, creates a new session automatically.
func GetWithContextCheck(baseDir string) (*Session, error) {
	return GetOrCreate(baseDir)
}

// ForceNewSession creates a new session regardless of context
func ForceNewSession(baseDir string) (*Session, error) {
	sessionPath := filepath.Join(baseDir, sessionFile)
	currentContextID := getContextID()

	// Get previous session ID if exists
	var previousID string
	if existing, err := Get(baseDir); err == nil {
		previousID = existing.ID
	}

	return createNewSession(baseDir, sessionPath, currentContextID, previousID)
}

// ParseDuration parses human-readable duration strings
func ParseDuration(s string) (time.Duration, error) {
	// Try standard duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle day format
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid duration: %s", s)
}
