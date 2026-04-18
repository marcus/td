package db

import "github.com/marcus/td/internal/models"

// HasImplementationHistory checks whether any session ever entered implementation
// flow for an issue. This is used to keep creator-only close bypasses limited to
// never-started throwaway issues.
func (db *DB) HasImplementationHistory(issueID string) (bool, error) {
	issueID = NormalizeIssueID(issueID)
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM issue_session_history
		WHERE issue_id = ?
		  AND action IN (?, ?)
	`, issueID, models.ActionSessionStarted, models.ActionSessionUnstarted).Scan(&count)
	return count > 0, err
}
