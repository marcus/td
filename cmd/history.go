package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:     "history <issue-id>",
	Aliases: []string{"timeline"},
	Short:   "Show chronological timeline of an issue's lifecycle",
	Long: `Display a unified chronological timeline of all events for an issue.

Merges logs, handoffs, comments, git snapshots, and status changes
into a single narrative view.

Examples:
  td history td-abc1            # Full timeline
  td history td-abc1 --limit 10 # Last 10 events
  td history td-abc1 --json     # Machine-readable output`,
	GroupID: "query",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueID := args[0]
		if err := ValidateIssueID(issueID, "history <issue-id>"); err != nil {
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

		// Verify issue exists
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		limit, _ := cmd.Flags().GetInt("limit")
		events, err := database.GetIssueTimeline(issueID, limit)
		if err != nil {
			output.Error("failed to build timeline: %v", err)
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")
		if jsonOutput {
			return output.JSON(map[string]any{
				"issue_id": issue.ID,
				"title":    issue.Title,
				"status":   issue.Status,
				"events":   events,
			})
		}

		// Narrative output
		fmt.Printf("Timeline: %s \"%s\" [%s]\n\n", issue.ID, issue.Title, issue.Status)

		if len(events) == 0 {
			fmt.Println("  No events recorded.")
			return nil
		}

		for _, ev := range events {
			icon := eventIcon(ev.EventType)
			ts := output.FormatTimeAgo(ev.Timestamp)
			sessionInfo := ""
			if ev.SessionID != "" {
				sessionInfo = fmt.Sprintf(" (%s)", ev.SessionID)
			}
			fmt.Printf("  %s %s  %s%s\n", icon, ts, ev.Summary, sessionInfo)
			if ev.Detail != "" {
				// Indent detail lines
				fmt.Printf("    %s\n", ev.Detail)
			}
		}

		return nil
	},
}

func eventIcon(t models.EventType) string {
	switch t {
	case models.EventStatusChange:
		return "~"
	case models.EventLog:
		return "#"
	case models.EventHandoff:
		return ">"
	case models.EventGitSnapshot:
		return "@"
	case models.EventComment:
		return "*"
	case models.EventFileLink:
		return "+"
	default:
		return "-"
	}
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().Bool("json", false, "Machine-readable JSON output")
	historyCmd.Flags().Int("limit", 0, "Limit to last N events")
}
