package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// mapActionType converts td's action_log action types to sync event action types.
func mapActionType(tdAction string) string {
	switch tdAction {
	case "create", "handoff", "add_dependency", "link_file", "board_create", "board_update", "board_add_issue", "board_set_position", "work_session_tag":
		return "create"
	case "remove_dependency", "unlink_file", "board_delete", "work_session_untag":
		return "delete"
	case "delete", "board_unposition", "board_remove_issue", "soft_delete":
		return "soft_delete"
	case "restore":
		return "restore"
	default:
		return "update"
	}
}

// normalizeEntityType maps action_log entity types to canonical table names.
// Returns false when the entity type is not supported by the sync engine.
func normalizeEntityType(entityType string) (string, bool) {
	switch entityType {
	case "issue", "issues":
		return "issues", true
	case "handoff", "handoffs":
		return "handoffs", true
	case "board", "boards":
		return "boards", true
	case "log", "logs":
		return "logs", true
	case "comment", "comments":
		return "comments", true
	case "work_session", "work_sessions":
		return "work_sessions", true
	case "board_position", "board_issue_positions":
		return "board_issue_positions", true
	case "dependency", "issue_dependencies":
		return "issue_dependencies", true
	case "file_link", "issue_files":
		return "issue_files", true
	case "work_session_issue", "work_session_issues":
		return "work_session_issues", true
	case "note", "notes":
		return "notes", true
	default:
		return "", false
	}
}

