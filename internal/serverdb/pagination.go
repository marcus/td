package serverdb

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// PaginatedResult holds a page of results with cursor information.
type PaginatedResult[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// CursorData holds the opaque cursor state.
type CursorData struct {
	ID        string `json:"id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Value     string `json:"value,omitempty"`
}

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// NormalizeLimit clamps limit to valid range.
func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// EncodeCursor encodes cursor data to an opaque base64 string.
func EncodeCursor(data CursorData) string {
	b, _ := json.Marshal(data)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeCursor decodes an opaque cursor string back to CursorData.
func DecodeCursor(cursor string) (CursorData, error) {
	var data CursorData
	if cursor == "" {
		return data, nil
	}
	b, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return data, fmt.Errorf("invalid cursor")
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return data, fmt.Errorf("invalid cursor")
	}
	return data, nil
}

// PaginatedQuery executes a paginated query using cursor-based pagination.
//
// baseQuery is the SELECT query without ORDER BY or LIMIT clauses.
// args contains any existing WHERE clause parameters.
// cursor is the opaque cursor string from a previous PaginatedResult.NextCursor.
// cursorColumn is the column used for ordering and cursor comparison (e.g. "id", "created_at").
// scanRow scans a single row and returns (item, cursorValue, error) where cursorValue
// is the value to encode in the next cursor.
//
// The function fetches limit+1 rows to determine HasMore without a separate COUNT query.
func PaginatedQuery[T any](
	db *sql.DB,
	baseQuery string,
	args []any,
	limit int,
	cursor string,
	cursorColumn string,
	scanRow func(*sql.Rows) (T, string, error),
) (*PaginatedResult[T], error) {
	limit = NormalizeLimit(limit)

	cursorData, err := DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}

	query := baseQuery

	// Determine the cursor value to use for comparison
	cursorVal := cursorData.Value
	if cursorVal == "" {
		cursorVal = cursorData.ID
	}
	if cursorVal == "" {
		cursorVal = cursorData.CreatedAt
	}

	if cursorVal != "" {
		if !strings.Contains(strings.ToUpper(query), "WHERE") {
			query += " WHERE " + cursorColumn + " > ?"
		} else {
			query += " AND " + cursorColumn + " > ?"
		}
		args = append(args, cursorVal)
	}

	query += fmt.Sprintf(" ORDER BY %s ASC LIMIT %d", cursorColumn, limit+1)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("pagination query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []T
	var cursorVals []string
	for rows.Next() {
		item, cv, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		items = append(items, item)
		cursorVals = append(cursorVals, cv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		cursorVals = cursorVals[:limit]
	}

	result := &PaginatedResult[T]{
		Data:    items,
		HasMore: hasMore,
	}
	if result.Data == nil {
		result.Data = []T{}
	}
	if hasMore && len(cursorVals) > 0 {
		result.NextCursor = EncodeCursor(CursorData{Value: cursorVals[len(cursorVals)-1]})
	}

	return result, nil
}
