package cmd

import (
	"fmt"
	"sort"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var blockedByCmd = &cobra.Command{
	Use:   "blocked-by [issue-id]",
	Short: "Show what issues are waiting on this issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		directOnly, _ := cmd.Flags().GetBool("direct")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Get direct blocked issues
		blocked, err := database.GetBlockedBy(issueID)
		if err != nil {
			output.Error("failed to get blocked issues: %v", err)
			return err
		}

		result := map[string]interface{}{
			"issue":        issue,
			"direct":       blocked,
			"direct_count": len(blocked),
		}

		if !directOnly {
			// Get transitive blocked issues
			allBlocked := getTransitiveBlocked(database, issueID, make(map[string]bool))
			transitiveCount := len(allBlocked) - len(blocked)
			result["transitive_count"] = transitiveCount
			result["all"] = allBlocked
		}

		if jsonOutput {
			return output.JSON(result)
		}

		// Text output
		fmt.Printf("%s: %s %s\n", issue.ID, issue.Title, output.FormatStatus(issue.Status))

		if len(blocked) == 0 {
			fmt.Println("No issues blocked by this one")
			return nil
		}

		printBlockedTree(database, issueID, 0, make(map[string]bool), directOnly)

		directCount := len(blocked)
		if !directOnly {
			allBlocked := getTransitiveBlocked(database, issueID, make(map[string]bool))
			transitiveCount := len(allBlocked) - directCount
			fmt.Printf("\n%d issues blocked (%d direct, %d transitive)\n", len(allBlocked), directCount, transitiveCount)
		} else {
			fmt.Printf("\n%d issues directly blocked\n", directCount)
		}

		return nil
	},
}

func printBlockedTree(database *db.DB, issueID string, depth int, visited map[string]bool, directOnly bool) {
	blocked, _ := database.GetBlockedBy(issueID)

	if depth == 0 {
		fmt.Println("└── blocks:")
	}

	for i, id := range blocked {
		if visited[id] {
			continue
		}
		visited[id] = true

		issue, err := database.GetIssue(id)
		if err != nil {
			continue
		}

		prefix := "    "
		for j := 0; j < depth; j++ {
			prefix += "    "
		}

		isLast := i == len(blocked)-1
		if isLast {
			fmt.Printf("%s└── %s: %s %s\n", prefix, issue.ID, issue.Title, output.FormatStatus(issue.Status))
		} else {
			fmt.Printf("%s├── %s: %s %s\n", prefix, issue.ID, issue.Title, output.FormatStatus(issue.Status))
		}

		if !directOnly {
			printBlockedTree(database, id, depth+1, visited, directOnly)
		}
	}
}

func getTransitiveBlocked(database *db.DB, issueID string, visited map[string]bool) []string {
	if visited[issueID] {
		return nil
	}
	visited[issueID] = true

	blocked, _ := database.GetBlockedBy(issueID)
	var all []string
	all = append(all, blocked...)

	for _, id := range blocked {
		all = append(all, getTransitiveBlocked(database, id, visited)...)
	}

	return all
}

var dependsOnCmd = &cobra.Command{
	Use:   "depends-on [issue-id]",
	Short: "Show what issues this issue depends on",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		issueID := args[0]
		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		deps, err := database.GetDependencies(issueID)
		if err != nil {
			output.Error("failed to get dependencies: %v", err)
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")

		if jsonOutput {
			result := map[string]interface{}{
				"issue":        issue,
				"dependencies": deps,
			}
			return output.JSON(result)
		}

		fmt.Printf("%s: %s %s\n", issue.ID, issue.Title, output.FormatStatus(issue.Status))

		if len(deps) == 0 {
			fmt.Println("No dependencies")
			return nil
		}

		fmt.Println("└── depends on:")
		blocking := 0
		resolved := 0

		for _, depID := range deps {
			dep, err := database.GetIssue(depID)
			if err != nil {
				continue
			}

			statusMark := ""
			if dep.Status == models.StatusClosed {
				statusMark = " ✓"
				resolved++
			} else {
				blocking++
			}

			fmt.Printf("    %s: %s %s%s\n", dep.ID, dep.Title, output.FormatStatus(dep.Status), statusMark)
		}

		fmt.Printf("\n%d blocking, %d resolved\n", blocking, resolved)

		return nil
	},
}

