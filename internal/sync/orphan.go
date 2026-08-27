package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// OrphanedParentError reports that a remote row cannot be inserted because a
// row it references via a FOREIGN KEY does not exist locally.
//
// # Why this is a deliberate drop rather than a failure
//
// Every FK in the local schema that guards a synced child row is declared
// ON DELETE CASCADE (see internal/db/migration_fk_enforcement.go). A child of a
// cascade FK is not merely invalid without its parent — it is a row the schema
// says must not survive the parent. So when the parent is absent, there is no
// state in which applying the child is correct: had the parent still existed
// and been deleted a moment later, the cascade would have removed the child
// again.
//
// The absence is also terminal, not a matter of timing. Events are applied in
// server_seq order, and a parent's create always carries a lower server_seq
// than any child referencing it: the author cannot position an issue on a board
// it does not have, and its own action_log preserves that order on push. A
// missing parent therefore means the parent was deleted, not that its create is
// still in flight. (Confirmed empirically for td-8fe2bc: enabling
// PRAGMA defer_foreign_keys, which would let a parent land later in the same
// batch, did not help — the parent was genuinely gone.)
//
// Dropping the child converges peers rather than diverging them. The canonical
// case is a peer replaying its OWN events from a cursor suffix: it created a
// board, positioned an issue on it, then deleted the board. Its local state is
// already the end state (both rows gone). Replaying the position create would
// try to resurrect a row whose parent it has already deleted; every other peer,
// applying the same stream in order, also ends with both rows gone. Skipping is
// the only outcome that agrees with them.
//
// The event is recorded in sync_skipped_events, never silently discarded.
type OrphanedParentError struct {
	EntityType  string
	EntityID    string
	Column      string
	ParentTable string
	ParentID    string
}

func (e *OrphanedParentError) Error() string {
	return fmt.Sprintf("orphaned %s/%s: %s references missing %s/%s",
		e.EntityType, e.EntityID, e.Column, e.ParentTable, e.ParentID)
}

// foreignKeyRef describes one FOREIGN KEY column on a table.
type foreignKeyRef struct {
	Column      string // column on the child table
	ParentTable string
	ParentCol   string // referenced column on the parent (defaults to "id")
}

// tableForeignKeys returns the FOREIGN KEY references declared on a table.
//
// Read from PRAGMA foreign_key_list rather than a hardcoded list so a new
// synced table with a new FK is covered the day it is added, with no second
// place to update.
//
// Only ON DELETE CASCADE foreign keys are returned. That restriction is the
// whole basis for dropping the child: cascade means the schema itself says the
// child must not outlive the parent, so a missing parent proves the child is
// invalid. A plain (non-cascade) FK carries no such guarantee — a missing
// parent there might just be a peer that has not caught up, and dropping the
// child would lose data. Those are left to SQLite, which rejects them, and the
// event is quarantined rather than silently dropped.
//
// Composite (multi-column) foreign keys are deliberately ignored: PRAGMA
// reports them as several rows sharing an id, and checking one column of a
// composite key in isolation would be wrong. No synced table declares one
// today; if that changes, SQLite's own enforcement still catches the violation
// and the event is quarantined rather than dropped.
func tableForeignKeys(tx *sql.Tx, table string) ([]foreignKeyRef, error) {
	if !validColumnName.MatchString(table) {
		return nil, fmt.Errorf("invalid table name: %q", table)
	}
	rows, err := tx.Query(fmt.Sprintf("PRAGMA foreign_key_list(%s)", table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type entry struct {
		ref     foreignKeyRef
		count   int
		cascade bool
	}
	byID := map[int]*entry{}
	var order []int
	for rows.Next() {
		var id, seq int
		var parentTable, from string
		var to sql.NullString
		var onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &parentTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		e, ok := byID[id]
		if !ok {
			e = &entry{}
			byID[id] = e
			order = append(order, id)
		}
		e.count++
		e.cascade = strings.EqualFold(onDelete, "CASCADE")
		parentCol := "id"
		if to.Valid && to.String != "" {
			parentCol = to.String
		}
		e.ref = foreignKeyRef{Column: from, ParentTable: parentTable, ParentCol: parentCol}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var refs []foreignKeyRef
	for _, id := range order {
		e := byID[id]
		if e.count != 1 {
			continue // composite key — see doc comment
		}
		if !e.cascade {
			continue // not ON DELETE CASCADE — see doc comment
		}
		if !validColumnName.MatchString(e.ref.ParentTable) || !validColumnName.MatchString(e.ref.ParentCol) {
			continue
		}
		refs = append(refs, e.ref)
	}
	return refs, nil
}

// checkParentsPresent returns an *OrphanedParentError if the payload references
// a parent row that does not exist locally.
//
// It runs before the INSERT so a missing parent becomes a precise, per-event
// decision instead of an opaque "FOREIGN KEY constraint failed". This is a
// pre-check, not a relaxation: FK enforcement stays fully on, and anything this
// misses SQLite still rejects.
func checkParentsPresent(tx *sql.Tx, entityType string, fields map[string]any) error {
	// If FK enforcement is off, the insert would have succeeded. Dropping the
	// row here would change behaviour on a database that never asked for the
	// constraint, so leave it alone.
	var fkOn int
	if err := tx.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn); err != nil || fkOn == 0 {
		return nil
	}

	refs, err := tableForeignKeys(tx, entityType)
	if err != nil || len(refs) == 0 {
		return err
	}

	for _, ref := range refs {
		raw, ok := fields[ref.Column]
		if !ok || raw == nil {
			continue // absent or NULL — an FK is not enforced against NULL
		}
		parentID, ok := parentKeyString(raw)
		if !ok || parentID == "" {
			continue
		}

		var exists int
		q := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = ? LIMIT 1", ref.ParentTable, ref.ParentCol)
		switch err := tx.QueryRow(q, parentID).Scan(&exists); err {
		case nil:
			continue
		case sql.ErrNoRows:
			return &OrphanedParentError{
				EntityType:  entityType,
				EntityID:    stringField(fields, "id"),
				Column:      ref.Column,
				ParentTable: ref.ParentTable,
				ParentID:    parentID,
			}
		default:
			// Could not check (e.g. the parent table is absent on this peer).
			// Fall through and let SQLite decide; a real violation is then
			// quarantined rather than dropped.
			continue
		}
	}
	return nil
}

// parentKeyString renders a JSON-decoded payload value as an FK lookup key.
func parentKeyString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%.0f", t), "-0"), true
	case int64:
		return fmt.Sprintf("%d", t), true
	default:
		return "", false
	}
}

func stringField(fields map[string]any, key string) string {
	if s, ok := fields[key].(string); ok {
		return s
	}
	return ""
}
