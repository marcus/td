package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/version"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:     "info",
	Aliases: []string{"stats"},
	Short:   "Show database statistics and project overview",
	GroupID: "system",
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

		stats, err := database.GetStats()
		if err != nil {
			output.Error("failed to get stats: %v", err)
			return err
		}

		// Get project name from directory
		projectName := filepath.Base(baseDir)

		// Review queue
		reviewable, _ := database.ListIssues(db.ListIssuesOptions{
			ReviewableBy: sess.ID,
		})
		inReview, _ := database.ListIssues(db.ListIssuesOptions{
			Status: []models.Status{models.StatusInReview},
		})

		// JSON output
		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			result := map[string]interface{}{
				"project":         projectName,
				"database":        ".todos/issues.db",
				"current_session": sess.ID,
				"issues": map[string]interface{}{
					"total":       stats["total"],
					"open":        stats["open"],
					"in_progress": stats["in_progress"],
					"blocked":     stats["blocked"],
					"in_review":   stats["in_review"],
					"closed":      stats["closed"],
				},
				"review_queue": map[string]interface{}{
					"awaiting_review": len(inReview),
					"you_can_review":  len(reviewable),
				},
				"by_type": map[string]interface{}{
					"bug":     stats["type_bug"],
					"feature": stats["type_feature"],
					"task":    stats["type_task"],
					"epic":    stats["type_epic"],
					"chore":   stats["type_chore"],
				},
			}
			return output.JSON(result)
		}

		fmt.Printf("Project: %s\n", projectName)
		fmt.Printf("Database: .todos/issues.db\n")
		fmt.Printf("Current Session: %s\n", sess.Display())
		fmt.Println()

		fmt.Printf("Issues: %d total\n", stats["total"])
		fmt.Printf("  Open:        %d\n", stats["open"])
		fmt.Printf("  In Progress: %d\n", stats["in_progress"])
		fmt.Printf("  Blocked:     %d\n", stats["blocked"])
		fmt.Printf("  In Review:   %d\n", stats["in_review"])
		fmt.Printf("  Closed:      %d\n", stats["closed"])
		fmt.Println()

		fmt.Println("Review Queue:")
		fmt.Printf("  Awaiting review: %d\n", len(inReview))
		fmt.Printf("  You can review:  %d\n", len(reviewable))
		fmt.Println()

		fmt.Println("By Type:")
		fmt.Printf("  bug:     %d\n", stats["type_bug"])
		fmt.Printf("  feature: %d\n", stats["type_feature"])
		fmt.Printf("  task:    %d\n", stats["type_task"])
		fmt.Printf("  epic:    %d\n", stats["type_epic"])
		fmt.Printf("  chore:   %d\n", stats["type_chore"])

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Show version and check for updates",
	GroupID: "system",
	Run: func(cmd *cobra.Command, args []string) {
		short, _ := cmd.Flags().GetBool("short")
		if short {
			fmt.Print(versionStr)
			return
		}

		checkUpdates, _ := cmd.Flags().GetBool("check")

		fmt.Printf("td version %s\n", versionStr)

		// Skip check if dev version or --check=false
		if !checkUpdates || version.IsDevelopmentVersion(versionStr) {
			return
		}

		// Check cache first
		if cached, err := version.LoadCache(); err == nil && version.IsCacheValid(cached, versionStr) {
			if cached.HasUpdate {
				fmt.Printf("\nUpdate available: %s → %s\n", versionStr, cached.LatestVersion)
				if cmd := version.UpdateCommand(cached.LatestVersion); cmd != "" {
					fmt.Printf("Run: %s\n", cmd)
				}
			}
			return
		}

		// Fetch from GitHub
		result := version.Check(versionStr)

		// Cache successful checks
		if result.Error == nil {
			_ = version.SaveCache(&version.CacheEntry{
				LatestVersion:  result.LatestVersion,
				CurrentVersion: versionStr,
				CheckedAt:      time.Now(),
				HasUpdate:      result.HasUpdate,
			})
		}

		if result.Error != nil {
			// Silently ignore network errors
			return
		}

		if result.HasUpdate {
			fmt.Printf("\nUpdate available: %s → %s\n", versionStr, result.LatestVersion)
			if cmd := version.UpdateCommand(result.LatestVersion); cmd != "" {
				fmt.Printf("Run: %s\n", cmd)
			}
		}
	},
}

