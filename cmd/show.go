package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:     "show [issue-id...]",
	Aliases: []string{"context", "view", "get"},
	Short:   "Display full details of one or more issues",
	Long: `Display full details of one or more issues.

Git summaries are omitted by default. Use --git to include repository state and
changes since the issue started (limited to 2 seconds across all requested issues).

Examples:
  td show td-abc1                  # Show issue details
  td show td-abc1 --git            # Include the bounded git summary
  td show td-abc1 --children       # Show issue with child tasks
  td show td-abc1 td-abc2          # Show multiple issues`,
	GroupID: "core",
	Args:    cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Args are already validated; from here every failure carries its own
		// message, so cobra's usage block would be noise — and for a --json
		// caller it is noise interleaved with the envelope.
		cmd.SilenceUsage = true

		baseDir := getBaseDir()
		isJSON := jsonMode(cmd)

		database, err := db.Open(baseDir)
		if err != nil {
			return reportFailure(isJSON, output.ErrCodeDatabaseError, err)
		}
		defer func() { _ = database.Close() }()

		// If no args, try to show current work or provide helpful suggestions
		if len(args) == 0 {
			_, scope, err := getCurrentStateSession(database, baseDir)
			if err != nil {
				return reportFailure(isJSON, output.ErrCodeNoActiveSession, err)
			}

			// Try focused issue first
			focusedID, _ := database.GetFocus(scope)
			if focusedID != "" {
				args = []string{focusedID}
			} else {
				// Try to find in_progress issues
				inProgress, _ := database.ListIssues(db.ListIssuesOptions{
					Status: []models.Status{models.StatusInProgress},
					Limit:  5,
				})
				if len(inProgress) == 1 {
					args = []string{inProgress[0].ID}
				} else if len(inProgress) > 1 {
					// The disambiguation listing is human guidance printed on
					// stdout; emitting it for a --json caller would put
					// non-JSON lines ahead of the envelope.
					if !isJSON {
						output.Error("no issue ID specified. Multiple issues in progress:")
						for _, issue := range inProgress {
							fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
						}
						fmt.Printf("\nUsage: td show <issue-id>\n")
						return alreadyReported(withErrorCode(output.ErrCodeInvalidInput, fmt.Errorf("issue ID required")))
					}
					return reportFailure(isJSON, output.ErrCodeInvalidInput, fmt.Errorf("no issue ID specified: multiple issues in progress"))
				} else {
					inReview, _ := database.ListIssues(db.ListIssuesOptions{
						Status: []models.Status{models.StatusInReview},
						Limit:  5,
					})
					if len(inReview) == 1 {
						args = []string{inReview[0].ID}
					} else if len(inReview) > 1 {
						if !isJSON {
							output.Error("no issue ID specified. Multiple issues in review:")
							for _, issue := range inReview {
								fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
							}
							fmt.Printf("\nUsage: td show <issue-id>\n")
							return alreadyReported(withErrorCode(output.ErrCodeInvalidInput, fmt.Errorf("issue ID required")))
						}
						return reportFailure(isJSON, output.ErrCodeInvalidInput, fmt.Errorf("no issue ID specified: multiple issues in review"))
					} else {
						if !isJSON {
							output.Error("no issue ID specified and no issues in progress")
							fmt.Printf("\nUsage: td show <issue-id>\n")
							fmt.Printf("Try: td list        # see all issues\n")
							fmt.Printf("     td next        # see highest priority open issue\n")
							return alreadyReported(withErrorCode(output.ErrCodeInvalidInput, fmt.Errorf("issue ID required")))
						}
						return reportFailure(isJSON, output.ErrCodeInvalidInput, fmt.Errorf("no issue ID specified and no issues in progress"))
					}
				}
			}
		}

		// Validate issue IDs (catch empty strings)
		if err := ValidateIssueIDs(args, "show <issue-id>"); err != nil {
			return reportFailure(isJSON, output.ErrCodeInvalidInput, err)
		}

		// Share one git budget across every issue in this invocation.
		gitContext, cancelGit := newShowGitContext(cmd)
		defer cancelGit()

		// Handle multiple issues
		if len(args) > 1 {
			return showMultipleIssues(cmd, database, args, gitContext)
		}

		issueID := args[0]

		// If --tree flag, redirect to tree view
		if showTree, _ := cmd.Flags().GetBool("tree"); showTree {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				return reportFailure(isJSON, output.ErrCodeNotFound, err)
			}

			// Print root
			fmt.Printf("%s %s: %s\n", issue.Type, issue.ID, issue.Title)

			// Build and print children tree
			children := buildTreeNodes(database, issueID, 0, 0)
			treeOutput := output.RenderTree(output.TreeNode{Children: children}, output.TreeRenderOptions{
				MaxDepth:   0,
				ShowStatus: true,
				ShowType:   true,
			})
			if treeOutput != "" {
				fmt.Println(treeOutput)
			}
			return nil
		}

		issue, err := database.GetIssue(issueID)
		if err != nil {
			return reportFailure(jsonMode(cmd), output.ErrCodeNotFound, err)
		}

		// Get logs and handoff
		logs, _ := database.GetLogs(issueID, 0)
		handoff, _ := database.GetLatestHandoff(issueID)

		// Get review history (best-effort; never fail show on this)
		reviews, _ := database.ListIssueReviews(issueID)

		// Get linked files
		files, _ := database.GetLinkedFiles(issueID)

		// Get dependencies
		deps, _ := database.GetDependencies(issueID)
		blocked, _ := database.GetBlockedBy(issueID)

		gitDetails := getShowGitDetails(cmd, database, issueID, gitContext)

		// Check output format (support both --json and --format json)
		jsonOutput, _ := cmd.Flags().GetBool("json")
		if format, _ := cmd.Flags().GetString("format"); format == "json" {
			jsonOutput = true
		}
		if jsonOutput {
			result := map[string]interface{}{
				"id":                          issue.ID,
				"title":                       issue.Title,
				"description":                 issue.Description,
				"status":                      issue.Status,
				"type":                        issue.Type,
				"priority":                    issue.Priority,
				"points":                      issue.Points,
				"labels":                      jsonList(issue.Labels),
				"parent_id":                   issue.ParentID,
				"acceptance":                  issue.Acceptance,
				"implementer_session":         issue.ImplementerSession,
				"reviewer_session":            issue.ReviewerSession,
				"review_requested_by_session": issue.ReviewRequestedBySession,
				"closed_by_session":           issue.ClosedBySession,
				"created_at":                  issue.CreatedAt,
				"updated_at":                  issue.UpdatedAt,
				"minor":                       issue.Minor,
			}
			if issue.ReviewedAt != nil {
				result["reviewed_at"] = issue.ReviewedAt
			}
			if issue.ClosedAt != nil {
				result["closed_at"] = issue.ClosedAt
			}
			if len(reviews) > 0 {
				reviewEntries := make([]map[string]interface{}, 0, len(reviews))
				// Show last 3 in chronological (oldest first) order.
				start := 0
				if len(reviews) > 3 {
					start = len(reviews) - 3
				}
				for _, r := range reviews[start:] {
					e := map[string]interface{}{
						"id":               r.ID,
						"decision":         r.Decision,
						"reviewer_session": r.ReviewerSession,
						"summary":          r.Summary,
						"created_at":       r.CreatedAt,
						"requested_by":     r.RequestedBySession,
						"superseded":       r.SupersededAt != nil,
						"self_review":      r.SelfReview,
						"reviewed_by":      r.ReviewedBy,
					}
					if r.SupersededAt != nil {
						e["superseded_at"] = r.SupersededAt
					}
					reviewEntries = append(reviewEntries, e)
				}
				result["review_history"] = reviewEntries
			}
			if issue.DeferUntil != nil {
				result["defer_until"] = *issue.DeferUntil
			}
			if issue.DueDate != nil {
				result["due_date"] = *issue.DueDate
			}
			if issue.DeferCount > 0 {
				result["defer_count"] = issue.DeferCount
			}
			// handoff and logs are always present, even when empty: an
			// absent key is a third rendering of "nothing" that a caller
			// cannot field-access at all (KeyError in Python, undefined in
			// JS). Lists render as [], singular objects as null — the same
			// contract the rest of the --json family follows.
			result["handoff"] = nil
			if handoff != nil {
				result["handoff"] = map[string]interface{}{
					"timestamp": handoff.Timestamp,
					"session":   handoff.SessionID,
					"done":      jsonList(handoff.Done),
					"remaining": jsonList(handoff.Remaining),
					"decisions": jsonList(handoff.Decisions),
					"uncertain": jsonList(handoff.Uncertain),
				}
			}
			logEntries := make([]map[string]interface{}, 0, len(logs))
			for _, log := range logs {
				logEntries = append(logEntries, map[string]interface{}{
					"timestamp": log.Timestamp,
					"message":   log.Message,
					"type":      log.Type,
					"session":   log.SessionID,
				})
			}
			result["logs"] = jsonList(logEntries)
			if gitDetails != nil {
				result["git"] = gitDetails.json()
			}
			return output.JSON(result)
		}

		if short, _ := cmd.Flags().GetBool("short"); short {
			fmt.Println(output.FormatIssueShort(issue))
			return nil
		}

		renderMarkdown, _ := cmd.Flags().GetBool("render-markdown")

		// Sanitize stored terminal-facing text BEFORE any rendering step. Title,
		// description, and acceptance can all carry cursor controls; title is
		// also collapsed to one line so it cannot forge a section below the
		// header. Doing this after glamour would instead shred glamour's own
		// styling.
		issue = output.SanitizedForDisplay(issue)

		issueForOutput := issue
		if renderMarkdown {
			width := output.TerminalWidth(80)
			issueForOutput = renderIssueMarkdown(issue, width)
		}

		// Long format (default)
		fmt.Print(output.FormatIssueLong(issueForOutput, logs, handoff))

		// Reviewer/closer metadata: surfaced whenever any field is set so the
		// audit data is visible even for closed issues.
		if issue.ReviewerSession != "" || issue.ClosedBySession != "" || issue.ReviewedAt != nil || issue.ClosedAt != nil {
			fmt.Print(output.SectionHeader("Review / Close"))
			if issue.ReviewerSession != "" {
				line := fmt.Sprintf("  Reviewer of record: %s", issue.ReviewerSession)
				if issue.ReviewedAt != nil {
					line += fmt.Sprintf(" (reviewed %s)", output.FormatTimeAgo(*issue.ReviewedAt))
				}
				fmt.Println(line)
			}
			if issue.ReviewRequestedBySession != "" {
				fmt.Printf("  Review requested by: %s\n", issue.ReviewRequestedBySession)
			}
			if issue.ClosedBySession != "" {
				line := fmt.Sprintf("  Closed by: %s", issue.ClosedBySession)
				if issue.ClosedAt != nil {
					line += fmt.Sprintf(" (closed %s)", output.FormatTimeAgo(*issue.ClosedAt))
				}
				fmt.Println(line)
			}
		}

		// Recent review history: last 3 entries chronologically.
		if len(reviews) > 0 {
			fmt.Print(output.SectionHeader("Recent Reviews"))
			start := 0
			if len(reviews) > 3 {
				start = len(reviews) - 3
			}
			for _, r := range reviews[start:] {
				marker := ""
				switch {
				case r.ReviewedBy != "":
					// An involved session recorded a review someone else
					// performed. Naming the reviewer is the point — rendering
					// this as "(self-review)" would misreport the record, and
					// SelfReview is true here too.
					marker += " (reviewed by " + r.ReviewedBy + ")"
				case r.SelfReview:
					marker += " (self-review)"
				}
				if r.SupersededAt != nil {
					marker += " [superseded]"
				}
				// The review summary is free text that lands in the audit
				// section. Left raw, a crafted --reason renders a correctly
				// formatted, backdated line here — forging the very
				// attestation this section exists to display.
				summary := output.IndentContinuation(r.Summary)
				if summary == "" {
					summary = "(no summary)"
				}
				fmt.Printf("  [%s] %s by %s: %s%s\n",
					r.CreatedAt.Format("2006-01-02 15:04"),
					r.Decision, r.ReviewerSession, summary, marker)
			}
		}

		if gitDetails != nil {
			gitDetails.print()
		}

		// Add session history from logs
		sessionMap := make(map[string]bool)
		for _, log := range logs {
			if log.SessionID != "" {
				sessionMap[log.SessionID] = true
			}
		}
		if handoff != nil && handoff.SessionID != "" {
			sessionMap[handoff.SessionID] = true
		}
		if issue.ImplementerSession != "" {
			sessionMap[issue.ImplementerSession] = true
		}
		if issue.ReviewerSession != "" {
			sessionMap[issue.ReviewerSession] = true
		}
		if len(sessionMap) > 0 {
			fmt.Print(output.SectionHeader("Sessions Involved"))
			for sess := range sessionMap {
				role := ""
				if sess == issue.ImplementerSession {
					role = " (implementer)"
				}
				if sess == issue.ReviewerSession {
					role = " (reviewer)"
				}
				fmt.Printf("  %s%s\n", sess, role)
			}
		}

		// Show linked files
		if len(files) > 0 {
			fmt.Print(output.SectionHeader("Linked Files"))
			for _, f := range files {
				fmt.Printf("  %s (%s)\n", f.FilePath, f.Role)
			}
		}

		// Show dependencies
		if len(deps) > 0 {
			fmt.Print(output.SectionHeader("Blocked By"))
			for _, depID := range deps {
				dep, _ := database.GetIssue(depID)
				if dep != nil {
					fmt.Printf("  %s\n", output.IssueOneLiner(dep))
				} else {
					fmt.Printf("  %s\n", depID)
				}
			}
		}

		if len(blocked) > 0 {
			fmt.Print(output.SectionHeader("Blocks"))
			for _, id := range blocked {
				b, _ := database.GetIssue(id)
				if b != nil {
					fmt.Printf("  %s\n", output.IssueOneLiner(b))
				} else {
					fmt.Printf("  %s\n", id)
				}
			}
		}

		// Auto-show children for epics
		showChildrenFlag, _ := cmd.Flags().GetBool("children")
		if issue.Type == models.TypeEpic && !showChildrenFlag {
			children, _ := database.ListIssues(db.ListIssuesOptions{
				ParentID: issueID,
			})
			if len(children) > 0 {
				fmt.Print(output.SectionHeader("Stories"))
				nodes := make([]output.TreeNode, 0, len(children))
				for _, child := range children {
					nodes = append(nodes, output.TreeNode{
						ID:     child.ID,
						Title:  child.Title,
						Type:   child.Type,
						Status: child.Status,
					})
				}
				lines := output.RenderChildrenList(nodes)
				for _, line := range lines {
					fmt.Println(line)
				}
			}
		}

		// Show children if --children flag is set
		if showChildrenFlag {
			children, _ := database.ListIssues(db.ListIssuesOptions{
				ParentID: issueID,
			})
			if len(children) > 0 {
				fmt.Print(output.SectionHeader("Children"))
				nodes := make([]output.TreeNode, 0, len(children))
				for _, child := range children {
					nodes = append(nodes, output.TreeNode{
						ID:     child.ID,
						Title:  child.Title,
						Type:   child.Type,
						Status: child.Status,
					})
				}
				lines := output.RenderChildrenList(nodes)
				for _, line := range lines {
					fmt.Println(line)
				}
			}
		}

		return nil
	},
}

