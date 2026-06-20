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

var deferCmd = &cobra.Command{
	Use:     "defer [issue-id] [date]",
	Short:   "Defer an issue until a future date",
	GroupID: "workflow",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			emitErr("%v", err)
			return err
		}

		clearFlag, _ := cmd.Flags().GetBool("clear")

		if clearFlag {
			issue.DeferUntil = nil

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
				emitErr("failed to clear deferral for %s: %v", issueID, err)
				return err
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   "Deferral cleared",
				Type:      models.LogTypeProgress,
			})

			if isJSON {
				cleared, ferr := database.GetIssue(issueID)
				if ferr != nil {
					cleared = issue
				}
				return output.EmitIssue("deferral_cleared", cleared, nil)
			}

			fmt.Printf("DEFERRAL CLEARED %s\n", issueID)
			return nil
		}

		// Date argument is required when not clearing
		if len(args) < 2 {
			return fmt.Errorf("date argument required (or use --clear to remove deferral)")
		}

		dateStr, err := dateparse.ParseDate(args[1])
		if err != nil {
			emitErr("invalid date: %v", err)
			return err
		}

		// If already deferred and new date is later, increment defer count
		if issue.DeferUntil != nil && dateStr > *issue.DeferUntil {
			issue.DeferCount++
		}

		issue.DeferUntil = &dateStr

		if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
			emitErr("failed to defer %s: %v", issueID, err)
			return err
		}

		logMsg := fmt.Sprintf("Deferred until %s", dateStr)
		if issue.DeferCount > 1 {
			logMsg = fmt.Sprintf("Deferred until %s (deferred %d times)", dateStr, issue.DeferCount)
		}

		database.AddLog(&models.Log{
			IssueID:   issueID,
			SessionID: sess.ID,
			Message:   logMsg,
			Type:      models.LogTypeProgress,
		})

		if isJSON {
			deferred, ferr := database.GetIssue(issueID)
			if ferr != nil {
				deferred = issue
			}
			return output.EmitIssue("deferred", deferred, map[string]any{"defer_until": dateStr})
		}

		fmt.Printf("DEFERRED %s until %s\n", issueID, dateStr)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deferCmd)
	deferCmd.Flags().Bool("clear", false, "Remove deferral, make immediately actionable")
}
