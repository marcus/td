package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start [issue-id...]",
	Short: "Begin work on issue(s)",
	Long: `Records current session as implementer and captures git state.

Examples:
  td start td-abc1                    # Start single issue
  td start td-abc1 td-abc2 td-abc3    # Start multiple issues`,
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

		sess, err := session.GetOrCreate(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		force, _ := cmd.Flags().GetBool("force")
		reason, _ := cmd.Flags().GetString("reason")

		// Capture git state once for all issues
		gitState, gitErr := git.GetState()

		started := 0
		skipped := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Warning("issue not found: %s", issueID)
				skipped++
				continue
			}

			// Check if blocked
			if issue.Status == models.StatusBlocked && !force {
				output.Warning("cannot start blocked issue: %s (use --force to override)", issueID)
				skipped++
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update issue
			issue.Status = models.StatusInProgress
			issue.ImplementerSession = sess.ID

			if err := database.UpdateIssue(issue); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
				continue
			}

			// Log action for undo
			newData, _ := json.Marshal(issue)
			database.LogAction(&models.ActionLog{
				SessionID:    sess.ID,
				ActionType:   models.ActionStart,
				EntityType:   "issue",
				EntityID:     issueID,
				PreviousData: string(prevData),
				NewData:      string(newData),
			})

			// Log the start
			logMsg := "Started work"
			if reason != "" {
				logMsg = reason
			}

			database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			// Record git snapshot
			if gitErr == nil {
				database.AddGitSnapshot(&models.GitSnapshot{
					IssueID:    issueID,
					Event:      "start",
					CommitSHA:  gitState.CommitSHA,
					Branch:     gitState.Branch,
					DirtyFiles: gitState.DirtyFiles,
				})
			}

			fmt.Printf("STARTED %s (session: %s)\n", issueID, sess.ID)
			started++
		}

		// Set focus to first issue if single issue, or clear if multiple
		if len(args) == 1 && started == 1 {
			config.SetFocus(baseDir, args[0])
		}

		// Show git state once at the end
		if gitErr == nil && started > 0 {
			stateStr := "clean"
			if !gitState.IsClean {
				stateStr = fmt.Sprintf("%d modified, %d untracked", gitState.Modified, gitState.Untracked)
			}
			fmt.Printf("Git: %s (%s) %s\n", output.ShortSHA(gitState.CommitSHA), gitState.Branch, stateStr)

			if !gitState.IsClean {
				output.Warning("Starting with uncommitted changes")
			}
		}

		if len(args) > 1 {
			fmt.Printf("\nStarted %d, skipped %d\n", started, skipped)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().String("reason", "", "Reason for starting work")
	startCmd.Flags().Bool("force", false, "Force start even if blocked")
}
