package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:     "start [issue-id...]",
	Aliases: []string{"begin"},
	Short:   "Begin work on issue(s)",
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

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Check for too many in-progress issues
		inProgress, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInProgress},
			Implementer: sess.ID,
		})
		if len(inProgress) > 4 {
			fmt.Println()
			output.Warning("You have %d issues in progress!", len(inProgress))
			fmt.Println("  Before starting new work, move completed issues to review:")
			fmt.Println("    td review <id>    # for each completed issue")
			fmt.Println()
			fmt.Println("  In-progress issues:")
			for _, issue := range inProgress {
				fmt.Printf("    %s \"%s\"\n", issue.ID, issue.Title)
			}
			fmt.Println()
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

			// Validate transition with state machine
			sm := workflow.DefaultMachine()
			ctx := &workflow.TransitionContext{
				Issue:      issue,
				FromStatus: issue.Status,
				ToStatus:   models.StatusInProgress,
				SessionID:  sess.ID,
				Force:      force,
				Context:    workflow.ContextCLI,
			}

			if !sm.IsValidTransition(issue.Status, models.StatusInProgress) {
				output.Warning("cannot start %s: invalid transition from %s", issueID, issue.Status)
				skipped++
				continue
			}

			// Check if blocked without force (preserving existing behavior)
			if issue.Status == models.StatusBlocked && !force {
				output.Warning("cannot start blocked issue: %s (use --force to override)", issueID)
				skipped++
				continue
			}

			// Run guards (for advisory warnings in future)
			if results, _ := sm.Validate(ctx); len(results) > 0 {
				for _, r := range results {
					if !r.Passed {
						output.Warning("%s: %s", issueID, r.Message)
					}
				}
			}

			// Update issue (atomic update + action log)
			issue.Status = models.StatusInProgress
			issue.ImplementerSession = sess.ID

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionStart); err != nil {
				output.Warning("failed to update %s: %v", issueID, err)
				skipped++
				continue
			}

			// Record session action for bypass prevention
			if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionStarted); err != nil {
				output.Warning("failed to record session history: %v", err)
			}

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
