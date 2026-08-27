package serverdb

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

// --- NormalizeLimit tests ---

func TestNormalizeLimitZero(t *testing.T) {
	if got := NormalizeLimit(0); got != DefaultPageLimit {
		t.Fatalf("expected %d, got %d", DefaultPageLimit, got)
	}
}

func TestNormalizeLimitNegative(t *testing.T) {
	if got := NormalizeLimit(-5); got != DefaultPageLimit {
		t.Fatalf("expected %d, got %d", DefaultPageLimit, got)
	}
}

func TestNormalizeLimitNormal(t *testing.T) {
	if got := NormalizeLimit(25); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
}

func TestNormalizeLimitExceedsMax(t *testing.T) {
	if got := NormalizeLimit(500); got != MaxPageLimit {
		t.Fatalf("expected %d, got %d", MaxPageLimit, got)
	}
}

func TestNormalizeLimitAtMax(t *testing.T) {
	if got := NormalizeLimit(MaxPageLimit); got != MaxPageLimit {
		t.Fatalf("expected %d, got %d", MaxPageLimit, got)
	}
}

// --- EncodeCursor/DecodeCursor tests ---

func TestCursorRoundtrip(t *testing.T) {
	original := CursorData{ID: "abc123", CreatedAt: "2025-01-01T00:00:00Z", Value: "val"}
	encoded := EncodeCursor(original)
	if encoded == "" {
		t.Fatal("encoded cursor should not be empty")
	}
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ID != original.ID || decoded.CreatedAt != original.CreatedAt || decoded.Value != original.Value {
		t.Fatalf("roundtrip mismatch: got %+v", decoded)
	}
}

func TestDecodeCursorEmpty(t *testing.T) {
	data, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.ID != "" || data.Value != "" {
		t.Fatal("expected zero CursorData for empty string")
	}
}

func TestDecodeCursorInvalidBase64(t *testing.T) {
	_, err := DecodeCursor("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeCursorInvalidJSON(t *testing.T) {
	// Valid base64 but not valid JSON
	_, err := DecodeCursor("bm90LWpzb24=") // "not-json"
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- PaginatedQuery tests ---

func newPaginationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func insertItems(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		_, err := db.Exec("INSERT INTO items (id, name) VALUES (?, ?)", i, fmt.Sprintf("item-%d", i))
		if err != nil {
			t.Fatalf("insert item %d: %v", i, err)
		}
	}
}

func scanItem(rows *sql.Rows) (string, string, error) {
	var id int
	var name string
	if err := rows.Scan(&id, &name); err != nil {
		return "", "", err
	}
	return name, fmt.Sprintf("%d", id), nil
}

func TestPaginatedQueryEmptyTable(t *testing.T) {
	db := newPaginationTestDB(t)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 10, "", "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 0 {
		t.Fatalf("expected 0 items, got %d", len(result.Data))
	}
	if result.HasMore {
		t.Fatal("expected no more items")
	}
	if result.NextCursor != "" {
		t.Fatal("expected empty cursor")
	}
}

func TestPaginatedQuerySinglePage(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 5)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 10, "", "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 5 {
		t.Fatalf("expected 5 items, got %d", len(result.Data))
	}
	if result.HasMore {
		t.Fatal("should not have more")
	}
	if result.NextCursor != "" {
		t.Fatal("should not have cursor")
	}
	// Verify ordering
	if result.Data[0] != "item-1" || result.Data[4] != "item-5" {
		t.Fatalf("unexpected ordering: %v", result.Data)
	}
}

func TestPaginatedQueryMultiPage(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 7)

	// First page: limit 3
	page1, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, "", "id", scanItem)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Data) != 3 {
		t.Fatalf("page1: expected 3 items, got %d", len(page1.Data))
	}
	if !page1.HasMore {
		t.Fatal("page1: should have more")
	}
	if page1.NextCursor == "" {
		t.Fatal("page1: should have cursor")
	}
	if page1.Data[0] != "item-1" || page1.Data[2] != "item-3" {
		t.Fatalf("page1: unexpected data: %v", page1.Data)
	}

	// Second page
	page2, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, page1.NextCursor, "id", scanItem)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Data) != 3 {
		t.Fatalf("page2: expected 3 items, got %d", len(page2.Data))
	}
	if !page2.HasMore {
		t.Fatal("page2: should have more")
	}
	if page2.Data[0] != "item-4" || page2.Data[2] != "item-6" {
		t.Fatalf("page2: unexpected data: %v", page2.Data)
	}

	// Third page (last)
	page3, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, page2.NextCursor, "id", scanItem)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3.Data) != 1 {
		t.Fatalf("page3: expected 1 item, got %d", len(page3.Data))
	}
	if page3.HasMore {
		t.Fatal("page3: should not have more")
	}
	if page3.NextCursor != "" {
		t.Fatal("page3: should not have cursor")
	}
	if page3.Data[0] != "item-7" {
		t.Fatalf("page3: unexpected data: %v", page3.Data)
	}
}

