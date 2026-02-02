package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/input"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

var wsCmd = &cobra.Command{
	Use:     "ws",
	Aliases: []string{"worksession"},
	Short:   "Work session commands",
	Long:    `Manage multi-issue work sessions.`,
	GroupID: "session",
}

var wsStartCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start a named work session",
	Args:  cobra.ExactArgs(1),
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

		// Check for existing active work session
		activeWS, _ := config.GetActiveWorkSession(baseDir)
		if activeWS != "" {
			output.Error("work session already active: %s", activeWS)
			return fmt.Errorf("work session already active")
		}

		name := args[0]

		// Get git state
		gitState, _ := git.GetState()
		startSHA := ""
		if gitState != nil {
			startSHA = gitState.CommitSHA
		}

		ws := &models.WorkSession{
			Name:      name,
			SessionID: sess.ID,
			StartSHA:  startSHA,
		}

		if err := database.CreateWorkSession(ws); err != nil {
			output.Error("failed to create work session: %v", err)
			return err
		}

		// Set as active
		config.SetActiveWorkSession(baseDir, ws.ID)

		fmt.Printf("WORK SESSION STARTED: %s\n", ws.ID)
		fmt.Printf("Name: %s\n", name)
		fmt.Println()
		fmt.Printf("Tag issues with: td ws tag <issue-ids>\n")
		fmt.Printf("Log progress with: td ws log \"message\"\n")

		return nil
	},
}

var wsTagCmd = &cobra.Command{
	Use:   "tag [issue-ids...]",
	Short: "Associate issues with the current work session (auto-starts open issues)",
	Long:  `Associate issues with the current work session. By default, open issues are automatically started (status changed to in_progress). Use --no-start to tag without changing status.`,
	Args:  cobra.MinimumNArgs(1),
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

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			output.Error("no active work session. Run 'td ws start <name>' first")
			return fmt.Errorf("no active work session")
		}

		for _, issueID := range args {
			// Verify issue exists
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("%v", err)
				continue
			}

			// Tag to work session
			if err := database.TagIssueToWorkSession(wsID, issueID, sess.ID); err != nil {
				output.Warning("failed to tag %s: %v", issueID, err)
				continue
			}

			fmt.Printf("TAGGED %s → %s\n", issueID, wsID)

			// Start the issue if not already started (unless --no-start)
			noStart, _ := cmd.Flags().GetBool("no-start")
			if !noStart && issue.Status == models.StatusOpen {
				// Validate transition with state machine
				sm := workflow.DefaultMachine()
				if !sm.IsValidTransition(issue.Status, models.StatusInProgress) {
					output.Warning("cannot auto-start %s: invalid transition from %s", issueID, issue.Status)
					continue
				}
				issue.Status = models.StatusInProgress
				issue.ImplementerSession = sess.ID
				database.UpdateIssueLogged(issue, sess.ID, models.ActionStart)

				// Record session action for bypass prevention
				database.RecordSessionAction(issueID, sess.ID, models.ActionSessionStarted)

				// Log the start
				database.AddLog(&models.Log{
					IssueID:       issueID,
					SessionID:     sess.ID,
					WorkSessionID: wsID,
					Message:       "Started (via work session)",
					Type:          models.LogTypeProgress,
				})

				// Capture git state
				gitState, _ := git.GetState()
				if gitState != nil {
					database.AddGitSnapshot(&models.GitSnapshot{
						IssueID:    issueID,
						Event:      "start",
						CommitSHA:  gitState.CommitSHA,
						Branch:     gitState.Branch,
						DirtyFiles: gitState.DirtyFiles,
					})
				}

				fmt.Printf("STARTED %s (session: %s)\n", issueID, sess.ID)
			}
		}

		return nil
	},
}

var wsUntagCmd = &cobra.Command{
	Use:   "untag [issue-ids...]",
	Short: "Remove issues from work session",
	Args:  cobra.MinimumNArgs(1),
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

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			output.Error("no active work session")
			return fmt.Errorf("no active work session")
		}

		for _, issueID := range args {
			if err := database.UntagIssueFromWorkSession(wsID, issueID, sess.ID); err != nil {
				output.Warning("failed to untag %s: %v", issueID, err)
				continue
			}

			fmt.Printf("UNTAGGED %s from %s\n", issueID, wsID)
		}

		return nil
	},
}

