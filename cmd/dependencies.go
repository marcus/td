package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/dependency"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var blockedByCmd = &cobra.Command{
	Use:     "blocked-by [issue-id]",
	Short:   "Show what issues are waiting on this issue",
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
		fmt.Println(output.IssueOneLiner(issue))

		if len(blocked) == 0 {
			fmt.Println("No issues blocked by this one")
			return nil
		}

		// Build and render blocked tree
		nodes := buildBlockedTreeNodes(database, issueID, directOnly, make(map[string]bool))
		treeOutput := output.RenderBlockedTree(nodes, output.TreeRenderOptions{}, directOnly)
		if treeOutput != "" {
			fmt.Println(treeOutput)
		}

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

// buildBlockedTreeNodes builds TreeNodes from blocked issues
func buildBlockedTreeNodes(database *db.DB, issueID string, directOnly bool, visited map[string]bool) []output.TreeNode {
	blocked, _ := database.GetBlockedBy(issueID)
	nodes := make([]output.TreeNode, 0, len(blocked))

	for _, id := range blocked {
		if visited[id] {
			continue
		}
		visited[id] = true

		issue, err := database.GetIssue(id)
		if err != nil {
			continue
		}

		node := output.TreeNode{
			ID:     issue.ID,
			Title:  issue.Title,
			Type:   issue.Type,
			Status: issue.Status,
		}

		if !directOnly {
			node.Children = buildBlockedTreeNodes(database, id, directOnly, visited)
		}

		nodes = append(nodes, node)
	}

	return nodes
}

func getTransitiveBlocked(database *db.DB, issueID string, visited map[string]bool) []string {
	return dependency.GetTransitiveBlocked(database, issueID, visited)
}

func getTransitiveBlockedOpen(database *db.DB, issueID string, visited map[string]bool) []string {
	return dependency.GetTransitiveBlockedOpen(database, issueID, visited)
}

var dependsOnCmd = &cobra.Command{
	Use:     "depends-on [issue-id]",
	Aliases: []string{"deps", "dependencies"},
	Short:   "Show what issues this issue depends on",
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

		fmt.Println(output.IssueOneLiner(issue))

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

			if dep.Status == models.StatusClosed {
				resolved++
			} else {
				blocking++
			}

			fmt.Println(output.DependencyLine(dep, true))
		}

		fmt.Printf("\n%d blocking, %d resolved\n", blocking, resolved)

		return nil
	},
}

var criticalPathCmd = &cobra.Command{
	Use:     "critical-path",
	Short:   "Show the sequence of issues that unblocks the most work",
	GroupID: "query",
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

		// Get all open/in_progress issues (excluding epics - they're containers, not blocking work)
		allIssues, err := database.ListIssues(db.ListIssuesOptions{
			Status: []models.Status{models.StatusOpen, models.StatusInProgress, models.StatusBlocked},
		})
		if err != nil {
			output.Error("failed to list issues: %v", err)
			return err
		}

		// Filter out epics
		var issues []models.Issue
		for _, issue := range allIssues {
			if issue.Type != models.TypeEpic {
				issues = append(issues, issue)
			}
		}

		// Build issue map for quick lookup
		issueMap := make(map[string]*models.Issue)
		for i := range issues {
			issueMap[issues[i].ID] = &issues[i]
		}

		// Calculate how many open issues each issue blocks (including transitive)
		blockCounts := make(map[string]int)
		for _, issue := range issues {
			count := len(getTransitiveBlockedOpen(database, issue.ID, make(map[string]bool)))
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

var depCmd = &cobra.Command{
	Use:   "dep [issue] [depends-on-issue]",
	Short: "Manage dependencies between issues",
	Long: `Manage dependencies between issues.

Usage:
  td dep add <issue> <depends-on>   Add a dependency
  td dep rm <issue> <depends-on>    Remove a dependency
  td dep <issue>                    Show what issue depends on
  td dep <issue> --blocking         Show what depends on issue

Backward compatible:
  td dep <issue> <depends-on>       Same as 'td dep add'

Examples:
  td dep add td-abc td-xyz    # td-abc now depends on td-xyz
  td dep rm td-abc td-xyz     # remove that dependency
  td dep td-abc               # show what td-abc depends on
  td dep td-abc --blocking    # show what depends on td-abc`,
	GroupID: "workflow",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		blocking, _ := cmd.Flags().GetBool("blocking")
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Single arg: show dependencies (or blocking issues with --blocking)
		if len(args) == 1 {
			issueID := args[0]
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("issue not found: %s", issueID)
				return err
			}

			if blocking {
				// Show what depends on this issue (reverse deps)
				return showBlocking(database, issue, jsonOutput)
			}
			// Show what this issue depends on
			return showDependencies(database, issue, jsonOutput)
		}

		// Two args: add dependency (backward compat)
		issueID := args[0]
		dependsOnID := args[1]
		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		return addDependency(database, issueID, dependsOnID, sess.ID)
	},
}

