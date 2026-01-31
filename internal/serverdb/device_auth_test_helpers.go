package serverdb

import "time"

// ForceExpireAuthRequestForTest forces an auth request's expiry time (test-only helper).
func (db *ServerDB) ForceExpireAuthRequestForTest(id string, expiresAt time.Time) {
	db.conn.Exec(`UPDATE auth_requests SET expires_at = ? WHERE id = ?`, expiresAt, id)
}