var wsLogCmd = &cobra.Command{
	Use:   "log \"message\"",
	Short: "Log to the work session",
	Long:  `Log entry is attached to the session AND all tagged issues.`,
	Args:  cobra.ExactArgs(1),
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

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			output.Error("no active work session")
			return fmt.Errorf("no active work session")
		}

		// Determine log type
		logType := models.LogTypeProgress
		typeLabel := ""

		if blocker, _ := cmd.Flags().GetBool("blocker"); blocker {
			logType = models.LogTypeBlocker
			typeLabel = " [blocker]"
		} else if decision, _ := cmd.Flags().GetBool("decision"); decision {
			logType = models.LogTypeDecision
			typeLabel = " [decision]"
		} else if hypothesis, _ := cmd.Flags().GetBool("hypothesis"); hypothesis {
			logType = models.LogTypeHypothesis
			typeLabel = " [hypothesis]"
		} else if tried, _ := cmd.Flags().GetBool("tried"); tried {
			logType = models.LogTypeTried
			typeLabel = " [tried]"
		} else if result, _ := cmd.Flags().GetBool("result"); result {
			logType = models.LogTypeResult
			typeLabel = " [result]"
		}

		// Filter by --only if specified - log to specific issue only
		only, _ := cmd.Flags().GetString("only")
		if only != "" {
			// Log to specific issue only
			database.AddLog(&models.Log{
				IssueID:       only,
				SessionID:     sess.ID,
				WorkSessionID: wsID,
				Message:       args[0],
				Type:          logType,
			})
			fmt.Printf("LOGGED %s%s → %s\n", wsID, typeLabel, only)
		} else {
			// Store single log entry with work_session_id, no issue_id
			database.AddLog(&models.Log{
				IssueID:       "",
				SessionID:     sess.ID,
				WorkSessionID: wsID,
				Message:       args[0],
				Type:          logType,
			})

			// Get tagged issues for display
			issueIDs, _ := database.GetWorkSessionIssues(wsID)
			if len(issueIDs) > 0 {
				fmt.Printf("LOGGED %s%s → %s\n", wsID, typeLabel, strings.Join(issueIDs, ", "))
			} else {
				fmt.Printf("LOGGED %s%s (no issues tagged)\n", wsID, typeLabel)
			}
		}

		return nil
	},
}

var wsCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show current work session state",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			fmt.Println("No active work session")
			return nil
		}

		ws, err := database.GetWorkSession(wsID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Get tagged issues
		issueIDs, _ := database.GetWorkSessionIssues(wsID)

		if jsonOutput {
			result := map[string]interface{}{
				"work_session": ws,
				"issues":       issueIDs,
			}
			return output.JSON(result)
		}

		fmt.Printf("WORK SESSION: %s \"%s\"\n", ws.ID, ws.Name)
		fmt.Printf("Started: %s\n", output.FormatTimeAgo(ws.StartedAt))

		if ws.StartSHA != "" {
			gitState, _ := git.GetState()
			if gitState != nil {
				commits, _ := git.GetCommitsSince(ws.StartSHA)
				fmt.Printf("Git: %s → %s (+%d commits)\n", output.ShortSHA(ws.StartSHA), output.ShortSHA(gitState.CommitSHA), commits)
			}
		}

		fmt.Println()

		if len(issueIDs) > 0 {
			fmt.Println("TAGGED ISSUES:")
			for _, id := range issueIDs {
				issue, err := database.GetIssue(id)
				if err != nil {
					continue
				}
				fmt.Printf("  %s  %s  %s  %s%s\n",
					issue.ID, issue.Title, output.FormatStatus(issue.Status), issue.Priority, output.FormatPointsSuffix(issue.Points))
			}
			fmt.Println()
		}

		// Show changed files with diff stats
		if ws.StartSHA != "" {
			changes, _ := git.GetChangedFilesSince(ws.StartSHA)
			if len(changes) > 0 {
				fmt.Println("FILES CHANGED:")
				for _, change := range changes {
					stats := ""
					if change.Additions > 0 || change.Deletions > 0 {
						stats = fmt.Sprintf(" +%d -%d", change.Additions, change.Deletions)
					}
					fmt.Printf("  %s%s\n", change.Path, stats)
				}
				fmt.Println()
			}
		}

		// Show recent logs from this session
		fmt.Println("SESSION LOG (recent):")
		for _, issueID := range issueIDs {
			logs, _ := database.GetLogs(issueID, 5)
			for _, log := range logs {
				if log.WorkSessionID == wsID {
					typeLabel := ""
					if log.Type != models.LogTypeProgress {
						typeLabel = fmt.Sprintf(" [%s]", log.Type)
					}
					fmt.Printf("  [%s]%s %s\n", log.Timestamp.Format("15:04"), typeLabel, log.Message)
				}
			}
		}

		return nil
	},
}

var wsHandoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "End work session and generate handoffs for all tagged issues",
	Long: `End work session and generate handoffs for all tagged issues.

Flags support values, stdin (-), or file (@path):
  --done "item"          Single item
  --done @done.txt       Items from file (one per line)
  echo "item" | td ws handoff --done -   Items from stdin`,
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

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			output.Error("no active work session")
			return fmt.Errorf("no active work session")
		}

		ws, err := database.GetWorkSession(wsID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get tagged issues
		issueIDs, _ := database.GetWorkSessionIssues(wsID)

		// Parse handoff content
		handoff := &models.Handoff{
			SessionID: sess.ID,
		}

		// Get from flags with stdin/file expansion
		done, _ := cmd.Flags().GetStringArray("done")
		remaining, _ := cmd.Flags().GetStringArray("remaining")
		decisions, _ := cmd.Flags().GetStringArray("decision")
		uncertain, _ := cmd.Flags().GetStringArray("uncertain")

		var stdinUsed bool
		handoff.Done, stdinUsed = input.ExpandFlagValues(done, stdinUsed)
		handoff.Remaining, stdinUsed = input.ExpandFlagValues(remaining, stdinUsed)
		handoff.Decisions, stdinUsed = input.ExpandFlagValues(decisions, stdinUsed)
		handoff.Uncertain, stdinUsed = input.ExpandFlagValues(uncertain, stdinUsed)

		// Check if stdin has data (YAML format) - only if not already used by flag expansion
		if !stdinUsed {
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				if stat.Size() > 0 {
					parseWSHandoffInput(handoff)
				}
			}
		}

		// Auto-populate from session logs if no explicit content provided
		if len(handoff.Done) == 0 && len(handoff.Remaining) == 0 && len(handoff.Decisions) == 0 && len(handoff.Uncertain) == 0 {
			logs, _ := database.GetLogsByWorkSession(wsID)
			for _, log := range logs {
				switch log.Type {
				case models.LogTypeProgress, models.LogTypeResult:
					handoff.Done = append(handoff.Done, log.Message)
				case models.LogTypeDecision:
					handoff.Decisions = append(handoff.Decisions, log.Message)
				case models.LogTypeBlocker:
					handoff.Uncertain = append(handoff.Uncertain, log.Message)
				case models.LogTypeTried, models.LogTypeHypothesis:
					// These are context, skip for handoff summary
				}
			}
		}

		// Create handoff for each tagged issue
		for _, issueID := range issueIDs {
			issueHandoff := &models.Handoff{
				IssueID:   issueID,
				SessionID: sess.ID,
				Done:      handoff.Done,
				Remaining: filterForIssue(handoff.Remaining, issueID),
				Decisions: handoff.Decisions,
				Uncertain: handoff.Uncertain,
			}

			database.AddHandoff(issueHandoff)

			// Capture git state
			gitState, _ := git.GetState()
			if gitState != nil {
				database.AddGitSnapshot(&models.GitSnapshot{
					IssueID:    issueID,
					Event:      "handoff",
					CommitSHA:  gitState.CommitSHA,
					Branch:     gitState.Branch,
					DirtyFiles: gitState.DirtyFiles,
				})
			}
		}

		// End work session unless --continue
		continueSession, _ := cmd.Flags().GetBool("continue")
		submitReview, _ := cmd.Flags().GetBool("review")

		if !continueSession {
			now := time.Now()
			ws.EndedAt = &now

			gitState, _ := git.GetState()
			if gitState != nil {
				ws.EndSHA = gitState.CommitSHA
			}

			database.UpdateWorkSession(ws)
			config.ClearActiveWorkSession(baseDir)
		}

		fmt.Printf("HANDOFF RECORDED %s\n", wsID)
		fmt.Println("Generated handoffs:")
		for _, id := range issueIDs {
			handoff, _ := database.GetLatestHandoff(id)
			if handoff != nil {
				fmt.Printf("  %s: done=%d, remaining=%d\n", id, len(handoff.Done), len(handoff.Remaining))
			}
		}

		// Submit for review if requested
		if submitReview {
			for _, issueID := range issueIDs {
				issue, _ := database.GetIssue(issueID)
				if issue != nil && issue.Status == models.StatusInProgress {
					result := submitIssueForReview(database, issue, sess, baseDir, "Submitted for review via ws handoff --review")
					if !result.Success {
						output.Warning("%s", result.Message)
						continue
					}
					fmt.Printf("REVIEW REQUESTED %s (session: %s)\n", issueID, sess.ID)
				}
			}
		}

		if !continueSession {
			fmt.Println()
			fmt.Println("Work session ended.")
			if !submitReview && len(issueIDs) > 0 {
				fmt.Printf("Next: `td review <id>` to submit for review\n")
			}
		}

		return nil
	},
}

