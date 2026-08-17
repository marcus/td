package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// ListNotesOptions contains filter options for listing notes
type ListNotesOptions struct {
	Pinned         *bool  // nil = don't filter, true = only pinned, false = only unpinned
	Archived       *bool  // nil = don't filter, true = only archived, false = only unarchived
	IncludeDeleted bool   // include soft-deleted notes
	Search         string // search title/content
	Limit          int    // max results; <=0 means unlimited
}

// marshalNote returns a JSON representation of a note for action_log storage.
func marshalNote(note *models.Note) string {
	data, _ := json.Marshal(note)
	return string(data)
}

// parseNoteDeletedAt treats any non-empty deleted_at as deleted.
// Historical rows were written as time.Time.String() or with a space instead of T.
func parseNoteDeletedAt(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05+00:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t
		}
	}
	t := time.Unix(0, 0).UTC()
	return &t
}

// scanNoteRow reads a note row from the DB. Caller must hold write lock if used inside withWriteLock.
func (db *DB) scanNoteRow(id string) (*models.Note, error) {
	var note models.Note
	var createdAtStr, updatedAtStr string
	var deletedAt sql.NullString

	err := db.conn.QueryRow(`
		SELECT id, title, content, created_at, updated_at, pinned, archived, deleted_at
		FROM notes WHERE id = ?
	`, id).Scan(
		&note.ID, &note.Title, &note.Content, &createdAtStr, &updatedAtStr,
		&note.Pinned, &note.Archived, &deletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("note not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	note.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	note.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)

	if deletedAt.Valid {
		note.DeletedAt = parseNoteDeletedAt(deletedAt.String)
	}

	return &note, nil
}

// CreateNote creates a new note and logs the action for undo support.
func (db *DB) CreateNote(title, content string) (*models.Note, error) {
	var note models.Note
	err := db.withWriteLock(func() error {
		now := time.Now()
		note.Title = title
		note.Content = content
		note.CreatedAt = now
		note.UpdatedAt = now

		const maxRetries = 3
		for attempt := range maxRetries {
			id, err := generateNoteID()
			if err != nil {
				return err
			}
			note.ID = id

			_, err = db.conn.Exec(`
				INSERT INTO notes (id, title, content, created_at, updated_at, pinned, archived)
				VALUES (?, ?, ?, ?, ?, 0, 0)
			`, note.ID, note.Title, note.Content, note.CreatedAt.Format(time.RFC3339), note.UpdatedAt.Format(time.RFC3339))

			if err == nil {
				break
			}
			if !strings.Contains(err.Error(), "UNIQUE constraint") {
				return err
			}
			if attempt == maxRetries-1 {
				return fmt.Errorf("failed to generate unique note ID after %d attempts", maxRetries)
			}
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData := marshalNote(&note)
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionCreate), "note", note.ID, "", newData, actionTS)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return &note, nil
}

// GetNote retrieves a note by ID. Returns error if not found or soft-deleted.
func (db *DB) GetNote(id string) (*models.Note, error) {
	note, err := db.scanNoteRow(id)
	if err != nil {
		return nil, err
	}
	if note.DeletedAt != nil {
		return nil, fmt.Errorf("note not found: %s", id)
	}
	return note, nil
}

// GetNoteIncludingDeleted retrieves a note by ID, including soft-deleted rows.
func (db *DB) GetNoteIncludingDeleted(id string) (*models.Note, error) {
	return db.scanNoteRow(id)
}

// ListNotes returns notes matching the filter options.
func (db *DB) ListNotes(opts ListNotesOptions) ([]models.Note, error) {
	query := `SELECT id, title, content, created_at, updated_at, pinned, archived, deleted_at
	          FROM notes WHERE 1=1`
	var args []any

	// Soft-delete filter
	if !opts.IncludeDeleted {
		query += " AND deleted_at IS NULL"
	}

	// Pinned filter
	if opts.Pinned != nil {
		if *opts.Pinned {
			query += " AND pinned = 1"
		} else {
			query += " AND pinned = 0"
		}
	}

	// Archived filter
	if opts.Archived != nil {
		if *opts.Archived {
			query += " AND archived = 1"
		} else {
			query += " AND archived = 0"
		}
	}

	// Search filter
	if opts.Search != "" {
		query += " AND (title LIKE ? OR content LIKE ?)"
		searchPattern := "%" + opts.Search + "%"
		args = append(args, searchPattern, searchPattern)
	}

	query += " ORDER BY pinned DESC, updated_at DESC"

	// Limit<=0 means unlimited (pkg/notes and the Sidecar UI list everything).
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []models.Note
	for rows.Next() {
		var note models.Note
		var createdAtStr, updatedAtStr string
		var deletedAt sql.NullString

		err := rows.Scan(
			&note.ID, &note.Title, &note.Content, &createdAtStr, &updatedAtStr,
			&note.Pinned, &note.Archived, &deletedAt,
		)
		if err != nil {
			return nil, err
		}

		note.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		note.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)

		if deletedAt.Valid {
			note.DeletedAt = parseNoteDeletedAt(deletedAt.String)
		}

		notes = append(notes, note)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

// UpdateNote updates a note's title and content, and logs the action for undo support.
func (db *DB) UpdateNote(id, title, content string) (*models.Note, error) {
	var updated models.Note
	err := db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanNoteRow(id)
		if err != nil {
			return err
		}
		if prev.DeletedAt != nil {
			return fmt.Errorf("note not found: %s", id)
		}
		previousData := marshalNote(prev)

		now := time.Now()
		_, err = db.conn.Exec(`
			UPDATE notes SET title = ?, content = ?, updated_at = ? WHERE id = ?
		`, title, content, now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}

		updated = *prev
		updated.Title = title
		updated.Content = content
		updated.UpdatedAt = now

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData := marshalNote(&updated)
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionUpdate), "note", id, previousData, newData, actionTS)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteNote soft-deletes a note and logs the action for undo support.
func (db *DB) DeleteNote(id string) error {
	return db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanNoteRow(id)
		if err != nil {
			return err
		}
		if prev.DeletedAt != nil {
			return fmt.Errorf("note not found: %s", id)
		}
		previousData := marshalNote(prev)

		now := time.Now()
		_, err = db.conn.Exec(`UPDATE notes SET deleted_at = ?, updated_at = ? WHERE id = ?`,
			now.Format(time.RFC3339), now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		actionTS := formatActionLogTimestamp(now)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionDelete), "note", id, previousData, "", actionTS)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// PinNote sets a note's pinned status to true.
func (db *DB) PinNote(id string) error {
	return db.setNoteFlag(id, "pinned", true)
}

// UnpinNote sets a note's pinned status to false.
func (db *DB) UnpinNote(id string) error {
	return db.setNoteFlag(id, "pinned", false)
}

// ArchiveNote sets a note's archived status to true.
func (db *DB) ArchiveNote(id string) error {
	return db.setNoteFlag(id, "archived", true)
}

// UnarchiveNote sets a note's archived status to false.
func (db *DB) UnarchiveNote(id string) error {
	return db.setNoteFlag(id, "archived", false)
}

// RestoreNote clears deleted_at on a soft-deleted note and logs the action.
func (db *DB) RestoreNote(id string) (*models.Note, error) {
	var restored models.Note
	err := db.withWriteLock(func() error {
		prev, err := db.scanNoteRow(id)
		if err != nil {
			return err
		}
		if prev.DeletedAt == nil {
			return fmt.Errorf("note not deleted: %s", id)
		}
		previousData := marshalNote(prev)

		now := time.Now()
		_, err = db.conn.Exec(`UPDATE notes SET deleted_at = NULL, updated_at = ? WHERE id = ?`,
			now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}

		restored = *prev
		restored.DeletedAt = nil
		restored.UpdatedAt = now
		return db.insertNoteAction(string(models.ActionRestore), id, previousData, marshalNote(&restored), now)
	})
	if err != nil {
		return nil, err
	}
	return &restored, nil
}

func (db *DB) setNoteFlag(id, column string, value bool) error {
	if column != "pinned" && column != "archived" {
		return fmt.Errorf("unsupported note flag %q", column)
	}
	return db.withWriteLock(func() error {
		prev, err := db.scanNoteRow(id)
		if err != nil {
			return err
		}
		if prev.DeletedAt != nil {
			return fmt.Errorf("note not found: %s", id)
		}
		previousData := marshalNote(prev)

		now := time.Now()
		flag := 0
		if value {
			flag = 1
		}
		_, err = db.conn.Exec(
			`UPDATE notes SET `+column+` = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			flag, now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}

		next := *prev
		next.UpdatedAt = now
		switch column {
		case "pinned":
			next.Pinned = value
		case "archived":
			next.Archived = value
		}
		return db.insertNoteAction(string(models.ActionUpdate), id, previousData, marshalNote(&next), now)
	})
}

func (db *DB) insertNoteAction(actionType, noteID, previousData, newData string, now time.Time) error {
	actionID, err := generateActionID()
	if err != nil {
		return fmt.Errorf("generate action ID: %w", err)
	}
	_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		actionID, "", actionType, "note", noteID, previousData, newData, formatActionLogTimestamp(now))
	if err != nil {
		return fmt.Errorf("log action: %w", err)
	}
	return nil
}