// GetPendingEvents reads unsynced, non-undone action_log rows and returns them as Events.
// It uses rowid for ordering and as ClientActionID.
// Before querying, it backfills synthetic "create" entries for any entities that
// exist in syncable tables but have no action_log row (e.g. pre-existing data).
func GetPendingEvents(tx *sql.Tx, deviceID, sessionID string) ([]Event, error) {
	if n, err := BackfillOrphanEntities(tx, sessionID); err != nil {
		slog.Warn("backfill orphans", "err", err)
	} else if n > 0 {
		slog.Info("backfilled orphan entities", "count", n)
	}
	if n, err := BackfillStaleIssues(tx, sessionID); err != nil {
		slog.Warn("backfill stale issues", "err", err)
	} else if n > 0 {
		slog.Info("backfilled stale issues", "count", n)
	}

	rows, err := tx.Query(`
		SELECT rowid, id, action_type, entity_type, entity_id, new_data, previous_data, timestamp
		FROM action_log
		WHERE synced_at IS NULL AND undone = 0
		ORDER BY rowid ASC`)
	if err != nil {
		return nil, fmt.Errorf("query pending events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var (
			rowid                                   int64
			id                                      sql.NullString
			actionType, entityType, entityID, tsStr string
			newDataStr, prevDataStr                 sql.NullString
		)
		if err := rows.Scan(&rowid, &id, &actionType, &entityType, &entityID, &newDataStr, &prevDataStr, &tsStr); err != nil {
			return nil, fmt.Errorf("scan action_log row: %w", err)
		}
		if !id.Valid || id.String == "" {
			slog.Warn("sync: skipping action_log with NULL/empty id", "rowid", rowid)
			continue
		}

		clientTS, err := parseTimestamp(tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp rowid=%d: %w", rowid, err)
		}

		canonicalType, ok := normalizeEntityType(entityType)
		if !ok {
			slog.Warn("sync: skipping unsupported entity type", "entity_type", entityType, "action_id", id.String)
			continue
		}

		// Build payload wrapper with schema_version, new_data, previous_data
		newData := json.RawMessage("{}")
		if newDataStr.Valid && newDataStr.String != "" {
			newData = json.RawMessage(newDataStr.String)
		}
		prevData := json.RawMessage("{}")
		if prevDataStr.Valid && prevDataStr.String != "" {
			prevData = json.RawMessage(prevDataStr.String)
		}

		payload := map[string]any{
			"schema_version": 1,
			"new_data":       newData,
			"previous_data":  prevData,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload rowid=%d: %w", rowid, err)
		}

		events = append(events, Event{
			ClientActionID:  rowid,
			DeviceID:        deviceID,
			SessionID:       sessionID,
			ActionType:      mapActionType(actionType),
			EntityType:      canonicalType,
			EntityID:        entityID,
			Payload:         payloadBytes,
			ClientTimestamp: clientTS,
			ServerSeq:       0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return events, nil
}

// ApplyRemoteEvents applies a batch of remote events to the local database.
// Events with invalid entity types are logged and added to the Failed list.
// lastSyncAt gates conflict detection: overwrites are only flagged as conflicts
// when the local row was modified after lastSyncAt. Pass nil to skip conflict recording.
func ApplyRemoteEvents(tx *sql.Tx, events []Event, myDeviceID string, validator EntityValidator, lastSyncAt *time.Time) (ApplyResult, error) {
	var result ApplyResult

	for _, ev := range events {
		// Extract new_data and previous_data from the payload wrapper
		var wrapper struct {
			NewData      json.RawMessage `json:"new_data"`
			PreviousData json.RawMessage `json:"previous_data"`
		}
		if err := json.Unmarshal(ev.Payload, &wrapper); err != nil {
			slog.Warn("apply remote: unmarshal payload", "seq", ev.ServerSeq, "err", err)
			result.Failed = append(result.Failed, FailedEvent{ServerSeq: ev.ServerSeq, Error: err})
			continue
		}

		// Build event with raw new_data as payload for ApplyEvent
		applyEv := Event{
			ClientActionID:  ev.ClientActionID,
			DeviceID:        ev.DeviceID,
			SessionID:       ev.SessionID,
			ActionType:      ev.ActionType,
			EntityType:      ev.EntityType,
			EntityID:        ev.EntityID,
			Payload:         wrapper.NewData,
			ClientTimestamp: ev.ClientTimestamp,
			ServerSeq:       ev.ServerSeq,
		}

		res, err := applyEventWithPrevious(tx, applyEv, validator, wrapper.PreviousData)
		if err != nil {
			slog.Warn("apply remote: apply event", "seq", ev.ServerSeq, "err", err)
			result.Failed = append(result.Failed, FailedEvent{ServerSeq: ev.ServerSeq, Error: err})
			continue
		}
		if res.Overwritten && localModifiedSinceSync(res.OldData, lastSyncAt) {
			result.Overwrites++
			result.Conflicts = append(result.Conflicts, ConflictRecord{
				EntityType:    ev.EntityType,
				EntityID:      ev.EntityID,
				ServerSeq:     ev.ServerSeq,
				LocalData:     res.OldData,
				RemoteData:    wrapper.NewData,
				OverwrittenAt: time.Now().UTC(),
			})
		}

		result.Applied++
		result.LastAppliedSeq = ev.ServerSeq
	}

	return result, nil
}

// localModifiedSinceSync checks if the old row data has a timestamp field
// (updated_at, timestamp, or created_at) that is after lastSyncAt.
// Returns true (conflict) when: lastSyncAt is nil, oldData is empty,
// or the local row was modified after last sync.
func localModifiedSinceSync(oldData json.RawMessage, lastSyncAt *time.Time) bool {
	if lastSyncAt == nil {
		return false // first sync / bootstrap — don't flag conflicts
	}
	if len(oldData) == 0 {
		return false
	}

	var fields map[string]any
	if err := json.Unmarshal(oldData, &fields); err != nil {
		return true // can't parse — be safe, record conflict
	}

	// Try timestamp fields in priority order
	for _, key := range []string{"updated_at", "timestamp", "created_at"} {
		if val, ok := fields[key]; ok && val != nil {
			tsStr, ok := val.(string)
			if !ok {
				continue
			}
			ts, err := parseTimestamp(tsStr)
			if err != nil {
				continue
			}
			return ts.After(*lastSyncAt)
		}
	}

	// No timestamp field found — be conservative, record conflict
	return true
}

// MarkEventsSynced updates action_log rows with their server-assigned sequence numbers.
func MarkEventsSynced(tx *sql.Tx, acks []Ack) error {
	for _, ack := range acks {
		_, err := tx.Exec(
			`UPDATE action_log SET synced_at = CURRENT_TIMESTAMP, server_seq = ? WHERE rowid = ?`,
			ack.ServerSeq, ack.ClientActionID,
		)
		if err != nil {
			return fmt.Errorf("mark synced rowid=%d: %w", ack.ClientActionID, err)
		}
	}
	return nil
}