var depAddCmd = &cobra.Command{
	Use:   "add <issue> <depends-on>...",
	Short: "Add one or more dependencies (issue depends on others)",
	Long: `Add dependencies to an issue. Supports batch operations:
  td dep add td-abc td-xyz               # td-abc depends on td-xyz
  td dep add td-abc td-xyz1 td-xyz2      # td-abc depends on both td-xyz1 and td-xyz2
  td dep add td-abc --depends-on td-xyz  # flag-based syntax also supported`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
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

		issueID := args[0]

		// Collect dependencies from positional args and --depends-on flag
		var depIDs []string
		depIDs = append(depIDs, args[1:]...)

		// Also support --depends-on flag
		if dependsOn, _ := cmd.Flags().GetString("depends-on"); dependsOn != "" {
			for _, dep := range strings.Split(dependsOn, ",") {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					depIDs = append(depIDs, dep)
				}
			}
		}

		if len(depIDs) == 0 {
			output.Error("no dependencies specified. Usage: td dep add <issue> <depends-on> or td dep add <issue> --depends-on <id>")
			return fmt.Errorf("no dependencies specified")
		}

		added := 0
		for _, depID := range depIDs {
			if err := addDependency(database, issueID, depID, sess.ID); err == nil {
				added++
			}
		}
		if len(depIDs) > 1 {
			fmt.Printf("\nAdded %d dependencies\n", added)
		}
		return nil
	},
}

var depRmCmd = &cobra.Command{
	Use:     "rm <issue> <depends-on>",
	Aliases: []string{"remove"},
	Short:   "Remove a dependency",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
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

		issueID := args[0]
		dependsOnID := args[1]

		issue, err := database.GetIssue(issueID)
		if err != nil {
			output.Error("issue not found: %s", issueID)
			return err
		}

		depIssue, err := database.GetIssue(dependsOnID)
		if err != nil {
			output.Error("issue not found: %s", dependsOnID)
			return err
		}

		err = database.RemoveDependencyLogged(issueID, dependsOnID, sess.ID)
		if err != nil {
			output.Error("failed to remove dependency: %v", err)
			return err
		}

		fmt.Printf("REMOVED: %s no longer depends on %s\n", issue.ID, depIssue.ID)
		return nil
	},
}

// addDependency adds a dependency between two issues
func addDependency(database *db.DB, issueID, dependsOnID, sessionID string) error {
	issue, err := database.GetIssue(issueID)
	if err != nil {
		output.Error("issue not found: %s", issueID)
		return err
	}

	depIssue, err := database.GetIssue(dependsOnID)
	if err != nil {
		output.Error("issue not found: %s", dependsOnID)
		return err
	}

	err = dependency.Validate(database, issueID, dependsOnID)
	if err == dependency.ErrDependencyExists {
		output.Warning("%s already depends on %s", issueID, dependsOnID)
		return nil
	}
	if err != nil {
		output.Error("%v", err)
		return err
	}

	if err := database.AddDependencyLogged(issueID, dependsOnID, "depends_on", sessionID); err != nil {
		output.Error("failed to add dependency: %v", err)
		return err
	}

	fmt.Printf("ADDED: %s depends on %s\n", issue.ID, depIssue.ID)
	fmt.Printf("  %s: %s\n", issue.ID, issue.Title)
	fmt.Printf("  └── now depends on: %s: %s\n", depIssue.ID, depIssue.Title)
	return nil
}

// showDependencies shows what an issue depends on
func showDependencies(database *db.DB, issue *models.Issue, jsonOutput bool) error {
	deps, err := database.GetDependencies(issue.ID)
	if err != nil {
		output.Error("failed to get dependencies: %v", err)
		return err
	}

	if jsonOutput {
		result := map[string]interface{}{
			"issue":        issue,
			"dependencies": deps,
		}
		return output.JSON(result)
	}

	fmt.Println(output.IssueOneLiner(issue))

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

		if dep.Status == models.StatusClosed {
			resolved++
		} else {
			blocking++
		}

		fmt.Println(output.DependencyLine(dep, true))
	}

	fmt.Printf("\n%d blocking, %d resolved\n", blocking, resolved)
	return nil
}

// showBlocking shows what issues depend on the given issue
func showBlocking(database *db.DB, issue *models.Issue, jsonOutput bool) error {
	blocked, err := database.GetBlockedBy(issue.ID)
	if err != nil {
		output.Error("failed to get blocked issues: %v", err)
		return err
	}

	if jsonOutput {
		result := map[string]interface{}{
			"issue":   issue,
			"blocked": blocked,
		}
		return output.JSON(result)
	}

	fmt.Println(output.IssueOneLiner(issue))

	if len(blocked) == 0 {
		fmt.Println("No issues depend on this one")
		return nil
	}

	fmt.Println("└── blocks:")
	for _, id := range blocked {
		dep, err := database.GetIssue(id)
		if err != nil {
			continue
		}
		fmt.Println(output.DependencyLine(dep, false))
	}

	fmt.Printf("\n%d issues blocked\n", len(blocked))
	return nil
}

func init() {
	rootCmd.AddCommand(blockedByCmd)
	rootCmd.AddCommand(dependsOnCmd)
	rootCmd.AddCommand(depCmd)
	rootCmd.AddCommand(criticalPathCmd)

	// Add subcommands to dep
	depCmd.AddCommand(depAddCmd)
	depCmd.AddCommand(depRmCmd)

	// Flag-based syntax for dep add (for agent compatibility)
	depAddCmd.Flags().String("depends-on", "", "Dependency ID(s) to add (comma-separated)")

	blockedByCmd.Flags().Bool("direct", false, "Only show direct dependencies")
	blockedByCmd.Flags().Bool("json", false, "JSON output")

	dependsOnCmd.Flags().Bool("json", false, "JSON output")

	depCmd.Flags().Bool("blocking", false, "Show what depends on this issue (reverse)")
	depCmd.Flags().Bool("json", false, "JSON output")

	criticalPathCmd.Flags().Int("limit", 10, "Max issues to show")
	criticalPathCmd.Flags().Bool("json", false, "JSON output")
}
