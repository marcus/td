package db

import (
	"database/sql"
	"time"
)

// SkippedSyncEvent is a row from sync_skipped_events: a remote event this peer
// did not apply, kept so it is auditable rather than silently discarded.
type SkippedSyncEvent struct {
	ServerSeq  int64
	DeviceID   string
	ActionType string
	EntityType string
	EntityID   string
	Reason     string
	Error      string
	Payload    string
	SkippedAt  time.Time
}

// RecordSkippedEventsTx records skipped events inside the caller's transaction,
// so the record commits with the batch that skipped them — never one without
// the other.
//
// Keyed on server_seq with INSERT OR REPLACE: re-recording the same event (e.g.
// a batch replayed after an unrelated rollback) updates the row instead of
// accumulating duplicates.
func RecordSkippedEventsTx(tx *sql.Tx, events []SkippedSyncEvent) error {
	if len(events) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO sync_skipped_events
			(server_seq, device_id, action_type, entity_type, entity_id, reason, error, payload, skipped_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	for _, e := range events {
		var payload any
		if e.Payload != "" {
			payload = e.Payload
		}
		if _, err := stmt.Exec(e.ServerSeq, e.DeviceID, e.ActionType, e.EntityType,
			e.EntityID, e.Reason, e.Error, payload, now); err != nil {
			return err
		}
	}
	return nil
}

// CountSkippedEvents returns how many remote events this peer has skipped,
// broken down by reason.
func (db *DB) CountSkippedEvents() (map[string]int, error) {
	rows, err := db.conn.Query(`SELECT reason, COUNT(*) FROM sync_skipped_events GROUP BY reason`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int{}
	for rows.Next() {
		var reason string
		var n int
		if err := rows.Scan(&reason, &n); err != nil {
			return nil, err
		}
		counts[reason] = n
	}
	return counts, rows.Err()
}

// GetSkippedEvents returns the most recently skipped events, newest first.
func (db *DB) GetSkippedEvents(limit int) ([]SkippedSyncEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(`
		SELECT server_seq, device_id, action_type, entity_type, entity_id,
		       reason, error, COALESCE(payload, ''), skipped_at
		FROM sync_skipped_events
		ORDER BY skipped_at DESC, server_seq DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []SkippedSyncEvent
	for rows.Next() {
		var e SkippedSyncEvent
		var ts string
		if err := rows.Scan(&e.ServerSeq, &e.DeviceID, &e.ActionType, &e.EntityType,
			&e.EntityID, &e.Reason, &e.Error, &e.Payload, &ts); err != nil {
			return nil, err
		}
		if parsed, perr := parseTimestamp(ts); perr == nil {
			e.SkippedAt = parsed
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
