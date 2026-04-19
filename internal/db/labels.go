package db

import (
	"fmt"
	"sort"
	"strings"
)

// ListDistinctLabels returns the sorted set of labels found on non-deleted
// issues in the current database.
func (db *DB) ListDistinctLabels() ([]string, error) {
	rows, err := db.conn.Query(`
		SELECT labels
		FROM issues
		WHERE deleted_at IS NULL
		  AND labels IS NOT NULL
		  AND labels != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("query labels: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	labels := make([]string, 0)

	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan labels: %w", err)
		}
		for _, label := range strings.Split(raw, ",") {
			label = strings.TrimSpace(label)
			if label == "" {
				continue
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels: %w", err)
	}

	sort.Strings(labels)
	if labels == nil {
		return []string{}, nil
	}
	return labels, nil
}
