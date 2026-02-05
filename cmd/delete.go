package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:     "delete [issue-id...]",
	Short:   "Soft-delete one or more issues",
	GroupID: "core",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, _ := session.GetOrCreate(database)

		for _, issueID := range args {
			if err := database.DeleteIssueLogged(issueID, sess.ID); err != nil {
				output.Error("failed to delete %s: %v", issueID, err)
				continue
			}

			fmt.Printf("DELETED %s\n", issueID)
		}

		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use:     "restore [issue-id...]",
	Short:   "Restore soft-deleted issues",
	GroupID: "core",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, _ := session.GetOrCreate(database)

		for _, issueID := range args {
			if err := database.RestoreIssueLogged(issueID, sess.ID); err != nil {
				output.Error("failed to restore %s: %v", issueID, err)
				continue
			}

			fmt.Printf("RESTORED %s\n", issueID)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(restoreCmd)

	// Accept --force and --yes as no-ops for LLM compatibility
	deleteCmd.Flags().BoolP("force", "f", false, "No-op (delete always succeeds)")
	deleteCmd.Flags().BoolP("yes", "y", false, "No-op (delete always succeeds, alias for --force)")
}
