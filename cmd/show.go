package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/config"
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

Examples:
  td show td-abc1                  # Show issue details
  td show td-abc1 --children       # Show issue with child tasks
  td show td-abc1 td-abc2          # Show multiple issues`,
	GroupID: "core",
	Args:    cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// If no args, try to show current work or provide helpful suggestions
		if len(args) == 0 {
			// Try focused issue first
			focusedID, _ := config.GetFocus(baseDir)
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
					output.Error("no issue ID specified. Multiple issues in progress:")
					for _, issue := range inProgress {
						fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
					}
					fmt.Printf("\nUsage: td show <issue-id>\n")
					return fmt.Errorf("issue ID required")
				} else {
					output.Error("no issue ID specified and no issues in progress")
					fmt.Printf("\nUsage: td show <issue-id>\n")
					fmt.Printf("Try: td list        # see all issues\n")
					fmt.Printf("     td next        # see highest priority open issue\n")
					return fmt.Errorf("issue ID required")
				}
			}
		}

		// Validate issue IDs (catch empty strings)
		if err := ValidateIssueIDs(args, "show <issue-id>"); err != nil {
			output.Error("%v", err)
			return err
		}

		// Handle multiple issues
		if len(args) > 1 {
			return showMultipleIssues(cmd, database, args)
		}

		issueID := args[0]

		// If --tree flag, redirect to tree view
		if showTree, _ := cmd.Flags().GetBool("tree"); showTree {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("%v", err)
				return err
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
			if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
				output.JSONError("not_found", err.Error())
			} else {
				output.Error("%v", err)
			}
			return err
		}

		// Get logs and handoff
		logs, _ := database.GetLogs(issueID, 0)
		handoff, _ := database.GetLatestHandoff(issueID)

		// Get linked files
		files, _ := database.GetLinkedFiles(issueID)

		// Get dependencies
		deps, _ := database.GetDependencies(issueID)
		blocked, _ := database.GetBlockedBy(issueID)

		// Get git snapshots
		startSnapshot, _ := database.GetStartSnapshot(issueID)
		var gitState *git.State
		var commitsSinceStart int
		var diffStats *git.DiffStats
		if startSnapshot != nil {
			gitState, _ = git.GetState()
			commitsSinceStart, _ = git.GetCommitsSince(startSnapshot.CommitSHA)
			diffStats, _ = git.GetDiffStatsSince(startSnapshot.CommitSHA)
		}

		// Check output format (support both --json and --format json)
		jsonOutput, _ := cmd.Flags().GetBool("json")
		if format, _ := cmd.Flags().GetString("format"); format == "json" {
			jsonOutput = true
		}
		if jsonOutput {
			result := map[string]interface{}{
				"id":                  issue.ID,
				"title":               issue.Title,
				"description":         issue.Description,
				"status":              issue.Status,
				"type":                issue.Type,
				"priority":            issue.Priority,
				"points":              issue.Points,
				"labels":              issue.Labels,
				"parent_id":           issue.ParentID,
				"acceptance":          issue.Acceptance,
				"implementer_session": issue.ImplementerSession,
				"reviewer_session":    issue.ReviewerSession,
				"created_at":          issue.CreatedAt,
				"updated_at":          issue.UpdatedAt,
				"minor":               issue.Minor,
			}
			if issue.ClosedAt != nil {
				result["closed_at"] = issue.ClosedAt
			}
			if handoff != nil {
				result["handoff"] = map[string]interface{}{
					"timestamp": handoff.Timestamp,
					"session":   handoff.SessionID,
					"done":      handoff.Done,
					"remaining": handoff.Remaining,
					"decisions": handoff.Decisions,
					"uncertain": handoff.Uncertain,
				}
			}
			if len(logs) > 0 {
				logEntries := make([]map[string]interface{}, len(logs))
				for i, log := range logs {
					logEntries[i] = map[string]interface{}{
						"timestamp": log.Timestamp,
						"message":   log.Message,
						"type":      log.Type,
						"session":   log.SessionID,
					}
				}
				result["logs"] = logEntries
			}
			if startSnapshot != nil {
				gitInfo := map[string]interface{}{
					"start_commit": startSnapshot.CommitSHA,
					"start_branch": startSnapshot.Branch,
					"started_at":   startSnapshot.Timestamp,
				}
				if gitState != nil {
					gitInfo["current_commit"] = gitState.CommitSHA
					gitInfo["current_branch"] = gitState.Branch
					gitInfo["commits_since_start"] = commitsSinceStart
					gitInfo["dirty_files"] = gitState.DirtyFiles
				}
				if diffStats != nil {
					gitInfo["files_changed"] = diffStats.FilesChanged
					gitInfo["additions"] = diffStats.Additions
					gitInfo["deletions"] = diffStats.Deletions
				}
				result["git"] = gitInfo
			}
			return output.JSON(result)
		}

		if short, _ := cmd.Flags().GetBool("short"); short {
			fmt.Println(output.FormatIssueShort(issue))
			return nil
		}

		renderMarkdown, _ := cmd.Flags().GetBool("render-markdown")
		issueForOutput := issue
		if renderMarkdown {
			width := output.TerminalWidth(80)
			issueForOutput = renderIssueMarkdown(issue, width)
		}

		// Long format (default)
		fmt.Print(output.FormatIssueLong(issueForOutput, logs, handoff))

		// Add git state section
		if startSnapshot != nil {
			fmt.Print(output.SectionHeader("Git State"))
			fmt.Printf("  Started: %s (%s) %s\n",
				output.ShortSHA(startSnapshot.CommitSHA), startSnapshot.Branch, output.FormatTimeAgo(startSnapshot.Timestamp))
			if gitState != nil {
				fmt.Printf("  Current: %s (%s) +%d commits\n",
					output.ShortSHA(gitState.CommitSHA), gitState.Branch, commitsSinceStart)
				if diffStats != nil && diffStats.FilesChanged > 0 {
					fmt.Printf("  Changed: %d files (+%d -%d)\n",
						diffStats.FilesChanged, diffStats.Additions, diffStats.Deletions)
				}
			}
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
func showMultipleIssues(cmd *cobra.Command, database *db.DB, issueIDs []string) error {
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
			issues = append(issues, map[string]interface{}{
				"id":          issue.ID,
				"title":       issue.Title,
				"status":      issue.Status,
				"priority":    issue.Priority,
				"type":        issue.Type,
				"description": issue.Description,
			})
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
		}

		// Add separator between issues
		if i < len(issueIDs)-1 {
			fmt.Println("---")
		}
	}

	return nil
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
	showCmd.Flags().Bool("json", false, "Machine-readable JSON")
	showCmd.Flags().StringP("format", "f", "", "Output format (json)")
	showCmd.Flags().Bool("children", false, "Display child issues inline (alternative to 'td tree')")
	showCmd.Flags().Bool("tree", false, "Display issue as tree with descendants (alias for 'td tree')")
	showCmd.Flags().BoolP("render-markdown", "m", false, "Render markdown in description and acceptance")
}