var whoamiCmd = &cobra.Command{
	Use:     "whoami",
	Short:   "Show current session identity",
	GroupID: "session",
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

		// Get issues touched by this session
		touchedIssues, _ := database.GetIssueSessionLog(sess.ID)

		fmt.Printf("SESSION: %s\n", sess.Display())
		fmt.Printf("STARTED: %s\n", sess.StartedAt.Format("2006-01-02T15:04:05Z"))

		if sess.PreviousSessionID != "" {
			fmt.Printf("PREVIOUS SESSION: %s\n", sess.PreviousSessionID)
		}

		if len(touchedIssues) > 0 {
			fmt.Printf("ISSUES TOUCHED: %s\n", joinItems(touchedIssues))
		}

		return nil
	},
}

var sessionNameCmd = &cobra.Command{
	Use:     "session [name]",
	Short:   "Name session, or --new at context start (not mid-work—bypasses review)",
	GroupID: "session",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		newSession, _ := cmd.Flags().GetBool("new")

		if newSession {
			// Force create a new session
			sess, err := session.ForceNewSession(database)
			if err != nil {
				output.Error("failed to create session: %v", err)
				return err
			}

			if sess.PreviousSessionID != "" {
				fmt.Printf("NEW SESSION: %s on branch: %s (previous: %s)\n", sess.ID, sess.Branch, sess.PreviousSessionID)
			} else {
				fmt.Printf("NEW SESSION: %s on branch: %s\n", sess.ID, sess.Branch)
			}

			// Set name if provided
			if len(args) > 0 {
				if _, err := session.SetName(database, args[0]); err != nil {
					output.Error("failed to save session name: %v", err)
					return err
				}
				fmt.Printf("SESSION NAMED \"%s\"\n", args[0])
			}
			return nil
		}

		// Name existing session
		if len(args) == 0 {
			// Just show current session
			sess, err := session.GetOrCreate(database)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			fmt.Printf("SESSION: %s on branch: %s\n", sess.DisplayWithAgent(), sess.Branch)
			return nil
		}

		name := args[0]

		sess, err := session.SetName(database, name)
		if err != nil {
			output.Error("failed to set session name: %v", err)
			return err
		}

		fmt.Printf("SESSION NAMED %s \"%s\" on branch: %s\n", sess.ID, name, sess.Branch)
		return nil
	},
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions (branch + agent scoped)",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sessions, err := session.ListSessions(database)
		if err != nil {
			output.Error("failed to list sessions: %v", err)
			return err
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		currentBranch := session.GetCurrentBranch()
		fp := session.GetAgentFingerprint()

		fmt.Printf("%-16s %-14s %-12s %-18s %s\n", "BRANCH", "AGENT", "SESSION", "LAST ACTIVITY", "AGE")
		fmt.Println(strings.Repeat("-", 80))

		for _, sess := range sessions {
			marker := " "
			if sess.Branch == currentBranch && sess.AgentType == string(fp.Type) && sess.AgentPID == fp.PID {
				marker = "*"
			}

			lastActive := sess.LastActivity
			if lastActive.IsZero() {
				lastActive = sess.StartedAt
			}
			age := time.Since(lastActive).Truncate(time.Minute)

			agentInfo := sess.AgentType
			if agentInfo == "" {
				agentInfo = "(legacy)"
			}

			fmt.Printf("%s%-15s %-14s %-12s %-18s %s\n",
				marker,
				sess.Branch,
				agentInfo,
				sess.ID,
				lastActive.Format("2006-01-02 15:04"),
				age.String())
		}

		return nil
	},
}

var sessionCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stale session files",
	RunE: func(cmd *cobra.Command, args []string) error {
		database, err := db.Open(getBaseDir())
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		olderThan, _ := cmd.Flags().GetString("older-than")
		force, _ := cmd.Flags().GetBool("force")

		maxAge, err := session.ParseDuration(olderThan)
		if err != nil {
			output.Error("invalid duration: %v", err)
			return err
		}

		// Preview what would be deleted
		sessions, err := session.ListSessions(database)
		if err != nil {
			output.Error("failed to list sessions: %v", err)
			return err
		}

		now := time.Now()
		var toDelete []session.Session
		for _, sess := range sessions {
			lastActive := sess.LastActivity
			if lastActive.IsZero() {
				lastActive = sess.StartedAt
			}
			if now.Sub(lastActive) > maxAge {
				toDelete = append(toDelete, sess)
			}
		}

		if len(toDelete) == 0 {
			fmt.Printf("No sessions older than %s found.\n", olderThan)
			return nil
		}

		if !force {
			fmt.Printf("Will delete %d session(s) older than %s:\n", len(toDelete), olderThan)
			for _, sess := range toDelete {
				fmt.Printf("  - %s (branch: %s)\n", sess.ID, sess.Branch)
			}
			fmt.Println("\nRun with --force to delete.")
			return nil
		}

		deleted, err := session.CleanupStaleSessions(database, maxAge)
		if err != nil {
			output.Error("cleanup failed: %v", err)
			return err
		}

		fmt.Printf("Deleted %d stale session(s).\n", deleted)
		return nil
	},
}