// showMultipleIssues displays multiple issues with separators
func showMultipleIssues(cmd *cobra.Command, database *db.DB, issueIDs []string, gitContext context.Context) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if format, _ := cmd.Flags().GetString("format"); format == "json" {
		jsonOutput = true
	}

	if jsonOutput {
		issues := make([]map[string]interface{}, 0)
		for _, id := range issueIDs {
			issue, err := database.GetIssue(id)
			if err != nil {
				continue
			}
			entry := map[string]interface{}{
				"id":          issue.ID,
				"title":       issue.Title,
				"status":      issue.Status,
				"priority":    issue.Priority,
				"type":        issue.Type,
				"description": issue.Description,
			}
			if issue.DeferUntil != nil {
				entry["defer_until"] = *issue.DeferUntil
			}
			if issue.DueDate != nil {
				entry["due_date"] = *issue.DueDate
			}
			if issue.DeferCount > 0 {
				entry["defer_count"] = issue.DeferCount
			}
			if details := getShowGitDetails(cmd, database, id, gitContext); details != nil {
				entry["git"] = details.json()
			}
			issues = append(issues, entry)
		}
		return output.JSON(issues)
	}

	short, _ := cmd.Flags().GetBool("short")
	renderMarkdown, _ := cmd.Flags().GetBool("render-markdown")
	markdownWidth := 0
	if renderMarkdown {
		markdownWidth = output.TerminalWidth(80)
	}

	for i, id := range issueIDs {
		issue, err := database.GetIssue(id)
		if err != nil {
			output.Warning("issue not found: %s", id)
			continue
		}

		// Same pre-render sanitization as the single-issue path above.
		issue = output.SanitizedForDisplay(issue)

		if short {
			fmt.Println(output.FormatIssueShort(issue))
		} else {
			logs, _ := database.GetLogs(id, 5)
			handoff, _ := database.GetLatestHandoff(id)
			issueForOutput := issue
			if renderMarkdown {
				issueForOutput = renderIssueMarkdown(issue, markdownWidth)
			}
			fmt.Print(output.FormatIssueLong(issueForOutput, logs, handoff))
			if details := getShowGitDetails(cmd, database, id, gitContext); details != nil {
				details.print()
			}
		}

		// Add separator between issues
		if i < len(issueIDs)-1 {
			fmt.Println("---")
		}
	}

	return nil
}

