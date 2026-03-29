package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/dateparse"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var dueCmd = &cobra.Command{
	Use:     "due [issue-id] [date]",
	Short:   "Set a due date on an issue",
	Long: `Set a due date on an issue.

Assigns a deadline to the issue. Supports natural date expressions
(e.g., friday, +2w, 2026-03-15). Use --clear to remove the due date.
Issues past their due date appear in 'td list --overdue'.`,
	GroupID: "workflow",
	Args:    cobra.RangeArgs(1, 2),
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

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		clearFlag, _ := cmd.Flags().GetBool("clear")

		if clearFlag {
			issue.DueDate = nil

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
				output.Error("failed to update %s: %v", issueID, err)
				return err
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   "Due date cleared",
				Type:      models.LogTypeProgress,
			})

			fmt.Printf("DUE DATE CLEARED %s\n", issueID)
		} else {
			if len(args) < 2 {
				return fmt.Errorf("date argument required (or use --clear)")
			}

			dateStr, err := dateparse.ParseDate(args[1])
			if err != nil {
				output.Error("invalid date: %v", err)
				return err
			}

			issue.DueDate = &dateStr

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
				output.Error("failed to update %s: %v", issueID, err)
				return err
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   "Due date set: " + dateStr,
				Type:      models.LogTypeProgress,
			})

			fmt.Printf("DUE DATE SET %s: %s\n", issueID, dateStr)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dueCmd)
	dueCmd.Flags().Bool("clear", false, "Remove due date")
}
