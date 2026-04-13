package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/internal/output"
	"github.com/spf13/cobra"
)

var commitMessageCmd = &cobra.Command{
	Use:     "commit-message [summary]",
	Aliases: []string{"commit-msg"},
	Short:   "Normalize a commit subject for the current td issue",
	Long: `Normalize a commit subject to td's conventional format:
  <type>: <summary> (td-<id>)

The issue ID comes from --issue, a trailing (td-<id>) suffix already present in
the subject, or the focused issue. When no issue is available, td still allows
typed conventional subjects such as docs/test/chore release commits. When
--file is set, td rewrites only the first line of the commit message file in
place and preserves the body/trailers. Git-generated merge and autosquash
subjects are left unchanged.`,
	Example: `  td commit-message "normalize commit hook docs"
  td commit-message --issue td-a1b2 "normalize commit hook docs"
  td commit-message --type docs "Update changelog for v0.43.0"
  td commit-message --file .git/COMMIT_EDITMSG`,
	GroupID: "system",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" && len(args) > 0 {
			return fmt.Errorf("summary argument cannot be used with --file")
		}
		if filePath == "" && len(args) == 0 {
			return fmt.Errorf(`summary required. Use: td commit-message [--issue <id>] "summary"`)
		}

		subject, err := commitMessageSubject(args, filePath)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		if git.ShouldSkipCommitMessageNormalization(subject) {
			if filePath == "" {
				fmt.Println(strings.TrimSpace(subject))
			}
			return nil
		}

		issueID, issueType, err := resolveCommitMessageContext(baseDir, cmd, subject)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		explicitType, _ := cmd.Flags().GetString("type")
		opts := git.CommitMessageOptions{
			IssueID:   issueID,
			IssueType: issueType,
			Type:      git.CommitType(explicitType),
		}

		if filePath != "" {
			if err := git.RewriteCommitMessageFile(filePath, opts); err != nil {
				output.Error("%v", err)
				return err
			}
			return nil
		}

		normalized, err := git.NormalizeCommitSubject(subject, opts)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		fmt.Println(normalized)
		return nil
	},
}

func commitMessageSubject(args []string, filePath string) (string, error) {
	if filePath == "" {
		return args[0], nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	message := string(data)
	if idx := strings.Index(message, "\n"); idx >= 0 {
		return strings.TrimSuffix(message[:idx], "\r"), nil
	}

	return message, nil
}

func resolveCommitMessageContext(baseDir string, cmd *cobra.Command, subject string) (string, models.Type, error) {
	issueFlag, _ := cmd.Flags().GetString("issue")
	issueID, err := normalizeCommitMessageIssueRef(baseDir, issueFlag)
	if err != nil {
		return "", "", err
	}

	if issueID == "" {
		issueID, err = git.ExtractCommitIssueID(subject)
		if err != nil {
			return "", "", err
		}
	}

	if issueID == "" {
		focusedID, err := config.GetFocus(baseDir)
		if err != nil {
			return "", "", err
		}
		issueID, err = git.NormalizeCommitIssueID(focusedID)
		if err != nil {
			return "", "", err
		}
	}

	if issueID == "" {
		return "", "", nil
	}

	database, err := db.Open(baseDir)
	if err != nil {
		return "", "", err
	}
	defer database.Close()

	issue, err := database.GetIssue(issueID)
	if err != nil {
		return "", "", err
	}

	return issueID, issue.Type, nil
}

func normalizeCommitMessageIssueRef(baseDir, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if trimmed == "." {
		focusedID, err := config.GetFocus(baseDir)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(focusedID) == "" {
			return "", fmt.Errorf("no focused issue available for --issue .")
		}
		trimmed = focusedID
	}

	return git.NormalizeCommitIssueID(trimmed)
}

func init() {
	rootCmd.AddCommand(commitMessageCmd)

	commitMessageCmd.Flags().StringP("issue", "i", "", "Issue ID (default: subject suffix or focused issue)")
	commitMessageCmd.Flags().StringP("type", "t", "", "Commit type override (feat, fix, docs, test, chore, ci)")
	commitMessageCmd.Flags().StringP("file", "f", "", "Rewrite a commit message file in place")
}
