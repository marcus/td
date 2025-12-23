package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

// Display returns the session ID with name if set: "ses_abc123 (my-name)" or just "ses_abc123"
func (s *Session) Display() string {
	if s.Name != "" {
		return fmt.Sprintf("%s (%s)", s.ID, s.Name)
	}
	return s.ID
}

// FormatSessionID formats a session ID with optional name lookup.
// Use this when you only have a session ID string and need to display it.
// If the session has a name, returns "ses_xxx (name)", otherwise just "ses_xxx".
func FormatSessionID(baseDir, sessionID string) string {
	// Try to look up the session to get its name
	sess, err := Get(baseDir)
	if err == nil && sess.ID == sessionID && sess.Name != "" {
		return fmt.Sprintf("%s (%s)", sessionID, sess.Name)
	}
	return sessionID
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
		"TERM_SESSION_ID",       // iTerm2
		"WINDOWID",              // X11 window ID
		"TMUX_PANE",             // tmux pane
		"STY",                   // screen session
		"KONSOLE_DBUS_SESSION",  // KDE Konsole
		"GNOME_TERMINAL_SCREEN", // GNOME Terminal
		"SSH_TTY",               // stable-ish per SSH terminal
	} {
		if val := os.Getenv(envVar); val != "" {
			return "term:" + envVar + "=" + val
		}
	}

	// Priority 4: Best-effort process + tty fingerprint.
	// os.Getppid() should be stable across commands in the same shell, and differ across terminals.
	ppid := os.Getppid()

	// Prefer a tty path if available. This helps disambiguate scenarios where ppid alone is too coarse.
	tty := ""
	if link, err := os.Readlink("/dev/fd/0"); err == nil {
		tty = link
	}

	if tty != "" {
		return fmt.Sprintf("proc:ppid=%d tty=%s", ppid, tty)
	}
	if shlvl := os.Getenv("SHLVL"); shlvl != "" {
		return fmt.Sprintf("proc:ppid=%d shlvl=%s", ppid, shlvl)
	}
	return fmt.Sprintf("proc:ppid=%d", ppid)
}

// GetOrCreate returns the current session, creating a new one if:
// 1. No session file exists
// 2. The context has changed (new terminal/AI session)
func GetOrCreate(baseDir string) (*Session, error) {
	sessionPath := filepath.Join(baseDir, sessionFile)
	currentContextID := getContextID()

	// Ensure project is initialized. Avoid creating .todos/ as a side effect.
	todosDir := filepath.Join(baseDir, ".todos")
	if _, err := os.Stat(todosDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: run 'td init' first")
		}
		return nil, fmt.Errorf("stat %s: %w", todosDir, err)
	}

	// Check if session file exists
	data, err := os.ReadFile(sessionPath)
	if err == nil {
		var sess Session

		// Try JSON format first
		if err := json.Unmarshal(data, &sess); err == nil {
			// If context matches, reuse existing session
			if sess.ContextID == currentContextID {
				sess.IsNew = false
				return &sess, nil
			}
			// Context changed - create new session, track previous
			return createNewSession(baseDir, sessionPath, currentContextID, sess.ID)
		}

		// Fallback: legacy line-based format for backward compatibility
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
				// Migrate to JSON format on next save
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

// Save writes the session to disk as JSON
func Save(baseDir string, sess *Session) error {
	sessionPath := filepath.Join(baseDir, sessionFile)

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
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

	// Try JSON format first
	var sess Session
	if err := json.Unmarshal(data, &sess); err == nil {
		return &sess, nil
	}

	// Fallback: legacy line-based format
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid session file")
	}

	sess = Session{
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

	return &sess, nil
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

	// Ensure project is initialized. Avoid creating .todos/ as a side effect.
	todosDir := filepath.Join(baseDir, ".todos")
	if _, err := os.Stat(todosDir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: run 'td init' first")
		}
		return nil, fmt.Errorf("stat %s: %w", todosDir, err)
	}

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
