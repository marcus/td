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

// errUnstartAllFailed signals that every non-idempotent named unstart failed.
// Each failure is emitted in the per-issue path; the sentinel only sets the
// process exit status without adding Cobra usage or a second JSON envelope.
var errUnstartAllFailed = fmt.Errorf("no issues unstarted: %w", errSilentExit)

var unstartCmd = &cobra.Command{
	Use:     "unstart [issue-id...]",
	Aliases: []string{"stop"},
	Short:   "Revert issue(s) from in_progress to open",
	Long: `Reverts issue(s) back to open status. Clears implementer session.
Useful for undoing accidental starts or when you need to release an issue.

Examples:
  td unstart td-abc1                    # Unstart single issue
  td unstart td-abc1 td-abc2 td-abc3    # Unstart multiple issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}
		emitWarn := func(format string, args ...interface{}) {
			if !isJSON {
				output.Warning(format, args...)
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
		scope := currentStateScope(baseDir, sess)

		reason, _ := cmd.Flags().GetString("reason")

		unstarted := 0
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
			// unstart is an idempotent success — checked before the transition
			// validation, which has no open → open edge.
			if issue.Status == models.StatusOpen {
				noopTransition(isJSON, issueID, issue.Status, "already unstarted")
				noop++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
				failTransition(isJSON, output.ErrCodeInvalidInput, "cannot unstart %s: invalid transition from %s", issueID, issue.Status)
				failed++
				continue
			}

			// Only unstart in_progress issues (preserving existing behavior)
			if issue.Status != models.StatusInProgress {
				failTransition(isJSON, output.ErrCodeInvalidInput, "issue not in_progress: %s (status: %s)", issueID, issue.Status)
				failed++
				continue
			}

			// Record session action BEFORE clearing ImplementerSession (for bypass prevention)
			// This tracks that this session touched the issue, even though it's being unstarted
			if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionUnstarted); err != nil {
				emitWarn("failed to record session history: %v", err)
			}

			// Update issue (atomic update + action log)
			issue.Status = models.StatusOpen
			issue.ImplementerSession = ""

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReopen); err != nil {
				failTransition(isJSON, output.ErrCodeDatabaseError, "failed to update %s: %v", issueID, err)
				failed++
				continue
			}

			// Log the unstart
			logMsg := "Reverted to open"
			if reason != "" {
				logMsg = reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			// Clear focus if this was the focused issue
			focusedID, _ := database.GetFocus(scope)
			if focusedID == issueID {
				_ = database.ClearFocus(scope)
			}

			if isJSON {
				// Re-fetch the now-open issue and emit one JSON object per id
				// (NDJSON in the bulk case).
				unstarted2, ferr := database.GetIssue(issueID)
				if ferr != nil {
					unstarted2 = issue
				}
				if err := output.EmitIssue("unstarted", unstarted2, nil); err != nil {
					return err
				}
			} else {
				fmt.Printf("UNSTARTED %s → open\n", issueID)
			}
			unstarted++
		}

		if len(args) > 1 && !isJSON {
			fmt.Printf("\nUnstarted %d, skipped %d\n", unstarted, failed+noop)
		}

		// A named batch succeeds when at least one issue unstarted, and an
		// already-open retry succeeds as an idempotent no-op. If nothing
		// unstarted and any true failure occurred, report failure even when the
		// same batch also contained no-ops.
		if unstarted == 0 && failed > 0 {
			return errUnstartAllFailed
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(unstartCmd)

	unstartCmd.Flags().String("reason", "", "Reason for unstarting")
	unstartCmd.SilenceUsage = true
}