var exportCmd = &cobra.Command{
	Use:     "export",
	Short:   "Export database",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		format, _ := cmd.Flags().GetString("format")
		outputPath, _ := cmd.Flags().GetString("output")
		includeAll, _ := cmd.Flags().GetBool("all")
		renderMarkdown, _ := cmd.Flags().GetBool("render-markdown")

		opts := db.ListIssuesOptions{}
		if includeAll {
			opts.IncludeDeleted = true
		}

		issues, err := database.ListIssues(opts)
		if err != nil {
			output.Error("failed to list issues: %v", err)
			return err
		}

		var data []byte

		if format == "json" {
			// Build full export with logs and handoffs
			exportData := make([]map[string]interface{}, 0)
			for _, issue := range issues {
				logs, _ := database.GetLogs(issue.ID, 0)
				handoff, _ := database.GetLatestHandoff(issue.ID)
				deps, _ := database.GetDependencies(issue.ID)
				files, _ := database.GetLinkedFiles(issue.ID)

				item := map[string]interface{}{
					"issue":        issue,
					"logs":         logs,
					"handoff":      handoff,
					"dependencies": deps,
					"files":        files,
				}
				exportData = append(exportData, item)
			}

			data, err = json.MarshalIndent(exportData, "", "  ")
			if err != nil {
				output.Error("failed to marshal: %v", err)
				return err
			}
		} else {
			// Markdown format
			md := "# Issues Export\n\n"
			for _, issue := range issues {
				md += fmt.Sprintf("## %s: %s\n\n", issue.ID, issue.Title)
				md += fmt.Sprintf("- Status: %s\n", issue.Status)
				md += fmt.Sprintf("- Type: %s\n", issue.Type)
				md += fmt.Sprintf("- Priority: %s\n", issue.Priority)
				if issue.Points > 0 {
					md += fmt.Sprintf("- Points: %d\n", issue.Points)
				}
				if len(issue.Labels) > 0 {
					md += fmt.Sprintf("- Labels: %s\n", joinItems(issue.Labels))
				}
				if issue.Description != "" {
					md += fmt.Sprintf("\n%s\n", issue.Description)
				}
				md += "\n"
			}
			if renderMarkdown {
				rendered, err := output.RenderMarkdown(md)
				if err != nil {
					output.Error("failed to render markdown: %v", err)
					return err
				}
				md = rendered
			}
			data = []byte(md)
		}

		if outputPath != "" {
			if err := os.WriteFile(outputPath, data, 0644); err != nil {
				output.Error("failed to write file: %v", err)
				return err
			}
			fmt.Printf("Exported to %s\n", outputPath)
		} else {
			fmt.Println(string(data))
		}

		return nil
	},
}

var importCmd = &cobra.Command{
	Use:     "import [file]",
	Short:   "Import issues",
	GroupID: "system",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		filePath := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		force, _ := cmd.Flags().GetBool("force")
		format, _ := cmd.Flags().GetString("format")

		// Auto-detect format from extension if not specified
		if format == "" || format == "json" {
			if strings.HasSuffix(filePath, ".md") {
				format = "md"
			}
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			output.Error("failed to read file: %v", err)
			return err
		}

		sess, err := session.GetOrCreate(database)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		var imported int

		if format == "md" {
			imported, err = importMarkdown(database, string(data), dryRun, force, sess.ID)
		} else {
			imported, err = importJSON(database, data, dryRun, force, sess.ID)
		}

		if err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("\nImported %d issues\n", imported)

		return nil
	},
}

