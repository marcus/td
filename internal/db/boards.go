package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/marcus/td/internal/models"
)

// ============================================================================
// Board CRUD
// ============================================================================

// parseAndValidateQuery validates TDQ syntax using the registered QueryValidator
func parseAndValidateQuery(queryStr string) error {
	if queryStr == "" {
		return nil
	}
	if QueryValidator == nil {
		return nil // No validator registered, skip validation
	}
	return QueryValidator(queryStr)
}

// CreateBoard creates a new board with a TDQ query
func (db *DB) CreateBoard(name, queryStr string) (*models.Board, error) {
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

		return err
	})
	return board, err
}

// GetBoard retrieves a board by ID
func (db *DB) GetBoard(id string) (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards WHERE id = ?
	`, id).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("board not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// GetBoardByName retrieves a board by name (case-insensitive)
func (db *DB) GetBoardByName(name string) (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards WHERE name = ? COLLATE NOCASE
	`, name).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("board not found: %s", name)
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// ResolveBoardRef resolves a board reference (ID or name)
func (db *DB) ResolveBoardRef(ref string) (*models.Board, error) {
	// Try by ID first
	if strings.HasPrefix(ref, boardIDPrefix) {
		return db.GetBoard(ref)
	}
	// Try by name
	return db.GetBoardByName(ref)
}

// ListBoards returns all boards sorted by last_viewed_at DESC
func (db *DB) ListBoards() ([]models.Board, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards
		ORDER BY CASE WHEN last_viewed_at IS NULL THEN 1 ELSE 0 END, last_viewed_at DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var boards []models.Board
	for rows.Next() {
		var board models.Board
		var isBuiltin int
		var lastViewedAt sql.NullTime

		if err := rows.Scan(
			&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
			&board.CreatedAt, &board.UpdatedAt,
		); err != nil {
			return nil, err
		}

		board.IsBuiltin = isBuiltin == 1
		if lastViewedAt.Valid {
			board.LastViewedAt = &lastViewedAt.Time
		}

		boards = append(boards, board)
	}

	return boards, nil
}

