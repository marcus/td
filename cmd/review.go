package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

// clearFocusIfNeeded clears focus if the focused issue matches
func clearFocusIfNeeded(baseDir, issueID string) {
	focusedID, _ := config.GetFocus(baseDir)
	if focusedID == issueID {
		config.ClearFocus(baseDir)
	}
}

var reviewCmd = &cobra.Command{
	Use:   "review [issue-id...]",
	Short: "Submit one or more issues for review",
	Long: `Submits the issue(s) for review. Requires a handoff to be recorded first.

Supports bulk operations:
  td review td-abc1 td-abc2 td-abc3    # Submit multiple issues for review`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
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

		reviewed := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Check for handoff
			handoff, err := database.GetLatestHandoff(issueID)
			if err != nil || handoff == nil {
				errMsg := fmt.Sprintf("handoff required before review: %s", issueID)
				if jsonOutput {
					output.JSONError(output.ErrCodeHandoffRequired, errMsg)
				} else {
					output.Warning("%s", errMsg)
				}
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update issue
			issue.Status = models.StatusInReview
			if issue.ImplementerSession == "" {
				issue.ImplementerSession = sess.ID
			}

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionReview,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

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

			// Clear focus if this was the focused issue
			clearFocusIfNeeded(baseDir, issueID)

			fmt.Printf("REVIEW REQUESTED %s (session: %s)\n", issueID, sess.ID)
			reviewed++
		}

		if len(args) > 1 {
			fmt.Printf("\nReviewed %d, skipped %d\n", reviewed, skipped)
		}
		return nil
	},
}

var approveCmd = &cobra.Command{
	Use:   "approve [issue-id...]",
	Short: "Approve and close one or more issues",
	Long: `Approves and closes the issue(s). Must be a different session than the implementer.

Supports bulk operations:
  td approve td-abc1 td-abc2 td-abc3    # Approve multiple issues
  td approve --all                      # Approve all reviewable issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(0),
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

		jsonOutput, _ := cmd.Flags().GetBool("json")
		all, _ := cmd.Flags().GetBool("all")

		// Build list of issue IDs to approve
		var issueIDs []string
		if all {
			// Get all reviewable issues (in_review and not implemented by current session)
			issues, err := database.ListIssues(db.ListIssuesOptions{
				ReviewableBy: sess.ID,
			})
			if err != nil {
				output.Error("failed to list reviewable issues: %v", err)
				return err
			}
			for _, issue := range issues {
				issueIDs = append(issueIDs, issue.ID)
			}
		} else {
			issueIDs = args
		}

		if len(issueIDs) == 0 {
			output.Error("no issues to approve. Provide issue IDs or use --all")
			return fmt.Errorf("no issues specified")
		}

		approved := 0
		skipped := 0
		for _, issueID := range issueIDs {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Check that reviewer is different from implementer (unless minor task)
			if issue.ImplementerSession == sess.ID && !issue.Minor {
				if !all { // Only show error for explicit requests
					errMsg := fmt.Sprintf("cannot approve own implementation: %s (use --minor on create for self-review)", issueID)
					if jsonOutput {
						output.JSONError(output.ErrCodeCannotSelfApprove, errMsg)
					} else {
						output.Error("%s", errMsg)
					}
				}
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update issue
			issue.Status = models.StatusClosed
			issue.ReviewerSession = sess.ID
			now := issue.UpdatedAt
			issue.ClosedAt = &now

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
				continue
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

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionApprove,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Clear focus if this was the focused issue
			clearFocusIfNeeded(baseDir, issueID)

			fmt.Printf("APPROVED %s (reviewer: %s)\n", issueID, sess.ID)
			approved++
		}

		if len(issueIDs) > 1 {
			fmt.Printf("\nApproved %d, skipped %d\n", approved, skipped)
		}
		return nil
	},
}

var rejectCmd = &cobra.Command{
	Use:   "reject [issue-id...]",
	Short: "Reject and return to in_progress",
	Long: `Rejects the issue(s) and returns them to in_progress status.

Supports bulk operations:
  td reject td-abc1 td-abc2    # Reject multiple issues`,
	GroupID: "workflow",
	Args:    cobra.MinimumNArgs(1),
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

		rejected := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					output.Warning("issue not found: %s", issueID)
				}
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update issue
			issue.Status = models.StatusInProgress

			if err := database.UpdateIssue(issue); err != nil {
				if jsonOutput {
					output.JSONError(output.ErrCodeDatabaseError, err.Error())
				} else {
					output.Warning("failed to update %s: %v", issueID, err)
				}
				skipped++
				continue
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

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionReject,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			if jsonOutput {
				result := map[string]interface{}{
					"id":      issueID,
					"status":  "in_progress",
					"action":  "rejected",
					"session": sess.ID,
				}
				if reason != "" {
					result["reason"] = reason
				}
				output.JSON(result)
			} else {
				fmt.Printf("REJECTED %s → in_progress\n", issueID)
			}
			rejected++
		}

		if len(args) > 1 && !jsonOutput {
			fmt.Printf("\nRejected %d, skipped %d\n", rejected, skipped)
		}
		return nil
	},
}

var closeCmd = &cobra.Command{
	Use:   "close [issue-id...]",
	Short: "Close one or more issues without review",
	Long: `Closes the issue(s) directly. Useful for trivial fixes, duplicates, or won't-fix scenarios.

Examples:
  td close td-abc1                    # Close single issue
  td close td-abc1 td-abc2 td-abc3    # Close multiple issues`,
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

		closed := 0
		skipped := 0
		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update issue
			issue.Status = models.StatusClosed
			now := issue.UpdatedAt
			issue.ClosedAt = &now

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionClose,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Log
			reason, _ := cmd.Flags().GetString("reason")
			logMsg := "Closed"
			if reason != "" {
				logMsg = "Closed: " + reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			// Clear focus if this was the focused issue
			clearFocusIfNeeded(baseDir, issueID)

			fmt.Printf("CLOSED %s\n", issueID)
			closed++
		}

		if len(args) > 1 {
			fmt.Printf("\nClosed %d, skipped %d\n", closed, skipped)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(rejectCmd)
	rootCmd.AddCommand(closeCmd)

	reviewCmd.Flags().String("reason", "", "Reason for submitting")
	reviewCmd.Flags().Bool("json", false, "JSON output")
	approveCmd.Flags().String("reason", "", "Reason for approval")
	approveCmd.Flags().Bool("json", false, "JSON output")
	approveCmd.Flags().Bool("all", false, "Approve all reviewable issues")
	rejectCmd.Flags().String("reason", "", "Reason for rejection")
	rejectCmd.Flags().Bool("json", false, "JSON output")
	closeCmd.Flags().String("reason", "", "Reason for closing")
}
