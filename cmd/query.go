package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/query"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query [expression]",
	Short: "Search issues with TDQ query language",
	Long: `Search issues using TDQ (Todo Query Language), a powerful query syntax.

QUICK START:
  td query "status = open"            All open issues
  td query "type = bug AND is(open)"  Open bugs
  td query "created >= -7d"           Created in last 7 days

BASIC SYNTAX:
  field = value         Exact match
  field != value        Not equal
  field ~ "text"        Contains (case-insensitive)
  field !~ "text"       Does not contain
  field < value         Less than
  field > value         Greater than
  field <= value        Less than or equal
  field >= value        Greater than or equal

BOOLEAN OPERATORS:
  expr AND expr         Both must match
  expr OR expr          Either matches
  NOT expr              Negation
  -field = value        Shorthand for NOT
  (expr)                Grouping

FIELDS:
  status      open, in_progress, blocked, in_review, closed
  type        bug, feature, task, epic, chore
  priority    P0, P1, P2, P3, P4 (supports <=, >=)
  points      1, 2, 3, 5, 8, 13, 21
  labels      comma-separated tags
  title       issue title
  description issue description
  created     creation date (supports relative: -7d, today, this_week)
  updated     last update date
  closed      closure date
  implementer session that started work
  reviewer    session that reviewed
  parent      direct parent issue ID
  epic        ancestor epic ID (recursive)

CROSS-ENTITY SEARCH:
  log.message ~ "text"     Search log messages
  log.type = blocker       Filter by log type
  comment.text ~ "text"    Search comments
  handoff.remaining ~ "x"  Search handoff data
  file.role = test         Issues with test files

FUNCTIONS:
  has(field)             Field is not empty
  is(status)             Shorthand: is(open) = status = open
  any(field, v1, v2)     Field matches any value
  blocks(id)             Issues that block given id
  blocked_by(id)         Issues blocked by given id
  descendant_of(id)      All children of epic (recursive)
  rework()               Issues rejected and awaiting rework

SPECIAL VALUES:
  @me                    Current session ID
  EMPTY                  Empty/null field

RELATIVE DATES:
  today, yesterday, this_week, last_week, this_month
  -7d (7 days ago), -2w (2 weeks ago), -1m (1 month ago)

EXAMPLES:
  td query "status = open"
  td query "type = bug AND priority <= P1"
  td query "created >= -7d"
  td query "implementer = @me AND is(in_progress)"
  td query "log.type = blocker"
  td query "title ~ auth OR description ~ auth"
  td query "rework()"

BOARDS:
  Save queries as reusable boards with td board:
    td board create "My Bugs" "type = bug AND implementer = @me"
    td board show "My Bugs"
    td board list                    See all saved boards

  Run 'td board --help' for full board commands.`,
	GroupID: "query",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Handle special flags
		if showExamples, _ := cmd.Flags().GetBool("examples"); showExamples {
			printQueryExamples()
			return nil
		}

		if showFields, _ := cmd.Flags().GetBool("fields"); showFields {
			printQueryFields()
			return nil
		}

		if len(args) == 0 {
			return cmd.Help()
		}

		queryStr := args[0]

		// Parse and validate query first (for --explain)
		parsedQuery, err := query.Parse(queryStr)
		if err != nil {
			output.Error("Parse error: %v", err)
			printQuerySyntaxHelp()
			return err
		}

		if errs := parsedQuery.Validate(); len(errs) > 0 {
			output.Error("Validation errors:")
			for _, e := range errs {
				output.Error("  - %v", e)
			}
			return errs[0]
		}

		// Show explain if requested
		if explain, _ := cmd.Flags().GetBool("explain"); explain {
			fmt.Printf("Query: %s\n", queryStr)
			fmt.Printf("Parsed: %s\n", parsedQuery.String())
			return nil
		}

		// Execute query
		baseDir := getBaseDir()
		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, _ := session.GetOrCreate(database)
		sessionID := ""
		if sess != nil {
			sessionID = sess.ID
		}

		limit, _ := cmd.Flags().GetInt("limit")
		sortBy, _ := cmd.Flags().GetString("sort")
		sortDesc := false
		if strings.HasPrefix(sortBy, "-") {
			sortDesc = true
			sortBy = strings.TrimPrefix(sortBy, "-")
		}

		opts := query.ExecuteOptions{
			Limit:    limit,
			SortBy:   sortBy,
			SortDesc: sortDesc,
		}

		results, err := query.Execute(database, queryStr, sessionID, opts)
		if err != nil {
			output.Error("Query error: %v", err)
			return err
		}

		// Output
		outputFormat, _ := cmd.Flags().GetString("output")
		switch outputFormat {
		case "json":
			return output.JSON(results)
		case "ids":
			for _, issue := range results {
				fmt.Println(issue.ID)
			}
		case "count":
			fmt.Printf("%d\n", len(results))
		default: // "table"
			for _, issue := range results {
				fmt.Println(output.FormatIssueShort(&issue))
			}
		}

		if len(results) == 0 && outputFormat != "count" {
			fmt.Printf("No issues matching query\n")
		}

		return nil
	},
}

func printQuerySyntaxHelp() {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "TDQ Syntax: field operator value")
	fmt.Fprintln(os.Stderr, "  Operators: = != < > <= >= ~ !~")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  td query \"status = open\"")
	fmt.Fprintln(os.Stderr, "  td query \"type = bug AND priority <= P1\"")
	fmt.Fprintln(os.Stderr, "  td query \"title ~ auth\"")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Run 'td query --examples' for more examples")
}

