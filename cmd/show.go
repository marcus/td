package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:     "show [issue-id...]",
	Aliases: []string{"context"},
	Short:   "Display full details of one or more issues",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Handle multiple issues
		if len(args) > 1 {
			return showMultipleIssues(cmd, database, args)
		}

		issueID := args[0]

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

		// Check output format
		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
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

		// Long format (default)
		fmt.Print(output.FormatIssueLong(issue, logs, handoff))

		// Add git state section
		if startSnapshot != nil {
			fmt.Printf("\nGIT STATE:\n")
			fmt.Printf("  Started: %s (%s) %s\n",
				startSnapshot.CommitSHA[:7], startSnapshot.Branch, output.FormatTimeAgo(startSnapshot.Timestamp))
			if gitState != nil {
				fmt.Printf("  Current: %s (%s) +%d commits\n",
					gitState.CommitSHA[:7], gitState.Branch, commitsSinceStart)
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
			fmt.Printf("\nSESSIONS INVOLVED:\n")
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
			fmt.Printf("\nLINKED FILES:\n")
			for _, f := range files {
				fmt.Printf("  %s (%s)\n", f.FilePath, f.Role)
			}
		}

		// Show dependencies
		if len(deps) > 0 {
			fmt.Printf("\nBLOCKED BY:\n")
			for _, depID := range deps {
				dep, _ := database.GetIssue(depID)
				if dep != nil {
					fmt.Printf("  %s \"%s\" [%s]\n", dep.ID, dep.Title, dep.Status)
				} else {
					fmt.Printf("  %s\n", depID)
				}
			}
		}

		if len(blocked) > 0 {
			fmt.Printf("\nBLOCKS:\n")
			for _, id := range blocked {
				b, _ := database.GetIssue(id)
				if b != nil {
					fmt.Printf("  %s \"%s\" [%s]\n", b.ID, b.Title, b.Status)
				} else {
					fmt.Printf("  %s\n", id)
				}
			}
		}

		return nil
	},
}

// showMultipleIssues displays multiple issues with separators
func showMultipleIssues(cmd *cobra.Command, database *db.DB, issueIDs []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

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
			fmt.Print(output.FormatIssueLong(issue, logs, handoff))
		}

		// Add separator between issues
		if i < len(issueIDs)-1 {
			fmt.Println("---")
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(showCmd)

	showCmd.Flags().Bool("long", false, "Detailed multi-line output (default)")
	showCmd.Flags().Bool("short", false, "Compact summary")
	showCmd.Flags().Bool("json", false, "Machine-readable JSON")
}
