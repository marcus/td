package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list [filters]",
	Aliases: []string{"ls"},
	Short:   "List issues matching given filters",
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Handle --filter flag (TDQ query expression)
		filterQuery, _ := cmd.Flags().GetString("filter")
		filterFlagProvided := cmd.Flags().Changed("filter")
		positionalQuery := ""
		if len(args) > 0 {
			positionalQuery = strings.TrimSpace(strings.Join(args, " "))
		}

		// Error if --filter provided but empty
		if filterFlagProvided && filterQuery == "" {
			output.Error("--filter requires a non-empty query expression")
			return fmt.Errorf("--filter requires a non-empty query expression")
		}

		if filterQuery != "" && positionalQuery != "" {
			output.Error("cannot use both --filter and positional query")
			return fmt.Errorf("cannot use both --filter and positional query")
		}

		queryStr := filterQuery
		if positionalQuery != "" {
			queryStr = positionalQuery
		}

		if queryStr != "" {
			// Use TDQ query engine
			sess, _ := session.GetOrCreate(database)
			sessionID := ""
			if sess != nil {
				sessionID = sess.ID
			}

			limit, _ := cmd.Flags().GetInt("limit")
			sortBy, _ := cmd.Flags().GetString("sort")
			sortDesc, _ := cmd.Flags().GetBool("reverse")

			results, err := query.Execute(database, queryStr, sessionID, query.ExecuteOptions{
				Limit:    limit,
				SortBy:   sortBy,
				SortDesc: sortDesc,
			})
			if err != nil {
				output.Error("Query error: %v", err)
				return err
			}

			// Output format
			format, _ := cmd.Flags().GetString("format")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			if format == "json" || jsonOutput {
				return output.JSON(results)
			}

			long, _ := cmd.Flags().GetBool("long")
			if format == "long" || long {
				for _, issue := range results {
					logs, _ := database.GetLogs(issue.ID, 5)
					handoff, _ := database.GetLatestHandoff(issue.ID)
					fmt.Print(output.FormatIssueLong(&issue, logs, handoff))
					fmt.Println("---")
				}
				return nil
			}

			for _, issue := range results {
				fmt.Println(output.FormatIssueShort(&issue))
			}
			if len(results) == 0 {
				fmt.Println("No issues found")
			}
			return nil
		}

		opts := db.ListIssuesOptions{}

		// Check if --all flag is set
		showAll, _ := cmd.Flags().GetBool("all")

		// Parse status filter (supports both --status open --status closed and --status open,closed)
		// Also accepts "review" as alias for "in_review" and "all" to show all statuses
		if statusStr, _ := cmd.Flags().GetStringArray("status"); len(statusStr) > 0 {
			for _, s := range statusStr {
				// Split on comma to support --status in_progress,in_review
				for _, part := range strings.Split(s, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						// Handle "all" as special value to show all statuses
						if strings.EqualFold(part, "all") {
							showAll = true
							continue
						}
						status := models.NormalizeStatus(part)
						if !models.IsValidStatus(status) {
							output.Error("invalid status: %s (valid: open, in_progress, blocked, in_review, closed, all)", part)
							return fmt.Errorf("invalid status: %s", part)
						}
						opts.Status = append(opts.Status, status)
					}
				}
			}
		}
		if !showAll && len(opts.Status) == 0 {
			// Default: exclude closed issues unless --all is specified
			opts.Status = []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			}
		}

		// Parse type filter (accepts "story" as alias for "feature")
		if typeStr, _ := cmd.Flags().GetStringArray("type"); len(typeStr) > 0 {
			for _, t := range typeStr {
				typ := models.NormalizeType(t)
				if !models.IsValidType(typ) {
					output.Error("invalid type: %s (valid: bug, feature, task, epic, chore)", t)
					return fmt.Errorf("invalid type: %s", t)
				}
				opts.Type = append(opts.Type, typ)
			}
		}

		// Parse ID filter
		if ids, _ := cmd.Flags().GetStringArray("id"); len(ids) > 0 {
			opts.IDs = ids
		}

		// Parse labels filter
		if labels, _ := cmd.Flags().GetStringArray("labels"); len(labels) > 0 {
			opts.Labels = labels
		}

		// Priority filter
		opts.Priority, _ = cmd.Flags().GetString("priority")

		// Points filter
		if pointsStr, _ := cmd.Flags().GetString("points"); pointsStr != "" {
			opts.PointsMin, opts.PointsMax = parsePointsFilter(pointsStr)
		}

		// Search filter
		opts.Search, _ = cmd.Flags().GetString("search")

		// Implementer/reviewer filters
		opts.Implementer, _ = cmd.Flags().GetString("implementer")
		opts.Reviewer, _ = cmd.Flags().GetString("reviewer")

		// Parent filter
		if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
			opts.ParentID = parentID
		}

		// Epic filter
		if epicID, _ := cmd.Flags().GetString("epic"); epicID != "" {
			opts.EpicID = epicID
		}

		// Reviewable filter
		if reviewable, _ := cmd.Flags().GetBool("reviewable"); reviewable {
			sess, err := session.GetOrCreate(database)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			opts.ReviewableBy = sess.ID
		}

		// Mine filter (issues where current session is implementer)
		if mine, _ := cmd.Flags().GetBool("mine"); mine {
			sess, err := session.GetOrCreate(database)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			opts.Implementer = sess.ID
		}

		// Open shorthand (--open is equivalent to --status open)
		if open, _ := cmd.Flags().GetBool("open"); open {
			opts.Status = []models.Status{models.StatusOpen}
		}

		// Date filters
		if created, _ := cmd.Flags().GetString("created"); created != "" {
			opts.CreatedAfter, opts.CreatedBefore = parseDateFilter(created)
		}
		if updated, _ := cmd.Flags().GetString("updated"); updated != "" {
			opts.UpdatedAfter, opts.UpdatedBefore = parseDateFilter(updated)
		}
		if closed, _ := cmd.Flags().GetString("closed"); closed != "" {
			opts.ClosedAfter, opts.ClosedBefore = parseDateFilter(closed)
		}

		// Sorting
		opts.SortBy, _ = cmd.Flags().GetString("sort")
		opts.SortDesc, _ = cmd.Flags().GetBool("reverse")

		// Limit
		opts.Limit, _ = cmd.Flags().GetInt("limit")
		if opts.Limit == 0 {
			opts.Limit = 50
		}

		issues, err := database.ListIssues(opts)
		if err != nil {
			output.Error("failed to list issues: %v", err)
			return err
		}

		// Output format (supports --json, --long, --short, and --format)
		format, _ := cmd.Flags().GetString("format")
		jsonOutput, _ := cmd.Flags().GetBool("json")
		if format == "json" || jsonOutput {
			return output.JSON(issues)
		}

		long, _ := cmd.Flags().GetBool("long")
		if format == "long" || long {
			for _, issue := range issues {
				logs, _ := database.GetLogs(issue.ID, 5)
				handoff, _ := database.GetLatestHandoff(issue.ID)
				fmt.Print(output.FormatIssueLong(&issue, logs, handoff))
				fmt.Println("---")
			}
			return nil
		}

		// Short format (default)
		for _, issue := range issues {
			fmt.Println(output.FormatIssueShort(&issue))
		}

		if len(issues) == 0 {
			fmt.Println("No issues found")
		}

		return nil
	},
}

