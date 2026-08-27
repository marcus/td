package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

// errStartAllFailed signals that every non-idempotent named start failed. Each
// failure is emitted in the per-issue path; the sentinel only sets the process
// exit status without adding Cobra usage or a second JSON envelope.
var errStartAllFailed = fmt.Errorf("no issues started: %w", errSilentExit)

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
		// Args have already validated; from here every error is operational
		// and carries its own message, so Cobra's usage block would be noise.
		// Genuine usage errors (bad arg count, unknown flag) are reported
		// before RunE runs and still print usage.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		emitErr := func(format string, args ...interface{}) {
			if !isJSON {
				output.Error(format, args...)
			}
		}
		emitWarn := func(format string, args ...interface{}) {
			if !isJSON {
				output.Warning(format, args...)
			}
		}

		database, err := db.Open(baseDir)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		defer func() { _ = database.Close() }()

		sess, err := session.GetOrCreate(database)
		if err != nil {
			emitErr("%v", err)
			return err
		}
		scope := currentStateScope(baseDir, sess)

		// Check for too many in-progress issues
		inProgress, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInProgress},
			Implementer: sess.ID,
		})
		if len(inProgress) > 4 && !isJSON {
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
		failed := 0
		noop := 0

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				if isJSON {
					output.JSONError(output.ErrCodeNotFound, err.Error())
				} else {
					emitWarn("issue not found: %s", issueID)
				}
				failed++
				continue
			}

			if issue.Status == models.StatusInProgress {
				if issue.ImplementerSession == sess.ID {
					message := fmt.Sprintf("already started %s", issueID)
					if isJSON {
						if err := output.JSON(map[string]interface{}{
							"id":      issueID,
							"status":  string(issue.Status),
							"action":  "already started",
							"message": message,
						}); err != nil {
							output.JSONError(output.ErrCodeDatabaseError, err.Error())
						}
					} else {
						emitWarn("%s", message)
					}
					noop++
					continue
				}

				message := fmt.Sprintf("cannot start %s: already in_progress", issueID)
				if issue.ImplementerSession == "" {
					message += " with no recorded implementer"
				} else {
					message += fmt.Sprintf(" under session %s", issue.ImplementerSession)
				}
				if isJSON {
					output.JSONError(output.ErrCodeInvalidInput, message)
				} else {
					emitWarn("%s", message)
				}
				failed++
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
				if isJSON {
					output.JSONError(output.ErrCodeInvalidInput, fmt.Sprintf("cannot start %s: invalid transition from %s", issueID, issue.Status))
				} else {
					emitWarn("cannot start %s: invalid transition from %s", issueID, issue.Status)
				}
				failed++
				continue
			}

			// Check if blocked without force (preserving existing behavior)
			if issue.Status == models.StatusBlocked && !force {
				if isJSON {
					output.JSONError(output.ErrCodeInvalidInput, fmt.Sprintf("cannot start blocked issue: %s (use --force to override)", issueID))
				} else {
					emitWarn("cannot start blocked issue: %s (use --force to override)", issueID)
				}
				failed++
				continue
			}

			// Run guards (for advisory warnings in future)
			if results, _ := sm.Validate(ctx); len(results) > 0 {
				for _, r := range results {
					if !r.Passed {
						emitWarn("%s: %s", issueID, r.Message)
					}
				}
			}

			// Update issue (atomic update + action log)
			issue.Status = models.StatusInProgress
			issue.ImplementerSession = sess.ID

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionStart); err != nil {
				message := describeIssueWriteFailure(database, "start", issueID, err)
				if isJSON {
					output.JSONError(output.ErrCodeDatabaseError, message)
				} else {
					emitWarn("%s", message)
				}
				failed++
				continue
			}

			// Record session action for bypass prevention
			if err := database.RecordSessionAction(issueID, sess.ID, models.ActionSessionStarted); err != nil {
				emitWarn("failed to record session history: %v", err)
			}

			// Log the start
			logMsg := "Started work"
			if reason != "" {
				logMsg = reason
			}

			_ = database.AddLog(&models.Log{
				IssueID:   issueID,
				SessionID: sess.ID,
				Message:   logMsg,
				Type:      models.LogTypeProgress,
			})

			// Record git snapshot
			if gitErr == nil {
				_ = database.AddGitSnapshot(&models.GitSnapshot{
					IssueID:    issueID,
					Event:      "start",
					CommitSHA:  gitState.CommitSHA,
					Branch:     gitState.Branch,
					DirtyFiles: gitState.DirtyFiles,
				})
			}

			if isJSON {
				// Re-fetch the persisted record and emit one JSON object per id
				// (NDJSON in the bulk case), mirroring the review family.
				started2, ferr := database.GetIssue(issueID)
				if ferr != nil {
					started2 = issue
				}
				extra := map[string]any{"session": sess.ID}
				if gitErr == nil {
					extra["git"] = map[string]any{
						"commit_sha": gitState.CommitSHA,
						"branch":     gitState.Branch,
						"is_clean":   gitState.IsClean,
						"modified":   gitState.Modified,
						"untracked":  gitState.Untracked,
					}
				}
				if err := output.EmitIssue("started", started2, extra); err != nil {
					return err
				}
			} else {
				fmt.Printf("STARTED %s (session: %s)\n", issueID, sess.ID)
			}
			started++
		}

		// Set focus to first issue if single issue, or clear if multiple
		if len(args) == 1 && started == 1 {
			_ = database.SetFocus(scope, args[0])
		}

		// Show git state once at the end
		if gitErr == nil && started > 0 && !isJSON {
			stateStr := "clean"
			if !gitState.IsClean {
				stateStr = fmt.Sprintf("%d modified, %d untracked", gitState.Modified, gitState.Untracked)
			}
			fmt.Printf("Git: %s (%s) %s\n", output.ShortSHA(gitState.CommitSHA), gitState.Branch, stateStr)

			if !gitState.IsClean {
				output.Warning("Starting with uncommitted changes")
			}
		}

		if len(args) > 1 && !isJSON {
			fmt.Printf("\nStarted %d, skipped %d\n", started, failed+noop)
		}

		// A named batch succeeds when at least one issue started, and an
		// already-in-progress retry succeeds as an idempotent no-op. If no
		// issue started and any true failure occurred, report failure even
		// when the same batch also contained no-ops.
		if started == 0 && failed > 0 {
			return errStartAllFailed
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().String("reason", "", "Reason for starting work")
	startCmd.Flags().Bool("force", false, "Force start even if blocked")
}