func TestPaginatedQueryExactlyLimitRows(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 3)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, "", "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Data))
	}
	if result.HasMore {
		t.Fatal("should not have more when rows == limit")
	}
	if result.NextCursor != "" {
		t.Fatal("should not have cursor")
	}
}

func TestPaginatedQueryMaxLimitEnforced(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 5)

	// Request 500 but should be capped
	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 500, "", "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 5 {
		t.Fatalf("expected 5 items, got %d", len(result.Data))
	}
}

func TestPaginatedQueryInvalidCursor(t *testing.T) {
	db := newPaginationTestDB(t)

	_, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 10, "garbage!!!", "id", scanItem)
	if err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestPaginatedQueryWithWhereClause(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 10)

	// Query with existing WHERE clause
	result, err := PaginatedQuery(
		db,
		"SELECT id, name FROM items WHERE id <= 7",
		nil,
		3,
		"",
		"id",
		scanItem,
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Data))
	}
	if !result.HasMore {
		t.Fatal("should have more")
	}

	// Second page with WHERE
	page2, err := PaginatedQuery(
		db,
		"SELECT id, name FROM items WHERE id <= 7",
		nil,
		3,
		result.NextCursor,
		"id",
		scanItem,
	)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Data) != 3 {
		t.Fatalf("page2: expected 3, got %d", len(page2.Data))
	}
	if !page2.HasMore {
		t.Fatal("page2: should have more")
	}

	// Third page
	page3, err := PaginatedQuery(
		db,
		"SELECT id, name FROM items WHERE id <= 7",
		nil,
		3,
		page2.NextCursor,
		"id",
		scanItem,
	)
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3.Data) != 1 {
		t.Fatalf("page3: expected 1, got %d", len(page3.Data))
	}
	if page3.HasMore {
		t.Fatal("page3: should not have more")
	}
}

func TestPaginatedQueryWithArgs(t *testing.T) {
	db := newPaginationTestDB(t)
	insertItems(t, db, 10)

	// Query with parameterized WHERE
	result, err := PaginatedQuery(
		db,
		"SELECT id, name FROM items WHERE id >= ?",
		[]any{3},
		4,
		"",
		"id",
		scanItem,
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 4 {
		t.Fatalf("expected 4, got %d", len(result.Data))
	}
	if result.Data[0] != "item-3" {
		t.Fatalf("expected item-3, got %s", result.Data[0])
	}
	if !result.HasMore {
		t.Fatal("should have more")
	}
}

func TestPaginatedQueryDataNeverNil(t *testing.T) {
	db := newPaginationTestDB(t)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 10, "", "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Data == nil {
		t.Fatal("Data should be empty slice, not nil")
	}
}

func TestCursorDataIDFallback(t *testing.T) {
	// When Value is empty but ID is set, cursor should use ID
	cursor := EncodeCursor(CursorData{ID: "5"})
	db := newPaginationTestDB(t)
	insertItems(t, db, 10)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, cursor, "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3, got %d", len(result.Data))
	}
	if result.Data[0] != "item-6" {
		t.Fatalf("expected item-6, got %s", result.Data[0])
	}
}

func TestCursorDataCreatedAtFallback(t *testing.T) {
	// When Value and ID are empty but CreatedAt is set
	cursor := EncodeCursor(CursorData{CreatedAt: "3"})
	db := newPaginationTestDB(t)
	insertItems(t, db, 10)

	result, err := PaginatedQuery(db, "SELECT id, name FROM items", nil, 3, cursor, "id", scanItem)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3, got %d", len(result.Data))
	}
	if result.Data[0] != "item-4" {
		t.Fatalf("expected item-4, got %s", result.Data[0])
	}
}
