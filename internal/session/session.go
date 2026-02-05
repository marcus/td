// Package session manages terminal and agent work sessions scoped by git
// branch and agent identity.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
)

const (
	sessionPrefix = "ses_"
	defaultBranch = "default" // used when git not available
)

// Session represents the current terminal session
type Session struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	Branch            string    `json:"branch,omitempty"`            // git branch for session scoping
	AgentType         string    `json:"agent_type,omitempty"`        // agent type (claude-code, cursor, terminal, etc.)
	AgentPID          int       `json:"agent_pid,omitempty"`         // stable parent agent process ID
	ContextID         string    `json:"context_id,omitempty"`        // audit only, not used for matching
	PreviousSessionID string    `json:"previous_session_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	LastActivity      time.Time `json:"last_activity,omitempty"` // heartbeat for session liveness
	IsNew             bool      `json:"-"`                       // True if session was just created (not persisted)
}

// Display returns the session ID with name if set: "ses_abc123 (my-name)" or just "ses_abc123"
func (s *Session) Display() string {
	if s.Name != "" {
		return fmt.Sprintf("%s (%s)", s.ID, s.Name)
	}
	return s.ID
}

// DisplayWithAgent returns session info including agent: "ses_abc123 [claude-code]" or with name
func (s *Session) DisplayWithAgent() string {
	base := s.ID
	if s.Name != "" {
		base = fmt.Sprintf("%s (%s)", s.ID, s.Name)
	}
	if s.AgentType != "" {
		return fmt.Sprintf("%s [%s]", base, s.AgentType)
	}
	return base
}

// FormatSessionID formats a session ID with optional name lookup.
func FormatSessionID(database *db.DB, sessionID string) string {
	row, err := database.GetSessionByID(sessionID)
	if err == nil && row != nil && row.Name != "" {
		return fmt.Sprintf("%s (%s)", sessionID, row.Name)
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

// getCurrentBranch returns the current git branch, or "default" if not in a repo
func getCurrentBranch() string {
	state, err := git.GetState()
	if err != nil {
		return defaultBranch
	}
	branch := state.Branch
	if branch == "" || branch == "HEAD" {
		// Detached HEAD - use short commit SHA
		if len(state.CommitSHA) >= 8 {
			return "detached-" + state.CommitSHA[:8]
		}
		return defaultBranch
	}
	return branch
}

// getContextID generates a unique identifier for the current execution context.
func getContextID() string {
	if val := os.Getenv("TD_SESSION_ID"); val != "" {
		return "explicit:" + val
	}

	for _, envVar := range []string{
		"CLAUDE_CODE_SSE_PORT",
		"CLAUDE_SESSION_ID",
		"ANTHROPIC_SESSION_ID",
		"AI_SESSION_ID",
		"CURSOR_SESSION_ID",
		"COPILOT_SESSION_ID",
	} {
		if val := os.Getenv(envVar); val != "" {
			return "ai:" + val
		}
	}

	for _, envVar := range []string{
		"TERM_SESSION_ID",
		"WINDOWID",
		"TMUX_PANE",
		"STY",
		"KONSOLE_DBUS_SESSION",
		"GNOME_TERMINAL_SCREEN",
		"SSH_TTY",
	} {
		if val := os.Getenv(envVar); val != "" {
			return "term:" + envVar + "=" + val
		}
	}

	ppid := os.Getppid()
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

// sessionFromRow converts a db.SessionRow to a Session
func sessionFromRow(row *db.SessionRow) *Session {
	return &Session{
		ID:                row.ID,
		Name:              row.Name,
		Branch:            row.Branch,
		AgentType:         row.AgentType,
		AgentPID:          row.AgentPID,
		ContextID:         row.ContextID,
		PreviousSessionID: row.PreviousSessionID,
		StartedAt:         row.StartedAt,
		LastActivity:      row.LastActivity,
	}
}

// GetOrCreate returns the current session for the current git branch and agent.
// Sessions are scoped by branch + agent fingerprint - same agent on same branch = same session.
// Creates a new session if none exists for this branch/agent combination.
func GetOrCreate(database *db.DB) (*Session, error) {
	branch := getCurrentBranch()
	fp := GetAgentFingerprint()

	// One-time migration from filesystem (no-op after first run)
	// Migrate from the resolved root dir and also from cwd (for worktrees)
	database.MigrateFileSystemSessions(database.BaseDir())
	if cwd, err := os.Getwd(); err == nil && cwd != database.BaseDir() {
		database.MigrateFileSystemSessions(cwd)
	}

	// Look up existing session for this branch + agent fingerprint
	row, err := database.GetSessionByBranchAgent(branch, fp.String(), fp.PID)
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if row != nil {
		// Found existing session - update heartbeat
		now := time.Now()
		database.UpdateSessionActivity(row.ID, now)
		sess := sessionFromRow(row)
		sess.LastActivity = now
		sess.IsNew = false
		return sess, nil
	}

	// No session found - create new one
	return createSession(database, branch, fp, "")
}

// Get returns the current session without creating one
func Get(database *db.DB) (*Session, error) {
	branch := getCurrentBranch()
	fp := GetAgentFingerprint()

	row, err := database.GetSessionByBranchAgent(branch, fp.String(), fp.PID)
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	if row == nil {
		return nil, fmt.Errorf("session not found: run 'td init' first")
	}
	return sessionFromRow(row), nil
}

// ForceNewSession creates a new session on the current branch/agent, regardless of existing session
func ForceNewSession(database *db.DB) (*Session, error) {
	branch := getCurrentBranch()
	fp := GetAgentFingerprint()

	// Get previous session ID if exists
	var previousID string
	row, err := database.GetSessionByBranchAgent(branch, fp.String(), fp.PID)
	if err == nil && row != nil {
		previousID = row.ID
	}

	return createSession(database, branch, fp, previousID)
}

// SetName sets the session name
func SetName(database *db.DB, name string) (*Session, error) {
	sess, err := GetOrCreate(database)
	if err != nil {
		return nil, err
	}

	if err := database.UpdateSessionName(sess.ID, name); err != nil {
		return nil, err
	}
	sess.Name = name
	return sess, nil
}

// ListSessions returns all sessions
func ListSessions(database *db.DB) ([]Session, error) {
	rows, err := database.ListAllSessions()
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, len(rows))
	for i, row := range rows {
		r := row // avoid loop variable capture
		sessions[i] = *sessionFromRow(&r)
	}
	return sessions, nil
}

// CleanupStaleSessions removes sessions older than maxAge
func CleanupStaleSessions(database *db.DB, maxAge time.Duration) (int, error) {
	before := time.Now().Add(-maxAge)
	count, err := database.DeleteStaleSessions(before)
	return int(count), err
}

// createSession creates a new session in the DB
func createSession(database *db.DB, branch string, fp AgentFingerprint, previousID string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	row := &db.SessionRow{
		ID:                id,
		Name:              "",
		Branch:            branch,
		AgentType:         fp.String(),
		AgentPID:          fp.PID,
		ContextID:         getContextID(),
		PreviousSessionID: previousID,
		StartedAt:         now,
		LastActivity:      now,
	}

	if err := database.UpsertSession(row); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}

	sess := sessionFromRow(row)
	sess.IsNew = true
	return sess, nil
}

// GetWithContextCheck returns the current session and checks if context changed.
func GetWithContextCheck(database *db.DB) (*Session, error) {
	return GetOrCreate(database)
}

// ParseDuration parses human-readable duration strings
func ParseDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid duration: %s", s)
}

// GetCurrentBranch returns the current git branch (exported for display)
func GetCurrentBranch() string {
	return getCurrentBranch()
}
