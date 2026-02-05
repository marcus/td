package cmd

import (
	"fmt"
	"strings"

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

			// Check description and its aliases (--desc, --body, -d)
			desc, _ := cmd.Flags().GetString("description")
			if desc == "" {
				desc, _ = cmd.Flags().GetString("desc")
			}
			if desc == "" {
				desc, _ = cmd.Flags().GetString("body")
			}
			if desc != "" {
				if appendMode && issue.Description != "" {
					issue.Description = issue.Description + "\n\n" + desc
				} else {
					issue.Description = desc
				}
			}

			if acceptance, _ := cmd.Flags().GetString("acceptance"); acceptance != "" {
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

			if labels, _ := cmd.Flags().GetString("labels"); cmd.Flags().Changed("labels") {
				if labels == "" {
					issue.Labels = nil
				} else {
					issue.Labels = strings.Split(labels, ",")
					for i := range issue.Labels {
						issue.Labels[i] = strings.TrimSpace(issue.Labels[i])
					}
				}
			}

			if sprint, _ := cmd.Flags().GetString("sprint"); cmd.Flags().Changed("sprint") {
				issue.Sprint = sprint
			}

			if parent, _ := cmd.Flags().GetString("parent"); cmd.Flags().Changed("parent") {
				issue.ParentID = parent
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

			// Update dependencies
			if dependsOn, _ := cmd.Flags().GetString("depends-on"); cmd.Flags().Changed("depends-on") {
				// Clear existing and set new
				existingDeps, _ := database.GetDependencies(issueID)
				for _, dep := range existingDeps {
					database.RemoveDependencyLogged(issueID, dep, sess.ID)
				}
				// Add new deps
				if dependsOn != "" {
					for _, dep := range strings.Split(dependsOn, ",") {
						dep = strings.TrimSpace(dep)
						database.AddDependencyLogged(issueID, dep, "depends_on", sess.ID)
					}
				}
			}

			if blocks, _ := cmd.Flags().GetString("blocks"); cmd.Flags().Changed("blocks") {
				// For blocks, we need to find issues that depend on this one and update them
				blocked, _ := database.GetBlockedBy(issueID)
				for _, b := range blocked {
					database.RemoveDependencyLogged(b, issueID, sess.ID)
				}
				// Add new blocks
				if blocks != "" {
					for _, b := range strings.Split(blocks, ",") {
						b = strings.TrimSpace(b)
						database.AddDependencyLogged(b, issueID, "depends_on", sess.ID)
					}
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
	updateCmd.Flags().String("acceptance", "", "New acceptance criteria")
	updateCmd.Flags().String("type", "", "New type")
	updateCmd.Flags().String("priority", "", "New priority")
	updateCmd.Flags().Int("points", 0, "New story points")
	updateCmd.Flags().String("labels", "", "Replace labels")
	updateCmd.Flags().String("sprint", "", "New sprint name (empty string to clear)")
	updateCmd.Flags().String("parent", "", "New parent issue ID")
	updateCmd.Flags().String("depends-on", "", "Replace dependencies")
	updateCmd.Flags().String("blocks", "", "Replace blocked issues")
	updateCmd.Flags().Bool("append", false, "Append to text fields instead of replacing")
	updateCmd.Flags().String("status", "", "New status (open, in_progress, in_review, blocked, closed)")
	updateCmd.Flags().StringP("comment", "m", "", "Add a comment to the updated issue(s)")
	updateCmd.Flags().StringP("note", "c", "", "Alias for --comment")
	updateCmd.Flags().MarkHidden("note")
}
