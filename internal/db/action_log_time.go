package db

import "time"

// actionLogTimestampNow returns the canonical action_log timestamp format:
// UTC RFC3339Nano text for reliable SQLite lexicographic comparisons.
func actionLogTimestampNow() string {
	return formatActionLogTimestamp(time.Now())
}

func formatActionLogTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
