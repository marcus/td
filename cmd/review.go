package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review [issue-id]",
	Short: "Submit issue for review",
	Long:  `Submits the issue for review. Requires a handoff to be recorded first.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()
		jsonOutput, _ := cmd.Flags().GetBool("json")

		database, err := db.Open(baseDir)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeDatabaseError, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeNoActiveSession, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeNotFound, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		// Check for handoff
		handoff, err := database.GetLatestHandoff(issueID)
		if err != nil || handoff == nil {
			errMsg := fmt.Sprintf("handoff required before review: %s", issueID)
			if jsonOutput {
				output.JSONError(output.ErrCodeHandoffRequired, errMsg)
			} else {
				output.Error("%s", errMsg)
			}
			return fmt.Errorf("handoff required")
		}

		// Update issue
		issue.Status = models.StatusInReview
		if issue.ImplementerSession == "" {
			issue.ImplementerSession = sess.ID
		}

		if err := database.UpdateIssue(issue); err != nil {
			output.Error("failed to update issue: %v", err)
			return err
		}

		// Log
		reason, _ := cmd.Flags().GetString("reason")
		logMsg := "Submitted for review"
		if reason != "" {
			logMsg = reason
		}

		database.AddLog(&models.Log{
			IssueID:   issueID,
			SessionID: sess.ID,
			Message:   logMsg,
			Type:      models.LogTypeProgress,
		})

		fmt.Printf("REVIEW REQUESTED %s (session: %s)\n", issueID, sess.ID)
		return nil
	},
}

var approveCmd = &cobra.Command{
	Use:   "approve [issue-id]",
	Short: "Approve and close an issue",
	Long:  `Approves and closes the issue. Must be a different session than the implementer.`,
	Args:  cobra.ExactArgs(1),
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

		issueID := args[0]
		jsonOutput, _ := cmd.Flags().GetBool("json")

		issue, err := database.GetIssue(issueID)
		if err != nil {
			if jsonOutput {
				output.JSONError(output.ErrCodeNotFound, err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		// Check that reviewer is different from implementer
		if issue.ImplementerSession == sess.ID {
			errMsg := fmt.Sprintf("cannot approve own implementation: %s (implemented by current session)", issueID)
			if jsonOutput {
				output.JSONError(output.ErrCodeCannotSelfApprove, errMsg)
				cmd.SilenceErrors = true
				cmd.SilenceUsage = true
			} else {
				output.Error("%s", errMsg)
			}
			return fmt.Errorf("cannot self-approve")
		}

		// Update issue
		issue.Status = models.StatusClosed
		issue.ReviewerSession = sess.ID
		now := issue.UpdatedAt
		issue.ClosedAt = &now

		if err := database.UpdateIssue(issue); err != nil {
			output.Error("failed to update issue: %v", err)
			return err
		}

		// Log
		reason, _ := cmd.Flags().GetString("reason")
		logMsg := "Approved"
		if reason != "" {
			logMsg = reason
		}

		database.AddLog(&models.Log{
			IssueID:   issueID,
			SessionID: sess.ID,
			Message:   logMsg,
			Type:      models.LogTypeProgress,
		})

		fmt.Printf("APPROVED %s (reviewer: %s)\n", issueID, sess.ID)
		return nil
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject [issue-id]",
	Short: "Reject and return to in_progress",
	Long:  `Rejects the issue and returns it to in_progress status.`,
	Args:  cobra.ExactArgs(1),
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

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Update issue
		issue.Status = models.StatusInProgress

		if err := database.UpdateIssue(issue); err != nil {
			output.Error("failed to update issue: %v", err)
			return err
		}

		// Log
		reason, _ := cmd.Flags().GetString("reason")
		logMsg := "Rejected"
		if reason != "" {
			logMsg = "Rejected: " + reason
		}

		database.AddLog(&models.Log{
			IssueID:   issueID,
			SessionID: sess.ID,
			Message:   logMsg,
			Type:      models.LogTypeProgress,
		})

		fmt.Printf("REJECTED %s → in_progress\n", issueID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)

	reviewCmd.Flags().String("reason", "", "Reason for submitting")
	reviewCmd.Flags().Bool("json", false, "JSON output")
	approveCmd.Flags().String("reason", "", "Reason for approval")
	approveCmd.Flags().Bool("json", false, "JSON output")
	rejectCmd.Flags().String("reason", "", "Reason for rejection")
	rejectCmd.Flags().Bool("json", false, "JSON output")
}
