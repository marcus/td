package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:     "resume [issue-id]",
	Short:   "Show context and set focus",
	GroupID: "session",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// Show issue details (using show command)
		if showCmd.RunE == nil {
			return fmt.Errorf("show command is not executable")
		}
		if err := showCmd.RunE(showCmd, args); err != nil {
			return err
		}

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		_, scope, err := getCurrentStateSession(database, baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Set focus
		_ = database.SetFocus(scope, args[0])
		fmt.Printf("FOCUSED %s\n", args[0])

		return nil
	},
}

var usageCmd = &cobra.Command{
	Use:     "usage",
	Short:   "Generate optimized context block for AI agents",
	GroupID: "session",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		compact, _ := cmd.Flags().GetBool("compact")
		quiet, _ := cmd.Flags().GetBool("quiet")
		jsonOutput := jsonMode(cmd)
		newSession, _ := cmd.Flags().GetBool("new-session")

		// Use GetOrCreate to detect context changes and auto-rotate sessions.
		// If --new-session is set, force creation of a new session (useful at conversation start / after /clear).
		var sess *session.Session
		if newSession {
			sess, err = session.ForceNewSession(database)
		} else {
			sess, err = session.GetOrCreate(database)
		}
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get focused issue
		scope := currentStateScope(baseDir, sess)

		focusedID, _ := database.GetFocus(scope)
		var focusedIssue *models.Issue
		if focusedID != "" {
			focusedIssue, _ = database.GetIssue(focusedID)
		}

		// Get active work session
		wsID, _ := database.GetActiveWorkSession(scope)
		var activeWS *models.WorkSession
		var wsIssues []string
		if wsID != "" {
			activeWS, _ = database.GetWorkSession(wsID)
			wsIssues, _ = database.GetWorkSessionIssues(wsID)
		}

		// Get in-progress issues for this session
		inProgress, _ := database.ListIssues(db.ListIssuesOptions{
			Status:      []models.Status{models.StatusInProgress},
			Implementer: sess.ID,
			SortBy:      "priority",
		})

		// Get reviewable issues and split into awaiting/ready-to-close.
		allReviewable, _ := database.ListIssues(reviewableByOptions(baseDir, sess.ID))
		reviewable := make([]models.Issue, 0, len(allReviewable))
		readyToClose := make([]models.Issue, 0)
		readyReviews := make(map[string]*models.IssueReview)
		for _, issue := range allReviewable {
			rev, _ := database.GetActiveApprovalReview(issue.ID)
			if rev == nil {
				reviewable = append(reviewable, issue)
				continue
			}
			if closerAllowed(&issue, sess.ID, rev) {
				readyToClose = append(readyToClose, issue)
				readyReviews[issue.ID] = rev
			}
		}

		// Get ready issues (open, not blocked by dependencies)
		ready, _ := database.ListIssues(db.ListIssuesOptions{
			Status:             []models.Status{models.StatusOpen},
			SortBy:             "priority",
			Limit:              3,
			ExcludeHasOpenDeps: true,
		})

		if jsonOutput {
			result := map[string]interface{}{
				"session":        sess.ID,
				"focused":        focusedIssue,
				"work_session":   activeWS,
				"ws_issues":      jsonList(wsIssues),
				"in_progress":    jsonList(inProgress),
				"reviewable":     jsonList(reviewable),
				"ready_to_close": jsonList(readyToClose),
				"ready":          jsonList(ready),
			}
			return output.JSON(result)
		}

		// Text output
		fmt.Println("You have access to `td`, a local task management CLI.")
		fmt.Println()

		// Show NEW SESSION notice if session just rotated
		if sess.IsNew && sess.PreviousSessionID != "" {
			fmt.Printf("NEW SESSION: %s on branch: %s (previous: %s)\n", sess.ID, sess.Branch, sess.PreviousSessionID)
			fmt.Println("  You are a new context. Issues from the previous session may be awaiting review.")
			fmt.Println()
		} else if sess.IsNew {
			fmt.Printf("NEW SESSION: %s on branch: %s\n", sess.ID, sess.Branch)
			fmt.Println()
		}

		fmt.Printf("CURRENT SESSION: %s on branch: %s\n", sess.DisplayWithAgent(), sess.Branch)

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

		if len(inProgress) > 0 {
			fmt.Printf("IN PROGRESS (%d issues):\n", len(inProgress))
			for _, issue := range inProgress {
				fmt.Printf("  %s \"%s\" %s %s\n", issue.ID, issue.Title, issue.Priority, issue.Type)
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

		if len(readyToClose) > 0 {
			fmt.Printf("READY TO CLOSE (%d issues) — review already recorded:\n", len(readyToClose))
			for _, issue := range readyToClose {
				rev := readyReviews[issue.ID]
				fmt.Printf("  %s \"%s\" %s - reviewed by %s (run `td approve %s` to close)\n", issue.ID, issue.Title, issue.Priority, rev.ReviewerSession, issue.ID)
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

		if !compact && !quiet {
			fmt.Println("WORKFLOW:")
			fmt.Println()
			fmt.Println("  1. `td start <id>` to begin work")
			fmt.Println("     Multi-issue: `td ws start \"name\"` then `td ws tag <ids>`")
			fmt.Println("  2. `td log \"msg\"` to track progress")
			fmt.Println("     Multi-issue: `td ws log \"msg\"`")
			fmt.Println("  3. `td handoff <id>` to capture state for another context")
			fmt.Println("     Multi-issue: `td ws handoff`")
			fmt.Println("  4. `td review <id>` to submit for review")
			fmt.Println("     (the submitting session is recorded as review_requested_by_session)")
			fmt.Println("  5. Reviewer: `td approve <id>` to review + close, or `td reject <id>` to return to open")
			fmt.Println("     Prefer an independent reviewer when practical; they run")
			fmt.Println("     `td approve <id> --reason \"...\"` to approve and close.")
			fmt.Println("     Trusted mode (default), if you implemented it: name whoever reviewed it")
			fmt.Println("     with `td approve <id> --reviewed-by \"<who>\"`, or if you reviewed your")
			fmt.Println("     own work, `td approve <id> --self-review --reason \"...\"`.")
			fmt.Println()
			fmt.Println("  Use `td ws` commands when implementing multiple related issues.")
			fmt.Println()
			fmt.Println("KEY COMMANDS:")
			fmt.Println("  td current              What you're working on")
			fmt.Println("  td ws current           Current work session state")
			fmt.Println("  td context <id>         Full context for resuming")
			fmt.Println("  td next                 Highest priority open issue")
			fmt.Println("  td critical-path        What unblocks the most work")
			fmt.Println("  td status --json        Machine-readable session and review state")
			fmt.Println("  td list --json          Machine-readable issue listings for scripts")
			fmt.Println("  td reviewable           Issues you can review")
			fmt.Println("  td reviewable --include-approved  Also show reviewed issues you can close")
			fmt.Println("  td approve/reject <id>  Complete review")
			fmt.Println()
			fmt.Println("REVIEW (review_policy_mode=trusted, the default):")
			fmt.Println("  Closing needs a review, and td asks who performed it.")
			fmt.Println("  An independent reviewer runs `td approve <id> --reason \"...\"`,")
			fmt.Println("  or attests without closing via `--record-only --reason \"...\"`")
			fmt.Println("  so any session can close after.")
			fmt.Println("  If you implemented it and a sub-agent reviewed it, name them:")
			fmt.Println("  `td approve <id> --reviewed-by \"<who>\"` (no --reason needed).")
			fmt.Println("  If you reviewed it yourself, say so:")
			fmt.Println("  `td approve <id> --self-review --reason \"...\"` (audited).")
			fmt.Println("  td cannot verify --reviewed-by. Never name a reviewer who did not review.")
			fmt.Println("  Pin delegated|strict for a mechanical independence boundary.")
			fmt.Println("  Exception: `td add \"title\" --minor` creates self-reviewable tasks.")
			fmt.Println()
			fmt.Println("Use `td review` -> `td approve` for completed work.")
			fmt.Println("  `td close` is for admin closures: duplicates, won't-fix, cleanup.")
			fmt.Println()
			fmt.Println("Leave a `td handoff` or `td ws handoff` when work will continue elsewhere.")
			fmt.Println()
			fmt.Println("For a future agent context, start with `td usage --new-session -q`.")
			fmt.Println("Use `td ws start` when related issues benefit from a shared handoff.")
			fmt.Println("  - session = identity (always exists)  |  ws = work container (optional)")
			fmt.Println()
			fmt.Println("Run `td <command> --help` for command details.")
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
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(usageCmd)

	usageCmd.Flags().Bool("compact", false, "Shorter output")
	usageCmd.Flags().BoolP("quiet", "q", false, "Hide workflow instructions (show only actionable items)")
	usageCmd.Flags().Bool("new-session", false, "Force create a new session (use at conversation start / after /clear)")
}
