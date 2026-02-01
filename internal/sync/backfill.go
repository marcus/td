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
	Table       string   // canonical table name (e.g. "issues")
	ActionType  string   // entity_type to use in the synthetic action_log row
	Aliases     []string // all entity_type strings that may appear in action_log for this table
	CreateTypes []string // action_type values that indicate a create event for this table
}

// syncableTables lists every table the sync engine pushes/pulls.
// Aliases must cover both singular and plural forms used by existing code paths.
var syncableTables = []syncableTable{
	{"issues", "issue", []string{"issue", "issues"}, []string{"create"}},
	{"logs", "logs", []string{"log", "logs"}, []string{"create"}},
	{"comments", "comments", []string{"comment", "comments"}, []string{"create"}},
	{"handoffs", "handoff", []string{"handoff", "handoffs"}, []string{"handoff"}},
	{"boards", "boards", []string{"board", "boards"}, []string{"board_create"}},
	{"work_sessions", "work_sessions", []string{"work_session", "work_sessions"}, []string{"create"}},
	{"board_issue_positions", "board_position", []string{"board_position", "board_issue_positions"}, []string{"board_set_position", "board_add_issue"}},
	{"issue_dependencies", "dependency", []string{"dependency", "issue_dependencies"}, []string{"add_dependency"}},
	{"issue_files", "file_link", []string{"file_link", "issue_files"}, []string{"link_file"}},
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

	query := fmt.Sprintf(
		`SELECT * FROM %s t WHERE NOT EXISTS (
			SELECT 1 FROM action_log al
			WHERE al.entity_id = t.id
			AND al.entity_type IN (%s)
			AND al.action_type IN (%s)
		)`, st.Table, strings.Join(typePH, ","), strings.Join(createPH, ","))

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
