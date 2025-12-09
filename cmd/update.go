package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:     "update [issue-id...]",
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

		sess, _ := session.Get(baseDir)

		for _, issueID := range args {
			issue, err := database.GetIssue(issueID)
			if err != nil {
				output.Error("%v", err)
				continue
			}

			// Capture previous state for undo
			prevData, _ := json.Marshal(issue)

			// Update fields if flags are set
			if title, _ := cmd.Flags().GetString("title"); title != "" {
				issue.Title = title
			}

			if desc, _ := cmd.Flags().GetString("description"); desc != "" {
				issue.Description = desc
			}

			if acceptance, _ := cmd.Flags().GetString("acceptance"); acceptance != "" {
				issue.Acceptance = acceptance
			}

			if t, _ := cmd.Flags().GetString("type"); t != "" {
				issue.Type = models.Type(t)
				if !models.IsValidType(issue.Type) {
					output.Error("invalid type: %s", t)
					continue
				}
			}

			if p, _ := cmd.Flags().GetString("priority"); p != "" {
				issue.Priority = models.Priority(p)
				if !models.IsValidPriority(issue.Priority) {
					output.Error("invalid priority: %s", p)
					continue
				}
			}

			if pts, _ := cmd.Flags().GetInt("points"); cmd.Flags().Changed("points") {
				if pts > 0 && !models.IsValidPoints(pts) {
					output.Error("invalid points: %d", pts)
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

			if parent, _ := cmd.Flags().GetString("parent"); cmd.Flags().Changed("parent") {
				issue.ParentID = parent
			}

			// Update dependencies
			if dependsOn, _ := cmd.Flags().GetString("depends-on"); cmd.Flags().Changed("depends-on") {
				// Clear existing and set new
				// First get existing deps and remove them
				existingDeps, _ := database.GetDependencies(issueID)
				for _, dep := range existingDeps {
					// Log removal for undo
					depData, _ := json.Marshal(map[string]string{"issue_id": issueID, "depends_on_id": dep})
					database.LogAction(&models.ActionLog{
						SessionID:  sess.ID,
						ActionType: models.ActionRemoveDep,
						EntityType: "dependency",
						EntityID:   issueID + ":" + dep,
						NewData:    string(depData),
					})
					database.RemoveDependency(issueID, dep)
				}
				// Add new deps
				if dependsOn != "" {
					for _, dep := range strings.Split(dependsOn, ",") {
						dep = strings.TrimSpace(dep)
						// Log addition for undo
						depData, _ := json.Marshal(map[string]string{"issue_id": issueID, "depends_on_id": dep})
						database.LogAction(&models.ActionLog{
							SessionID:  sess.ID,
							ActionType: models.ActionAddDep,
							EntityType: "dependency",
							EntityID:   issueID + ":" + dep,
							NewData:    string(depData),
						})
						database.AddDependency(issueID, dep, "depends_on")
					}
				}
			}

			if blocks, _ := cmd.Flags().GetString("blocks"); cmd.Flags().Changed("blocks") {
				// For blocks, we need to find issues that depend on this one and update them
				blocked, _ := database.GetBlockedBy(issueID)
				for _, b := range blocked {
					// Log removal for undo
					depData, _ := json.Marshal(map[string]string{"issue_id": b, "depends_on_id": issueID})
					database.LogAction(&models.ActionLog{
						SessionID:  sess.ID,
						ActionType: models.ActionRemoveDep,
						EntityType: "dependency",
						EntityID:   b + ":" + issueID,
						NewData:    string(depData),
					})
					database.RemoveDependency(b, issueID)
				}
				// Add new blocks
				if blocks != "" {
					for _, b := range strings.Split(blocks, ",") {
						b = strings.TrimSpace(b)
						// Log addition for undo
						depData, _ := json.Marshal(map[string]string{"issue_id": b, "depends_on_id": issueID})
						database.LogAction(&models.ActionLog{
							SessionID:  sess.ID,
							ActionType: models.ActionAddDep,
							EntityType: "dependency",
							EntityID:   b + ":" + issueID,
							NewData:    string(depData),
						})
						database.AddDependency(b, issueID, "depends_on")
					}
				}
			}

			if err := database.UpdateIssue(issue); err != nil {
				output.Error("failed to update %s: %v", issueID, err)
				continue
			}

			// Log action for undo
			if sess != nil {
				newData, _ := json.Marshal(issue)
				database.LogAction(&models.ActionLog{
					SessionID:    sess.ID,
					ActionType:   models.ActionUpdate,
					EntityType:   "issue",
					EntityID:     issueID,
					PreviousData: string(prevData),
					NewData:      string(newData),
				})
			}

			fmt.Printf("UPDATED %s\n", issueID)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().String("description", "", "New description")
	updateCmd.Flags().String("acceptance", "", "New acceptance criteria")
	updateCmd.Flags().String("type", "", "New type")
	updateCmd.Flags().String("priority", "", "New priority")
	updateCmd.Flags().Int("points", 0, "New story points")
	updateCmd.Flags().String("labels", "", "Replace labels")
	updateCmd.Flags().String("parent", "", "New parent issue ID")
	updateCmd.Flags().String("depends-on", "", "Replace dependencies")
	updateCmd.Flags().String("blocks", "", "Replace blocked issues")
}
