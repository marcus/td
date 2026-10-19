package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/models"
)

// MutateIssueLogged applies an edit to the current persisted issue under the
// write lock. A supplied expected snapshot preserves optimistic concurrency.
// The issue edit, comment, and their action/sync records commit together.
// mutate must be state-free and must not call database methods.
func (db *DB) MutateIssueLogged(issueID, sessionID string, expected *models.Issue, labels models.LabelChange, comment string, mutate func(*models.Issue) error) (*models.Issue, error) {
	if _, err := labels.Apply(nil); err != nil {
		return nil, err
	}
	var updated *models.Issue
	err := db.withWriteLock(func() error {
		return db.withReviewSyncTxLocked(func(tx *sql.Tx) error {
			current, err := db.scanIssueRowFrom(tx, NormalizeIssueID(issueID))
			if err != nil {
				return err
			}
			if current.DeletedAt != nil || (expected != nil && marshalIssue(current) != marshalIssue(expected)) {
				return ErrIssueChanged
			}
			previous := *current
			if mutate != nil {
				if err := mutate(current); err != nil {
					return err
				}
			}
			current.Labels, err = labels.Apply(current.Labels)
			if err != nil {
				return err
			}
			if err := db.updateIssueAndLogFromPreviousStore(tx, current, &previous, sessionID, models.ActionUpdate); err != nil {
				return err
			}
			if comment != "" {
				if strings.TrimSpace(comment) == "" {
					return fmt.Errorf("comment must not be blank")
				}
				if err := db.addCommentStore(tx, &models.Comment{IssueID: current.ID, SessionID: sessionID, Text: comment}); err != nil {
					return err
				}
			}
			updated = current
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}
