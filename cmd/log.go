package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log [message]",
	Short: "Append a log entry to the current issue",
	Long: `Low-friction progress tracking during a session.

Supports stdin input for multi-line messages or piped input:
  echo "message" | td log
  td log < notes.txt
  td log <<EOF
  Multi-line
  log message
  EOF`,
	GroupID: "workflow",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		database, err := db.Open(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		defer database.Close()

		sess, err := session.Get(baseDir)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Get issue ID from flag or focus
		issueID, _ := cmd.Flags().GetString("issue")
		if issueID == "" {
			issueID, err = config.GetFocus(baseDir)
			if err != nil || issueID == "" {
				output.Error("no focused issue. Use --issue or td focus <issue-id>")
				return fmt.Errorf("no focused issue")
			}
		}

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Determine log type
		logType := models.LogTypeProgress
		typeLabel := ""

		if blocker, _ := cmd.Flags().GetBool("blocker"); blocker {
			logType = models.LogTypeBlocker
			typeLabel = " [blocker]"
		} else if decision, _ := cmd.Flags().GetBool("decision"); decision {
			logType = models.LogTypeDecision
			typeLabel = " [decision]"
		} else if hypothesis, _ := cmd.Flags().GetBool("hypothesis"); hypothesis {
			logType = models.LogTypeHypothesis
			typeLabel = " [hypothesis]"
		} else if tried, _ := cmd.Flags().GetBool("tried"); tried {
			logType = models.LogTypeTried
			typeLabel = " [tried]"
		} else if result, _ := cmd.Flags().GetBool("result"); result {
			logType = models.LogTypeResult
			typeLabel = " [result]"
		}

		// Get message from args or stdin
		var message string
		if len(args) > 0 {
			message = args[0]
		} else {
			// Check if stdin has data - only read if it's a pipe AND has available data
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				if stat.Size() > 0 {
					// Read from stdin
					reader := bufio.NewReader(os.Stdin)
					data, err := io.ReadAll(reader)
					if err != nil {
						output.Error("failed to read stdin: %v", err)
						return err
					}
					message = strings.TrimSpace(string(data))
				}
			}
		}

		if message == "" {
			output.Error("no message provided. Use: td log \"message\" or pipe input")
			return fmt.Errorf("no message provided")
		}

		// Get active work session if any
		wsID, _ := config.GetActiveWorkSession(baseDir)

		log := &models.Log{
			IssueID:       issueID,
			SessionID:     sess.ID,
			WorkSessionID: wsID,
			Message:       message,
			Type:          logType,
		}

		if err := database.AddLog(log); err != nil {
			output.Error("failed to add log: %v", err)
			return err
		}

		fmt.Printf("LOGGED %s%s\n", issueID, typeLabel)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logCmd)

	logCmd.Flags().String("issue", "", "Issue ID (default: focused issue)")
	logCmd.Flags().Bool("blocker", false, "Mark as blocker")
	logCmd.Flags().Bool("decision", false, "Mark as decision")
	logCmd.Flags().Bool("hypothesis", false, "Mark as hypothesis")
	logCmd.Flags().Bool("tried", false, "Mark as attempted approach")
	logCmd.Flags().Bool("result", false, "Mark as result")
}
