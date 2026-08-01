package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

// Sentinels for the block-family batches: every non-idempotent named mutation
// failed. Each failure is emitted in the per-issue path, so these only set the
// process exit status without adding Cobra usage or a second JSON envelope.
var (
	errBlockAllFailed   = fmt.Errorf("no issues blocked: %w", errSilentExit)
	errReopenAllFailed  = fmt.Errorf("no issues reopened: %w", errSilentExit)
	errUnblockAllFailed = fmt.Errorf("no issues unblocked: %w", errSilentExit)
)

var blockCmd = &cobra.Command{
	Use:     "block [issue-id...]",
	Short:   "Mark issue(s) as blocked",
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Args have already validated; from here every error is operational
		// and carries its own message, so Cobra's usage block would be noise.
		// Genuine usage errors (bad arg count, unknown flag) are reported
		// before RunE runs and still print usage.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			emitErr("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")

		blocked := 0
		failed := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				failTransition(isJSON, output.ErrCodeNotFound, "issue not found: %s", issueID)
				failed++
				continue
			}

			// An already-blocked issue is the requested end state, so a
			// repeated block is an idempotent success — checked before the
			// transition validation, which has no blocked → blocked edge.
			if issue.Status == models.StatusBlocked {
				noopTransition(isJSON, issueID, issue.Status, "already blocked")
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusBlocked) {
				failTransition(isJSON, output.ErrCodeInvalidInput, "cannot block %s: invalid transition from %s", issueID, issue.Status)
				failed++
				continue
			}

			issue.Status = models.StatusBlocked

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionBlock); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "%s", describeIssueWriteFailure(database, "block", issueID, err))
				failed++
				continue
			}

			// Log
			logMsg := "Blocked"
			if reason != "" {
				logMsg = "Blocked: " + reason
			}

			_ = database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeBlocker,
			})

			if isJSON {
				blockedIssue, ferr := database.GetIssue(issueID)
				if ferr != nil {
					blockedIssue = issue
				}
				var extra map[string]any
				if reason != "" {
					extra = map[string]any{"reason": reason}
				}
				if err := output.EmitIssue("blocked", blockedIssue, extra); err != nil {
					return err
				}
			} else {
				fmt.Printf("BLOCKED %s\n", issueID)
			}
			blocked++
		}

		// A named batch succeeds when at least one issue blocked, and an
		// already-blocked retry succeeds as an idempotent no-op. If nothing
		// blocked and any true failure occurred, report failure.
		if blocked == 0 && failed > 0 {
			return errBlockAllFailed
		}

		return nil
	},
}

