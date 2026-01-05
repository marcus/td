package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/input"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var handoffCmd = &cobra.Command{
	Use:   "handoff [issue-id]",
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
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate issue ID (catch empty strings)
		if err := ValidateIssueID(args[0], "handoff <issue-id>"); err != nil {
			output.Error("%v", err)
			return err
		}
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

		issueID := args[0]
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

		// Handle --note/-n flag for simple handoffs
		if note, _ := cmd.Flags().GetString("note"); note != "" {
			handoff.Done = append(handoff.Done, note)
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
		if err := database.UpdateIssue(issue); err != nil {
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

					// Log action for undo
					handoffData, err := json.Marshal(childHandoff)
					if err != nil {
						output.Warning("marshal handoff %s: %v", child.ID, err)
						continue
					}
					if err := database.LogAction(&models.ActionLog{
						SessionID:  sess.ID,
						ActionType: models.ActionHandoff,
						EntityType: "handoff",
						EntityID:   fmt.Sprintf("%d", childHandoff.ID),
						NewData:    string(handoffData),
					}); err != nil {
						output.Warning("log undo %s: %v", child.ID, err)
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
}