const showGitTimeout = 2 * time.Second

func newShowGitContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, showGitTimeout)
}

type showGitDetails struct {
	snapshot *models.GitSnapshot
	summary  *git.Summary
	err      error
}

func getShowGitDetails(cmd *cobra.Command, database *db.DB, issueID string, ctx context.Context) *showGitDetails {
	includeGit, _ := cmd.Flags().GetBool("git")
	short, _ := cmd.Flags().GetBool("short")
	if !includeGit || (short && !jsonMode(cmd)) {
		return nil
	}
	snapshot, _ := database.GetStartSnapshot(issueID)
	if snapshot == nil {
		return nil
	}
	summary, err := git.GetSummarySince(ctx, snapshot.CommitSHA)
	return &showGitDetails{snapshot: snapshot, summary: summary, err: err}
}

func (details *showGitDetails) json() map[string]interface{} {
	info := map[string]interface{}{
		"start_commit": details.snapshot.CommitSHA,
		"start_branch": details.snapshot.Branch,
		"started_at":   details.snapshot.Timestamp,
	}
	if details.err != nil {
		info["error"] = details.err.Error()
		return info
	}
	summary := details.summary
	info["current_commit"] = summary.State.CommitSHA
	info["current_branch"] = summary.State.Branch
	info["commits_since_start"] = summary.CommitsSinceStart
	info["dirty_files"] = summary.State.DirtyFiles
	info["files_changed"] = summary.DiffStats.FilesChanged
	info["additions"] = summary.DiffStats.Additions
	info["deletions"] = summary.DiffStats.Deletions
	return info
}

