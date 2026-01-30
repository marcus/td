package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"regexp"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/session"
	"github.com/spf13/cobra"
)

// issueIDPattern matches valid issue IDs like "td-a1b2c3" or "td-a1b2c3d4"
var issueIDPattern = regexp.MustCompile(`^td-[0-9a-f]{6,8}$`)

var logCmd = &cobra.Command{
	Use:   "log [issue-id] <message>",
	Short: "Append a log entry to the current issue",
	Long: `Low-friction progress tracking during a session.

Syntax:
  td log <message>              # Log to focused issue
  td log <issue-id> <message>   # Log to specific issue
  td log --issue <id> <message> # Log to specific issue (flag syntax)

Supports stdin input for multi-line messages or piped input:
  echo "message" | td log
  td log < notes.txt
  td log <<EOF
  Multi-line
  log message
  EOF`,
	GroupID: "workflow",
	Args:    cobra.MaximumNArgs(2),
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

		// Parse args to determine issue ID and message
		var issueID string
		var message string

		if len(args) == 2 {
			// Two args: detect which is issue ID regardless of order
			if issueIDPattern.MatchString(args[0]) {
				issueID = args[0]
				message = args[1]
			} else if issueIDPattern.MatchString(args[1]) {
				issueID = args[1]
				message = args[0]
			} else {
				// Neither matches ID pattern; treat as original order (first=ID, second=message)
				issueID = args[0]
				message = args[1]
			}
		} else if len(args) == 1 {
			// One arg: check if it's an issue ID or message
			// Issue IDs match pattern "td-[8 hex chars]", otherwise it's a message
			if issueIDPattern.MatchString(args[0]) {
				// It's an issue ID, get message from stdin
				issueID = args[0]
				// Check if stdin has data
				stat, _ := os.Stdin.Stat()
				if (stat.Mode() & os.ModeCharDevice) == 0 {
					if stat.Size() > 0 {
						reader := bufio.NewReader(os.Stdin)
						data, err := io.ReadAll(reader)
						if err != nil {
							output.Error("failed to read stdin: %v", err)
							return err
						}
						message = strings.TrimSpace(string(data))
					}
				}
			} else {
				// It's a message, get issue ID from flag or focus
				message = args[0]
				issueID, _ = cmd.Flags().GetString("issue")
			}
		}

		// If no issue ID yet, check flag or fall back to focus
		if issueID == "" {
			issueID, _ = cmd.Flags().GetString("issue")
			if issueID == "" {
				issueID, _ = cmd.Flags().GetString("task")
			}
			if issueID == "" {
				issueID, err = config.GetFocus(baseDir)
				if err != nil || issueID == "" {
					output.Error("no issue specified and no focused issue")
					fmt.Fprintln(os.Stderr, "  Use: td log <id> \"message\"  OR  td start <id> first")
					return fmt.Errorf("no issue specified")
				}
			}
		}

		// If still no message, try stdin
		if message == "" {
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				if stat.Size() > 0 {
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

		// Verify issue exists
		_, err = database.GetIssue(issueID)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		// Determine log type
		logType := models.LogTypeProgress
		typeLabel := ""

		if typeStr, _ := cmd.Flags().GetString("type"); typeStr != "" {
			logType = models.LogType(typeStr)
			typeLabel = " [" + typeStr + "]"
		} else if blocker, _ := cmd.Flags().GetBool("blocker"); blocker {
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

	logCmd.Flags().StringP("issue", "i", "", "Issue ID (default: focused issue)")
	logCmd.Flags().StringP("type", "t", "", "Log type (progress, blocker, decision, hypothesis, tried, result, orchestration)")
	logCmd.Flags().StringP("task", "T", "", "Alias for --issue (issue ID)")
	logCmd.Flags().Bool("blocker", false, "Mark as blocker")
	logCmd.Flags().Bool("decision", false, "Mark as decision")
	logCmd.Flags().Bool("hypothesis", false, "Mark as hypothesis")
	logCmd.Flags().Bool("tried", false, "Mark as attempted approach")
	logCmd.Flags().Bool("result", false, "Mark as result")
}
