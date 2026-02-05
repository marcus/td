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

// CreateBoard creates a new board with a TDQ query WITHOUT logging to action_log.
// For local mutations, use CreateBoardLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
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
		ORDER BY created_at ASC LIMIT 1
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

// UpdateBoard updates a board's name and/or query WITHOUT logging to action_log.
// For local mutations, use UpdateBoardLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
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

// DeleteBoard deletes a board (fails for builtin boards) WITHOUT logging to action_log.
// For local mutations, use DeleteBoardLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
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

		// Soft-delete positions first
		_, err = db.conn.Exec(`UPDATE board_issue_positions SET deleted_at = ? WHERE board_id = ? AND deleted_at IS NULL`, time.Now().UTC(), id)
		if err != nil {
			return err
		}

		// Delete board
		_, err = db.conn.Exec(`DELETE FROM boards WHERE id = ?`, id)
		return err
	})
}

// RestoreBoard re-inserts a previously deleted board from its stored state.
// This is used by undo to restore a deleted board.
func (db *DB) RestoreBoard(board *models.Board) error {
	return db.withWriteLock(func() error {
		isBuiltin := 0
		if board.IsBuiltin {
			isBuiltin = 1
		}
		_, err := db.conn.Exec(`
			INSERT INTO boards (id, name, query, is_builtin, view_mode, last_viewed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, board.ID, board.Name, board.Query, isBuiltin, board.ViewMode, board.LastViewedAt, board.CreatedAt, board.UpdatedAt)
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

// PositionGap is the spacing between sparse sort keys for board positions.
const PositionGap = 65536

// RespaceResult records a position change made during a respace operation.
type RespaceResult struct {
	IssueID     string
	OldPosition int
	NewPosition int
}

// BoardIssuePosition represents an explicit position for an issue on a board
type BoardIssuePosition struct {
	BoardID  string
	IssueID  string
	Position int
}

// SetIssuePosition sets an explicit sort-key position for an issue on a board.
// This directly sets the position value without shifting other rows.
// WARNING: This does NOT log to action_log.
// For local mutations, use SetIssuePositionLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) SetIssuePosition(boardID, issueID string, position int) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
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
			// Update existing row: set new position and clear deleted_at
			_, err = tx.Exec(`UPDATE board_issue_positions SET position = ?, deleted_at = NULL, added_at = ? WHERE board_id = ? AND issue_id = ?`,
				position, time.Now(), boardID, issueID)
		} else {
			// Insert new row
			bipID := BoardIssuePosID(boardID, issueID)
			_, err = tx.Exec(`
				INSERT INTO board_issue_positions (id, board_id, issue_id, position, added_at)
				VALUES (?, ?, ?, ?, ?)
			`, bipID, boardID, issueID, position, time.Now())
		}
		if err != nil {
			return err
		}

		return tx.Commit()
	})
}

// ComputeInsertPosition computes a sparse sort key for inserting at visual slot (1-based).
// If the board is empty, returns PositionGap.
// If slot <= 1 (top): returns min_position - PositionGap.
// If slot > count (bottom): returns max_position + PositionGap.
// Otherwise: returns midpoint of positions[slot-2] and positions[slot-1].
// If the gap between neighbors is < 2, calls RespaceBoardPositions first and recomputes.
func (db *DB) ComputeInsertPosition(boardID string, slot int) (int, []RespaceResult, error) {
	positions, err := db.queryBoardPositionsSorted(boardID)
	if err != nil {
		return 0, nil, err
	}

	if len(positions) == 0 {
		return PositionGap, nil, nil
	}

	if slot <= 1 {
		return positions[0].Position - PositionGap, nil, nil
	}

	if slot > len(positions) {
		return positions[len(positions)-1].Position + PositionGap, nil, nil
	}

	lo := positions[slot-2].Position
	hi := positions[slot-1].Position
	mid := (lo + hi) / 2

	if mid == lo || mid == hi {
		// Gap exhausted, respace and recompute
		results, err := db.RespaceBoardPositions(boardID)
		if err != nil {
			return 0, nil, fmt.Errorf("respace failed: %w", err)
		}

		positions, err = db.queryBoardPositionsSorted(boardID)
		if err != nil {
			return 0, results, err
		}

		if slot > len(positions) {
			return positions[len(positions)-1].Position + PositionGap, results, nil
		}

		lo = positions[slot-2].Position
		hi = positions[slot-1].Position
		mid = (lo + hi) / 2
		return mid, results, nil
	}

	return mid, nil, nil
}

// queryBoardPositionsSorted returns all board positions sorted by position ASC.
func (db *DB) queryBoardPositionsSorted(boardID string) ([]BoardIssuePosition, error) {
	rows, err := db.conn.Query(`
		SELECT board_id, issue_id, position
		FROM board_issue_positions
		WHERE board_id = ? AND deleted_at IS NULL
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

// RespaceBoardPositions reassigns all positions on a board with fresh PositionGap gaps.
func (db *DB) RespaceBoardPositions(boardID string) ([]RespaceResult, error) {
	var results []RespaceResult
	err := db.withWriteLock(func() error {
		tx, err := db.conn.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		rows, err := tx.Query(`
			SELECT issue_id, position
			FROM board_issue_positions
			WHERE board_id = ? AND deleted_at IS NULL
			ORDER BY position ASC
		`, boardID)
		if err != nil {
			return err
		}

		type entry struct {
			issueID     string
			oldPosition int
		}
		var entries []entry
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.issueID, &e.oldPosition); err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, e)
		}
		rows.Close()

		for i, e := range entries {
			newPos := (i + 1) * PositionGap
			_, err := tx.Exec(`
				UPDATE board_issue_positions SET position = ?
				WHERE board_id = ? AND issue_id = ?
			`, newPos, boardID, e.issueID)
			if err != nil {
				return err
			}
			results = append(results, RespaceResult{
				IssueID:     e.issueID,
				OldPosition: e.oldPosition,
				NewPosition: newPos,
			})
		}

		return tx.Commit()
	})
	return results, err
}

// RemoveIssuePosition soft-deletes an explicit position for an issue by setting deleted_at.
// WARNING: This does NOT log to action_log.
// For local mutations, use RemoveIssuePositionLogged instead.
// This unlogged variant exists for sync receiver applying remote events.
func (db *DB) RemoveIssuePosition(boardID, issueID string) error {
	issueID = NormalizeIssueID(issueID)
	return db.withWriteLock(func() error {
		_, err := db.conn.Exec(`UPDATE board_issue_positions SET deleted_at = ? WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			time.Now().UTC(), boardID, issueID)
		return err
	})
}

// GetIssuePosition returns the current position of an issue on a board, or 0 if not positioned.
func (db *DB) GetIssuePosition(boardID, issueID string) (int, error) {
	issueID = NormalizeIssueID(issueID)
	var pos sql.NullInt64
	err := db.conn.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`, boardID, issueID).Scan(&pos)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if pos.Valid {
		return int(pos.Int64), nil
	}
	return 0, nil
}

// GetBoardIssuePositions returns all explicit (non-deleted) positions for a board
func (db *DB) GetBoardIssuePositions(boardID string) ([]BoardIssuePosition, error) {
	rows, err := db.conn.Query(`
		SELECT board_id, issue_id, position
		FROM board_issue_positions
		WHERE board_id = ? AND deleted_at IS NULL
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
		SELECT MAX(position) FROM board_issue_positions WHERE board_id = ? AND deleted_at IS NULL
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
		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			boardID, id1).Scan(&pos1)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id1)
		}

		err = tx.QueryRow(`SELECT position FROM board_issue_positions WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			boardID, id2).Scan(&pos2)
		if err != nil {
			return fmt.Errorf("issue %s not positioned on board", id2)
		}

		// Swap positions directly (no UNIQUE constraint on position)
		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			pos2, boardID, id1)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`UPDATE board_issue_positions SET position = ? WHERE board_id = ? AND issue_id = ? AND deleted_at IS NULL`,
			pos1, boardID, id2)
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