func (details *showGitDetails) print() {
	fmt.Print(output.SectionHeader("Git State"))
	fmt.Printf("  Started: %s (%s) %s\n",
		output.ShortSHA(details.snapshot.CommitSHA), details.snapshot.Branch, output.FormatTimeAgo(details.snapshot.Timestamp))
	if details.err != nil {
		fmt.Printf("  Summary unavailable: %s\n", output.IndentContinuation(details.err.Error()))
		return
	}
	summary := details.summary
	fmt.Printf("  Current: %s (%s) +%d commits\n",
		output.ShortSHA(summary.State.CommitSHA), summary.State.Branch, summary.CommitsSinceStart)
	if summary.DiffStats.FilesChanged > 0 {
		fmt.Printf("  Changed: %d files (+%d -%d)\n",
			summary.DiffStats.FilesChanged, summary.DiffStats.Additions, summary.DiffStats.Deletions)
	}
}

func renderIssueMarkdown(issue *models.Issue, width int) *models.Issue {
	if issue == nil {
		return issue
	}

	rendered := *issue

	if rendered.Description != "" {
		description, err := output.RenderMarkdownWithWidth(rendered.Description, width)
		if err != nil {
			output.Warning("failed to render description markdown: %v", err)
		} else {
			rendered.Description = description
		}
	}

	if rendered.Acceptance != "" {
		acceptance, err := output.RenderMarkdownWithWidth(rendered.Acceptance, width)
		if err != nil {
			output.Warning("failed to render acceptance markdown: %v", err)
		} else {
			rendered.Acceptance = acceptance
		}
	}

	return &rendered
}

func init() {
	rootCmd.AddCommand(showCmd)

	showCmd.Flags().Bool("long", false, "Detailed multi-line output (default)")
	showCmd.Flags().Bool("short", false, "Compact summary")
	showCmd.Flags().Bool("git", false, "Include git state and changes since start (2-second budget; omitted by default)")
	showCmd.Flags().Bool("json", false, "Machine-readable JSON")
	showCmd.Flags().StringP("format", "f", "", "Output format (json)")
	showCmd.Flags().Bool("children", false, "Display child issues inline (alternative to 'td tree')")
	showCmd.Flags().Bool("tree", false, "Display issue as tree with descendants (alias for 'td tree')")
	showCmd.Flags().BoolP("render-markdown", "m", false, "Render markdown in description and acceptance")
}
