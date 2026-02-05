package db

import (
	"database/sql"
	"time"
)

// SyncHistoryEntry represents a row from the sync_history table.
type SyncHistoryEntry struct {
	ID         int64
	Direction  string // "push" or "pull"
	ActionType string // "create", "update", "delete"
	EntityType string // "issues", "logs", etc.
	EntityID   string
	ServerSeq  int64
	DeviceID   string
	Timestamp  time.Time
}

// parseTimestamp tries common SQLite timestamp formats.
func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, &time.ParseError{Layout: "2006-01-02 15:04:05", Value: s}
}

// RecordSyncHistoryTx batch-inserts sync history entries within the provided transaction.
// Returns nil if entries is empty.
func RecordSyncHistoryTx(tx *sql.Tx, entries []SyncHistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	stmt, err := tx.Prepare(`
		INSERT INTO sync_history (direction, action_type, entity_type, entity_id, server_seq, device_id, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		_, err := stmt.Exec(e.Direction, e.ActionType, e.EntityType, e.EntityID, e.ServerSeq, e.DeviceID, e.Timestamp)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetSyncHistoryTail returns the last N entries in chronological order (oldest first).
func (db *DB) GetSyncHistoryTail(limit int) ([]SyncHistoryEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, direction, action_type, entity_type, entity_id,
		       COALESCE(server_seq, 0), COALESCE(device_id, ''), timestamp
		FROM sync_history
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SyncHistoryEntry
	for rows.Next() {
		var e SyncHistoryEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.Direction, &e.ActionType, &e.EntityType, &e.EntityID, &e.ServerSeq, &e.DeviceID, &ts); err != nil {
			return nil, err
		}
		parsed, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, parseErr
		}
		e.Timestamp = parsed
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to chronological order
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
}

// GetSyncHistory returns entries with id > afterID, ordered by id ASC, limited to limit.
// Used for follow-mode polling.
func (db *DB) GetSyncHistory(afterID int64, limit int) ([]SyncHistoryEntry, error) {
	rows, err := db.conn.Query(`
		SELECT id, direction, action_type, entity_type, entity_id,
		       COALESCE(server_seq, 0), COALESCE(device_id, ''), timestamp
		FROM sync_history
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SyncHistoryEntry
	for rows.Next() {
		var e SyncHistoryEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.Direction, &e.ActionType, &e.EntityType, &e.EntityID, &e.ServerSeq, &e.DeviceID, &ts); err != nil {
			return nil, err
		}
		parsed, parseErr := parseTimestamp(ts)
		if parseErr != nil {
			return nil, parseErr
		}
		e.Timestamp = parsed
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// PruneSyncHistory deletes rows not in the newest maxRows entries.
func PruneSyncHistory(tx *sql.Tx, maxRows int) error {
	_, err := tx.Exec(`
		DELETE FROM sync_history WHERE id NOT IN (
			SELECT id FROM sync_history ORDER BY id DESC LIMIT ?
		)
	`, maxRows)
	return err
}
