package db

import (
	"database/sql"
	"time"
)

// SyncConflict represents a row from the sync_conflicts table.
type SyncConflict struct {
	ID            int64
	EntityType    string
	EntityID      string
	ServerSeq     int64
	LocalData     string
	RemoteData    string
	OverwrittenAt time.Time
}

// GetRecentConflicts returns recent sync conflicts, ordered by most recent first.
// If since is non-nil, only conflicts after that time are returned.
func (db *DB) GetRecentConflicts(limit int, since *time.Time) ([]SyncConflict, error) {
	var rows *sql.Rows
	var err error

	if since != nil {
		rows, err = db.conn.Query(`
			SELECT id, entity_type, entity_id, server_seq, COALESCE(local_data,'null'), COALESCE(remote_data,'null'), overwritten_at
			FROM sync_conflicts
			WHERE overwritten_at >= ?
			ORDER BY overwritten_at DESC
			LIMIT ?
		`, since.Format("2006-01-02 15:04:05"), limit)
	} else {
		rows, err = db.conn.Query(`
			SELECT id, entity_type, entity_id, server_seq, COALESCE(local_data,'null'), COALESCE(remote_data,'null'), overwritten_at
			FROM sync_conflicts
			ORDER BY overwritten_at DESC
			LIMIT ?
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conflicts []SyncConflict
	for rows.Next() {
		var c SyncConflict
		var ts string
		if err := rows.Scan(&c.ID, &c.EntityType, &c.EntityID, &c.ServerSeq, &c.LocalData, &c.RemoteData, &ts); err != nil {
			return nil, err
		}
		parsed, parseErr := time.Parse("2006-01-02 15:04:05", ts)
		if parseErr != nil {
			return nil, parseErr
		}
		c.OverwrittenAt = parsed
		conflicts = append(conflicts, c)
	}
	return conflicts, rows.Err()
}

// SyncState holds the sync configuration for a linked project.
type SyncState struct {
	ProjectID           string
	LastPushedActionID  int64
	LastPulledServerSeq int64
	LastSyncAt          *time.Time
	SyncDisabled        bool
}

// Conn returns the underlying *sql.DB connection for use in transactions
// (e.g., by the sync library which needs raw DB access).
func (db *DB) Conn() *sql.DB {
	return db.conn
}

// GetSyncState returns the current sync state, or nil if the project is not linked.
func (db *DB) GetSyncState() (*SyncState, error) {
	var s SyncState
	var lastSync sql.NullTime
	var disabled int

	err := db.conn.QueryRow(`
		SELECT project_id, last_pushed_action_id, last_pulled_server_seq, last_sync_at, sync_disabled
		FROM sync_state LIMIT 1
	`).Scan(&s.ProjectID, &s.LastPushedActionID, &s.LastPulledServerSeq, &lastSync, &disabled)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if lastSync.Valid {
		s.LastSyncAt = &lastSync.Time
	}
	s.SyncDisabled = disabled != 0
	return &s, nil
}

// SetSyncState creates or replaces the sync state for a project (used for link).
func (db *DB) SetSyncState(projectID string) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			INSERT OR REPLACE INTO sync_state (project_id, last_pushed_action_id, last_pulled_server_seq, sync_disabled)
			VALUES (?, 0, 0, 0)
		`, projectID)
		return err
	})
}

// UpdateSyncPushed updates the last pushed action ID and sync time.
func (db *DB) UpdateSyncPushed(lastActionID int64) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			UPDATE sync_state SET last_pushed_action_id = ?, last_sync_at = CURRENT_TIMESTAMP
		`, lastActionID)
		return err
	})
}

// UpdateSyncPulled updates the last pulled server sequence and sync time.
func (db *DB) UpdateSyncPulled(lastServerSeq int64) error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`
			UPDATE sync_state SET last_pulled_server_seq = ?, last_sync_at = CURRENT_TIMESTAMP
		`, lastServerSeq)
		return err
	})
}

// ClearSyncState removes the sync state (used for unlink).
func (db *DB) ClearSyncState() error {
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM sync_state`)
		return err
	})
}

// CountPendingEvents returns the number of unsynced action_log entries.
func (db *DB) CountPendingEvents() (int64, error) {
	var count int64
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NULL AND undone = 0`).Scan(&count)
	return count, err
}

// ClearActionLogSyncState sets synced_at and server_seq to NULL on all action_log entries,
// allowing them to be re-pushed to a new server. Returns the number of rows affected.
func (db *DB) ClearActionLogSyncState() (int64, error) {
	var affected int64
	err := db.withWriteLock(func() error {
		result, err := db.conn.Exec(`UPDATE action_log SET synced_at = NULL, server_seq = NULL WHERE synced_at IS NOT NULL`)
		if err != nil {
			return err
		}
		affected, _ = result.RowsAffected()
		return nil
	})
	return affected, err
}

// CountSyncedEvents returns the number of action_log entries with synced_at set.
func (db *DB) CountSyncedEvents() (int64, error) {
	var count int64
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM action_log WHERE synced_at IS NOT NULL`).Scan(&count)
	return count, err
}
