package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/input"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var handoffCmd = &cobra.Command{
	Use:   "handoff <issue-id> [message]",
	Short: "Capture structured working state",
	Long: `Required before submitting for review. Captures git state automatically.

Accepts YAML-like format via stdin:
  done:
    - Item completed
  remaining:
    - Item to do
  decisions:
    - Decision made
  uncertain:
    - Question/uncertainty

Or use flags with values, stdin (-), or file (@path):
  --done "item"          Single item
  --done @done.txt       Items from file (one per line)
  echo "item" | td handoff ID --done -   Items from stdin`,
	GroupID: "workflow",
	Args:    cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		// Resolve issue ID: from args or focused issue
		var issueArg string
		var messageArg string
		switch len(args) {
		case 2:
			issueArg = args[0]
			messageArg = args[1]
		case 1:
			// Check if the single arg is an issue ID or a message
			if issueIDPattern.MatchString(args[0]) {
				issueArg = args[0]
			} else {
				// Treat as message, infer issue from focus
				messageArg = args[0]
			}
		}

		if issueArg == "" {
			// Infer from focused issue
			focusedID, err := config.GetFocus(baseDir)
			if err != nil || focusedID == "" {
				output.Error("no issue specified and no focused issue")
				fmt.Fprintln(os.Stderr, "  Use: td handoff <id> [message]  OR  td start <id> first")
				return fmt.Errorf("no issue specified")
			}
			issueArg = focusedID
		}

		if err := ValidateIssueID(issueArg, "handoff <issue-id> [message]"); err != nil {
			output.Error("%v", err)
			return err
		}

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

		issueID := issueArg
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		handoff := &models.Handoff{
			IssueID:   issueID,
			SessionID: sess.ID,
		}

		// Get from flags with stdin/file expansion
		done, _ := cmd.Flags().GetStringArray("done")
		remaining, _ := cmd.Flags().GetStringArray("remaining")
		decisions, _ := cmd.Flags().GetStringArray("decision")
		uncertain, _ := cmd.Flags().GetStringArray("uncertain")

		var stdinUsed bool
		handoff.Done, stdinUsed = input.ExpandFlagValues(done, stdinUsed)
		handoff.Remaining, stdinUsed = input.ExpandFlagValues(remaining, stdinUsed)
		handoff.Decisions, stdinUsed = input.ExpandFlagValues(decisions, stdinUsed)
		handoff.Uncertain, stdinUsed = input.ExpandFlagValues(uncertain, stdinUsed)

		// Handle --note/-n or --message/-m flag for simple handoffs
		note, _ := cmd.Flags().GetString("note")
		message, _ := cmd.Flags().GetString("message")
		if note != "" {
			handoff.Done = append(handoff.Done, note)
		}
		if message != "" {
			handoff.Done = append(handoff.Done, message)
		}

		// Handle positional message argument
		if messageArg != "" {
			handoff.Done = append(handoff.Done, messageArg)
		}

		// Check if stdin has data (YAML format) - only if not already used by flag expansion
		if !stdinUsed {
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				if stat.Size() > 0 {
					parseHandoffInput(handoff)
				}
			}
		}

		if err := database.AddHandoff(handoff); err != nil {
			output.Error("failed to record handoff: %v", err)
			return err
		}

		// Capture git state
		gitState, gitErr := git.GetState()
		if gitErr == nil {
			if err := database.AddGitSnapshot(&models.GitSnapshot{
				IssueID:    issueID,
				Event:      "handoff",
				CommitSHA:  gitState.CommitSHA,
				Branch:     gitState.Branch,
				DirtyFiles: gitState.DirtyFiles,
			}); err != nil {
				output.Warning("failed to save git snapshot: %v", err)
			}
		}

		// Update issue timestamp
		if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
			output.Warning("failed to update issue: %v", err)
		}

		// Output
		fmt.Printf("HANDOFF RECORDED %s\n", issueID)

		if gitErr == nil {
			// Check for commits since start
			startSnapshot, _ := database.GetStartSnapshot(issueID)
			if startSnapshot != nil {
				commits, _ := git.GetCommitsSince(startSnapshot.CommitSHA)
				fmt.Printf("Git: %s (%s) +%d commits since start\n",
					output.ShortSHA(gitState.CommitSHA), gitState.Branch, commits)

				// Show diff stats
				diffStats, err := git.GetDiffStatsSince(startSnapshot.CommitSHA)
				if err == nil && diffStats.FilesChanged > 0 {
					fmt.Printf("Changed: %d files (+%d -%d)\n",
						diffStats.FilesChanged, diffStats.Additions, diffStats.Deletions)
				}
			}
		}

		// Cascade to descendants if this is a parent issue
		hasChildren, err := database.HasChildren(issueID)
		if err != nil {
			output.Warning("check children: %v", err)
		}
		if hasChildren {
			descendants, err := database.GetDescendantIssues(issueID, []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
			})
			if err == nil && len(descendants) > 0 {
				cascaded := 0
				skippedExisting := 0

				for _, child := range descendants {
					// Skip children that already have handoffs
					existingHandoff, err := database.GetLatestHandoff(child.ID)
					if err != nil {
						output.Warning("check handoff %s: %v", child.ID, err)
						continue
					}
					if existingHandoff != nil {
						skippedExisting++
						continue
					}

					// Create lightweight cascaded handoff
					childHandoff := &models.Handoff{
						IssueID:   child.ID,
						SessionID: sess.ID,
						Done:      []string{fmt.Sprintf("Cascaded from %s", issueID)},
					}

					if err := database.AddHandoff(childHandoff); err != nil {
						output.Warning("cascade handoff %s: %v", child.ID, err)
						continue
					}

					// Add log entry for visibility
					database.AddLog(&models.Log{
						IssueID:   child.ID,
						SessionID: sess.ID,
						Message:   fmt.Sprintf("Cascaded handoff from %s", issueID),
						Type:      models.LogTypeProgress,
					})

					cascaded++
				}

				if cascaded > 0 {
					fmt.Printf("  + %d descendant(s) also received handoffs\n", cascaded)
				}
				if skippedExisting > 0 {
					fmt.Printf("  - %d descendant(s) skipped (existing handoffs)\n", skippedExisting)
				}
			}
		}

		fmt.Printf("\nNext: `td review %s` to submit for review\n", issueID)

		return nil
	},
}

