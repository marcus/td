package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:     "create [title]",
	Aliases: []string{"add", "new"},
	Short:   "Create a new issue",
	Long:    `Create a new issue with optional flags for type, priority, labels, and more.`,
	GroupID: "core",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		// Get title from args or flag
		title, _ := cmd.Flags().GetString("title")
		if len(args) > 0 {
			title = args[0]
		}

		if title == "" {
			output.Error("title is required")
			return fmt.Errorf("title is required")
		}

		// Build issue
		issue := &models.Issue{
			Title: title,
		}

		// Type (supports "story" as alias for "feature")
		if t, _ := cmd.Flags().GetString("type"); t != "" {
			issue.Type = models.NormalizeType(t)
			if !models.IsValidType(issue.Type) {
				output.Error("invalid type: %s", t)
				return fmt.Errorf("invalid type: %s", t)
			}
		}

		// Priority (supports numeric: "1" as alias for "P1")
		if p, _ := cmd.Flags().GetString("priority"); p != "" {
			issue.Priority = models.NormalizePriority(p)
			if !models.IsValidPriority(issue.Priority) {
				output.Error("invalid priority: %s", p)
				return fmt.Errorf("invalid priority: %s", p)
			}
		}

		// Points
		if pts, _ := cmd.Flags().GetInt("points"); pts > 0 {
			if !models.IsValidPoints(pts) {
				output.Error("invalid points: %d (must be Fibonacci: 1,2,3,5,8,13,21)", pts)
				return fmt.Errorf("invalid points")
			}
			issue.Points = pts
		}

		// Labels (support --labels, --label, --tags, --tag)
		labelsStr, _ := cmd.Flags().GetString("labels")
		if labelsStr == "" {
			if s, _ := cmd.Flags().GetString("label"); s != "" {
				labelsStr = s
			}
		}
		if labelsStr == "" {
			if s, _ := cmd.Flags().GetString("tags"); s != "" {
				labelsStr = s
			}
		}
		if labelsStr == "" {
			if s, _ := cmd.Flags().GetString("tag"); s != "" {
				labelsStr = s
			}
		}
		if labelsStr != "" {
			issue.Labels = strings.Split(labelsStr, ",")
			for i := range issue.Labels {
				issue.Labels[i] = strings.TrimSpace(issue.Labels[i])
			}
		}

		// Description (support --description, --desc, and --body)
		issue.Description, _ = cmd.Flags().GetString("description")
		if issue.Description == "" {
			if desc, _ := cmd.Flags().GetString("desc"); desc != "" {
				issue.Description = desc
			}
		}
		if issue.Description == "" {
			if body, _ := cmd.Flags().GetString("body"); body != "" {
				issue.Description = body
			}
		}
		if issue.Description == "" {
			if notes, _ := cmd.Flags().GetString("notes"); notes != "" {
				issue.Description = notes
			}
		}

		// Acceptance
		issue.Acceptance, _ = cmd.Flags().GetString("acceptance")

		// Parent (supports --parent and --epic)
		issue.ParentID, _ = cmd.Flags().GetString("parent")
		if issue.ParentID == "" {
			if epic, _ := cmd.Flags().GetString("epic"); epic != "" {
				issue.ParentID = epic
			}
		}

		// Minor (allows self-review)
		issue.Minor, _ = cmd.Flags().GetBool("minor")

		// Get session BEFORE creating issue (needed for CreatorSession)
		sess, err := session.GetOrCreate(baseDir)
		if err != nil {
			output.Error("failed to create session: %v", err)
			return fmt.Errorf("failed to create session: %w", err)
		}
		issue.CreatorSession = sess.ID

		// Capture current git branch
		gitState, _ := git.GetState()
		if gitState != nil {
			issue.CreatedBranch = gitState.Branch
		}

		// Create the issue
		if err := database.CreateIssue(issue); err != nil {
			output.Error("failed to create issue: %v", err)
			return err
		}

		// Record session action for bypass prevention
		if err := database.RecordSessionAction(issue.ID, sess.ID, models.ActionSessionCreated); err != nil {
			output.Warning("failed to record session history: %v", err)
		}

		// Log action for undo
		newData, _ := json.Marshal(issue)
		database.LogAction(&models.ActionLog{
			SessionID:  sess.ID,
			ActionType: models.ActionCreate,
			EntityType: "issue",
			EntityID:   issue.ID,
			NewData:    string(newData),
		})

		// Handle dependencies
		if dependsOn, _ := cmd.Flags().GetString("depends-on"); dependsOn != "" {
			for _, dep := range strings.Split(dependsOn, ",") {
				dep = strings.TrimSpace(dep)
				if err := database.AddDependency(issue.ID, dep, "depends_on"); err != nil {
					output.Warning("failed to add dependency %s: %v", dep, err)
				}
			}
		}

		if blocks, _ := cmd.Flags().GetString("blocks"); blocks != "" {
			for _, blocked := range strings.Split(blocks, ",") {
				blocked = strings.TrimSpace(blocked)
				if err := database.AddDependency(blocked, issue.ID, "depends_on"); err != nil {
					output.Warning("failed to add blocks %s: %v", blocked, err)
				}
			}
		}

		fmt.Printf("CREATED %s\n", issue.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createCmd)

	createCmd.Flags().String("title", "", "Issue title")
	createCmd.Flags().StringP("type", "t", "", "Issue type (bug, feature, task, epic, chore)")
	createCmd.Flags().StringP("priority", "p", "", "Priority (P0, P1, P2, P3, P4)")
	createCmd.Flags().Int("points", 0, "Story points (Fibonacci: 1,2,3,5,8,13,21)")
	createCmd.Flags().StringP("labels", "l", "", "Comma-separated labels")
	createCmd.Flags().String("label", "", "Alias for --labels (single or comma-separated)")
	createCmd.Flags().String("tags", "", "Alias for --labels")
	createCmd.Flags().String("tag", "", "Alias for --labels")
	createCmd.Flags().StringP("description", "d", "", "Description text")
	createCmd.Flags().String("desc", "", "Alias for --description")
	createCmd.Flags().String("body", "", "Alias for --description")
	createCmd.Flags().String("notes", "", "Alias for --description")
	createCmd.Flags().String("acceptance", "", "Acceptance criteria")
	createCmd.Flags().String("parent", "", "Parent issue ID")
	createCmd.Flags().String("epic", "", "Parent issue ID (alias for --parent)")
	createCmd.Flags().String("depends-on", "", "Issues this depends on")
	createCmd.Flags().String("blocks", "", "Issues this blocks")
	createCmd.Flags().Bool("minor", false, "Mark as minor task (allows self-review)")
}