// UpdateBoard updates a board's name and/or query
func (db *DB) UpdateBoard(board *models.Board) error {
	return db.withWriteLock(func() error {
		// Check if builtin
		var isBuiltin int
		err := db.conn.QueryRow(`SELECT is_builtin FROM boards WHERE id = ?`, board.ID).Scan(&isBuiltin)
		if err != nil {
			return fmt.Errorf("board not found: %s", board.ID)
		}
		if isBuiltin == 1 {
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

		return err
	})
}

// DeleteBoard deletes a board (fails for builtin boards)
func (db *DB) DeleteBoard(id string) error {
	return db.withWriteLock(func() error {
		// Check if builtin
		var isBuiltin int
		err := db.conn.QueryRow(`SELECT is_builtin FROM boards WHERE id = ?`, id).Scan(&isBuiltin)
		if err == sql.ErrNoRows {
			return fmt.Errorf("board not found: %s", id)
		}
		if err != nil {
			return err
		}
		if isBuiltin == 1 {
			return fmt.Errorf("cannot delete builtin board")
		}

		// Delete positions first
		_, err = db.conn.Exec(`DELETE FROM board_issue_positions WHERE board_id = ?`, id)
		if err != nil {
			return err
		}

		// Delete board
		_, err = db.conn.Exec(`DELETE FROM boards WHERE id = ?`, id)
		return err
	})
}

// GetLastViewedBoard returns the most recently viewed board
func (db *DB) GetLastViewedBoard() (*models.Board, error) {
	var board models.Board
	var isBuiltin int
	var lastViewedAt sql.NullTime

	err := db.conn.QueryRow(`
		SELECT id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at
		FROM boards
		WHERE last_viewed_at IS NOT NULL
		ORDER BY last_viewed_at DESC
		LIMIT 1
	`).Scan(
		&board.ID, &board.Name, &board.Query, &isBuiltin, &board.ViewMode, &lastViewedAt,
		&board.CreatedAt, &board.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Return the builtin All Issues board
		return db.GetBoard("bd-all-issues")
	}
	if err != nil {
		return nil, err
	}

	board.IsBuiltin = isBuiltin == 1
	if lastViewedAt.Valid {
		board.LastViewedAt = &lastViewedAt.Time
	}

	return &board, nil
}

// UpdateBoardLastViewed updates the last_viewed_at timestamp for a board
func (db *DB) UpdateBoardLastViewed(boardID string) error {
	return db.withWriteLock(func() error {
		now := time.Now()
		_, err := db.conn.Exec(`UPDATE boards SET last_viewed_at = ? WHERE id = ?`, now, boardID)
		return err
	})
}

// UpdateBoardViewMode updates the view_mode for a board (swimlanes or backlog)
func (db *DB) UpdateBoardViewMode(boardID, viewMode string) error {
	if viewMode != "swimlanes" && viewMode != "backlog" {
		return fmt.Errorf("invalid view mode: %s (must be 'swimlanes' or 'backlog')", viewMode)
	}
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE boards SET view_mode = ?, updated_at = ? WHERE id = ?`,
			viewMode, time.Now(), boardID)
		return err
	})
}

// ============================================================================
// Board Issue Positions
// ============================================================================

// BoardIssuePosition represents an explicit position for an issue on a board
type BoardIssuePosition struct {
	BoardID  string
	IssueID  string
	Position int
}

// SetIssuePosition sets an explicit position for an issue on a board
func (db *DB) SetIssuePosition(boardID, issueID string, position int) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Remove existing position for this issue
		_, err = tx.Exec(`DELETE FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, issueID)
		if err != nil {
			return err
		}

		// Shift positions >= target by +1 to make room
		// Use two-step approach to avoid unique constraint violations:
		// 1. Add large offset to positions being shifted (moves them out of conflict range)
		// 2. Subtract offset-1 to get final positions (large+offset -> position+1)
		const shiftOffset = 1000000
		_, err = tx.Exec(`
			UPDATE board_issue_positions
			SET position = position + ?
			WHERE board_id = ? AND position >= ?
		`, shiftOffset, boardID, position)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
			UPDATE board_issue_positions
			SET position = position - ? + 1
			WHERE board_id = ? AND position >= ?
		`, shiftOffset, boardID, shiftOffset)
		if err != nil {
			return err
		}

		// Insert the new position
		_, err = tx.Exec(`
			INSERT INTO board_issue_positions (board_id, issue_id, position, added_at)
			VALUES (?, ?, ?, ?)
		`, boardID, issueID, position, time.Now())
		if err != nil {
			return err
		}

		return tx.Commit()
	})
}

// RemoveIssuePosition removes an explicit position for an issue
func (db *DB) RemoveIssuePosition(boardID, issueID string) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`DELETE FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, issueID)
		return err
	})
}

