package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:     "task",
	Short:   "Shortcuts for working with tasks",
	Long:    `Convenience commands for creating and viewing tasks.`,
	GroupID: "core",
}

var taskCreateCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new task",
	Long: `Create a new task. Shorthand for 'td add --type task'.

Examples:
  td task create "Implement login endpoint"
  td task create "Fix auth bug" --priority P1`,
	Args: func(cmd *cobra.Command, args []string) error {
		title, _ := cmd.Flags().GetString("title")
		if len(args) == 0 && title == "" {
			return fmt.Errorf("requires a title argument or --title flag")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set type=task on this command's flags so createCmd.RunE reads it correctly
		if err := cmd.Flags().Set("type", "task"); err != nil {
			return err
		}
		if len(args) == 0 {
			title, _ := cmd.Flags().GetString("title")
			args = []string{title}
		}
		return createCmd.RunE(cmd, args)
	},
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tasks",
	Long:  `List all tasks. Shorthand for 'td list --type task'.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		showAll, _ := cmd.Flags().GetBool("all")

		opts := db.ListIssuesOptions{
			Type: []models.Type{models.TypeTask},
		}

		// Default: exclude closed tasks unless --all is specified
		if !showAll {
			opts.Status = []models.Status{
				models.StatusOpen,
				models.StatusInProgress,
				models.StatusBlocked,
				models.StatusInReview,
			}
		}

		issues, err := database.ListIssues(opts)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if len(issues) == 0 {
			fmt.Println("No tasks found")
			return nil
		}

		for _, issue := range issues {
			fmt.Printf("%s [%s] %s: %s\n",
				issue.Priority, issue.Status, issue.ID, issue.Title)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskListCmd)

	// Copy relevant flags from createCmd to taskCreateCmd
	taskCreateCmd.Flags().String("title", "", "Issue title (max 200 characters)")
	taskCreateCmd.Flags().StringP("priority", "p", "", "Priority (P0, P1, P2, P3, P4)")
	taskCreateCmd.Flags().StringP("description", "d", "", "Description text")
	taskCreateCmd.Flags().StringArrayP("labels", "l", nil, "Labels (repeatable, comma-separated)")
	taskCreateCmd.Flags().String("parent", "", "Parent issue ID")
	taskCreateCmd.Flags().String("epic", "", "Parent issue ID (alias for --parent)")
	taskCreateCmd.Flags().StringArray("depends-on", nil, "Issues this depends on (repeatable, comma-separated)")
	taskCreateCmd.Flags().StringArray("blocks", nil, "Issues this blocks (repeatable, comma-separated)")
	taskCreateCmd.Flags().Bool("minor", false, "Mark as minor task (allows self-review)")
	// Hidden type flag - set programmatically to "task"
	taskCreateCmd.Flags().StringP("type", "t", "", "")
	taskCreateCmd.Flags().MarkHidden("type")

	// taskListCmd flags
	taskListCmd.Flags().BoolP("all", "a", false, "Show all tasks including closed")
}
