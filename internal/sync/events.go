package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
)

var validColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

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

// applyEvent is the internal version that also returns old row data on overwrite.
func applyEvent(tx *sql.Tx, event Event, validator EntityValidator) (applyResult, error) {
	if !validator(event.EntityType) {
		return applyResult{}, fmt.Errorf("invalid entity type: %q", event.EntityType)
	}

	if event.EntityID == "" {
		return applyResult{}, fmt.Errorf("empty entity ID for %q event", event.ActionType)
	}

	switch event.ActionType {
	case "create", "update":
		return upsertEntity(tx, event.EntityType, event.EntityID, event.Payload)
	case "delete":
		return applyResult{}, deleteEntity(tx, event.EntityType, event.EntityID)
	case "soft_delete":
		return applyResult{}, softDeleteEntity(tx, event.EntityType, event.EntityID, event.ClientTimestamp)
	default:
		return applyResult{}, fmt.Errorf("unknown action type: %q", event.ActionType)
	}
}

// upsertEntity inserts or replaces a row using the JSON payload fields.
// Returns applyResult with Overwritten=true and OldData populated if an existing row was replaced.
func upsertEntity(tx *sql.Tx, entityType, entityID string, newData json.RawMessage) (applyResult, error) {
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

	fields["id"] = entityID

	normalizeFieldsForDB(entityType, fields)

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
func deleteEntity(tx *sql.Tx, entityType, entityID string) error {
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
