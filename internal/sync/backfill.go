package sync

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// syncableTable describes a table whose rows should be backfilled into action_log
// when no corresponding event exists.
type syncableTable struct {
	Table         string   // canonical table name (e.g. "issues")
	ActionType    string   // entity_type to use in the synthetic action_log row
	Aliases       []string // all entity_type strings that may appear in action_log for this table
	CreateTypes   []string // action_type values that indicate a create event for this table
	HasSoftDelete bool     // true if table has deleted_at column; backfill excludes soft-deleted rows
}

// syncableTables lists every table the sync engine pushes/pulls.
// Aliases must cover both singular and plural forms used by existing code paths.
var syncableTables = []syncableTable{
	{"issues", "issue", []string{"issue", "issues"}, []string{"create"}, false},
	{"logs", "logs", []string{"log", "logs"}, []string{"create"}, false},
	{"comments", "comments", []string{"comment", "comments"}, []string{"create"}, false},
	{"handoffs", "handoff", []string{"handoff", "handoffs"}, []string{"handoff"}, false},
	{"boards", "boards", []string{"board", "boards"}, []string{"board_create"}, false},
	{"work_sessions", "work_sessions", []string{"work_session", "work_sessions"}, []string{"create"}, false},
	{"board_issue_positions", "board_position", []string{"board_position", "board_issue_positions"}, []string{"board_set_position", "board_add_issue"}, true},
	{"issue_dependencies", "dependency", []string{"dependency", "issue_dependencies"}, []string{"add_dependency"}, false},
	{"issue_files", "file_link", []string{"file_link", "issue_files"}, []string{"link_file"}, false},
	{"work_session_issues", "work_session_issues", []string{"work_session_issue", "work_session_issues"}, []string{"work_session_tag"}, false},
	{"notes", "notes", []string{"note", "notes"}, []string{"create"}, true},
}

// BackfillOrphanEntities scans all syncable tables for rows that have no
// corresponding action_log entry and inserts synthetic "create" events so they
// get picked up by the normal push pipeline.
//
// Only runs when the client has never pulled from the server (last_pulled_server_seq == 0).
// After the first pull, entities in the DB may have come from the server, and
// backfilling those would create duplicate events.
//
// Returns the total number of entities backfilled.
func BackfillOrphanEntities(tx *sql.Tx, sessionID string) (int, error) {
	// Only backfill before the first pull — after pulling, entities may have
	// come from the server and don't need synthetic action_log entries.
	var lastPulled int64
	err := tx.QueryRow(`SELECT COALESCE(MAX(last_pulled_server_seq), 0) FROM sync_state`).Scan(&lastPulled)
	if err != nil || lastPulled > 0 {
		return 0, nil
	}

	total := 0

	for _, st := range syncableTables {
		n, err := backfillTable(tx, st, sessionID)
		if err != nil {
			return total, fmt.Errorf("backfill %s: %w", st.Table, err)
		}
		if n > 0 {
			slog.Info("backfilled orphan entities", "table", st.Table, "count", n)
		}
		total += n
	}

	return total, nil
}

