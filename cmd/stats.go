package cmd

import (
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "View usage statistics, security events, and errors",
	Long: `Unified command for viewing td analytics and diagnostic information.

Subcommands:
  analytics  - Command usage statistics (most/least used, never used)
  security   - Security exception audit log
  errors     - Failed command attempts`,
	GroupID: "system",
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
