package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/db"
)

// TestDescribeIssueWriteFailureExplainsStaleRejection checks the message an
// agent actually has to act on: it must say the write did not land and that
// re-running is the fix, rather than printing two raw updated_at timestamps.
func TestDescribeIssueWriteFailureExplainsStaleRejection(t *testing.T) {
	staleErr := &db.StaleIssueUpdateError{
		IssueID: "td-abc123",
		Loaded:  time.Now().Add(-time.Minute),
		Current: time.Now(),
	}

	// Wrapped, to confirm the helper unwraps rather than matching on text.
	msg := describeIssueWriteFailure(nil, "update", "td-abc123", fmt.Errorf("update issue: %w", staleErr))

	for _, want := range []string{
		"cannot update td-abc123",
		"modified by another session",
		"Nothing was written",
		"Re-run the command",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "updated_at=") {
		t.Errorf("message should not surface raw updated_at timestamps:\n%s", msg)
	}
}

// TestDescribeIssueWriteFailurePassesThroughOtherErrors keeps the helper from
// dressing up unrelated failures as conflicts.
func TestDescribeIssueWriteFailurePassesThroughOtherErrors(t *testing.T) {
	msg := describeIssueWriteFailure(nil, "update", "td-abc123", errors.New("disk on fire"))

	want := "failed to update td-abc123: disk on fire"
	if msg != want {
		t.Errorf("message = %q, want %q", msg, want)
	}
}
