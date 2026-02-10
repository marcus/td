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
	Limit          int    // max results (default 50 if 0)
}

// marshalNote returns a JSON representation of a note for action_log storage.
func marshalNote(note *models.Note) string {
	data, _ := json.Marshal(note)
	return string(data)
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

	if deletedAt.Valid && deletedAt.String != "" {
		t, err := time.Parse(time.RFC3339, deletedAt.String)
		if err == nil {
			note.DeletedAt = &t
		}
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
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionCreate), "note", note.ID, "", newData, now)
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

	// Limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)

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

		if deletedAt.Valid && deletedAt.String != "" {
			t, err := time.Parse(time.RFC3339, deletedAt.String)
			if err == nil {
				note.DeletedAt = &t
			}
		}

		notes = append(notes, note)
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
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionUpdate), "note", id, previousData, newData, now)
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
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, "", string(models.ActionDelete), "note", id, previousData, "", now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// PinNote sets a note's pinned status to true.
func (db *DB) PinNote(id string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		result, err := db.conn.Exec(`UPDATE notes SET pinned = 1, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("note not found: %s", id)
		}
		return nil
	})
}

// UnpinNote sets a note's pinned status to false.
func (db *DB) UnpinNote(id string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		result, err := db.conn.Exec(`UPDATE notes SET pinned = 0, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("note not found: %s", id)
		}
		return nil
	})
}

// ArchiveNote sets a note's archived status to true.
func (db *DB) ArchiveNote(id string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		result, err := db.conn.Exec(`UPDATE notes SET archived = 1, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("note not found: %s", id)
		}
		return nil
	})
}

// UnarchiveNote sets a note's archived status to false.
func (db *DB) UnarchiveNote(id string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		result, err := db.conn.Exec(`UPDATE notes SET archived = 0, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			now.Format(time.RFC3339), id)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("note not found: %s", id)
		}
		return nil
	})
}