var reopenCmd = &cobra.Command{
	Use:   "reopen [issue-id...]",
	Short: "Reopen closed issues",
	Long: `Reopens closed issue(s) back to open status.

Examples:
  td reopen td-abc1                    # Reopen single issue
  td reopen td-abc1 td-abc2 td-abc3    # Reopen multiple issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Args have already validated; from here every error is operational
		// and carries its own message, so Cobra's usage block would be noise.
		// Genuine usage errors (bad arg count, unknown flag) are reported
		// before RunE runs and still print usage.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			emitErr("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")
		reopened := 0
		failed := 0
		noop := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				failTransition(isJSON, output.ErrCodeNotFound, "issue not found: %s", issueID)
				failed++
				continue
			}

			// An already-open issue is the requested end state, so a repeated
			// reopen is an idempotent success — checked before the transition
			// validation, which has no open → open edge.
			if issue.Status == models.StatusOpen {
				noopTransition(isJSON, issueID, issue.Status, "already reopened")
				noop++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
				failTransition(isJSON, output.ErrCodeInvalidInput, "cannot reopen %s: invalid transition from %s", issueID, issue.Status)
				failed++
				continue
			}

			if issue.Status != models.StatusClosed {
				failTransition(isJSON, output.ErrCodeInvalidInput, "%s is not closed (status: %s)", issueID, issue.Status)
				failed++
				continue
			}

			issue.Status = models.StatusOpen
			issue.ReviewerSession = ""
			issue.ClosedAt = nil
			// A reopened issue is unclaimed work. Leaving implementer_session
			// set produced an issue that is open AND held: `td unstart` read
			// the status alone and reported "already unstarted" while the
			// claim was still there, and `td unstart --stale` could not see it
			// because it lists in_progress only. The claim leaked with no
			// surface that could report it.
			issue.ImplementerSession = ""

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReopen); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "%s", describeIssueWriteFailure(database, "reopen", issueID, err))
				failed++
				continue
			}

			// Log
			logMsg := "Reopened"
			if reason != "" {
				logMsg = "Reopened: " + reason
			}

			_ = database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			if isJSON {
				reopenedIssue, ferr := database.GetIssue(issueID)
				if ferr != nil {
					reopenedIssue = issue
				}
				if err := output.EmitIssue("reopened", reopenedIssue, nil); err != nil {
					return err
				}
			} else {
				fmt.Printf("REOPENED %s\n", issueID)
			}
			reopened++
		}

		if len(args) > 1 && !isJSON {
			fmt.Printf("\nReopened %d, unchanged %d, failed %d\n", reopened, noop, failed)
		}

		if reopened == 0 && failed > 0 {
			return errReopenAllFailed
		}
		return nil
	},
}

var unblockCmd = &cobra.Command{
	Use:   "unblock [issue-id...]",
	Short: "Unblock issue(s) back to open status",
	Long: `Unblocks blocked issue(s) back to open status.

Examples:
  td unblock td-abc1                    # Unblock single issue
  td unblock td-abc1 td-abc2 td-abc3    # Unblock multiple issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Args have already validated; from here every error is operational
		// and carries its own message, so Cobra's usage block would be noise.
		// Genuine usage errors (bad arg count, unknown flag) are reported
		// before RunE runs and still print usage.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			emitErr("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")
		unblocked := 0
		failed := 0
		noop := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				failTransition(isJSON, output.ErrCodeNotFound, "issue not found: %s", issueID)
				failed++
				continue
			}

			// An already-open issue is the requested end state, so a repeated
			// unblock is an idempotent success — checked before the transition
			// validation, which has no open → open edge.
			if issue.Status == models.StatusOpen {
				noopTransition(isJSON, issueID, issue.Status, "already unblocked")
				noop++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
				failTransition(isJSON, output.ErrCodeInvalidInput, "cannot unblock %s: invalid transition from %s", issueID, issue.Status)
				failed++
				continue
			}

			if issue.Status != models.StatusBlocked {
				failTransition(isJSON, output.ErrCodeInvalidInput, "%s is not blocked (status: %s)", issueID, issue.Status)
				failed++
				continue
			}

			issue.Status = models.StatusOpen

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUnblock); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "%s", describeIssueWriteFailure(database, "unblock", issueID, err))
				failed++
				continue
			}

			// Log
			logMsg := "Unblocked"
			if reason != "" {
				logMsg = "Unblocked: " + reason
			}

			_ = database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			if isJSON {
				unblockedIssue, ferr := database.GetIssue(issueID)
				if ferr != nil {
					unblockedIssue = issue
				}
				if err := output.EmitIssue("unblocked", unblockedIssue, nil); err != nil {
					return err
				}
			} else {
				fmt.Printf("UNBLOCKED %s\n", issueID)
			}
			unblocked++
		}

		if len(args) > 1 && !isJSON {
			fmt.Printf("\nUnblocked %d, unchanged %d, failed %d\n", unblocked, noop, failed)
		}

		if unblocked == 0 && failed > 0 {
			return errUnblockAllFailed
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(blockCmd)
	rootCmd.AddCommand(unblockCmd)
	rootCmd.AddCommand(reopenCmd)

	blockCmd.Flags().String("reason", "", "Reason for blocking")
	unblockCmd.Flags().String("reason", "", "Reason for unblocking")
	reopenCmd.Flags().String("reason", "", "Reason for reopening")

}
