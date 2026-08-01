package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/db"
)

// describeIssueWriteFailure renders a failed issue write for the CLI.
//
// A stale-snapshot rejection (db.StaleIssueUpdateError) means another session
// wrote the issue between this command reading it and writing it back, so td
// refused the write instead of silently reverting that change. The raw error
// carries two updated_at timestamps, which tell neither a human nor an agent
// anything actionable; this says what happened, that nothing was written, and
// what to do about it.
//
// This is the sibling of describeStaleTransitionUpdate, which covers the
// status-guarded variant (db.StaleIssueStatusError) used by the review flow.
// Any other error is passed through unchanged.
func describeIssueWriteFailure(database *db.DB, action, issueID string, err error) string {
	var stale *db.StaleIssueUpdateError
	if !errors.As(err, &stale) {
		return fmt.Sprintf("failed to %s %s: %v", action, issueID, err)
	}

	lines := []string{
		fmt.Sprintf("cannot %s %s: it was modified by another session after this command read it", action, issueID),
		"  Nothing was written, so the other session's change is intact.",
	}
	if database != nil {
		if current, getErr := database.GetIssue(issueID); getErr == nil {
			lines = append(lines, fmt.Sprintf("  Current status: %s", current.Status))
			if recent := recentWorkflowTransitionContext(database, issueID); recent != "" {
				lines = append(lines, recent)
			}
		}
	}
	lines = append(lines, "  Re-run the command to apply your change on top of the current state.")

	return strings.Join(lines, "\n")
}