var criticalPathCmd = &cobra.Command{
	Use:   "critical-path",
	Short: "Show the sequence of issues that unblocks the most work",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		limit, _ := cmd.Flags().GetInt("limit")
		if limit == 0 {
			limit = 10
		}
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Get all open/in_progress issues
		issues, err := database.ListIssues(db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked},
		})
		if err != nil {
			output.Error("failed to list issues: %v", err)
			return err
		}

		// Build issue map for quick lookup
		issueMap := make(map[string]*models.Issue)
		for i := range issues {
			issueMap[issues[i].ID] = &issues[i]
		}

		// Calculate how many issues each issue blocks (including transitive)
		blockCounts := make(map[string]int)
		for _, issue := range issues {
			count := len(getTransitiveBlocked(database, issue.ID, make(map[string]bool)))
			if count > 0 {
				blockCounts[issue.ID] = count
			}
		}

		// Find issues with no unsatisfied dependencies (can be started now)
		readyIssues := make([]string, 0)
		for _, issue := range issues {
			deps, _ := database.GetDependencies(issue.ID)
			allResolved := true
			for _, depID := range deps {
				if dep, exists := issueMap[depID]; exists && dep.Status != models.StatusClosed {
					allResolved = false
					break
				}
			}
			if allResolved && blockCounts[issue.ID] > 0 {
				readyIssues = append(readyIssues, issue.ID)
			}
		}

		// Sort ready issues by how much they unblock
		sort.Slice(readyIssues, func(i, j int) bool {
			return blockCounts[readyIssues[i]] > blockCounts[readyIssues[j]]
		})

		// Build critical path sequence - each step resolves dependencies for the next
		criticalPath := buildCriticalPathSequence(database, issueMap, blockCounts)

		// Sort by block count for bottleneck ranking
		type issueScore struct {
			id    string
			score int
		}
		var scores []issueScore
		for id, count := range blockCounts {
			scores = append(scores, issueScore{id, count})
		}
		sort.Slice(scores, func(i, j int) bool {
			return scores[i].score > scores[j].score
		})

		if jsonOutput {
			result := map[string]interface{}{
				"critical_path":      criticalPath,
				"ready_to_start":     readyIssues,
				"bottleneck_ranking": scores,
			}
			return output.JSON(result)
		}

		if len(scores) == 0 && len(criticalPath) == 0 {
			fmt.Println("No blocking dependencies found")
			return nil
		}

		if len(criticalPath) > 0 {
			fmt.Println("CRITICAL PATH SEQUENCE (resolve in order):")
			fmt.Println()
			for i, id := range criticalPath {
				if i >= limit {
					break
				}
				issue := issueMap[id]
				if issue == nil {
					continue
				}
				unblocks := blockCounts[id]
				fmt.Printf("  %d. %s  %s  %s\n", i+1, id, issue.Title, output.FormatStatus(issue.Status))
				if unblocks > 0 {
					fmt.Printf("     └─▶ unblocks %d\n", unblocks)
				}
			}
			fmt.Println()
		}

		if len(readyIssues) > 0 {
			fmt.Println("START NOW (no blockers, unblocks others):")
			for i, id := range readyIssues {
				if i >= 3 {
					break
				}
				issue := issueMap[id]
				if issue == nil {
					continue
				}
				fmt.Printf("  ▶ %s  %s  (unblocks %d)\n", id, issue.Title, blockCounts[id])
			}
			fmt.Println()
		}

		if len(scores) > 0 {
			fmt.Println("BOTTLENECKS (blocking most issues):")
			shown := 0
			for _, s := range scores {
				if shown >= 3 {
					break
				}
				fmt.Printf("  %s: %d issues waiting\n", s.id, s.score)
				shown++
			}
		}

		return nil
	},
}

// buildCriticalPathSequence builds the optimal sequence of issues to resolve
// using a topological sort weighted by block counts
func buildCriticalPathSequence(database *db.DB, issueMap map[string]*models.Issue, blockCounts map[string]int) []string {
	// Build dependency graph
	inDegree := make(map[string]int)
	dependsOn := make(map[string][]string)

	for id := range issueMap {
		if issueMap[id].Status == models.StatusClosed {
			continue
		}
		inDegree[id] = 0
	}

	for id := range issueMap {
		if issueMap[id].Status == models.StatusClosed {
			continue
		}
		deps, _ := database.GetDependencies(id)
		for _, depID := range deps {
			if dep, exists := issueMap[depID]; exists && dep.Status != models.StatusClosed {
				inDegree[id]++
				dependsOn[depID] = append(dependsOn[depID], id)
			}
		}
	}

	// Kahn's algorithm with priority queue (weighted by block count)
	var ready []string
	for id, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}

	var sequence []string
	for len(ready) > 0 {
		// Sort by block count (highest first) then by priority
		sort.Slice(ready, func(i, j int) bool {
			if blockCounts[ready[i]] != blockCounts[ready[j]] {
				return blockCounts[ready[i]] > blockCounts[ready[j]]
			}
			// Secondary sort by priority
			pi := issueMap[ready[i]]
			pj := issueMap[ready[j]]
			if pi != nil && pj != nil {
				return pi.Priority < pj.Priority
			}
			return ready[i] < ready[j]
		})

		// Take the highest priority item
		id := ready[0]
		ready = ready[1:]
		sequence = append(sequence, id)

		// Update dependencies
		for _, dependentID := range dependsOn[id] {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				ready = append(ready, dependentID)
			}
		}
	}

	return sequence
}

func init() {
	rootCmd.AddCommand(blockedByCmd)
	rootCmd.AddCommand(dependsOnCmd)
	rootCmd.AddCommand(criticalPathCmd)

	blockedByCmd.Flags().Bool("direct", false, "Only show direct dependencies")
	blockedByCmd.Flags().Bool("json", false, "JSON output")

	dependsOnCmd.Flags().Bool("json", false, "JSON output")

	criticalPathCmd.Flags().Int("limit", 10, "Max issues to show")
	criticalPathCmd.Flags().Bool("json", false, "JSON output")
}