// listShortcutResult holds the result of a shortcut list operation
type listShortcutResult struct {
	issues []models.Issue
}

// runListShortcut is the shared core for all list shortcut commands
func runListShortcut(opts db.ListIssuesOptions) (*listShortcutResult, error) {
	baseDir := getBaseDir()

	database, err := db.Open(baseDir)
	if err != nil {
		output.Error("%v", err)
		return nil, err
	}
	defer database.Close()

	issues, err := database.ListIssues(opts)
	if err != nil {
		output.Error("failed to list issues: %v", err)
		return nil, err
	}

	return &listShortcutResult{issues: issues}, nil
}

var reviewableCmd = &cobra.Command{
	Use:     "reviewable",
	Short:   "Show issues awaiting review that you can review",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
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

		result, err := runListShortcut(db.ListIssuesOptions{ReviewableBy: sess.ID})
		if err != nil {
			return err
		}

		for _, issue := range result.issues {
			fmt.Printf("%s  (impl: %s)\n", output.FormatIssueShort(&issue), issue.ImplementerSession)
		}

		if len(result.issues) == 0 {
			fmt.Println("No issues awaiting your review")
		}
		return nil
	},
}

var blockedListCmd = &cobra.Command{
	Use:     "blocked",
	Short:   "List blocked issues",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := runListShortcut(db.ListIssuesOptions{
			Status: []models.Status{models.StatusBlocked},
		})
		if err != nil {
			return err
		}

		for _, issue := range result.issues {
			fmt.Println(output.FormatIssueShort(&issue))
		}

		if len(result.issues) == 0 {
			fmt.Println("No blocked issues")
		}
		return nil
	},
}

