package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var blockCmd = &cobra.Command{
	Use:     "block [issue-id...]",
	Short:   "Mark issue(s) as blocked",
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

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("%v", err)
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			issue.Status = models.StatusBlocked

			if err := database.UpdateIssue(issue); err != nil {
				output.Error("failed to block %s: %v", issueID, err)
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionBlock,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Log
			logMsg := "Blocked"
			if reason != "" {
				logMsg = "Blocked: " + reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeBlocker,
			})

			fmt.Printf("BLOCKED %s\n", issueID)
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
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")
		reopened := 0
		skipped := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			if issue.Status != models.StatusClosed {
				output.Warning("%s is not closed (status: %s)", issueID, issue.Status)
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			issue.Status = models.StatusOpen
			issue.ReviewerSession = ""
			issue.ClosedAt = nil

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to reopen %s: %v", issueID, err)
				skipped++
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionReopen,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Log
			logMsg := "Reopened"
			if reason != "" {
				logMsg = "Reopened: " + reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			fmt.Printf("REOPENED %s\n", issueID)
			reopened++
		}

		if len(args) > 1 {
			fmt.Printf("\nReopened %d, skipped %d\n", reopened, skipped)
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
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		reason, _ := cmd.Flags().GetString("reason")
		unblocked := 0
		skipped := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			if issue.Status != models.StatusBlocked {
				output.Warning("%s is not blocked (status: %s)", issueID, issue.Status)
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			issue.Status = models.StatusOpen

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to unblock %s: %v", issueID, err)
				skipped++
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionUnblock,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Log
			logMsg := "Unblocked"
			if reason != "" {
				logMsg = "Unblocked: " + reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			fmt.Printf("UNBLOCKED %s\n", issueID)
			unblocked++
		}

		if len(args) > 1 {
			fmt.Printf("\nUnblocked %d, skipped %d\n", unblocked, skipped)
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
