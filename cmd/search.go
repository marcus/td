package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:     "search [query]",
	Short:   "Full-text search across issues",
	Long:    `Search title, description, logs, and handoff content.`,
	GroupID: "query",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

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
		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			return output.JSON(results)
		}

		showScore, _ := cmd.Flags().GetBool("show-score")
		for _, result := range results {
			line := output.FormatIssueShort(&result.Issue)
			if showScore {
				line += fmt.Sprintf(" [score:%d]", result.Score)
			}
			fmt.Println(line)
		}

		if len(results) == 0 {
			fmt.Printf("No issues matching '%s'\n", query)
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
	searchCmd.Flags().Bool("json", false, "JSON output")
	searchCmd.Flags().Bool("show-score", false, "Show relevance scores")
}
