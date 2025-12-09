package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var focusCmd = &cobra.Command{
	Use:     "focus [issue-id]",
	Short:   "Set the current working issue",
	GroupID: "session",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if err := config.SetFocus(baseDir, issueID); err != nil {
			output.Error("failed to set focus: %v", err)
			return err
		}

		fmt.Printf("FOCUSED %s\n", issueID)
		return nil
	},
}

var unfocusCmd = &cobra.Command{
	Use:     "unfocus",
	Short:   "Clear focus",
	GroupID: "session",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		if err := config.ClearFocus(baseDir); err != nil {
			output.Error("failed to clear focus: %v", err)
			return err
		}

		fmt.Println("UNFOCUSED")
		return nil
	},
}

var currentCmd = &cobra.Command{
	Use:     "current",
	Short:   "Show focused issue, active work, and pending reviews",
	GroupID: "session",
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

		result := map[string]interface{}{
			"session": sess.ID,
		}

		// Get focused issue
		focusedID, _ := config.GetFocus(baseDir)
		if focusedID != "" {
			issue, err := database.GetIssue(focusedID)
			if err == nil {
				if jsonOutput {
					result["focused"] = issue
				} else {
					fmt.Printf("FOCUSED: %s  %s  %s%s  %s\n",
						issue.ID, issue.Title, issue.Priority, output.FormatPointsSuffix(issue.Points), output.FormatStatus(issue.Status))
					fmt.Println()
				}
			}
		} else if !jsonOutput {
			fmt.Println("No focused issue")
			fmt.Println()
		}

		// Get in-progress issues for this session
		inProgress, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInProgress},
			Implementer: sess.ID,
		})

		if len(inProgress) > 0 {
			if jsonOutput {
				result["in_progress"] = inProgress
			} else {
				fmt.Println("IN PROGRESS (this session):")
				for _, issue := range inProgress {
					fmt.Printf("  %s  %s  %s%s\n",
						issue.ID, issue.Title, issue.Priority, output.FormatPointsSuffix(issue.Points))
				}
				fmt.Println()
			}
		}

		// Get issues awaiting review (that this session can review)
		reviewable, _ := database.ListIssues(db.ListIssuesOptions{
			ReviewableBy: sess.ID,
		})

		if len(reviewable) > 0 {
			if jsonOutput {
				result["awaiting_review"] = reviewable
			} else {
				fmt.Println("AWAITING YOUR REVIEW:")
				for _, issue := range reviewable {
					fmt.Printf("  %s  %s  %s%s  (impl: %s)\n",
						issue.ID, issue.Title, issue.Priority, output.FormatPointsSuffix(issue.Points), issue.ImplementerSession)
				}
				fmt.Println()
			}
		}

		// Get issues this session submitted for review
		submittedForReview, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInReview},
			Implementer: sess.ID,
		})

		if len(submittedForReview) > 0 {
			if jsonOutput {
				result["submitted_for_review"] = submittedForReview
			} else {
				fmt.Println("SUBMITTED FOR REVIEW (by you):")
				for _, issue := range submittedForReview {
					fmt.Printf("  %s  %s  %s%s\n",
						issue.ID, issue.Title, issue.Priority, output.FormatPointsSuffix(issue.Points))
				}
			}
		}

		if jsonOutput {
			return output.JSON(result)
		}

		return nil
	},
}

var checkHandoffCmd = &cobra.Command{
	Use:   "check-handoff",
	Short: "Check if handoff is needed before exiting (returns error if yes)",
	Long: `Check if the current session has in-progress work that needs handoff.

Returns exit code 0 if no handoff needed, exit code 1 if handoff needed.
Can be used in scripts or AI agent exit hooks to remind about handoff.

Example in bash: td check-handoff || echo "Don't forget to run td handoff!"`,
	GroupID: "session",
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

		quiet, _ := cmd.Flags().GetBool("quiet")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Check for in-progress issues by this session
		inProgress, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInProgress},
			Implementer: sess.ID,
		})

		// Check for active work session
		wsID, _ := config.GetActiveWorkSession(baseDir)

		// Check for any focused issue
		focusedID, _ := config.GetFocus(baseDir)

		needsHandoff := len(inProgress) > 0 || wsID != ""

		if jsonOutput {
			result := map[string]interface{}{
				"needs_handoff":      needsHandoff,
				"session":            sess.ID,
				"in_progress_count":  len(inProgress),
				"active_work_session": wsID,
				"focused_issue":      focusedID,
			}
			if len(inProgress) > 0 {
				issueIDs := make([]string, len(inProgress))
				for i, issue := range inProgress {
					issueIDs[i] = issue.ID
				}
				result["in_progress_issues"] = issueIDs
			}
			return output.JSON(result)
		}

		if needsHandoff {
			if !quiet {
				fmt.Println("⚠️  HANDOFF NEEDED")
				fmt.Println()
				if len(inProgress) > 0 {
					fmt.Printf("You have %d issue(s) in progress:\n", len(inProgress))
					for _, issue := range inProgress {
						fmt.Printf("  %s  %s\n", issue.ID, issue.Title)
					}
					fmt.Println()
					fmt.Println("Run `td handoff <id>` for each issue before stopping work.")
				}
				if wsID != "" {
					fmt.Printf("You have an active work session: %s\n", wsID)
					fmt.Println()
					fmt.Println("Run `td ws handoff` to capture state and end session.")
				}
			}
			// Use SilentErr to suppress Cobra's error output while still returning exit code 1
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			return fmt.Errorf("handoff needed")
		}

		if !quiet {
			fmt.Println("✓ No handoff needed - safe to exit")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(focusCmd)
	rootCmd.AddCommand(unfocusCmd)
	rootCmd.AddCommand(currentCmd)
	rootCmd.AddCommand(checkHandoffCmd)

	currentCmd.Flags().Bool("json", false, "JSON output")
	checkHandoffCmd.Flags().Bool("quiet", false, "Suppress output, only return exit code")
	checkHandoffCmd.Flags().Bool("json", false, "JSON output")
}
