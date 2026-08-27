package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Full-text search across issues",
	Long: `Search issue id, title and description, plus the work recorded against
each issue: log messages and handoff content (done, remaining, decisions,
uncertain).

Matches found only in logs or handoffs rank below matches in the issue's own
title or description. Use --show-score to see which field matched.

Comments are not searched; use 'td query "comment.text ~ ..."' for those.`,
	GroupID: "query",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer func() { _ = database.Close() }()

		query := args[0]

		opts := db.ListIssuesOptions{
			Search: query,
		}

		// Parse status filter
		if statusStr, _ := cmd.Flags().GetStringArray("status"); len(statusStr) > 0 {
			for _, s := range statusStr {
				opts.Status = append(opts.Status, models.Status(s))
			}
		}

		// Parse type filter
		if typeStr, _ := cmd.Flags().GetStringArray("type"); len(typeStr) > 0 {
			for _, t := range typeStr {
				opts.Type = append(opts.Type, models.Type(t))
			}
		}

		// Parse labels filter
		if labels, _ := cmd.Flags().GetStringArray("labels"); len(labels) > 0 {
			opts.Labels = labels
		}

		// Priority filter
		opts.Priority, _ = cmd.Flags().GetString("priority")

		// Limit
		opts.Limit, _ = cmd.Flags().GetInt("limit")
		if opts.Limit == 0 {
			opts.Limit = 50
		}

		results, err := database.SearchIssuesRanked(query, opts)
		if err != nil {
			output.Error("search failed: %v", err)
			return err
		}

		// Output
		if jsonOutput := jsonMode(cmd); jsonOutput {
			return output.JSON(results)
		}

		showScore, _ := cmd.Flags().GetBool("show-score")
		for _, result := range results {
			line := output.FormatIssueShort(&result.Issue)
			if showScore {
				line += fmt.Sprintf(" [score:%d", result.Score)
				if result.MatchField != "" {
					line += " " + result.MatchField
				}
				line += "]"
			}
			fmt.Println(line)
		}

		if len(results) == 0 {
			// State the scope, so an empty result is not read as "this text
			// exists nowhere in td".
			fmt.Printf("No issues matching '%s'\n", query)
			fmt.Printf("Searched: id, title, description, logs, handoffs. Comments are not searched (try: td query \"comment.text ~ %s\")\n", query)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)

	searchCmd.Flags().StringArrayP("status", "s", nil, "Filter by status")
	searchCmd.Flags().StringArrayP("type", "t", nil, "Filter by type")
	searchCmd.Flags().StringArrayP("labels", "l", nil, "Filter by labels")
	searchCmd.Flags().StringP("priority", "p", "", "Filter by priority")
	searchCmd.Flags().IntP("limit", "n", 50, "Limit results")
	searchCmd.Flags().Bool("show-score", false, "Show relevance scores")
}