var inReviewCmd = &cobra.Command{
	Use:     "in-review",
	Aliases: []string{"ir"},
	Short:   "List all issues currently in review",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
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

		result, err := runListShortcut(db.ListIssuesOptions{
			Status: []models.Status{models.StatusInReview},
			SortBy: "priority",
		})
		if err != nil {
			return err
		}

		for _, issue := range result.issues {
			reviewable := ""
			if issue.ImplementerSession != sess.ID {
				reviewable = " [reviewable]"
			}
			fmt.Printf("%s  (impl: %s)%s\n", output.FormatIssueShort(&issue), issue.ImplementerSession, reviewable)
		}

		if len(result.issues) == 0 {
			fmt.Println("No issues in review")
		}
		return nil
	},
}

var readyCmd = &cobra.Command{
	Use:     "ready",
	Short:   "List open issues sorted by priority",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := runListShortcut(db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen},
			SortBy: "priority",
		})
		if err != nil {
			return err
		}

		for _, issue := range result.issues {
			fmt.Println(output.FormatIssueShort(&issue))
		}

		if len(result.issues) == 0 {
			fmt.Println("No open issues")
		}
		return nil
	},
}

var nextCmd = &cobra.Command{
	Use:     "next",
	Short:   "Show highest-priority open issue",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := runListShortcut(db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen},
			SortBy: "priority",
			Limit:  1,
		})
		if err != nil {
			return err
		}

		if len(result.issues) == 0 {
			fmt.Println("No open issues")
			return nil
		}

		issue := result.issues[0]
		fmt.Println(output.FormatIssueShort(&issue))
		fmt.Println()
		fmt.Printf("Run `td start %s` to begin working on this issue.\n", issue.ID)
		return nil
	},
}

var deletedCmd = &cobra.Command{
	Use:     "deleted",
	Short:   "Show soft-deleted issues",
	GroupID: "shortcuts",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := runListShortcut(db.ListIssuesOptions{
			OnlyDeleted: true,
		})
		if err != nil {
			return err
		}

		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			return output.JSON(result.issues)
		}

		for _, issue := range result.issues {
			fmt.Println(output.FormatIssueDeleted(&issue))
		}

		if len(result.issues) == 0 {
			fmt.Println("No deleted issues")
		}
		return nil
	},
}

func parsePointsFilter(s string) (min, max int) {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, ">=") {
		if n, err := fmt.Sscanf(strings.TrimPrefix(s, ">="), "%d", &min); n != 1 || err != nil {
			return 0, 0 // Invalid format, no filter
		}
		return min, 0
	}
	if strings.HasPrefix(s, "<=") {
		if n, err := fmt.Sscanf(strings.TrimPrefix(s, "<="), "%d", &max); n != 1 || err != nil {
			return 0, 0
		}
		return 0, max
	}
	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) == 2 {
			n1, _ := fmt.Sscanf(parts[0], "%d", &min)
			n2, _ := fmt.Sscanf(parts[1], "%d", &max)
			if n1 == 1 && n2 == 1 {
				return min, max
			}
			return 0, 0
		}
	}

	// Exact match
	var exact int
	if n, err := fmt.Sscanf(s, "%d", &exact); n != 1 || err != nil {
		return 0, 0
	}
	return exact, exact
}

