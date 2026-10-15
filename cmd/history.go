package cmd

import (
	"fmt"
	"strings"

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

Merges status changes, field updates, reviews, logs, handoffs, comments,
git snapshots, and linked files into a single view. The log a lifecycle
command writes (e.g. an approval reason) is shown under its status change.

Times are local in text output and UTC in --json output.

Examples:
  td history td-abc1            # Full timeline
  td history td-abc1 --limit 10 # Last 10 events
  td history td-abc1 --json     # Machine-readable output`,
	GroupID: "query",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Args are validated by cobra; every later failure carries its own
		// message, and for a --json caller usage text would corrupt the output.
		cmd.SilenceUsage = true
		isJSON := jsonMode(cmd)

		issueID := args[0]
		if err := ValidateIssueID(issueID, "history <issue-id>"); err != nil {
			return reportFailure(isJSON, output.ErrCodeInvalidInput, err)
		}

		database, err := db.Open(getBaseDir())
		if err != nil {
			return reportFailure(isJSON, output.ErrCodeDatabaseError, err)
		}
		defer func() { _ = database.Close() }()

		issue, err := database.GetIssue(issueID)
		if err != nil {
			return reportFailure(isJSON, output.ErrCodeNotFound, err)
		}

		limit, _ := cmd.Flags().GetInt("limit")
		events, err := database.GetIssueTimeline(issue.ID, limit)
		if err != nil {
			return reportFailure(isJSON, output.ErrCodeDatabaseError, fmt.Errorf("build timeline: %w", err))
		}
		if events == nil {
			events = []models.TimelineEvent{}
		}

		if isJSON {
			return output.JSON(map[string]any{
				"issue_id": issue.ID,
				"title":    issue.Title,
				"status":   issue.Status,
				"events":   events,
			})
		}

		fmt.Printf("Timeline: %s %q [%s]\n\n", issue.ID, issue.Title, issue.Status)
		if len(events) == 0 {
			fmt.Println("  No events recorded.")
			return nil
		}
		printTimeline(events)
		return nil
	},
}

// historyIndent aligns continuation lines under an event's summary:
// two spaces, a 16-char time, two spaces, a padded label, a space.
var historyIndent = strings.Repeat(" ", 2+16+2+historyLabelWidth+1)

const historyLabelWidth = 7

var historyLabels = map[models.EventType]string{
	models.EventCreated:      "created",
	models.EventStatusChange: "status",
	models.EventUpdate:       "update",
	models.EventReview:       "review",
	models.EventLog:          "log",
	models.EventHandoff:      "handoff",
	models.EventGitSnapshot:  "git",
	models.EventComment:      "comment",
	models.EventFileLink:     "file",
}

func printTimeline(events []models.TimelineEvent) {
	lastSession := ""
	for _, ev := range events {
		label := historyLabels[ev.EventType]
		if label == "" {
			label = string(ev.EventType)
		}
		summary := indentContinuation(ev.Summary)
		if ev.Count > 1 {
			summary += fmt.Sprintf(" (×%d)", ev.Count)
		}
		// Name the session only when it changes, so a run of work by one
		// session reads as a block.
		if ev.SessionID != "" && ev.SessionID != lastSession {
			summary += fmt.Sprintf("  (%s)", ev.SessionID)
			lastSession = ev.SessionID
		}
		fmt.Printf("  %s  %-*s %s\n", ev.Timestamp.Local().Format("2006-01-02 15:04"), historyLabelWidth, label, summary)
		if ev.Detail != "" {
			fmt.Printf("%s%s\n", historyIndent, indentContinuation(ev.Detail))
		}
	}
}

// indentContinuation indents every line after the first, leaving blank
// lines empty.
func indentContinuation(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			lines[i] = historyIndent + lines[i]
		} else {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func init() {
	rootCmd.AddCommand(historyCmd)

	historyCmd.Flags().Bool("json", false, "Machine-readable JSON output")
	historyCmd.Flags().Int("limit", 0, "Limit to last N events")
}