// importJSON imports issues from JSON format
func importJSON(database *db.DB, data []byte, dryRun, force bool, sessionID string) (int, error) {
	var importData []map[string]interface{}
	if err := json.Unmarshal(data, &importData); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %v", err)
	}

	imported := 0
	for _, item := range importData {
		issueData, ok := item["issue"].(map[string]interface{})
		if !ok {
			continue
		}

		title, _ := issueData["title"].(string)
		if title == "" {
			continue
		}

		// Check if issue with same ID exists
		existingID, _ := issueData["id"].(string)
		var existing *models.Issue
		if existingID != "" {
			existing, _ = database.GetIssue(existingID)
		}

		if existing != nil && !force {
			output.Warning("skipping '%s' - already exists (use --force to overwrite)", existingID)
			continue
		}

		if dryRun {
			if existing != nil {
				fmt.Printf("[dry-run] Would overwrite: %s\n", existingID)
			} else {
				fmt.Printf("[dry-run] Would import: %s\n", title)
			}
			imported++
			continue
		}

		issue := &models.Issue{
			Title: title,
		}

		if desc, ok := issueData["description"].(string); ok {
			issue.Description = desc
		}
		if t, ok := issueData["type"].(string); ok {
			issue.Type = models.Type(t)
		}
		if p, ok := issueData["priority"].(string); ok {
			issue.Priority = models.Priority(p)
		}
		if pts, ok := issueData["points"].(float64); ok {
			issue.Points = int(pts)
		}
		if labels, ok := issueData["labels"].([]interface{}); ok {
			for _, l := range labels {
				if label, ok := l.(string); ok {
					issue.Labels = append(issue.Labels, label)
				}
			}
		}

		if existing != nil && force {
			// Update existing issue
			issue.ID = existingID
			issue.CreatedAt = existing.CreatedAt
			if err := database.UpdateIssueLogged(issue, sessionID, models.ActionUpdate); err != nil {
				output.Warning("failed to overwrite '%s': %v", existingID, err)
				continue
			}
			fmt.Printf("OVERWRITTEN %s: %s\n", existingID, title)
			imported++
		} else {
			if err := database.CreateIssueLogged(issue, sessionID); err != nil {
				output.Warning("failed to import '%s': %v", title, err)
				continue
			}
			fmt.Printf("IMPORTED %s: %s\n", issue.ID, title)
			imported++
		}
	}

	return imported, nil
}

