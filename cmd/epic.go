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
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Delegate to createCmd with --type epic
		if err := createCmd.Flags().Set("type", "epic"); err != nil {
			return err
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

		issues, err := database.ListIssues(db.ListIssuesOptions{
			Type: []models.Type{models.TypeEpic},
		})
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
	epicCreateCmd.Flags().StringP("priority", "p", "", "Priority (P0, P1, P2, P3, P4)")
	epicCreateCmd.Flags().StringP("description", "d", "", "Description text")
	epicCreateCmd.Flags().String("labels", "", "Comma-separated labels")
}