// GetBoardIssuePositions returns all explicit positions for a board
func (db *DB) GetBoardIssuePositions(boardID string) ([]BoardIssuePosition, error) {
	rows, err := db.conn.Query(`
		SELECT board_id, issue_id, position
		FROM board_issue_positions
		WHERE board_id = ?
		ORDER BY position ASC
	`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []BoardIssuePosition
	for rows.Next() {
		var p BoardIssuePosition
		if err := rows.Scan(&p.BoardID, &p.IssueID, &p.Position); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}

	return positions, nil
}

// GetMaxBoardPosition returns the highest position value for a board, or 0 if no positioned issues.
func (db *DB) GetMaxBoardPosition(boardID string) (int, error) {
	var maxPos sql.NullInt64
	err := db.conn.QueryRow(`
		SELECT MAX(position) FROM board_issue_positions WHERE board_id = ?
	`, boardID).Scan(&maxPos)
	if err != nil {
		return 0, fmt.Errorf("failed to get max position: %w", err)
	}
	if !maxPos.Valid {
		return 0, nil
	}
	return int(maxPos.Int64), nil
}

// SwapIssuePositions swaps the positions of two issues on a board
func (db *DB) SwapIssuePositions(boardID, id1, id2 string) error {
	id1 = NormalizeIssueID(id1)
	id2 = NormalizeIssueID(id2)
	return db.withWriteLock(func() error {
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// Get positions
		var pos1, pos2 int
		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, id1).Scan(&pos1)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id1)
		}

		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ?`,
			boardID, id2).Scan(&pos2)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id2)
		}

		// Swap using a temp position to avoid unique constraint
		_, err = tx.Exec(`UPDATE board_issue_positions SET position = -1 WHERE board_id = ? AND issue_id = ?`,
			boardID, id1)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ?`,
			pos1, boardID, id2)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ?`,
			pos2, boardID, id1)
		if err != nil {
			return err
		}

		return tx.Commit()
	})
}

// ============================================================================
// Board Issues with Positions
// ============================================================================

// GetBoardIssues returns issues for a board with their positions.
// For boards with empty query, it fetches all issues directly.
// For boards with TDQ queries, callers should use ApplyBoardPositions with
// pre-executed query results to avoid circular import issues.
// Issues are returned: positioned first (by position), then unpositioned (by query order).
func (db *DB) GetBoardIssues(boardID, sessionID string, statusFilter []models.Status) ([]models.BoardIssueView, error) {
	// Get the board
	board, err := db.GetBoard(boardID)
	if err != nil {
		return nil, err
	}

	// For boards with queries, callers should use ApplyBoardPositions
	// This function only handles empty-query boards (All Issues) correctly
	if board.Query != "" {
		// Fallback: list all issues with status filter
		// NOTE: This doesn't execute the TDQ query - callers should use
		// query.Execute() + ApplyBoardPositions() for proper TDQ support
		opts := ListIssuesOptions{
			Status: statusFilter,
			SortBy: "priority",
		}
		issues, err := db.ListIssues(opts)
		if err != nil {
			return nil, err
		}
		return db.ApplyBoardPositions(boardID, issues)
	}

	// Empty query matches all issues
	opts := ListIssuesOptions{
		Status: statusFilter,
		SortBy: "priority",
	}
	issues, err := db.ListIssues(opts)
	if err != nil {
		return nil, err
	}

	return db.ApplyBoardPositions(boardID, issues)
}

// ApplyBoardPositions takes a list of issues and applies board positions.
// Issues with explicit positions are sorted by position and returned first,
// followed by unpositioned issues in their original order.
// This function should be used with query.Execute() results for boards with TDQ queries.
func (db *DB) ApplyBoardPositions(boardID string, issues []models.Issue) ([]models.BoardIssueView, error) {
	// Get explicit positions
	positions, err := db.GetBoardIssuePositions(boardID)
	if err != nil {
		return nil, err
	}

	// Build a map of issue ID to position
	positionMap := make(map[string]int)
	for _, p := range positions {
		positionMap[p.IssueID] = p.Position
	}

	// Build result with positioned and unpositioned issues
	var positioned []models.BoardIssueView
	var unpositioned []models.BoardIssueView

	for _, issue := range issues {
		view := models.BoardIssueView{
			BoardID: boardID,
			Issue:   issue,
		}
		if pos, ok := positionMap[issue.ID]; ok {
			view.Position = pos
			view.HasPosition = true
			positioned = append(positioned, view)
		} else {
			unpositioned = append(unpositioned, view)
		}
	}

	// Sort positioned by position
	sort.Slice(positioned, func(i, j int) bool {
		return positioned[i].Position < positioned[j].Position
	})

	// Combine: positioned first, then unpositioned (already in query order)
	result := append(positioned, unpositioned...)
	return result, nil
}
