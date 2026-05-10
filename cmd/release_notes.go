package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gitutil "github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

const (
	releaseNotesSectionFeatures      = "Features"
	releaseNotesSectionBugFixes      = "Bug Fixes"
	releaseNotesSectionImprovements  = "Improvements"
	releaseNotesSectionDocumentation = "Documentation"
	releaseNotesSectionTesting       = "Testing"
	releaseNotesSectionOtherChanges  = "Other Changes"
)

var (
	conventionalCommitPattern = regexp.MustCompile(`(?i)^(feat(?:ure)?|fix|bugfix|bug|docs?|doc|test(?:s)?|refactor|perf|chore|build|ci|style|revert)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	releaseNotesSectionOrder  = []string{
		releaseNotesSectionFeatures,
		releaseNotesSectionBugFixes,
		releaseNotesSectionImprovements,
		releaseNotesSectionDocumentation,
		releaseNotesSectionTesting,
		releaseNotesSectionOtherChanges,
	}
)

var releaseNotesCmd = &cobra.Command{
	Use:     "release-notes",
	Short:   "Draft release notes from git history",
	GroupID: "system",
	Args:    cobra.NoArgs,
	Long: `Draft release notes from git history.

By default, td uses the latest reachable semver tag through HEAD as the
baseline and prints a markdown section you can review and paste into
CHANGELOG.md.`,
	Example: `  td release-notes --version v0.44.0
  td release-notes --from v0.43.0 --to HEAD --date 2026-04-02`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseDir := getBaseDir()

		fromRef, _ := cmd.Flags().GetString("from")
		toRef, _ := cmd.Flags().GetString("to")
		versionLabel, _ := cmd.Flags().GetString("version")
		dateLabel, _ := cmd.Flags().GetString("date")

		if dateLabel == "" {
			dateLabel = time.Now().Format("2006-01-02")
		} else if _, err := time.Parse("2006-01-02", dateLabel); err != nil {
			return fmt.Errorf("invalid --date %q: use YYYY-MM-DD", dateLabel)
		}

		data, err := gitutil.GetReleaseNotesData(baseDir, gitutil.ReleaseNotesOptions{
			FromRef: fromRef,
			ToRef:   toRef,
		})
		if err != nil {
			switch {
			case errors.Is(err, gitutil.ErrNotRepository):
				return fmt.Errorf("release notes require a git repository")
			default:
				return err
			}
		}

		_, _ = fmt.Fprint(cmd.OutOrStdout(), formatReleaseNotesMarkdown(data, versionLabel, dateLabel))
		return nil
	},
}

func formatReleaseNotesMarkdown(data *gitutil.ReleaseNotesData, versionLabel, dateLabel string) string {
	versionLabel = strings.TrimSpace(versionLabel)
	if versionLabel == "" {
		versionLabel = "Unreleased"
	}

	sections := categorizeReleaseNotes(data.Commits)

	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n\n", versionLabel, dateLabel)

	for _, section := range releaseNotesSectionOrder {
		entries := sections[section]
		if len(entries) == 0 {
			continue
		}

		fmt.Fprintf(&b, "### %s\n", section)
		for _, entry := range entries {
			fmt.Fprintf(&b, "- %s\n", entry)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func categorizeReleaseNotes(commits []gitutil.ReleaseCommit) map[string][]string {
	sections := make(map[string][]string)
	for _, commit := range commits {
		section, entry := releaseNoteEntry(commit)
		sections[section] = append(sections[section], entry)
	}
	return sections
}

func releaseNoteEntry(commit gitutil.ReleaseCommit) (string, string) {
	subject := strings.TrimSpace(commit.Subject)
	if subject == "" {
		shortSHA := commit.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		return releaseNotesSectionOtherChanges, fmt.Sprintf("Update in commit %s", shortSHA)
	}

	if matches := conventionalCommitPattern.FindStringSubmatch(subject); len(matches) == 4 {
		section := releaseNoteSectionForPrefix(strings.ToLower(matches[1]))
		scope := strings.TrimSpace(matches[2])
		description := prettifyReleaseSummary(matches[3])
		if scope != "" {
			return section, fmt.Sprintf("%s: %s", scope, description)
		}
		return section, description
	}

	switch {
	case releaseNotesAreDocumentationFiles(commit.Files):
		return releaseNotesSectionDocumentation, prettifyReleaseSummary(subject)
	case releaseNotesAreTestFiles(commit.Files):
		return releaseNotesSectionTesting, prettifyReleaseSummary(subject)
	default:
		return releaseNotesSectionImprovements, prettifyReleaseSummary(subject)
	}
}

func releaseNoteSectionForPrefix(prefix string) string {
	switch prefix {
	case "feat", "feature":
		return releaseNotesSectionFeatures
	case "fix", "bugfix", "bug":
		return releaseNotesSectionBugFixes
	case "docs", "doc":
		return releaseNotesSectionDocumentation
	case "test", "tests":
		return releaseNotesSectionTesting
	case "refactor", "perf", "chore", "build", "ci", "style":
		return releaseNotesSectionImprovements
	default:
		return releaseNotesSectionOtherChanges
	}
}

func releaseNotesAreDocumentationFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isDocumentationFile(file) {
			return false
		}
	}
	return true
}

func releaseNotesAreTestFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isTestFile(file) {
			return false
		}
	}
	return true
}

func isDocumentationFile(path string) bool {
	return strings.HasPrefix(path, "docs/") ||
		strings.HasPrefix(path, "website/docs/") ||
		strings.HasSuffix(path, ".md") ||
		path == "README" ||
		path == "README.md" ||
		path == "CHANGELOG.md"
}

func isTestFile(path string) bool {
	return strings.HasPrefix(path, "test/") ||
		strings.Contains(path, "/testdata/") ||
		strings.HasSuffix(path, "_test.go")
}

func prettifyReleaseSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return summary
	}

	r, size := utf8.DecodeRuneInString(summary)
	if r == utf8.RuneError && size == 0 {
		return summary
	}
	if !unicode.IsLower(r) {
		return summary
	}
	return string(unicode.ToUpper(r)) + summary[size:]
}

func init() {
	rootCmd.AddCommand(releaseNotesCmd)
	releaseNotesCmd.Flags().String("from", "", "Start the range at this git ref or tag")
	releaseNotesCmd.Flags().String("to", "HEAD", "End the range at this git ref")
	releaseNotesCmd.Flags().String("version", "", "Version label for the markdown header")
	releaseNotesCmd.Flags().String("date", "", "Date for the markdown header (YYYY-MM-DD)")
}
