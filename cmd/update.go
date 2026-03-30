package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/dateparse"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/marcus/td/internal/workflow"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:     "update [issue-id...]",
	Aliases: []string{"edit"},
	Short:   "Update one or more fields on existing issues",
	Example: "  td update td-a1b2 --priority P1 --description \"Short inline note\"\n" +
		"  td update td-a1b2 --description-file description.md\n" +
		"  cat acceptance.md | td update td-a1b2 --append --acceptance-file -",
	GroupID: "core",
	Args:    cobra.MinimumNArgs(1),
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

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("%v", err)
				continue
			}

			// (previous state captured atomically by UpdateIssueLogged)

			// Update fields if flags are set
			if title, _ := cmd.Flags().GetString("title"); title != "" {
				issue.Title = title
			}

			appendMode, _ := cmd.Flags().GetBool("append")
			stdinUsed := false

			description, descriptionProvided, nextStdinUsed, err := resolveRichTextField(
				cmd,
				[]string{"description", "desc", "body"},
				"description-file",
				stdinUsed,
			)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			if descriptionProvided {
				if appendMode && issue.Description != "" {
					issue.Description = issue.Description + "\n\n" + description
				} else {
					issue.Description = description
				}
			}
			stdinUsed = nextStdinUsed

			acceptance, acceptanceProvided, _, err := resolveRichTextField(
				cmd,
				[]string{"acceptance"},
				"acceptance-file",
				stdinUsed,
			)
			if err != nil {
				output.Error("%v", err)
				return err
			}
			if acceptanceProvided {
				if appendMode && issue.Acceptance != "" {
					issue.Acceptance = issue.Acceptance + "\n\n" + acceptance
				} else {
					issue.Acceptance = acceptance
				}
			}

			if t, _ := cmd.Flags().GetString("type"); t != "" {
				issue.Type = models.NormalizeType(t)
				if !models.IsValidType(issue.Type) {
					output.Error("invalid type: %s (valid: bug, feature, task, epic, chore)", t)
					continue
				}
			}

			if p, _ := cmd.Flags().GetString("priority"); p != "" {
				issue.Priority = models.NormalizePriority(p)
				if !models.IsValidPriority(issue.Priority) {
					output.Error("invalid priority: %s (valid: P0, P1, P2, P3, P4)", p)
					continue
				}
			}

			if pts, _ := cmd.Flags().GetInt("points"); cmd.Flags().Changed("points") {
				if pts > 0 && !models.IsValidPoints(pts) {
					output.Error("invalid points: %d (valid: 1, 2, 3, 5, 8, 13, 21)", pts)
					continue
				}
				issue.Points = pts
			}

			if cmd.Flags().Changed("labels") {
				labelsArr, _ := cmd.Flags().GetStringArray("labels")
				merged := mergeMultiValueFlag(labelsArr)
				if len(merged) == 0 {
					issue.Labels = nil
				} else {
					issue.Labels = merged
				}
			}

			if sprint, _ := cmd.Flags().GetString("sprint"); cmd.Flags().Changed("sprint") {
				issue.Sprint = sprint
			}

			if parent, _ := cmd.Flags().GetString("parent"); cmd.Flags().Changed("parent") {
				issue.ParentID = parent
			}

			// Defer date
			if deferStr, _ := cmd.Flags().GetString("defer"); cmd.Flags().Changed("defer") {
				if deferStr == "" {
					issue.DeferUntil = nil
				} else {
					parsed, err := dateparse.ParseDate(deferStr)
					if err != nil {
						output.Error("invalid defer date: %v", err)
						continue
					}
					// Increment defer count if pushing to a later date
					if issue.DeferUntil != nil && parsed > *issue.DeferUntil {
						issue.DeferCount++
					}
					issue.DeferUntil = &parsed
				}
			}

			// Due date
			if dueStr, _ := cmd.Flags().GetString("due"); cmd.Flags().Changed("due") {
				if dueStr == "" {
					issue.DueDate = nil
				} else {
					parsed, err := dateparse.ParseDate(dueStr)
					if err != nil {
						output.Error("invalid due date: %v", err)
						continue
					}
					issue.DueDate = &parsed
				}
			}

			// Handle --status flag for convenience
			if status, _ := cmd.Flags().GetString("status"); status != "" {
				newStatus := models.NormalizeStatus(status)
				if !models.IsValidStatus(newStatus) {
					output.Error("invalid status: %s (valid: open, in_progress, in_review, blocked, closed)", status)
					continue
				}
				// Validate transition with state machine
				sm := workflow.DefaultMachine()
				if !sm.IsValidTransition(issue.Status, newStatus) {
					output.Warning("cannot update %s: invalid transition from %s to %s", issueID, issue.Status, newStatus)
					continue
				}
				oldStatus := issue.Status
				issue.Status = newStatus

				// Record session action for bypass prevention based on transition type
				var sessionAction models.IssueSessionAction
				switch {
				case oldStatus == models.StatusOpen && newStatus == models.StatusInProgress:
					sessionAction = models.ActionSessionStarted
				case oldStatus == models.StatusInProgress && newStatus == models.StatusOpen:
					sessionAction = models.ActionSessionUnstarted
				case oldStatus == models.StatusInReview && newStatus == models.StatusClosed:
					sessionAction = models.ActionSessionReviewed
				}
				if sessionAction != "" {
					if err := database.RecordSessionAction(issueID, sess.ID, sessionAction); err != nil {
						output.Warning("failed to record session history: %v", err)
					}
				}
			}

			// Update dependencies (support repeated flags and comma-separated)
			if cmd.Flags().Changed("depends-on") {
				existingDeps, _ := database.GetDependencies(issueID)
				for _, dep := range existingDeps {
					database.RemoveDependencyLogged(issueID, dep, sess.ID)
				}
				dependsArr, _ := cmd.Flags().GetStringArray("depends-on")
				for _, dep := range mergeMultiValueFlag(dependsArr) {
					database.AddDependencyLogged(issueID, dep, "depends_on", sess.ID)
				}
			}

			if cmd.Flags().Changed("blocks") {
				blocked, _ := database.GetBlockedBy(issueID)
				for _, b := range blocked {
					database.RemoveDependencyLogged(b, issueID, sess.ID)
				}
				blocksArr, _ := cmd.Flags().GetStringArray("blocks")
				for _, b := range mergeMultiValueFlag(blocksArr) {
					database.AddDependencyLogged(b, issueID, "depends_on", sess.ID)
				}
			}

			if err := database.UpdateIssueLogged(issue, sess.ID, models.ActionUpdate); err != nil {
				output.Error("failed to update %s: %v", issueID, err)
				continue
			}

			fmt.Printf("UPDATED %s\n", issueID)

			// Add inline comment if --comment/-m or -c was provided
			commentText, _ := cmd.Flags().GetString("comment")
			if commentText == "" {
				commentText, _ = cmd.Flags().GetString("note")
			}
			if commentText != "" && sess != nil {
				comment := &models.Comment{
					IssueID:   issueID,
					SessionID: sess.ID,
					Text:      commentText,
				}
				if err := database.AddComment(comment); err != nil {
					output.Warning("failed to add comment to %s: %v", issueID, err)
				}
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().StringP("description", "d", "", "New description")
	updateCmd.Flags().String("desc", "", "Alias for --description")
	updateCmd.Flags().String("body", "", "Alias for --description")
	updateCmd.Flags().String("description-file", "", "Read description from file or - for stdin (preserves formatting)")
	updateCmd.Flags().String("acceptance", "", "New acceptance criteria")
	updateCmd.Flags().String("acceptance-file", "", "Read acceptance criteria from file or - for stdin (preserves formatting)")
	updateCmd.Flags().String("type", "", "New type")
	updateCmd.Flags().String("priority", "", "New priority")
	updateCmd.Flags().Int("points", 0, "New story points")
	updateCmd.Flags().StringArrayP("labels", "l", nil, "Replace labels (repeatable, comma-separated)")
	updateCmd.Flags().String("sprint", "", "New sprint name (empty string to clear)")
	updateCmd.Flags().String("parent", "", "New parent issue ID")
	updateCmd.Flags().StringArray("depends-on", nil, "Replace dependencies (repeatable, comma-separated)")
	updateCmd.Flags().StringArray("blocks", nil, "Replace blocked issues (repeatable, comma-separated)")
	updateCmd.Flags().Bool("append", false, "Append to text fields instead of replacing")
	updateCmd.Flags().String("status", "", "New status (open, in_progress, in_review, blocked, closed)")
	updateCmd.Flags().StringP("comment", "m", "", "Add a comment to the updated issue(s)")
	updateCmd.Flags().StringP("note", "c", "", "Alias for --comment")
	updateCmd.Flags().MarkHidden("note")
	updateCmd.Flags().String("defer", "", "Defer until date (e.g., +7d, monday, 2026-03-01; empty to clear)")
	updateCmd.Flags().String("due", "", "Due date (e.g., friday, +2w, 2026-03-15; empty to clear)")
}