// importMarkdown imports issues from markdown format
// Supports formats like:
//
//	## Title
//	- Status: open
//	- Type: feature
//	- Priority: P1
//	- Points: 3
//	- Labels: label1, label2
//	Description text
func importMarkdown(database *db.DB, data string, dryRun, force bool, sessionID string) (int, error) {
	scanner := bufio.NewScanner(strings.NewReader(data))
	imported := 0

	var currentIssue *models.Issue
	var descLines []string
	inDescription := false

	// Regex patterns
	// Match "## td-xxxx: Title" or "## Title"
	headerWithIDRegex := regexp.MustCompile(`^##\s+(td-[a-f0-9]+):\s*(.+)$`)
	headerRegex := regexp.MustCompile(`^##\s+(.+)$`)
	statusRegex := regexp.MustCompile(`^-\s*Status:\s*(.+)$`)
	typeRegex := regexp.MustCompile(`^-\s*Type:\s*(.+)$`)
	priorityRegex := regexp.MustCompile(`^-\s*Priority:\s*(.+)$`)
	pointsRegex := regexp.MustCompile(`^-\s*Points:\s*(\d+)$`)
	labelsRegex := regexp.MustCompile(`^-\s*Labels:\s*(.+)$`)

	var currentIssueID string

	saveIssue := func() {
		if currentIssue != nil {
			if len(descLines) > 0 {
				currentIssue.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
			}

			// Check for existing issue by ID
			var existing *models.Issue
			if currentIssueID != "" {
				existing, _ = database.GetIssue(currentIssueID)
			}

			if existing != nil && !force {
				output.Warning("skipping '%s' - already exists (use --force to overwrite)", currentIssueID)
				return
			}

			if dryRun {
				if existing != nil {
					fmt.Printf("[dry-run] Would overwrite: %s\n", currentIssueID)
				} else {
					fmt.Printf("[dry-run] Would import: %s (%s, %s)\n",
						currentIssue.Title, currentIssue.Type, currentIssue.Priority)
				}
				imported++
			} else if existing != nil && force {
				currentIssue.ID = currentIssueID
				currentIssue.CreatedAt = existing.CreatedAt
				if err := database.UpdateIssueLogged(currentIssue, sessionID, models.ActionUpdate); err != nil {
					output.Warning("failed to overwrite '%s': %v", currentIssueID, err)
				} else {
					fmt.Printf("OVERWRITTEN %s: %s\n", currentIssueID, currentIssue.Title)
					imported++
				}
			} else {
				if err := database.CreateIssueLogged(currentIssue, sessionID); err != nil {
					output.Warning("failed to import '%s': %v", currentIssue.Title, err)
				} else {
					fmt.Printf("IMPORTED %s: %s\n", currentIssue.ID, currentIssue.Title)
					imported++
				}
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Check for new issue header with ID (## td-xxxx: Title)
		if matches := headerWithIDRegex.FindStringSubmatch(line); matches != nil {
			saveIssue()
			currentIssueID = matches[1]
			currentIssue = &models.Issue{
				Title:    matches[2],
				Type:     models.TypeTask,
				Priority: models.PriorityP2,
			}
			descLines = nil
			inDescription = false
			continue
		}

		// Check for new issue header without ID (## Title)
		if matches := headerRegex.FindStringSubmatch(line); matches != nil {
			saveIssue()
			currentIssueID = ""
			currentIssue = &models.Issue{
				Title:    matches[1],
				Type:     models.TypeTask,
				Priority: models.PriorityP2,
			}
			descLines = nil
			inDescription = false
			continue
		}

		if currentIssue == nil {
			continue
		}

		// Parse metadata lines
		if matches := statusRegex.FindStringSubmatch(line); matches != nil {
			// Status is set on creation, ignore
			inDescription = false
			continue
		}
		if matches := typeRegex.FindStringSubmatch(line); matches != nil {
			currentIssue.Type = models.Type(strings.TrimSpace(matches[1]))
			inDescription = false
			continue
		}
		if matches := priorityRegex.FindStringSubmatch(line); matches != nil {
			currentIssue.Priority = models.Priority(strings.TrimSpace(matches[1]))
			inDescription = false
			continue
		}
		if matches := pointsRegex.FindStringSubmatch(line); matches != nil {
			var pts int
			fmt.Sscanf(matches[1], "%d", &pts)
			currentIssue.Points = pts
			inDescription = false
			continue
		}
		if matches := labelsRegex.FindStringSubmatch(line); matches != nil {
			labels := strings.Split(matches[1], ",")
			for _, l := range labels {
				l = strings.TrimSpace(l)
				if l != "" {
					currentIssue.Labels = append(currentIssue.Labels, l)
				}
			}
			inDescription = false
			continue
		}

		// Skip list items that aren't recognized metadata
		if strings.HasPrefix(strings.TrimSpace(line), "- ") && !inDescription {
			continue
		}

		// Anything else is description
		if strings.TrimSpace(line) != "" || inDescription {
			inDescription = true
			descLines = append(descLines, line)
		}
	}

	// Save last issue
	saveIssue()

	return imported, nil
}

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Short:   "Run database migrations",
	Long:    `Runs any pending database migrations to update the schema.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		currentVersion, _ := database.GetSchemaVersion()
		fmt.Printf("Current schema version: %d\n", currentVersion)
		fmt.Printf("Latest schema version: %d\n", db.SchemaVersion)

		if currentVersion >= db.SchemaVersion {
			fmt.Println("Database is up to date. No migrations needed.")
			return nil
		}

		migrationsRun, err := database.RunMigrations()
		if err != nil {
			output.Error("migration failed: %v", err)
			return err
		}

		if migrationsRun > 0 {
			fmt.Printf("Successfully ran %d migration(s)\n", migrationsRun)
		} else {
			fmt.Println("Database is up to date. No migrations needed.")
		}

		newVersion, _ := database.GetSchemaVersion()
		fmt.Printf("Schema version: %d\n", newVersion)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(sessionNameCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(upgradeCmd)

	infoCmd.Flags().Bool("json", false, "JSON output")

	exportCmd.Flags().String("format", "json", "Export format: json or md")
	exportCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	exportCmd.Flags().Bool("all", false, "Include closed/deleted")
	exportCmd.Flags().BoolP("render-markdown", "m", false, "Render markdown output for humans")

	importCmd.Flags().String("format", "json", "Import format: json or md")
	importCmd.Flags().Bool("dry-run", false, "Preview changes")
	importCmd.Flags().Bool("force", false, "Overwrite existing")

	sessionNameCmd.Flags().Bool("new", false, "Force create a new session")

	// Session subcommands
	sessionNameCmd.AddCommand(sessionListCmd)
	sessionNameCmd.AddCommand(sessionCleanupCmd)
	sessionCleanupCmd.Flags().String("older-than", "7d", "Delete sessions older than this duration")
	sessionCleanupCmd.Flags().Bool("force", false, "Actually delete (otherwise preview)")

	versionCmd.Flags().Bool("check", true, "Check for updates")
	versionCmd.Flags().Bool("short", false, "Output only version string")
}
