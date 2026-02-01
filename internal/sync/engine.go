package sync

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// InitServerEventLog creates the events table and index if they don't exist.
func InitServerEventLog(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			server_seq        INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id         TEXT NOT NULL,
			session_id        TEXT NOT NULL,
			client_action_id  INTEGER NOT NULL,
			action_type       TEXT NOT NULL,
			entity_type       TEXT NOT NULL,
			entity_id         TEXT NOT NULL,
			payload           JSON NOT NULL,
			client_timestamp  DATETIME NOT NULL,
			server_timestamp  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(device_id, session_id, client_action_id)
		);
		CREATE INDEX IF NOT EXISTS idx_events_entity ON events(entity_type, entity_id);
	`)
	if err != nil {
		return fmt.Errorf("init event log: %w", err)
	}
	return nil
}

// InsertServerEvents inserts events into the server event log within the given transaction.
// Duplicates (by device_id, session_id, client_action_id) are rejected, not errored.
func InsertServerEvents(tx *sql.Tx, events []Event) (PushResult, error) {
	var result PushResult

	for _, ev := range events {
		// Validate required fields
		if ev.DeviceID == "" {
			result.Rejected = append(result.Rejected, Rejection{
				ClientActionID: ev.ClientActionID,
				Reason:         "empty device_id",
			})
			continue
		}
		if ev.SessionID == "" {
			result.Rejected = append(result.Rejected, Rejection{
				ClientActionID: ev.ClientActionID,
				Reason:         "empty session_id",
			})
			continue
		}
		if ev.EntityID == "" {
			result.Rejected = append(result.Rejected, Rejection{
				ClientActionID: ev.ClientActionID,
				Reason:         "empty entity_id",
			})
			continue
		}

		res, err := tx.Exec(
			`INSERT OR IGNORE INTO events (device_id, session_id, client_action_id, action_type, entity_type, entity_id, payload, client_timestamp)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.DeviceID, ev.SessionID, ev.ClientActionID,
			ev.ActionType, ev.EntityType, ev.EntityID,
			ev.Payload, ev.ClientTimestamp,
		)
		if err != nil {
			return result, fmt.Errorf("insert event %d: %w", ev.ClientActionID, err)
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("rows affected: %w", err)
		}

		if rows == 0 {
			// Duplicate — look up existing server_seq so client can mark synced
			var existingSeq int64
			err := tx.QueryRow(
				`SELECT server_seq FROM events WHERE device_id=? AND session_id=? AND client_action_id=?`,
				ev.DeviceID, ev.SessionID, ev.ClientActionID,
			).Scan(&existingSeq)
			if err != nil {
				slog.Warn("duplicate lookup failed", "aid", ev.ClientActionID, "err", err)
			}
			result.Rejected = append(result.Rejected, Rejection{
				ClientActionID: ev.ClientActionID,
				Reason:         "duplicate",
				ServerSeq:      existingSeq,
			})
			continue
		}

		seq, err := res.LastInsertId()
		if err != nil {
			return result, fmt.Errorf("last insert id: %w", err)
		}

		slog.Debug("event inserted", "seq", seq, "aid", ev.ClientActionID)
		result.Accepted++
		result.Acks = append(result.Acks, Ack{
			ClientActionID: ev.ClientActionID,
			ServerSeq:      seq,
		})
	}

	return result, nil
}

// GetEventsSince retrieves events after the given sequence number.
// If excludeDevice is non-empty, events from that device are filtered out.
func GetEventsSince(tx *sql.Tx, afterSeq int64, limit int, excludeDevice string) (PullResult, error) {
	var result PullResult
	result.LastServerSeq = afterSeq

	var rows *sql.Rows
	var err error

	if excludeDevice != "" {
		rows, err = tx.Query(
			`SELECT server_seq, device_id, session_id, client_action_id, action_type, entity_type, entity_id, payload, client_timestamp
			 FROM events WHERE server_seq > ? AND device_id != ? ORDER BY server_seq ASC LIMIT ?`,
			afterSeq, excludeDevice, limit,
		)
	} else {
		rows, err = tx.Query(
			`SELECT server_seq, device_id, session_id, client_action_id, action_type, entity_type, entity_id, payload, client_timestamp
			 FROM events WHERE server_seq > ? ORDER BY server_seq ASC LIMIT ?`,
			afterSeq, limit,
		)
	}
	if err != nil {
		return result, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ev Event
		var clientTS string
		err := rows.Scan(&ev.ServerSeq, &ev.DeviceID, &ev.SessionID, &ev.ClientActionID,
			&ev.ActionType, &ev.EntityType, &ev.EntityID, &ev.Payload, &clientTS)
		if err != nil {
			return result, fmt.Errorf("scan event: %w", err)
		}

		ev.ClientTimestamp, err = parseTimestamp(clientTS)
		if err != nil {
			return result, fmt.Errorf("parse timestamp seq=%d: %w", ev.ServerSeq, err)
		}

		result.Events = append(result.Events, ev)
		result.LastServerSeq = ev.ServerSeq
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("rows iteration: %w", err)
	}

	result.HasMore = len(result.Events) == limit
	return result, nil
}

// parseTimestamp tries common SQLite timestamp formats.
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05 -0700 -0700", // Go time.Time.String() with numeric tz
		"2006-01-02 15:04:05 -0700 MST",   // Go time.Time.String() standard
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %q", s)
}
