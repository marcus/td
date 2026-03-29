package cmd

import (
	"fmt"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var epicCmd = &cobra.Command{
	Use:     "epic",
	Short:   "Shortcuts for working with epics",
	Long:    `Convenience commands for creating and viewing epics.`,
	GroupID: "core",
}

var epicCreateCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new epic",
	Long: `Create a new epic. Shorthand for 'td add --type epic'.

Examples:
  td epic create "Multi-user support"
  td epic create "Auth system" --priority P0`,
	Args: func(cmd *cobra.Command, args []string) error {
		title, _ := cmd.Flags().GetString("title")
		if len(args) == 0 && title == "" {
			return fmt.Errorf("requires a title argument or --title flag")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Set type=epic on this command's flags so createCmd.RunE reads it correctly
		if err := cmd.Flags().Set("type", "epic"); err != nil {
			return err
		}
		if len(args) == 0 {
			title, _ := cmd.Flags().GetString("title")
			args = []string{title}
		}
		return createCmd.RunE(cmd, args)
	},
}

var epicListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all epics",
	Long:  `List all epics. Shorthand for 'td list --type epic'.`,
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
			Type: []models.Type{models.TypeEpic},
		}

		// Default: exclude closed epics unless --all is specified
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
			fmt.Println("No epics found")
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
	rootCmd.AddCommand(epicCmd)
	epicCmd.AddCommand(epicCreateCmd)
	epicCmd.AddCommand(epicListCmd)

	// Copy relevant flags from createCmd to epicCreateCmd
	epicCreateCmd.Flags().String("title", "", "Issue title (max 200 characters)")
	epicCreateCmd.Flags().StringP("priority", "p", "", "Priority (P0, P1, P2, P3, P4)")
	epicCreateCmd.Flags().StringP("description", "d", "", "Description text")
	epicCreateCmd.Flags().StringArray("labels", nil, "Labels (repeatable, comma-separated)")
	epicCreateCmd.Flags().String("parent", "", "Parent issue ID")
	epicCreateCmd.Flags().String("epic", "", "Parent issue ID (alias for --parent)")
	epicCreateCmd.Flags().StringArray("depends-on", nil, "Issues this depends on (repeatable, comma-separated)")
	epicCreateCmd.Flags().StringArray("blocks", nil, "Issues this blocks (repeatable, comma-separated)")
	// Hidden type flag - set programmatically to "epic"
	epicCreateCmd.Flags().StringP("type", "t", "", "")
	epicCreateCmd.Flags().MarkHidden("type")

	// epicListCmd flags
	epicListCmd.Flags().BoolP("all", "a", false, "Show all epics including closed")
}
