package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/marcus/td/internal/models"
)

// marshalBoard returns a JSON representation of a board for action_log storage.
func marshalBoard(board *models.Board) string {
	data, _ := json.Marshal(board)
	return string(data)
}

// scanBoardRow reads a board row from the DB within a withWriteLock closure.
func (db *DB) scanBoardRow(id string) (*models.Board, error) {
	board, err := db.GetBoard(id)
	if err != nil {
		return nil, err
	}
	return board, nil
}

// CreateBoardLogged creates a board and logs the action atomically within a single withWriteLock call.
func (db *DB) CreateBoardLogged(name, queryStr, sessionID string) (*models.Board, error) {
	var board *models.Board
	err := db.withWriteLock(func() error {
		// Validate query syntax if not empty
		if queryStr != "" {
			if err := parseAndValidateQuery(queryStr); err != nil {
				return fmt.Errorf("invalid query: %w", err)
			}
		}

		id, err := generateBoardID()
		if err != nil {
			return err
		}

		now := time.Now()
		board = &models.Board{
			ID:        id,
			Name:      name,
			Query:     queryStr,
			IsBuiltin: false,
			ViewMode:  "swimlanes",
			CreatedAt: now,
			UpdatedAt: now,
		}

		_, err = db.conn.Exec(`
			INSERT INTO boards (id, name, query, is_builtin, view_mode, created_at, updated_at)
			VALUES (?, ?, ?, 0, ?, ?, ?)
		`, board.ID, board.Name, board.Query, board.ViewMode, board.CreatedAt, board.UpdatedAt)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData := marshalBoard(board)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionBoardCreate), "board", board.ID, "", newData, now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
	return board, err
}

// UpdateBoardLogged updates a board and logs the action atomically within a single withWriteLock call.
func (db *DB) UpdateBoardLogged(board *models.Board, sessionID string) error {
	return db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanBoardRow(board.ID)
		if err != nil {
			return err
		}
		previousData := marshalBoard(prev)

		// Check if builtin
		if prev.IsBuiltin {
			return fmt.Errorf("cannot modify builtin board")
		}

		// Validate query if provided
		if board.Query != "" {
			if err := parseAndValidateQuery(board.Query); err != nil {
				return fmt.Errorf("invalid query: %w", err)
			}
		}

		board.UpdatedAt = time.Now()
		_, err = db.conn.Exec(`
			UPDATE boards SET name = ?, query = ?, updated_at = ?
			WHERE id = ?
		`, board.Name, board.Query, board.UpdatedAt, board.ID)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		newData := marshalBoard(board)
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionBoardUpdate), "board", board.ID, previousData, newData, board.UpdatedAt)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// SetIssuePositionLogged sets an issue's board position and logs the action atomically.
func (db *DB) SetIssuePositionLogged(boardID, issueID string, position int, sessionID string) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		now := time.Now()
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Check if a (possibly soft-deleted) row exists
		var existing int
		err = tx.QueryRow(`SELECT COUNT(*) FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, issueID).Scan(&existing)
		if err != nil {
			return err
		}

		if existing > 0 {
			_, err = tx.Exec(`UPDATE board_issue_positions SET position = ?, deleted_at = NULL, added_at = ? WHERE board_id = ? AND issue_id = ?`,
				position, now, boardID, issueID)
		} else {
			bipID := BoardIssuePosID(boardID, issueID)
			_, err = tx.Exec(`INSERT INTO board_issue_positions (id, board_id, issue_id, position, added_at) VALUES (?, ?, ?, ?, ?)`,
				bipID, boardID, issueID, position, now)
		}
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		bipID := BoardIssuePosID(boardID, issueID)
		newData, _ := json.Marshal(map[string]interface{}{
			"id": bipID, "board_id": boardID, "issue_id": issueID,
			"position": position, "added_at": now.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
		_, err = tx.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionBoardSetPosition), "board_issue_positions", bipID, "", string(newData), now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return tx.Commit()
	})
}

// RemoveIssuePositionLogged soft-deletes an issue's board position and logs the action atomically.
func (db *DB) RemoveIssuePositionLogged(boardID, issueID, sessionID string) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		now := time.Now()

		// Read current position for PreviousData
		var pos int
		err := db.conn.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			boardID, issueID).Scan(&pos)
		if err != nil {
			return fmt.Errorf("issue not positioned on board: %w", err)
		}

		bipID := BoardIssuePosID(boardID, issueID)
		prevData, _ := json.Marshal(map[string]interface{}{
			"id": bipID, "board_id": boardID, "issue_id": issueID,
			"position": pos,
		})

		// Soft-delete
		_, err = db.conn.Exec(`UPDATE board_issue_positions SET deleted_at = ? WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			now.UTC(), boardID, issueID)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionBoardUnposition), "board_issue_positions", bipID, string(prevData), "", now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}

// DeleteBoardLogged deletes a board and logs the action atomically within a single withWriteLock call.
func (db *DB) DeleteBoardLogged(boardID, sessionID string) error {
	return db.withWriteLock(func() error {
		// Read current state for PreviousData
		prev, err := db.scanBoardRow(boardID)
		if err != nil {
			return err
		}
		if prev.IsBuiltin {
			return fmt.Errorf("cannot delete builtin board")
		}
		previousData := marshalBoard(prev)

		// Query active positions before soft-deleting them
		now := time.Now()
		rows, err := db.conn.Query(`SELECT issue_id, position FROM board_issue_positions WHERE board_id = ? AND deleted_at IS NULL`, boardID)
		if err != nil {
			return err
		}
		type posRow struct {
			issueID  string
			position int
		}
		var positions []posRow
		for rows.Next() {
			var r posRow
			if err := rows.Scan(&r.issueID, &r.position); err != nil {
				rows.Close()
				return err
			}
			positions = append(positions, r)
		}
		rows.Close()

		// Soft-delete positions
		_, err = db.conn.Exec(`UPDATE board_issue_positions SET deleted_at = ? WHERE board_id = ? AND deleted_at IS NULL`, now.UTC(), boardID)
		if err != nil {
			return err
		}

		// Log individual soft_delete event for each position row
		for _, p := range positions {
			bipID := BoardIssuePosID(boardID, p.issueID)
			prevData, _ := json.Marshal(map[string]interface{}{
				"id": bipID, "board_id": boardID, "issue_id": p.issueID,
				"position": p.position,
			})
			posActionID, err := generateActionID()
			if err != nil {
				return fmt.Errorf("generate action ID: %w", err)
			}
			_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				posActionID, sessionID, string(models.ActionBoardUnposition), "board_issue_positions", bipID, string(prevData), "", now)
			if err != nil {
				return fmt.Errorf("log position action: %w", err)
			}
		}

		// Delete board
		_, err = db.conn.Exec(`DELETE FROM boards WHERE id = ?`, boardID)
		if err != nil {
			return err
		}

		// Log the action
		actionID, err := generateActionID()
		if err != nil {
			return fmt.Errorf("generate action ID: %w", err)
		}
		_, err = db.conn.Exec(`INSERT INTO action_log (id, session_id, action_type, entity_type, entity_id, previous_data, new_data, timestamp, undone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			actionID, sessionID, string(models.ActionBoardDelete), "board", boardID, previousData, "", now)
		if err != nil {
			return fmt.Errorf("log action: %w", err)
		}

		return nil
	})
}
