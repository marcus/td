package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	changelogpkg "github.com/marcus/td/internal/changelog"
	gitutil "github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var (
	changelogNow         = time.Now
	changelogDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

var changelogCmd = &cobra.Command{
	Use:     "changelog",
	Short:   "Generate a CHANGELOG.md entry from git commits",
	GroupID: "system",
	Args:    cobra.NoArgs,
	Long: `Generate a paste-ready CHANGELOG.md entry from committed git history.

By default, td uses the nearest reachable semver tag through HEAD as the
starting point and prints markdown to stdout for review. It never edits
CHANGELOG.md automatically.`,
	Example: `  td changelog
  td changelog --version v0.5.0
  td changelog --version v0.5.0 --date 2026-05-09
  td changelog --from v0.4.0 --to HEAD --version v0.5.0`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fromRef, _ := cmd.Flags().GetString("from")
		toRef, _ := cmd.Flags().GetString("to")
		versionLabel, _ := cmd.Flags().GetString("version")
		dateValue, _ := cmd.Flags().GetString("date")

		fromRef = strings.TrimSpace(fromRef)
		toRef = strings.TrimSpace(toRef)
		versionLabel = strings.TrimSpace(versionLabel)
		dateValue = strings.TrimSpace(dateValue)

		if cmd.Flags().Changed("from") && fromRef == "" {
			return fmt.Errorf("--from cannot be empty")
		}
		if toRef == "" {
			return fmt.Errorf("--to cannot be empty")
		}

		var releaseDate time.Time
		if dateValue != "" {
			if versionLabel == "" {
				return fmt.Errorf("--version is required when --date is supplied")
			}
			if !changelogDatePattern.MatchString(dateValue) {
				return fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", dateValue)
			}
			parsedDate, err := time.Parse("2006-01-02", dateValue)
			if err != nil {
				return fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", dateValue)
			}
			releaseDate = parsedDate
		} else if versionLabel != "" {
			releaseDate = changelogNow()
		}

		draft, err := changelogpkg.Generate(gitHistoryRepoDir(), changelogpkg.Options{
			FromRef: fromRef,
			ToRef:   toRef,
			Version: versionLabel,
			Date:    releaseDate,
		})
		if err != nil {
			switch {
			case errors.Is(err, gitutil.ErrNotRepository):
				return fmt.Errorf("changelog requires a git repository")
			case errors.Is(err, gitutil.ErrNoSemverTag):
				return fmt.Errorf("no reachable semver tag found for %s; pass --from to set the starting ref", toRef)
			case errors.Is(err, changelogpkg.ErrNoRelevantCommits):
				return err
			default:
				return err
			}
		}

		_, err = fmt.Fprint(cmd.OutOrStdout(), draft.Markdown())
		return err
	},
}

// gitHistoryRepoDir uses the active worktree instead of td's resolved database
// root when possible, so changelogs follow the branch the user is actually on.
func gitHistoryRepoDir() string {
	if baseDirOverride != nil {
		return *baseDirOverride
	}
	if workDirFlag != "" {
		return normalizeWorkDir(workDirFlag)
	}
	cwd, err := os.Getwd()
	if err == nil && gitutil.IsRepoAt(cwd) {
		return cwd
	}
	if envDir := os.Getenv("TD_WORK_DIR"); envDir != "" {
		return normalizeWorkDir(envDir)
	}
	if err == nil {
		return cwd
	}
	return getBaseDir()
}

func init() {
	rootCmd.AddCommand(changelogCmd)
	changelogCmd.Flags().String("from", "", "Start the range at this git ref or tag (default: nearest reachable semver tag)")
	changelogCmd.Flags().String("to", "HEAD", "End the range at this git ref")
	changelogCmd.Flags().String("version", "", "Version label for the markdown header")
	changelogCmd.Flags().String("date", "", "Release date for the markdown header (YYYY-MM-DD; requires --version)")
}