func parseHandoffInput(handoff *models.Handoff) {
	scanner := bufio.NewScanner(os.Stdin)
	currentSection := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Check for section headers
		if strings.HasSuffix(trimmed, ":") {
			currentSection = strings.TrimSuffix(trimmed, ":")
			continue
		}

		// Check for list items
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			item := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			item = strings.TrimSpace(item)

			switch currentSection {
			case "done":
				handoff.Done = append(handoff.Done, item)
			case "remaining":
				handoff.Remaining = append(handoff.Remaining, item)
			case "decisions":
				handoff.Decisions = append(handoff.Decisions, item)
			case "uncertain":
				handoff.Uncertain = append(handoff.Uncertain, item)
			}
		}
	}
}

func init() {
	rootCmd.AddCommand(handoffCmd)

	handoffCmd.Flags().StringArray("done", nil, "Completed item (repeatable)")
	handoffCmd.Flags().StringArray("remaining", nil, "Remaining item (repeatable)")
	handoffCmd.Flags().StringArray("decision", nil, "Decision made (repeatable)")
	handoffCmd.Flags().StringArray("uncertain", nil, "Uncertainty (repeatable)")
	handoffCmd.Flags().StringP("note", "n", "", "Simple note for handoff (alternative to structured flags)")
	handoffCmd.Flags().StringP("message", "m", "", "Simple message for handoff (alias for --note)")
}
