package serverdb

import (
	"database/sql"
	"fmt"
	"time"
)

// SyncCursor tracks a client's sync position in a project.
type SyncCursor struct {
	ProjectID   string
	ClientID    string
	LastEventID int64
	LastSyncAt  *time.Time
}

// UpsertSyncCursor creates or updates a sync cursor for a project/client pair.
func (db *ServerDB) UpsertSyncCursor(projectID, clientID string, lastEventID int64) error {
	now := time.Now().UTC()
	_, err := db.conn.Exec(`
		INSERT INTO sync_cursors (project_id, client_id, last_event_id, last_sync_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, client_id)
		DO UPDATE SET last_event_id = excluded.last_event_id, last_sync_at = excluded.last_sync_at
	`, projectID, clientID, lastEventID, now)
	if err != nil {
		return fmt.Errorf("upsert sync cursor: %w", err)
	}
	return nil
}

// DeleteSyncCursor removes the cursor for a project/client pair. Used by
// tests and diagnostic tooling. Safe to call when no row exists.
func (db *ServerDB) DeleteSyncCursor(projectID, clientID string) error {
	_, err := db.conn.Exec(`DELETE FROM sync_cursors WHERE project_id = ? AND client_id = ?`, projectID, clientID)
	if err != nil {
		return fmt.Errorf("delete sync cursor: %w", err)
	}
	return nil
}

// GetSyncCursor returns the sync cursor for a project/client pair, or nil if not found.
func (db *ServerDB) GetSyncCursor(projectID, clientID string) (*SyncCursor, error) {
	c := &SyncCursor{}
	err := db.conn.QueryRow(
		`SELECT project_id, client_id, last_event_id, last_sync_at FROM sync_cursors WHERE project_id = ? AND client_id = ?`,
		projectID, clientID,
	).Scan(&c.ProjectID, &c.ClientID, &c.LastEventID, &c.LastSyncAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sync cursor: %w", err)
	}
	return c, nil
}