func parseDateFilter(s string) (after, before time.Time) {
	s = strings.TrimSpace(s)

	// Handle "after:DATE" format
	if strings.HasPrefix(s, "after:") {
		dateStr := strings.TrimPrefix(s, "after:")
		after, _ = time.Parse("2006-01-02", dateStr)
		return after, time.Time{}
	}

	// Handle "before:DATE" format
	if strings.HasPrefix(s, "before:") {
		dateStr := strings.TrimPrefix(s, "before:")
		before, _ = time.Parse("2006-01-02", dateStr)
		return time.Time{}, before
	}

	// Handle "DATE.." format (after)
	if strings.HasSuffix(s, "..") {
		dateStr := strings.TrimSuffix(s, "..")
		after, _ = time.Parse("2006-01-02", dateStr)
		return after, time.Time{}
	}

	// Handle "..DATE" format (before)
	if strings.HasPrefix(s, "..") {
		dateStr := strings.TrimPrefix(s, "..")
		before, _ = time.Parse("2006-01-02", dateStr)
		return time.Time{}, before
	}

	// Handle "DATE..DATE" format (range)
	if strings.Contains(s, "..") {
		parts := strings.Split(s, "..")
		if len(parts) == 2 {
			after, _ = time.Parse("2006-01-02", parts[0])
			before, _ = time.Parse("2006-01-02", parts[1])
			return after, before
		}
	}

	// Exact date - treat as entire day
	date, err := time.Parse("2006-01-02", s)
	if err == nil {
		return date, date.Add(24 * time.Hour)
	}

	return time.Time{}, time.Time{}
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(reviewableCmd)
	rootCmd.AddCommand(blockedListCmd)
	rootCmd.AddCommand(inReviewCmd)
	rootCmd.AddCommand(readyCmd)
	rootCmd.AddCommand(nextCmd)
	rootCmd.AddCommand(deletedCmd)

	listCmd.Flags().StringArrayP("id", "i", nil, "Filter by issue IDs")
	listCmd.Flags().StringArrayP("status", "s", nil, "Status filter")
	listCmd.Flags().StringArrayP("type", "t", nil, "Type filter")
	listCmd.Flags().StringArrayP("labels", "l", nil, "Labels filter")
	listCmd.Flags().StringP("priority", "p", "", "Priority filter")
	listCmd.Flags().String("points", "", "Points filter")
	listCmd.Flags().StringP("search", "q", "", "Search title/description")
	listCmd.Flags().String("implementer", "", "Filter by implementer session")
	listCmd.Flags().String("reviewer", "", "Filter by reviewer session")
	listCmd.Flags().Bool("reviewable", false, "Show issues you can review")
	listCmd.Flags().String("parent", "", "Filter by parent issue ID")
	listCmd.Flags().String("epic", "", "Filter by epic (shows all tasks within epic)")
	listCmd.Flags().BoolP("mine", "m", false, "Show issues where you are the implementer")
	listCmd.Flags().BoolP("open", "o", false, "Show only open issues (shorthand for --status open)")
	listCmd.Flags().String("created", "", "Created date filter")
	listCmd.Flags().String("updated", "", "Updated date filter")
	listCmd.Flags().String("closed", "", "Closed date filter")
	listCmd.Flags().String("sort", "", "Sort by field")
	listCmd.Flags().BoolP("reverse", "r", false, "Reverse sort order")
	listCmd.Flags().IntP("limit", "n", 50, "Limit results")
	listCmd.Flags().Bool("long", false, "Detailed output")
	listCmd.Flags().Bool("short", false, "Compact output (default)")
	listCmd.Flags().Bool("json", false, "JSON output")
	listCmd.Flags().BoolP("all", "a", false, "Include closed issues (by default, closed issues are hidden)")

	deletedCmd.Flags().Bool("json", false, "JSON output")

	listCmd.Flags().String("format", "", "Output format (short, long, json)")
	listCmd.Flags().Bool("no-pager", false, "Disable paging (no-op, td list does not page)")
	listCmd.Flags().StringP("filter", "f", "", "TDQ query expression (e.g., 'status=open AND type=bug')")
}