var wsEndCmd = &cobra.Command{
	Use:   "end",
	Short: "End work session without handoff",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		wsID, _ := config.GetActiveWorkSession(baseDir)
		if wsID == "" {
			output.Error("no active work session")
			return fmt.Errorf("no active work session")
		}

		ws, err := database.GetWorkSession(wsID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get tagged issues
		issueIDs, _ := database.GetWorkSessionIssues(wsID)

		// End session
		now := time.Now()
		ws.EndedAt = &now
		database.UpdateWorkSession(ws)
		config.ClearActiveWorkSession(baseDir)

		output.Warning("No handoff recorded for %s", wsID)
		if len(issueIDs) > 0 {
			fmt.Printf("Tagged issues remain in_progress: %s\n", strings.Join(issueIDs, ", "))
		}
		fmt.Println("WORK SESSION ENDED")

		return nil
	},
}

var wsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent work sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		activeWS, _ := config.GetActiveWorkSession(baseDir)

		sessions, err := database.ListWorkSessions(20)
		if err != nil {
			output.Error("failed to list work sessions: %v", err)
			return err
		}

		for _, ws := range sessions {
			issues, _ := database.GetWorkSessionIssues(ws.ID)
			issueStr := strings.Join(issues, ",")

			status := "[completed]"
			if ws.EndedAt == nil {
				if ws.ID == activeWS {
					status = "[active]"
				} else {
					status = "[abandoned]"
				}
			}

			fmt.Printf("%s  \"%s\"  %s  %s  %s\n",
				ws.ID, ws.Name, output.FormatTimeAgo(ws.StartedAt), issueStr, status)
		}

		if len(sessions) == 0 {
			fmt.Println("No work sessions")
		}

		return nil
	},
}

var wsShowCmd = &cobra.Command{
	Use:   "show [session-id]",
	Short: "Show details of a past work session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		wsID := args[0]
		ws, err := database.GetWorkSession(wsID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Printf("WORK SESSION: %s \"%s\"\n", ws.ID, ws.Name)

		duration := ""
		if ws.EndedAt != nil {
			dur := ws.EndedAt.Sub(ws.StartedAt)
			duration = fmt.Sprintf("Duration: %s", dur.Round(time.Minute))
		}
		fmt.Printf("%s (%s)\n", duration, output.FormatTimeAgo(ws.StartedAt))

		if ws.StartSHA != "" && ws.EndSHA != "" {
			commits, _ := git.GetCommitsSince(ws.StartSHA)
			fmt.Printf("Git: %s → %s (+%d commits)\n", output.ShortSHA(ws.StartSHA), output.ShortSHA(ws.EndSHA), commits)
		}

		fmt.Println()

		// Get tagged issues
		issueIDs, _ := database.GetWorkSessionIssues(wsID)

		fmt.Println("TAGGED ISSUES:")
		for _, id := range issueIDs {
			issue, err := database.GetIssue(id)
			if err != nil {
				continue
			}
			statusMark := ""
			if issue.Status == models.StatusClosed {
				statusMark = " ✓"
			}
			fmt.Printf("  %s  %s  %s%s\n", issue.ID, issue.Title, output.FormatStatus(issue.Status), statusMark)
		}
		fmt.Println()

		// Show handoff summary for all tagged issues (deduplicated)
		doneSet := make(map[string]bool)
		decisionsSet := make(map[string]bool)
		var allDone, allDecisions []string

		for _, id := range issueIDs {
			handoff, _ := database.GetLatestHandoff(id)
			if handoff != nil {
				for _, item := range handoff.Done {
					if !doneSet[item] {
						doneSet[item] = true
						allDone = append(allDone, item)
					}
				}
				for _, item := range handoff.Decisions {
					if !decisionsSet[item] {
						decisionsSet[item] = true
						allDecisions = append(allDecisions, item)
					}
				}
			}
		}

		if len(allDone) > 0 || len(allDecisions) > 0 {
			fmt.Println("HANDOFF SUMMARY:")
			if len(allDone) > 0 {
				fmt.Println("  Done:")
				for _, item := range allDone {
					fmt.Printf("    - %s\n", item)
				}
			}
			if len(allDecisions) > 0 {
				fmt.Println("  Decisions:")
				for _, item := range allDecisions {
					fmt.Printf("    - %s\n", item)
				}
			}
			fmt.Println()
		}

		// Show full session log if --full flag
		if full, _ := cmd.Flags().GetBool("full"); full {
			fmt.Println("FULL SESSION LOG:")
			// Collect all logs and deduplicate by timestamp+message
			type logEntry struct {
				timestamp time.Time
				issueID   string
				typeLabel string
				message   string
			}
			seen := make(map[string]bool)
			var allLogs []logEntry

			for _, id := range issueIDs {
				logs, _ := database.GetLogs(id, 0)
				for _, log := range logs {
					if log.WorkSessionID == wsID {
						// Create dedup key from timestamp and message
						key := fmt.Sprintf("%d:%s", log.Timestamp.Unix(), log.Message)
						if seen[key] {
							continue
						}
						seen[key] = true

						typeLabel := ""
						if log.Type != models.LogTypeProgress {
							typeLabel = fmt.Sprintf(" [%s]", log.Type)
						}
						allLogs = append(allLogs, logEntry{
							timestamp: log.Timestamp,
							issueID:   id,
							typeLabel: typeLabel,
							message:   log.Message,
						})
					}
				}
			}

			// Sort by timestamp
			sort.Slice(allLogs, func(i, j int) bool {
				return allLogs[i].timestamp.Before(allLogs[j].timestamp)
			})

			for _, log := range allLogs {
				fmt.Printf("  [%s]%s %s\n",
					log.timestamp.Format("2006-01-02 15:04"),
					log.typeLabel,
					log.message)
			}
		}

		return nil
	},
}