func printQueryExamples() {
	examples := []struct {
		query string
		desc  string
	}{
		// Basic queries
		{"status = open", "All open issues"},
		{"type = bug", "All bugs"},
		{"priority <= P1", "High priority (P0 or P1)"},
		{"points >= 5", "Large issues (5+ points)"},

		// Text search
		{`title ~ "auth"`, "Title contains 'auth'"},
		{`"authentication"`, "Text search in title/description/id"},

		// Boolean logic
		{"type = bug AND status = open", "Open bugs"},
		{"status = open OR status = blocked", "Open or blocked issues"},
		{"NOT status = closed", "Not closed"},
		{"-status = closed", "Same as NOT status = closed"},
		{"(type = bug OR type = feature) AND priority = P0", "Critical bugs or features"},

		// Date queries
		{"created >= -7d", "Created in last 7 days"},
		{"updated >= today", "Updated today"},
		{"created >= this_week", "Created this week"},
		{"updated < -30d AND status = open", "Stale open issues"},

		// Session queries
		{"implementer = @me", "Issues I'm implementing"},
		{"implementer = @me AND is(in_progress)", "My current work"},
		{"status = in_review AND implementer != @me", "Issues I can review"},

		// Functions
		{"has(labels)", "Issues with any labels"},
		{"is(open)", "Shorthand for status = open"},
		{"is(blocked)", "Blocked issues"},
		{"any(type, bug, feature)", "Bugs or features"},
		{"descendant_of(td-epic1)", "All tasks in epic"},
		{"rework()", "Issues rejected and awaiting rework"},

		// Cross-entity queries
		{"log.type = blocker", "Issues with blocker logs"},
		{`log.message ~ "fixed"`, "Logs mentioning 'fixed'"},
		{`comment.text ~ "approved"`, "Comments with 'approved'"},
		{`handoff.remaining ~ "TODO"`, "Handoffs with remaining TODOs"},
		{"file.role = test", "Issues with test files linked"},

		// Complex queries
		{"type = bug AND priority <= P1 AND created >= -7d", "Recent high-priority bugs"},
		{"is(open) AND has(labels) AND labels ~ urgent", "Urgent labeled issues"},
	}

	fmt.Println("TDQ Query Examples:")
	fmt.Println()
	for _, ex := range examples {
		fmt.Printf("  td query %q\n", ex.query)
		fmt.Printf("    → %s\n\n", ex.desc)
	}
}

func printQueryFields() {
	fmt.Println("TDQ Searchable Fields:")
	fmt.Println()

	fmt.Println("ISSUE FIELDS:")
	fields := []struct {
		name   string
		typ    string
		values string
	}{
		{"id", "string", "td-* format"},
		{"title", "string", "any text"},
		{"description", "string", "any text"},
		{"status", "enum", "open, in_progress, blocked, in_review, closed"},
		{"type", "enum", "bug, feature, task, epic, chore"},
		{"priority", "ordinal", "P0, P1, P2, P3, P4"},
		{"points", "number", "1, 2, 3, 5, 8, 13, 21"},
		{"labels", "string", "comma-separated"},
		{"parent", "string", "issue ID (direct parent)"},
		{"epic", "string", "issue ID (ancestor epic)"},
		{"implementer", "string", "session ID or @me"},
		{"reviewer", "string", "session ID or @me"},
		{"minor", "bool", "true, false"},
		{"branch", "string", "git branch name"},
		{"created", "date", "ISO or relative (-7d, today, etc.)"},
		{"updated", "date", "ISO or relative"},
		{"closed", "date", "ISO or relative"},
	}

	for _, f := range fields {
		fmt.Printf("  %-12s %-8s %s\n", f.name, f.typ, f.values)
	}

	fmt.Println()
	fmt.Println("CROSS-ENTITY FIELDS:")
	crossFields := []struct {
		prefix string
		fields string
	}{
		{"log.", "message, type (progress/blocker/decision/hypothesis/tried/result/orchestration), timestamp, session"},
		{"comment.", "text, created, session"},
		{"handoff.", "done, remaining, decisions, uncertain, timestamp"},
		{"file.", "path, role (implementation/test/reference/config)"},
	}

	for _, cf := range crossFields {
		fmt.Printf("  %s\n", cf.prefix)
		fmt.Printf("    %s\n", cf.fields)
	}

	fmt.Println()
	fmt.Println("SPECIAL VALUES:")
	fmt.Println("  @me     Current session ID")
	fmt.Println("  EMPTY   Empty/null field")
	fmt.Println("  NULL    Null field")

	fmt.Println()
	fmt.Println("RELATIVE DATES:")
	fmt.Println("  today, yesterday, this_week, last_week, this_month, last_month")
	fmt.Println("  -Nd (days ago), -Nw (weeks ago), -Nm (months ago), -Nh (hours ago)")
	fmt.Println("  +Nd (days from now), etc.")
}

func init() {
	rootCmd.AddCommand(queryCmd)

	queryCmd.Flags().StringP("output", "o", "table", "Output format: table, json, ids, count")
	queryCmd.Flags().IntP("limit", "n", 50, "Limit results")
	queryCmd.Flags().String("sort", "", "Sort by field (prefix with - for descending)")
	queryCmd.Flags().Bool("explain", false, "Show query parsing without executing")
	queryCmd.Flags().Bool("examples", false, "Show query examples")
	queryCmd.Flags().Bool("fields", false, "List all searchable fields")
}
