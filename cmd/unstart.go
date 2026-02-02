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

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")

		unstarted := 0
		skipped := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			if !sm.IsValidTransition(issue.Status, models.StatusOpen) {
				output.Warning("cannot unstart %s: invalid transition from %s", issueID, issue.Status)
				skipped++
				continue
			}

			// Only unstart in_progress issues (preserving existing behavior)
			if issue.Status != models.StatusInProgress {
				output.Warning("issue not in_progress: %s (status: %s)", issueID, issue.Status)
				skipped++
				continue
			}

			// Record session action BEFORE clearing ImplementerSession (for bypass prevention)
			// This tracks that this session touched the issue, even though it's being unstarted
			if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionUnstarted); err != nil {
				output.Warning("failed to record session history: %v", err)
			}

			// Update issue (atomic update + action log)
			issue.Status = models.StatusOpen
			issue.ImplementerSession = ""

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionReopen); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
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
			clearFocusIfNeeded(baseDir, issueID)

			fmt.Printf("UNSTARTED %s → open\n", issueID)
			unstarted++
		}

		if len(args) > 1 {
			fmt.Printf("\nUnstarted %d, skipped %d\n", unstarted, skipped)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(unstartCmd)

	unstartCmd.Flags().String("reason", "", "Reason for unstarting")
}