func parseWSHandoffInput(handoff *models.Handoff) {
	scanner := bufio.NewScanner(os.Stdin)
	currentSection := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasSuffix(trimmed, ":") {
			currentSection = strings.TrimSuffix(trimmed, ":")
			continue
		}

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			item := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			item = strings.TrimSpace(item)

			switch currentSection {
			case "done":
				handoff.Done = append(handoff.Done, item)
			case "remaining":
				handoff.Remaining = append(handoff.Remaining, item)
			case "decisions":
				handoff.Decisions = append(handoff.Decisions, item)
			case "uncertain":
				handoff.Uncertain = append(handoff.Uncertain, item)
			}
		}
	}
}

// filterForIssue filters remaining items tagged with (td-xxx) for a specific issue
func filterForIssue(items []string, issueID string) []string {
	var filtered []string
	for _, item := range items {
		// Check if item is tagged with a specific issue
		if strings.Contains(item, "("+issueID+")") {
			// Remove the tag
			cleaned := strings.Replace(item, "("+issueID+")", "", 1)
			filtered = append(filtered, strings.TrimSpace(cleaned))
		} else if !strings.Contains(item, "(td-") {
			// No tag, include for all
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func init() {
	rootCmd.AddCommand(wsCmd)

	wsCmd.AddCommand(wsStartCmd)
	wsCmd.AddCommand(wsTagCmd)
	wsCmd.AddCommand(wsUntagCmd)
	wsCmd.AddCommand(wsLogCmd)
	wsCmd.AddCommand(wsCurrentCmd)
	wsCmd.AddCommand(wsHandoffCmd)
	wsCmd.AddCommand(wsEndCmd)
	wsCmd.AddCommand(wsListCmd)
	wsCmd.AddCommand(wsShowCmd)

	wsLogCmd.Flags().Bool("blocker", false, "Mark as blocker")
	wsLogCmd.Flags().Bool("decision", false, "Mark as decision")
	wsLogCmd.Flags().Bool("hypothesis", false, "Mark as hypothesis")
	wsLogCmd.Flags().Bool("tried", false, "Mark as attempted approach")
	wsLogCmd.Flags().Bool("result", false, "Mark as result")
	wsLogCmd.Flags().String("only", "", "Log to specific issue only")

	wsCurrentCmd.Flags().Bool("json", false, "JSON output")

	wsTagCmd.Flags().Bool("no-start", false, "Tag without starting (don't change status to in_progress)")

	wsHandoffCmd.Flags().StringArray("done", nil, "Completed item (repeatable)")
	wsHandoffCmd.Flags().StringArray("remaining", nil, "Remaining item (repeatable)")
	wsHandoffCmd.Flags().StringArray("decision", nil, "Decision made (repeatable)")
	wsHandoffCmd.Flags().StringArray("uncertain", nil, "Uncertainty (repeatable)")
	wsHandoffCmd.Flags().Bool("continue", false, "Keep session open after handoff")
	wsHandoffCmd.Flags().Bool("review", false, "Submit all tagged issues for review")

	wsShowCmd.Flags().Bool("full", false, "Show complete session log")
}
