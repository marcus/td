package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context [issue-id]",
	Short: "Generate contextual summary for resuming work",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Get issue ID from args or focus
		issueID := ""
		if len(args) > 0 {
			issueID = args[0]
		} else {
			issueID, _ = config.GetFocus(baseDir)
		}

		if issueID == "" {
			output.Error("no issue specified and no focused issue")
			return fmt.Errorf("no issue specified")
		}

		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		full, _ := cmd.Flags().GetBool("full")

		// Get handoff
		handoff, _ := database.GetLatestHandoff(issueID)

		// Get logs
		logLimit := 5
		if full {
			logLimit = 0
		}
		logs, _ := database.GetLogs(issueID, logLimit)

		// Get linked files
		files, _ := database.GetLinkedFiles(issueID)

		// Get dependencies
		deps, _ := database.GetDependencies(issueID)
		blocked, _ := database.GetBlockedBy(issueID)

		// Get start snapshot and current git state
		startSnapshot, _ := database.GetStartSnapshot(issueID)
		var gitState *git.State
		var commitsSinceStart int
		var diffStats *git.DiffStats
		if startSnapshot != nil {
			gitState, _ = git.GetState()
			commitsSinceStart, _ = git.GetCommitsSince(startSnapshot.CommitSHA)
			diffStats, _ = git.GetDiffStatsSince(startSnapshot.CommitSHA)
		}

		if jsonOutput {
			result := map[string]interface{}{
				"issue":      issue,
				"handoff":    handoff,
				"logs":       logs,
				"files":      files,
				"depends_on": deps,
				"blocks":     blocked,
			}
			if startSnapshot != nil {
				gitInfo := map[string]interface{}{
					"start_commit": startSnapshot.CommitSHA,
					"start_branch": startSnapshot.Branch,
					"started_at":   startSnapshot.Timestamp,
				}
				if gitState != nil {
					gitInfo["current_commit"] = gitState.CommitSHA
					gitInfo["current_branch"] = gitState.Branch
					gitInfo["commits_since_start"] = commitsSinceStart
					gitInfo["dirty_files"] = gitState.DirtyFiles
				}
				if diffStats != nil {
					gitInfo["files_changed"] = diffStats.FilesChanged
					gitInfo["additions"] = diffStats.Additions
					gitInfo["deletions"] = diffStats.Deletions
				}
				result["git"] = gitInfo
			}
			return output.JSON(result)
		}

		// Text output
		fmt.Printf("CONTEXT: %s \"%s\"\n", issue.ID, issue.Title)
		fmt.Println()

		if handoff != nil {
			fmt.Printf("LATEST HANDOFF (%s, %s):\n", handoff.SessionID, output.FormatTimeAgo(handoff.Timestamp))
			if len(handoff.Done) > 0 {
				fmt.Printf("  Done: %s\n", joinItems(handoff.Done))
			}
			if len(handoff.Remaining) > 0 {
				fmt.Printf("  Remaining: %s\n", joinItems(handoff.Remaining))
			}
			if len(handoff.Decisions) > 0 {
				fmt.Printf("  Decisions: %s\n", joinItems(handoff.Decisions))
			}
			if len(handoff.Uncertain) > 0 {
				fmt.Printf("  Uncertain: %s\n", joinItems(handoff.Uncertain))
			}
			fmt.Println()
		}

		if len(files) > 0 {
			fmt.Println("FILES TOUCHED:")
			for _, f := range files {
				fmt.Printf("  %s\n", f.FilePath)
			}
			fmt.Println()
		}

		if len(logs) > 0 {
			fmt.Printf("SESSION LOG (last %d):\n", len(logs))
			for _, log := range logs {
				typeLabel := ""
				if log.Type != models.LogTypeProgress {
					typeLabel = fmt.Sprintf(" %s:", log.Type)
				}
				fmt.Printf("  [%s]%s %s\n", log.Timestamp.Format("15:04"), typeLabel, log.Message)
			}
			fmt.Println()
		}

		// Git state
		if startSnapshot != nil {
			fmt.Println("GIT STATE:")
			fmt.Printf("  Started: %s (%s) %s\n",
				startSnapshot.CommitSHA[:7], startSnapshot.Branch, output.FormatTimeAgo(startSnapshot.Timestamp))
			if gitState != nil {
				fmt.Printf("  Current: %s (%s) +%d commits\n",
					gitState.CommitSHA[:7], gitState.Branch, commitsSinceStart)
				if diffStats != nil && diffStats.FilesChanged > 0 {
					fmt.Printf("  Changed: %d files (+%d -%d)\n",
						diffStats.FilesChanged, diffStats.Additions, diffStats.Deletions)
				}
			}
			fmt.Println()
		}

		// Dependencies
		if len(deps) > 0 {
			fmt.Print("BLOCKED BY: ")
			for i, depID := range deps {
				if i > 0 {
					fmt.Print(", ")
				}
				dep, _ := database.GetIssue(depID)
				if dep != nil {
					fmt.Printf("%s \"%s\"", dep.ID, dep.Title)
				} else {
					fmt.Print(depID)
				}
			}
			fmt.Println()
		} else {
			fmt.Println("BLOCKED BY: nothing")
		}

		if len(blocked) > 0 {
			fmt.Print("BLOCKS: ")
			for i, id := range blocked {
				if i > 0 {
					fmt.Print(", ")
				}
				b, _ := database.GetIssue(id)
				if b != nil {
					fmt.Printf("%s \"%s\"", b.ID, b.Title)
				} else {
					fmt.Print(id)
				}
			}
			fmt.Println()
		}

		return nil
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume [issue-id]",
	Short: "Show context and set focus",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// Show context
		contextCmd.Run(cmd, args)

		// Set focus
		config.SetFocus(baseDir, args[0])
		fmt.Printf("FOCUSED %s\n", args[0])

		return nil
	},
}

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Generate optimized context block for AI agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Use GetOrCreate to detect context changes and auto-rotate sessions
		sess, err := session.GetOrCreate(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		compact, _ := cmd.Flags().GetBool("compact")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Get focused issue
		focusedID, _ := config.GetFocus(baseDir)
		var focusedIssue *models.Issue
		if focusedID != "" {
			focusedIssue, _ = database.GetIssue(focusedID)
		}

		// Get active work session
		wsID, _ := config.GetActiveWorkSession(baseDir)
		var activeWS *models.WorkSession
		var wsIssues []string
		if wsID != "" {
			activeWS, _ = database.GetWorkSession(wsID)
			wsIssues, _ = database.GetWorkSessionIssues(wsID)
		}

		// Get reviewable issues
		reviewable, _ := database.ListIssues(db.ListIssuesOptions{
			ReviewableBy: sess.ID,
		})

		// Get ready issues
		ready, _ := database.ListIssues(db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen},
			SortBy: "priority",
			Limit:  3,
		})

		if jsonOutput {
			result := map[string]interface{}{
				"session":     sess.ID,
				"focused":     focusedIssue,
				"work_session": activeWS,
				"ws_issues":   wsIssues,
				"reviewable":  reviewable,
				"ready":       ready,
			}
			return output.JSON(result)
		}

		// Text output
		fmt.Println("You have access to `td`, a local task management CLI.")
		fmt.Println()

		// Show NEW SESSION notice if session just rotated
		if sess.IsNew && sess.PreviousSessionID != "" {
			fmt.Printf("NEW SESSION: %s (previous: %s)\n", sess.ID, sess.PreviousSessionID)
			fmt.Println("  You are a new context. You can now review issues implemented by the previous session.")
			fmt.Println()
		} else if sess.IsNew {
			fmt.Printf("NEW SESSION: %s\n", sess.ID)
			fmt.Println()
		}

		fmt.Printf("CURRENT SESSION: %s\n", sess.ID)

		if activeWS != nil {
			fmt.Printf("WORK SESSION: %s \"%s\" (%d issues tagged)\n", activeWS.ID, activeWS.Name, len(wsIssues))
		}
		fmt.Println()

		if focusedIssue != nil {
			fmt.Printf("FOCUSED ISSUE: %s \"%s\" %s\n", focusedIssue.ID, focusedIssue.Title, output.FormatStatus(focusedIssue.Status))

			handoff, _ := database.GetLatestHandoff(focusedID)
			if handoff != nil {
				fmt.Printf("  Last handoff (%s):\n", output.FormatTimeAgo(handoff.Timestamp))
				if len(handoff.Done) > 0 {
					fmt.Printf("    Done: %s\n", joinItems(handoff.Done))
				}
				if len(handoff.Remaining) > 0 {
					fmt.Printf("    Remaining: %s\n", joinItems(handoff.Remaining))
				}
				if len(handoff.Uncertain) > 0 {
					fmt.Printf("    Uncertain: %s\n", joinItems(handoff.Uncertain))
				}
			}

			files, _ := database.GetLinkedFiles(focusedID)
			if len(files) > 0 {
				fmt.Printf("  Files: ")
				for i, f := range files {
					if i > 0 {
						fmt.Print(", ")
					}
					if i >= 3 {
						fmt.Printf("... +%d more", len(files)-3)
						break
					}
					fmt.Print(f.FilePath)
				}
				fmt.Println()
			}
			fmt.Println()
		}

		if len(reviewable) > 0 {
			fmt.Printf("AWAITING YOUR REVIEW (%d issues):\n", len(reviewable))
			for _, issue := range reviewable {
				fmt.Printf("  %s \"%s\" %s - impl by %s\n", issue.ID, issue.Title, issue.Priority, issue.ImplementerSession)
			}
			fmt.Println()
		}

		if len(ready) > 0 {
			fmt.Printf("READY TO START (%d issues):\n", len(ready))
			for _, issue := range ready {
				fmt.Printf("  %s \"%s\" %s %s\n", issue.ID, issue.Title, issue.Priority, issue.Type)
			}
			fmt.Println()
		}

		if !compact {
			fmt.Println("WORKFLOWS:")
			fmt.Println()
			fmt.Println("  Single-issue:")
			fmt.Println("    1. `td start <id>` to begin work")
			fmt.Println("    2. `td log \"message\"` to track progress")
			fmt.Println("    3. `td handoff <id>` to capture state (REQUIRED)")
			fmt.Println("    4. `td review <id>` to submit for review")
			fmt.Println()
			fmt.Println("  Multi-issue (recommended for agents):")
			fmt.Println("    1. `td ws start \"name\"` to begin work session")
			fmt.Println("    2. `td ws tag <ids>` to associate issues")
			fmt.Println("    3. `td ws log \"message\"` to track progress")
			fmt.Println("    4. `td ws handoff` to capture state and end session")
			fmt.Println()
			fmt.Println("KEY COMMANDS:")
			fmt.Println("  td current              What you're working on")
			fmt.Println("  td ws current           Current work session state")
			fmt.Println("  td context <id>         Full context for resuming")
			fmt.Println("  td next                 Highest priority open issue")
			fmt.Println("  td critical-path        What unblocks the most work")
			fmt.Println("  td reviewable           Issues you can review")
			fmt.Println("  td ws log \"msg\"         Track progress (multi-issue)")
			fmt.Println("  td ws handoff           Capture state, end session")
			fmt.Println("  td approve/reject <id>  Complete review")
			fmt.Println()
			fmt.Println("IMPORTANT: You cannot approve issues you implemented.")
			fmt.Println("Use `td handoff` or `td ws handoff` before stopping work.")
		}

		return nil
	},
}

func joinItems(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += ", " + items[i]
	}
	return result
}

func init() {
	rootCmd.AddCommand(contextCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(usageCmd)

	contextCmd.Flags().Bool("full", false, "Include complete session history")
	contextCmd.Flags().Bool("json", false, "JSON output")

	usageCmd.Flags().Bool("compact", false, "Shorter output")
	usageCmd.Flags().Bool("json", false, "JSON output")
}
