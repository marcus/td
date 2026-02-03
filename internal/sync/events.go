package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

var validColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// wouldCreateCycleTx checks if adding a dependency from issueID to dependsOnID would create a cycle.
// Uses a transaction for consistency with sync event application.
func wouldCreateCycleTx(tx *sql.Tx, issueID, dependsOnID string) bool {
	visited := make(map[string]bool)
	return hasCyclePathTx(tx, dependsOnID, issueID, visited)
}

// hasCyclePathTx checks if there's a path from 'from' to 'to' through the dependency graph.
func hasCyclePathTx(tx *sql.Tx, from, to string, visited map[string]bool) bool {
	if from == to {
		return true
	}
	if visited[from] {
		return false
	}
	visited[from] = true

	deps, err := getDependenciesTx(tx, from)
	if err != nil {
		slog.Debug("cycle check: get deps failed", "from", from, "err", err)
		return false
	}
	for _, dep := range deps {
		if hasCyclePathTx(tx, dep, to, visited) {
			return true
		}
	}
	return false
}

// getDependenciesTx returns issue IDs that the given issue depends on.
func getDependenciesTx(tx *sql.Tx, issueID string) ([]string, error) {
	rows, err := tx.Query(`SELECT depends_on_id FROM issue_dependencies WHERE issue_id = ? AND relation_type = 'depends_on'`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []string
	for rows.Next() {
		var dep string
		if err := rows.Scan(&dep); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

// checkAndResolveCyclicDependency checks if creating a dependency would form a cycle.
// If so, uses a deterministic rule for convergence: the edge with lexicographically
// smaller (issue_id, depends_on_id) wins. If incoming edge should win, removes the
// conflicting edge. Returns true if the incoming event should be skipped.
func checkAndResolveCyclicDependency(tx *sql.Tx, event Event) bool {
	var fields map[string]any
	if err := json.Unmarshal(event.Payload, &fields); err != nil {
		slog.Debug("cycle check: unmarshal failed", "err", err)
		return false // can't parse, let upsert handle it
	}

	issueID, _ := fields["issue_id"].(string)
	dependsOnID, _ := fields["depends_on_id"].(string)
	if issueID == "" || dependsOnID == "" {
		return false // missing fields, let upsert handle validation
	}

	if !wouldCreateCycleTx(tx, issueID, dependsOnID) {
		return false // no cycle, proceed with create
	}

	// Cycle detected. Find the conflicting edge (the reverse: dependsOnID -> issueID path)
	// For simple A->B vs B->A case, the conflicting edge is dependsOnID -> issueID
	conflictIssueID := dependsOnID
	conflictDependsOnID := issueID

	// Deterministic rule: smaller (issue_id, depends_on_id) wins
	incomingKey := issueID + "|" + dependsOnID
	conflictKey := conflictIssueID + "|" + conflictDependsOnID

	if incomingKey < conflictKey {
		// Incoming edge wins - remove the conflicting edge
		_, err := tx.Exec(`DELETE FROM issue_dependencies WHERE issue_id = ? AND depends_on_id = ?`,
			conflictIssueID, conflictDependsOnID)
		if err != nil {
			slog.Warn("cycle resolution: failed to remove conflicting edge",
				"conflict", conflictKey, "err", err)
			return true // skip incoming on error
		}
		slog.Info("cycle resolution: removed conflicting edge, applying incoming",
			"removed", conflictKey, "applying", incomingKey)
		return false // apply incoming
	}

	// Conflicting edge wins - skip incoming
	slog.Info("cycle resolution: keeping existing edge, skipping incoming",
		"kept", conflictKey, "skipped", incomingKey)
	return true
}

func getTableColumns(tx *sql.Tx, table string) (map[string]bool, error) {
	if !validColumnName.MatchString(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// applyResult holds the outcome of applying a single event.
type applyResult struct {
	Overwritten bool
	OldData     json.RawMessage // non-nil only when Overwritten is true
}

// ApplyEvent applies a single sync event to the database within the given transaction.
// The validator is called to check that the entity type is allowed before any SQL is executed.
// Returns true if an existing row was overwritten (create/update only).
func ApplyEvent(tx *sql.Tx, event Event, validator EntityValidator) (bool, error) {
	res, err := applyEvent(tx, event, validator)
	if err != nil {
		return false, err
	}
	return res.Overwritten, nil
}

// diffJSON compares previous and current JSON objects field by field and returns
// only the fields that changed (added, modified, or removed/set-to-null).
// The "id" field is always skipped — primary keys are never updated.
func diffJSON(previous, current map[string]any) map[string]any {
	changed := make(map[string]any)

	// Fields in current that are new or modified
	for k, v := range current {
		if k == "id" {
			continue
		}
		oldVal, existed := previous[k]
		if !existed || !reflect.DeepEqual(oldVal, v) {
			changed[k] = v
		}
	}

	// Fields removed in current (existed in previous but not in current)
	for k := range previous {
		if k == "id" {
			continue
		}
		if _, exists := current[k]; !exists {
			changed[k] = nil
		}
	}

	return changed
}

// applyPartialUpdate builds and executes an UPDATE statement for only the changed fields.
// Convergence relies on all clients seeing all events in server_seq order (excludeDevice="").
// Returns the number of rows affected. Returns nil error if changedFields is empty (no-op).
func applyPartialUpdate(tx *sql.Tx, entityType string, entityID string, changedFields map[string]any) (int64, error) {
	if len(changedFields) == 0 {
		return 0, nil
	}
	if entityID == "" {
		return 0, fmt.Errorf("partial update %s: empty entity ID", entityType)
	}

	// Validate column names against schema
	validCols, err := getTableColumns(tx, entityType)
	if err != nil {
		return 0, fmt.Errorf("partial update %s/%s: get columns: %w", entityType, entityID, err)
	}

	// Normalize values for DB storage
	normalizeFieldsForDB(entityType, changedFields)

	// Build SET clause with only valid, changed columns
	keys := make([]string, 0, len(changedFields))
	for k := range changedFields {
		if !validCols[k] {
			slog.Debug("partial update: dropping unknown column", "table", entityType, "column", k)
			continue
		}
		if !validColumnName.MatchString(k) {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return 0, nil
	}
	sort.Strings(keys)

	setClauses := make([]string, len(keys))
	vals := make([]any, len(keys)+1) // +1 for WHERE id=?
	for i, k := range keys {
		setClauses[i] = fmt.Sprintf("%s = ?", k)
		vals[i] = changedFields[k]
	}
	vals[len(keys)] = entityID

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", entityType, strings.Join(setClauses, ", "))
	slog.Debug("partial update", "table", entityType, "id", entityID, "fields", len(keys))
	res, err := tx.Exec(query, vals...)
	if err != nil {
		return 0, fmt.Errorf("partial update %s/%s: %w", entityType, entityID, err)
	}
	return res.RowsAffected()
}

// applyEvent is the internal version that also returns old row data on overwrite.
func applyEvent(tx *sql.Tx, event Event, validator EntityValidator) (applyResult, error) {
	return applyEventWithPrevious(tx, event, validator, nil)
}

// applyEventWithPrevious is like applyEvent but accepts optional previousData for partial updates.
func applyEventWithPrevious(tx *sql.Tx, event Event, validator EntityValidator, previousData json.RawMessage) (applyResult, error) {
	if !validator(event.EntityType) {
		return applyResult{}, fmt.Errorf("invalid entity type: %q", event.EntityType)
	}

	if event.EntityID == "" {
		return applyResult{}, fmt.Errorf("empty entity ID for %q event", event.ActionType)
	}

	switch event.ActionType {
	case "create", "update":
		// Check for dependency cycles before creating issue_dependencies
		if event.ActionType == "create" && event.EntityType == "issue_dependencies" {
			if skipped := checkAndResolveCyclicDependency(tx, event); skipped {
				slog.Warn("sync: skipped dependency create (cycle resolution)",
					"entity_id", event.EntityID, "seq", event.ServerSeq)
				return applyResult{}, nil
			}
		}
		if event.ActionType == "update" {
			// Try partial update when previous_data is available
			if len(previousData) > 0 && string(previousData) != "{}" {
				return applyPartialUpdateEvent(tx, event, previousData)
			}
			return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
		}
		return upsertEntity(tx, event.EntityType, event.EntityID, event.Payload)
	case "delete":
		return applyResult{}, deleteEntity(tx, event.EntityType, event.EntityID)
	case "soft_delete":
		return applyResult{}, softDeleteEntity(tx, event.EntityType, event.EntityID, event.ClientTimestamp)
	case "restore":
		return applyResult{}, restoreEntity(tx, event.EntityType, event.EntityID, event.ClientTimestamp)
	default:
		return applyResult{}, fmt.Errorf("unknown action type: %q", event.ActionType)
	}
}

// applyPartialUpdateEvent diffs previous_data vs new_data and applies only changed fields.
// Falls back to full upsert if the partial update fails or the row doesn't exist.
func applyPartialUpdateEvent(tx *sql.Tx, event Event, previousData json.RawMessage) (applyResult, error) {
	var prevFields map[string]any
	if err := json.Unmarshal(previousData, &prevFields); err != nil {
		slog.Debug("partial update: bad previous_data, falling back", "err", err)
		return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
	}

	var newFields map[string]any
	if err := json.Unmarshal(event.Payload, &newFields); err != nil {
		slog.Debug("partial update: bad new_data, falling back", "err", err)
		return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
	}

	changed := diffJSON(prevFields, newFields)
	if len(changed) == 0 {
		// No fields changed — fall back to full upsert to ensure entity exists
		slog.Debug("partial update: no diff, falling back to upsert", "table", event.EntityType, "id", event.EntityID)
		return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
	}

	rowsAffected, err := applyPartialUpdate(tx, event.EntityType, event.EntityID, changed)
	if err != nil {
		slog.Debug("partial update failed, falling back", "err", err)
		return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
	}

	if rowsAffected == 0 {
		// Row doesn't exist locally — fall back to full upsert with new_data
		slog.Debug("partial update: row missing, falling back to upsert", "table", event.EntityType, "id", event.EntityID)
		return upsertEntityIfExists(tx, event.EntityType, event.EntityID, event.Payload)
	}

	return applyResult{}, nil
}

// upsertEntity inserts or replaces a row using the JSON payload fields.
// Returns applyResult with Overwritten=true and OldData populated if an existing row was replaced.
func upsertEntity(tx *sql.Tx, entityType, entityID string, newData json.RawMessage) (applyResult, error) {
	return upsertEntityWithMode(tx, entityType, entityID, newData, false)
}

// upsertEntityIfExists updates a row only if it already exists.
// This prevents update events from resurrecting hard-deleted rows.
func upsertEntityIfExists(tx *sql.Tx, entityType, entityID string, newData json.RawMessage) (applyResult, error) {
	return upsertEntityWithMode(tx, entityType, entityID, newData, true)
}

// upsertEntityWithMode inserts or replaces a row using the JSON payload fields.
// If requireExisting is true, the operation is a no-op when the row does not exist.
func upsertEntityWithMode(tx *sql.Tx, entityType, entityID string, newData json.RawMessage, requireExisting bool) (applyResult, error) {
	if newData == nil {
		return applyResult{}, fmt.Errorf("upsert %s/%s: nil payload", entityType, entityID)
	}

	var fields map[string]any
	if err := json.Unmarshal(newData, &fields); err != nil {
		return applyResult{}, fmt.Errorf("upsert %s/%s: unmarshal payload: %w", entityType, entityID, err)
	}

	if len(fields) == 0 {
		return applyResult{}, fmt.Errorf("upsert %s/%s: payload has no fields", entityType, entityID)
	}

	// Check for existing row and capture its data before overwrite
	var oldData json.RawMessage
	overwritten := false
	checkQuery := fmt.Sprintf("SELECT * FROM %s WHERE id = ?", entityType)
	rows, err := tx.Query(checkQuery, entityID)
	if err != nil {
		return applyResult{}, fmt.Errorf("check existing %s/%s: %w", entityType, entityID, err)
	}
	cols, _ := rows.Columns()
	if rows.Next() {
		overwritten = true
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if scanErr := rows.Scan(ptrs...); scanErr != nil {
			slog.Debug("scan existing row", "table", entityType, "id", entityID, "err", scanErr)
		} else {
			rowMap := make(map[string]any, len(cols))
			for i, c := range cols {
				rowMap[c] = vals[i]
			}
			if marshalData, marshalErr := json.Marshal(rowMap); marshalErr != nil {
				slog.Warn("marshal old data", "table", entityType, "id", entityID, "err", marshalErr)
			} else {
				oldData = marshalData
			}
		}
	}
	// Close before INSERT to release shared lock
	rows.Close()

	if requireExisting && !overwritten {
		slog.Debug("upsert skipped (missing row)", "table", entityType, "id", entityID)
		return applyResult{}, nil
	}

	fields["id"] = entityID

	normalizeFieldsForDB(entityType, fields)

	// Filter unknown columns for forward compatibility (spec: ignore unknown fields)
	validCols, err := getTableColumns(tx, entityType)
	if err != nil {
		return applyResult{}, fmt.Errorf("upsert %s/%s: get columns: %w", entityType, entityID, err)
	}
	for k := range fields {
		if !validCols[k] {
			slog.Debug("upsert: dropping unknown column", "table", entityType, "column", k)
			delete(fields, k)
		}
	}

	if len(fields) <= 1 {
		return applyResult{}, fmt.Errorf("upsert %s/%s: no known fields in payload", entityType, entityID)
	}

	colStr, placeholders, insertVals, err := buildInsert(fields)
	if err != nil {
		return applyResult{}, fmt.Errorf("upsert %s/%s: %w", entityType, entityID, err)
	}
	query := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)", entityType, colStr, placeholders)

	slog.Debug("upsert", "table", entityType, "id", entityID)
	if _, err := tx.Exec(query, insertVals...); err != nil {
		return applyResult{}, fmt.Errorf("upsert %s/%s: %w", entityType, entityID, err)
	}
	return applyResult{Overwritten: overwritten, OldData: oldData}, nil
}

// deleteEntity hard-deletes a row. No-op if the row does not exist.
// For boards, also cascade soft-delete all board_issue_positions since
// PRAGMA foreign_keys is not enabled and ON DELETE CASCADE is inert.
func deleteEntity(tx *sql.Tx, entityType, entityID string) error {
	if entityType == "boards" {
		if _, err := tx.Exec(
			`UPDATE board_issue_positions SET deleted_at = CURRENT_TIMESTAMP WHERE board_id = ? AND deleted_at IS NULL`,
			entityID,
		); err != nil {
			return fmt.Errorf("cascade soft-delete positions for board %s: %w", entityID, err)
		}
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", entityType)
	if _, err := tx.Exec(query, entityID); err != nil {
		return fmt.Errorf("delete %s/%s: %w", entityType, entityID, err)
	}
	return nil
}

// softDeleteEntity sets deleted_at on a row. No-op if the row does not exist.
func softDeleteEntity(tx *sql.Tx, entityType, entityID string, timestamp time.Time) error {
	query := fmt.Sprintf("UPDATE %s SET deleted_at = ? WHERE id = ?", entityType)
	if _, err := tx.Exec(query, timestamp, entityID); err != nil {
		return fmt.Errorf("soft_delete %s/%s: %w", entityType, entityID, err)
	}
	return nil
}

// restoreEntity clears deleted_at on a row. No-op if the row does not exist.
func restoreEntity(tx *sql.Tx, entityType, entityID string, timestamp time.Time) error {
	query := fmt.Sprintf("UPDATE %s SET deleted_at = NULL, updated_at = ? WHERE id = ?", entityType)
	if _, err := tx.Exec(query, timestamp, entityID); err != nil {
		return fmt.Errorf("restore %s/%s: %w", entityType, entityID, err)
	}
	return nil
}

// buildInsert sorts fields alphabetically and returns column list, placeholders, and values.
// Returns an error if any key is not a valid SQL column name.
func buildInsert(fields map[string]any) (cols string, placeholders string, vals []any, err error) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if !validColumnName.MatchString(k) {
			return "", "", nil, fmt.Errorf("invalid column name: %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ph := make([]string, len(keys))
	vals = make([]any, len(keys))
	for i, k := range keys {
		ph[i] = "?"
		vals[i] = fields[k]
	}

	cols = strings.Join(keys, ", ")
	placeholders = strings.Join(ph, ", ")
	return
}

// normalizeFieldsForDB converts non-scalar values (slices, maps) to DB-compatible strings.
// Special case: issues.labels is stored as comma-separated text.
// All other array/object fields are stored as JSON strings.
func normalizeFieldsForDB(entityType string, fields map[string]any) {
	for k, v := range fields {
		if v == nil {
			if entityType == "issues" && (k == "implementer_session" || k == "reviewer_session" || k == "creator_session") {
				fields[k] = ""
			}
			continue
		}
		switch val := v.(type) {
		case []any:
			if entityType == "issues" && k == "labels" {
				// Convert ["a","b"] to "a,b"
				parts := make([]string, 0, len(val))
				for _, item := range val {
					parts = append(parts, fmt.Sprint(item))
				}
				fields[k] = strings.Join(parts, ",")
			} else {
				data, err := json.Marshal(val)
				if err != nil {
					slog.Warn("normalize field", "field", k, "err", err)
					fields[k] = "[]"
				} else {
					fields[k] = string(data)
				}
			}
		case map[string]any:
			data, err := json.Marshal(val)
			if err != nil {
				slog.Warn("normalize field", "field", k, "err", err)
				fields[k] = "{}"
			} else {
				fields[k] = string(data)
			}
		// json.RawMessage is []byte, handle it
		case json.RawMessage:
			fields[k] = string(val)
		}
	}
}
