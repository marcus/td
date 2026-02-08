package serverdb

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RateLimitEvent represents a rate limit violation event.
type RateLimitEvent struct {
	ID            int64
	KeyID         string // empty string if IP-based (nullable in DB)
	IP            string
	EndpointClass string // auth, push, pull, other
	CreatedAt     string
}

// InsertRateLimitEvent inserts a rate limit violation event.
// keyID may be empty for IP-based rate limiting (stored as NULL).
func (db *ServerDB) InsertRateLimitEvent(keyID, ip, endpointClass string) error {
	var keyIDParam any
	if keyID == "" {
		keyIDParam = nil
	} else {
		keyIDParam = keyID
	}
	_, err := db.conn.Exec(
		`INSERT INTO rate_limit_events (key_id, ip, endpoint_class) VALUES (?, ?, ?)`,
		keyIDParam, ip, endpointClass,
	)
	if err != nil {
		return fmt.Errorf("insert rate limit event: %w", err)
	}
	return nil
}

// QueryRateLimitEvents queries rate limit events with optional filters.
// Filters: keyID (exact), ip (exact), from/to (created_at range, RFC3339 or datetime).
// Uses cursor-based pagination via the PaginatedQuery helper.
func (db *ServerDB) QueryRateLimitEvents(keyID, ip, from, to string, limit int, cursor string) (*PaginatedResult[RateLimitEvent], error) {
	query := "SELECT id, key_id, ip, endpoint_class, created_at FROM rate_limit_events"
	var conditions []string
	var args []any

	if keyID != "" {
		conditions = append(conditions, "key_id = ?")
		args = append(args, keyID)
	}
	if ip != "" {
		conditions = append(conditions, "ip = ?")
		args = append(args, ip)
	}
	if from != "" {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, from)
	}
	if to != "" {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, to)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	scanRow := func(rows *sql.Rows) (RateLimitEvent, string, error) {
		var e RateLimitEvent
		var keyIDNull sql.NullString
		if err := rows.Scan(&e.ID, &keyIDNull, &e.IP, &e.EndpointClass, &e.CreatedAt); err != nil {
			return e, "", err
		}
		if keyIDNull.Valid {
			e.KeyID = keyIDNull.String
		}
		return e, fmt.Sprintf("%d", e.ID), nil
	}

	return PaginatedQuery(db.conn, query, args, limit, cursor, "id", scanRow)
}

// CleanupRateLimitEvents deletes events older than the given duration.
// Returns the number of rows deleted.
func (db *ServerDB) CleanupRateLimitEvents(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := db.conn.Exec(
		`DELETE FROM rate_limit_events WHERE created_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup rate limit events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