// BackfillStaleIssues inserts synthetic update events for issues whose
// updated_at timestamp is newer than the latest action_log entry.
// This helps sync older data where status changes weren't logged.
//
// Only runs when the client has never pulled from the server.
func BackfillStaleIssues(tx *sql.Tx, sessionID string) (int, error) {
	// Only backfill before the first pull.
	var lastPulled int64
	err := tx.QueryRow(`SELECT COALESCE(MAX(last_pulled_server_seq), 0) FROM sync_state`).Scan(&lastPulled)
	if err != nil || lastPulled > 0 {
		return 0, nil
	}

	var tableExists int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='issues'`,
	).Scan(&tableExists); err != nil || tableExists == 0 {
		return 0, nil
	}

	rows, err := tx.Query(`
		SELECT i.*,
		       (SELECT timestamp FROM action_log
		        WHERE entity_id = i.id AND entity_type IN ('issue', 'issues')
		        ORDER BY timestamp DESC, rowid DESC LIMIT 1) AS last_ts,
		       (SELECT new_data FROM action_log
		        WHERE entity_id = i.id AND entity_type IN ('issue', 'issues')
		        ORDER BY timestamp DESC, rowid DESC LIMIT 1) AS last_data,
		       (SELECT COUNT(*) FROM action_log
		        WHERE entity_id = i.id AND entity_type IN ('issue', 'issues')
		        AND action_type = 'delete' AND undone = 0
		        AND rowid > COALESCE(
		            (SELECT MAX(rowid) FROM action_log
		             WHERE entity_id = i.id AND entity_type IN ('issue', 'issues')
		             AND action_type = 'create' AND undone = 0), 0)
		       ) AS has_delete_after_create,
		       (SELECT COUNT(*) FROM action_log
		        WHERE entity_id = i.id AND entity_type IN ('issue', 'issues')
		        AND undone = 0) AS active_event_count
		FROM issues i
	`)
	if err != nil {
		return 0, fmt.Errorf("query issues for stale backfill: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("get columns: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone)
		VALUES (?, ?, 'create', 'issue', ?, ?, '', ?, 0)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	const staleThreshold = time.Second
	count := 0

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan row: %w", err)
		}

		rowMap := make(map[string]any, len(cols))
		var entityID string
		var updatedAtVal any
		var lastActionVal any
		var lastDataVal any
		var hasDeleteAfterCreate int64
		var activeEventCount int64
		for i, c := range cols {
			switch c {
			case "last_ts":
				lastActionVal = vals[i]
				continue
			case "last_data":
				lastDataVal = vals[i]
				continue
			case "has_delete_after_create":
				if v, ok := vals[i].(int64); ok {
					hasDeleteAfterCreate = v
				}
				continue
			case "active_event_count":
				if v, ok := vals[i].(int64); ok {
					activeEventCount = v
				}
				continue
			case "id":
				entityID = fmt.Sprint(vals[i])
			case "updated_at":
				updatedAtVal = vals[i]
			}
			rowMap[c] = vals[i]
		}
		if entityID == "" {
			continue
		}

		needsUpdate := false

		// Issue was deleted after its last create — needs re-creation on receiver
		if hasDeleteAfterCreate > 0 {
			needsUpdate = true
		}

		// Check updated_at staleness when we have timestamps
		if !needsUpdate && updatedAtVal != nil && lastActionVal != nil {
			if updatedAt, ok := parseAnyTime(updatedAtVal); ok {
				if lastActionAt, ok := parseAnyTime(lastActionVal); ok {
					if updatedAt.After(lastActionAt.Add(staleThreshold)) {
						needsUpdate = true
					}
				}
			}
		}

		// Check status mismatch or invalid/empty JSON in last action
		currentStatus := fmt.Sprint(rowMap["status"])
		if !needsUpdate && currentStatus != "" {
			if !statusMatches(lastDataVal, currentStatus) {
				needsUpdate = true
			}
		}

		// Issue has events but none would set the correct status on the receiver.
		// This catches cases where partial updates skip status because prev/new
		// have the same status, but the create event had a different status.
		if !needsUpdate && activeEventCount > 0 && currentStatus != "" {
			if !anyEventSetsStatus(tx, entityID, currentStatus) {
				needsUpdate = true
			}
		}

		if !needsUpdate {
			continue
		}

		newData, err := json.Marshal(rowMap)
		if err != nil {
			slog.Warn("stale backfill: marshal row", "id", entityID, "err", err)
			continue
		}

		actionID, err := generateBackfillActionID()
		if err != nil {
			return count, fmt.Errorf("generate action id: %w", err)
		}

		ts := time.Now()
		if updatedAtVal != nil {
			if parsed, ok := parseAnyTime(updatedAtVal); ok {
				ts = parsed
			}
		}

		if _, err := stmt.Exec(actionID, sessionID, entityID, string(newData), ts); err != nil {
			return count, fmt.Errorf("insert stale update %s: %w", entityID, err)
		}
		count++
	}

	return count, rows.Err()
}

// backfillTable finds orphan rows in a single table and inserts synthetic action_log entries.
func backfillTable(tx *sql.Tx, st syncableTable, sessionID string) (int, error) {
	// Check the table exists (it may not if schema is old)
	var tableExists int
	err := tx.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, st.Table,
	).Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return 0, nil
	}

	// Build NOT EXISTS subquery for all known entity_type aliases and create action types
	typePH := make([]string, len(st.Aliases))
	args := make([]any, 0, len(st.Aliases)+len(st.CreateTypes))
	for i, a := range st.Aliases {
		typePH[i] = "?"
		args = append(args, a)
	}
	createPH := make([]string, len(st.CreateTypes))
	for i, a := range st.CreateTypes {
		createPH[i] = "?"
		args = append(args, a)
	}

	extraFilter := ""
	if st.HasSoftDelete {
		extraFilter = " AND t.deleted_at IS NULL"
	}
	// Exclude builtin boards from backfill - they shouldn't be synced or undone
	if st.Table == "boards" {
		extraFilter += " AND t.is_builtin = 0"
	}

	query := fmt.Sprintf(
		`SELECT * FROM %s t WHERE NOT EXISTS (
			SELECT 1 FROM action_log al
			WHERE al.entity_id = t.id
			AND al.entity_type IN (%s)
			AND al.action_type IN (%s)
			AND al.undone = 0
		)%s`, st.Table, strings.Join(typePH, ","), strings.Join(createPH, ","), extraFilter)

	rows, err := tx.Query(query, args...)
	if err != nil {
		return 0, fmt.Errorf("query orphans: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("get columns: %w", err)
	}

	// Prepare INSERT statement for the batch
	stmt, err := tx.Prepare(`INSERT INTO action_log
		(id, session_id, action_type, entity_type, entity_id, new_data, previous_data, timestamp, undone)
		VALUES (?, ?, 'create', ?, ?, ?, '', ?, 0)`)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		// Scan all columns dynamically
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan row: %w", err)
		}

		// Build JSON map from columns
		rowMap := make(map[string]any, len(cols))
		var entityID string
		for i, c := range cols {
			rowMap[c] = vals[i]
			if c == "id" {
				entityID = fmt.Sprint(vals[i])
			}
		}
		if entityID == "" {
			continue
		}

		newData, err := json.Marshal(rowMap)
		if err != nil {
			slog.Warn("backfill: marshal row", "table", st.Table, "id", entityID, "err", err)
			continue
		}

		actionID, err := generateBackfillActionID()
		if err != nil {
			return count, fmt.Errorf("generate action id: %w", err)
		}

		ts := extractEntityTimestamp(rowMap)

		if _, err := stmt.Exec(actionID, sessionID, st.ActionType, entityID, string(newData), ts); err != nil {
			return count, fmt.Errorf("insert backfill %s/%s: %w", st.Table, entityID, err)
		}

		slog.Debug("backfilled entity", "table", st.Table, "id", entityID)
		count++
	}

	return count, rows.Err()
}

// generateBackfillActionID creates an action_log ID matching the al-XXXXXXXX pattern.
func generateBackfillActionID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "al-" + hex.EncodeToString(bytes), nil
}

// extractEntityTimestamp pulls a timestamp from common fields in the row data.
// Falls back to time.Now() if no recognizable timestamp field is found.
func extractEntityTimestamp(fields map[string]any) time.Time {
	for _, key := range []string{"created_at", "timestamp", "started_at"} {
		val, ok := fields[key]
		if !ok || val == nil {
			continue
		}
		switch v := val.(type) {
		case time.Time:
			return v
		case string:
			if t, err := parseTimestamp(v); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

func parseAnyTime(val any) (time.Time, bool) {
	switch v := val.(type) {
	case time.Time:
		return v, true
	case string:
		if t, err := parseTimestamp(v); err == nil {
			return t, true
		}
	case []byte:
		if t, err := parseTimestamp(string(v)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// anyEventSetsStatus checks whether any non-undone action_log entry for an issue
// would effectively set the given status on the receiver. This happens when:
//   - A "create" event has new_data with the target status, OR
//   - An "update" event's previous_data status differs from new_data status
//     (meaning the diff would include the status change)
func anyEventSetsStatus(tx *sql.Tx, entityID, status string) bool {
	pattern := fmt.Sprintf(`%%"status":"%s"%%`, status)

	// Check if any create event has the target status
	var createCount int
	_ = tx.QueryRow(`
		SELECT COUNT(*) FROM action_log
		WHERE entity_id = ? AND entity_type IN ('issue', 'issues')
		AND undone = 0 AND action_type = 'create' AND new_data LIKE ?`,
		entityID, pattern,
	).Scan(&createCount)
	if createCount > 0 {
		return true
	}

	// Check update events: status must differ between previous_data and new_data
	rows, err := tx.Query(`
		SELECT new_data, previous_data FROM action_log
		WHERE entity_id = ? AND entity_type IN ('issue', 'issues')
		AND undone = 0 AND action_type != 'create' AND new_data LIKE ?`,
		entityID, pattern,
	)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var newData, prevData sql.NullString
		if err := rows.Scan(&newData, &prevData); err != nil {
			continue
		}
		// If previous_data is empty/missing, this event will do a full upsert
		// (not a partial update), so status will be set
		if !prevData.Valid || prevData.String == "" || prevData.String == "{}" {
			return true
		}
		// If previous_data has a different status, the diff will include status
		if !statusMatches(prevData.String, status) {
			return true
		}
	}
	return false
}

func statusMatches(lastData any, currentStatus string) bool {
	if lastData == nil {
		return false
	}

	var raw string
	switch v := lastData.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		raw = fmt.Sprint(v)
	}
	if raw == "" {
		return false
	}

	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return false
	}
	val, ok := fields["status"]
	if !ok || val == nil {
		return false
	}
	return fmt.Sprint(val) == currentStatus
}
