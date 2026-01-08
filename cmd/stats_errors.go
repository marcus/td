package cmd

import (
	"github.com/spf13/cobra"
)

var statsErrorsCmd = &cobra.Command{
	Use:   "errors",
	Short: "View failed td command attempts (alias for 'td errors')",
	Long:  `Shows agent error log for analyzing failed td invocations.`,
	RunE:  errorsCmd.RunE,
}

func init() {
	statsCmd.AddCommand(statsErrorsCmd)

	// Copy flags from errors command
	statsErrorsCmd.Flags().Bool("clear", false, "Clear the error log")
	statsErrorsCmd.Flags().Bool("count", false, "Show count only")
	statsErrorsCmd.Flags().Int("limit", 20, "Max errors to show")
	statsErrorsCmd.Flags().String("session", "", "Filter by session ID")
	statsErrorsCmd.Flags().String("since", "", "Show errors since duration")
	statsErrorsCmd.Flags().Bool("json", false, "Output as JSONL")
}
