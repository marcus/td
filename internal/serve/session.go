package serve

import (
	"context"
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
)

const (
	webAgentType       = "web"
	webAgentPID        = 0
	webBranch          = "default"
	webSessionName     = "td-serve-web"
	heartbeatInterval  = 60 * time.Second
)

// GetOrCreateWebSession finds or creates the shared web session used by
// the td serve HTTP server. The session is identified by:
//   - agent_type = "web"
//   - agent_pid  = 0
//   - branch     = "default"
//
// If a matching session exists, its activity timestamp is bumped.
// If none exists, a new session named "td-serve-web" is created.
func GetOrCreateWebSession(database *db.DB) (*db.SessionRow, error) {
	row, err := database.GetSessionByBranchAgent(webBranch, webAgentType, webAgentPID)
	if err != nil {
		return nil, fmt.Errorf("lookup web session: %w", err)
	}

	if row != nil {
		// Found existing session - bump activity
		now := time.Now()
		if err := database.UpdateSessionActivity(row.ID, now); err != nil {
			return nil, fmt.Errorf("bump web session activity: %w", err)
		}
		row.LastActivity = now
		return row, nil
	}

	// Create new web session
	id, err := GenerateInstanceID() // reuse the random ID generator
	if err != nil {
		return nil, fmt.Errorf("generate web session id: %w", err)
	}
	// Replace srv_ prefix with ses_ for session IDs
	id = "ses_" + id[len(instancePrefix):]

	now := time.Now()
	row = &db.SessionRow{
		ID:           id,
		Name:         webSessionName,
		Branch:       webBranch,
		AgentType:    webAgentType,
		AgentPID:     webAgentPID,
		StartedAt:    now,
		LastActivity: now,
	}

	if err := database.UpsertSession(row); err != nil {
		return nil, fmt.Errorf("create web session: %w", err)
	}

	return row, nil
}

// BumpSessionActivity updates the last_activity timestamp for a session.
func BumpSessionActivity(database *db.DB, sessionID string) error {
	return database.UpdateSessionActivity(sessionID, time.Now())
}

// StartSessionHeartbeat launches a goroutine that periodically bumps the
// session's last_activity timestamp. The goroutine stops when the provided
// context is cancelled. Errors during bumps are silently ignored since
// heartbeats are best-effort.
func StartSessionHeartbeat(ctx context.Context, database *db.DB, sessionID string) {
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Best-effort: ignore errors from heartbeat bumps
				_ = database.UpdateSessionActivity(sessionID, time.Now())
			}
		}
	}()
}
