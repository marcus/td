package serverdb

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// AuthEvent represents a row in the auth_events table.
type AuthEvent struct {
	ID              int64  `json:"id"`
	AuthRequestID   string `json:"auth_request_id"`
	Email           string `json:"email"`
	EventType       string `json:"event_type"`
	Metadata        string `json:"metadata"`
	CreatedAt       string `json:"created_at"`
}

// Auth event type constants.
const (
	AuthEventStarted      = "started"
	AuthEventCodeVerified = "code_verified"
	AuthEventKeyIssued    = "key_issued"
	AuthEventExpired      = "expired"
	AuthEventFailed       = "failed"
)

// InsertAuthEvent inserts an auth event row.
func (db *ServerDB) InsertAuthEvent(authRequestID, email, eventType, metadata string) error {
	if metadata == "" {
		metadata = "{}"
	}
	_, err := db.conn.Exec(
		`INSERT INTO auth_events (auth_request_id, email, event_type, metadata) VALUES (?, ?, ?, ?)`,
		authRequestID, email, eventType, metadata,
	)
	if err != nil {
		return fmt.Errorf("insert auth event: %w", err)
	}
	return nil
}

// QueryAuthEvents queries auth events with optional filters and cursor-based pagination.
// Filters: eventType (exact match), email (LIKE), from/to (created_at range).
func (db *ServerDB) QueryAuthEvents(eventType, email, from, to string, limit int, cursor string) (*PaginatedResult[AuthEvent], error) {
	baseQuery := `SELECT id, auth_request_id, email, event_type, metadata, created_at FROM auth_events`
	var conditions []string
	var args []any

	if eventType != "" {
		conditions = append(conditions, "event_type = ?")
		args = append(args, eventType)
	}
	if email != "" {
		conditions = append(conditions, "email LIKE ?")
		args = append(args, "%"+email+"%")
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
		baseQuery += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				baseQuery += " AND "
			}
			baseQuery += c
		}
	}

	scanRow := func(rows *sql.Rows) (AuthEvent, string, error) {
		var e AuthEvent
		if err := rows.Scan(&e.ID, &e.AuthRequestID, &e.Email, &e.EventType, &e.Metadata, &e.CreatedAt); err != nil {
			return e, "", err
		}
		return e, strconv.FormatInt(e.ID, 10), nil
	}

	return PaginatedQuery(db.conn, baseQuery, args, limit, cursor, "id", scanRow)
}

// CleanupAuthEvents deletes auth events older than the given duration.
// Returns the number of rows deleted.
func (db *ServerDB) CleanupAuthEvents(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := db.conn.Exec(`DELETE FROM auth_events WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup auth events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetPendingExpiredAuthRequests returns pending auth requests that have expired,
// for use when logging "expired" auth events before cleanup marks them.
func (db *ServerDB) GetPendingExpiredAuthRequests() ([]AuthRequest, error) {
	rows, err := db.conn.Query(
		`SELECT id, email, device_code, user_code, status, user_id, api_key_id, expires_at, verified_at, created_at
		 FROM auth_requests WHERE status = ? AND expires_at <= ?`,
		AuthStatusPending, time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("get pending expired auth requests: %w", err)
	}
	defer rows.Close()

	var results []AuthRequest
	for rows.Next() {
		var ar AuthRequest
		if err := rows.Scan(&ar.ID, &ar.Email, &ar.DeviceCode, &ar.UserCode, &ar.Status,
			&ar.UserID, &ar.APIKeyID, &ar.ExpiresAt, &ar.VerifiedAt, &ar.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan expired auth request: %w", err)
		}
		results = append(results, ar)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired auth requests: %w", err)
	}
	return results, nil
}
