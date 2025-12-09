package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
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
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
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
		handoff.Done, stdinUsed = expandFlagValues(done, stdinUsed)
		handoff.Remaining, stdinUsed = expandFlagValues(remaining, stdinUsed)
		handoff.Decisions, stdinUsed = expandFlagValues(decisions, stdinUsed)
		handoff.Uncertain, stdinUsed = expandFlagValues(uncertain, stdinUsed)

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
			database.AddGitSnapshot(&models.GitSnapshot{
				IssueID:    issueID,
				Event:      "handoff",
				CommitSHA:  gitState.CommitSHA,
				Branch:     gitState.Branch,
				DirtyFiles: gitState.DirtyFiles,
			})
		}

		// Update issue timestamp
		database.UpdateIssue(issue)

		// Output
		fmt.Printf("HANDOFF RECORDED %s\n", issueID)

		if gitErr == nil {
			// Check for commits since start
			startSnapshot, _ := database.GetStartSnapshot(issueID)
			if startSnapshot != nil {
				commits, _ := git.GetCommitsSince(startSnapshot.CommitSHA)
				fmt.Printf("Git: %s (%s) +%d commits since start\n",
					gitState.CommitSHA[:7], gitState.Branch, commits)

				// Show diff stats
				diffStats, err := git.GetDiffStatsSince(startSnapshot.CommitSHA)
				if err == nil && diffStats.FilesChanged > 0 {
					fmt.Printf("Changed: %d files (+%d -%d)\n",
						diffStats.FilesChanged, diffStats.Additions, diffStats.Deletions)
				}
			}
		}

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

// expandFlagValues expands flag values that use - (stdin) or @file syntax
// Returns the expanded values and whether stdin was consumed
func expandFlagValues(values []string, stdinUsed bool) ([]string, bool) {
	var result []string
	for _, v := range values {
		if v == "-" {
			// Read from stdin (one item per line)
			if stdinUsed {
				output.Warning("stdin already used, ignoring additional - flag")
				continue
			}
			stdinUsed = true
			lines := readLinesFromReader(os.Stdin)
			result = append(result, lines...)
		} else if strings.HasPrefix(v, "@") {
			// Read from file
			path := strings.TrimPrefix(v, "@")
			file, err := os.Open(path)
			if err != nil {
				output.Warning("failed to read %s: %v", path, err)
				continue
			}
			lines := readLinesFromReader(file)
			file.Close()
			result = append(result, lines...)
		} else {
			result = append(result, v)
		}
	}
	return result, stdinUsed
}

// readLinesFromReader reads non-empty lines from a reader
func readLinesFromReader(r io.Reader) []string {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func init() {
	rootCmd.AddCommand(handoffCmd)

	handoffCmd.Flags().StringArray("done", nil, "Completed item (repeatable)")
	handoffCmd.Flags().StringArray("remaining", nil, "Remaining item (repeatable)")
	handoffCmd.Flags().StringArray("decision", nil, "Decision made (repeatable)")
	handoffCmd.Flags().StringArray("uncertain", nil, "Uncertainty (repeatable)")
}
