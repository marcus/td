package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var errorsCmd = &cobra.Command{
	Use:     "errors",
	Short:   "View failed td command attempts",
	Long:    `Shows agent error log for analyzing failed td invocations.`,
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		clearFlag, _ := cmd.Flags().GetBool("clear")
		if clearFlag {
			if err := db.ClearAgentErrors(baseDir); err != nil {
				output.Error("failed to clear errors: %v", err)
				return err
			}
			fmt.Println("Cleared agent error log")
			return nil
		}

		countFlag, _ := cmd.Flags().GetBool("count")
		if countFlag {
			count, err := db.CountAgentErrors(baseDir)
			if err != nil {
				output.Error("failed to count errors: %v", err)
				return err
			}
			fmt.Printf("%d\n", count)
			return nil
		}

		// Parse filters
		limit, _ := cmd.Flags().GetInt("limit")
		sessionFilter, _ := cmd.Flags().GetString("session")
		sinceStr, _ := cmd.Flags().GetString("since")
		jsonOut, _ := cmd.Flags().GetBool("json")

		var since time.Time
		if sinceStr != "" {
			dur, err := session.ParseDuration(sinceStr)
			if err != nil {
				output.Error("invalid duration: %v", err)
				return err
			}
			since = time.Now().Add(-dur)
		}

		errors, err := db.ReadAgentErrorsFiltered(baseDir, sessionFilter, since, limit)
		if err != nil {
			output.Error("failed to read errors: %v", err)
			return err
		}

		if len(errors) == 0 {
			fmt.Println("No agent errors logged")
			return nil
		}

		if jsonOut {
			for _, e := range errors {
				fmt.Printf(`{"ts":"%s","args":[%s],"error":"%s","session":"%s"}`+"\n",
					e.Timestamp.Format(time.RFC3339),
					formatArgsJSON(e.Args),
					escapeJSON(e.Error),
					e.SessionID)
			}
			return nil
		}

		// Human-readable output
		fmt.Printf("Agent Errors (%d):\n\n", len(errors))
		for _, e := range errors {
			ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
			argsStr := strings.Join(e.Args, " ")
			if argsStr == "" {
				argsStr = "(no args)"
			}

			fmt.Printf("%s  td %s\n", ts, argsStr)
			fmt.Printf("  Error: %s\n", e.Error)
			if e.SessionID != "" {
				fmt.Printf("  Session: %s\n", e.SessionID)
			}
			fmt.Println()
		}

		return nil
	},
}

func formatArgsJSON(args []string) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for _, a := range args {
		parts = append(parts, `"`+escapeJSON(a)+`"`)
	}
	return strings.Join(parts, ",")
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func init() {
	rootCmd.AddCommand(errorsCmd)

	errorsCmd.Flags().Bool("clear", false, "Clear the error log")
	errorsCmd.Flags().Bool("count", false, "Show count only")
	errorsCmd.Flags().Int("limit", 20, "Max errors to show")
	errorsCmd.Flags().String("session", "", "Filter by session ID")
	errorsCmd.Flags().String("since", "", "Show errors since duration (e.g., 1h, 24h, 7d)")
	errorsCmd.Flags().Bool("json", false, "Output as JSONL")
}
