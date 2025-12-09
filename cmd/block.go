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
	Use:   "block [issue-id...]",
	Short: "Mark issue(s) as blocked",
	Args:  cobra.MinimumNArgs(1),
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
	Long:  `Reopens closed issues. Requires new review cycle.`,
	Args:  cobra.MinimumNArgs(1),
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

			issue.Status = models.StatusOpen
			issue.ReviewerSession = ""
			issue.ClosedAt = nil

			if err := database.UpdateIssue(issue); err != nil {
				output.Error("failed to reopen %s: %v", issueID, err)
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

			fmt.Printf("REOPENED %s → open\n", issueID)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(blockCmd)
	rootCmd.AddCommand(reopenCmd)

	blockCmd.Flags().String("reason", "", "Reason for blocking")
	reopenCmd.Flags().String("reason", "", "Reason for reopening")
}
