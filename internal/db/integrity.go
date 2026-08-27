package db

import (
	"fmt"
	"strings"
)

// QuickCheck verifies SQLite's b-tree and index invariants without mutating the
// database. It is cheap enough to run before sync, where continuing after
// corruption could otherwise upload garbage produced by orphan backfill.
func (db *DB) QuickCheck() error {
	rows, err := db.conn.Query("PRAGMA quick_check")
	if err != nil {
		return fmt.Errorf("run SQLite quick_check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var problems []string
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return fmt.Errorf("read SQLite quick_check: %w", err)
		}
		if result != "ok" && len(problems) < 5 {
			problems = append(problems, result)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read SQLite quick_check: %w", err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("SQLite integrity check failed: %s", strings.Join(problems, "; "))
	}
	return nil
}
