package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	changelogpkg "github.com/marcus/td/internal/changelog"
	gitpkg "github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var releaseVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func newChangelogCmd() *cobra.Command {
	var version string
	var from string
	var to string
	var releaseDate string
	var includeMeta bool

	cmd := &cobra.Command{
		Use:     "changelog",
		Short:   "Generate a changelog entry from git commits",
		GroupID: "system",
		Long: `Generate a paste-ready Markdown changelog entry from commits in a git range.

By default, td uses the latest semver tag reachable from the end revision as the
start of the range and HEAD as the end of the range.`,
		Example: `  td changelog --version v0.44.0
  td changelog --version v0.44.0 --date 2026-04-06
  td changelog --version v0.44.0 --from v0.43.0 --to HEAD
  td changelog --version v0.44.0 --include-meta`,
		RunE: func(cmd *cobra.Command, args []string) error {
			version = strings.TrimSpace(version)
			if version == "" {
				return errors.New("--version is required")
			}
			if !releaseVersionPattern.MatchString(version) {
				return fmt.Errorf("invalid --version %q: expected semver like v1.2.3", version)
			}

			dateValue, err := time.Parse("2006-01-02", releaseDate)
			if err != nil {
				return fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", releaseDate)
			}

			repoStartDir, err := changelogRepoStartDir()
			if err != nil {
				return err
			}

			repoDir, err := gitpkg.GetRootDirInDir(repoStartDir)
			if err != nil {
				if errors.Is(err, gitpkg.ErrNotRepository) {
					return errors.New("not a git repository")
				}
				return err
			}

			from = strings.TrimSpace(from)
			to = strings.TrimSpace(to)

			targetRev := to
			if targetRev == "" {
				targetRev = "HEAD"
			}

			if from == "" {
				from, err = gitpkg.GetLatestSemverTagReachableFromInDir(repoDir, targetRev)
				if err != nil {
					switch {
					case errors.Is(err, gitpkg.ErrNoSemverTags):
						return errors.New("no semver tags found; use --from to specify a starting revision")
					case errors.Is(err, gitpkg.ErrNotRepository):
						return errors.New("not a git repository")
					default:
						return err
					}
				}
			}
			if to == "" {
				to = "HEAD"
			}

			commits, err := gitpkg.ListCommitsInRangeInDir(repoDir, from, to)
			if err != nil {
				if errors.Is(err, gitpkg.ErrNotRepository) {
					return errors.New("not a git repository")
				}
				return err
			}
			if len(commits) == 0 {
				return fmt.Errorf("no commits found in range %s..%s", from, to)
			}

			rendered, err := changelogpkg.Render(commits, changelogpkg.Options{
				Version:     version,
				Date:        dateValue,
				IncludeMeta: includeMeta,
			})
			if err != nil {
				if errors.Is(err, changelogpkg.ErrNoEntries) {
					return fmt.Errorf("no changelog-worthy commits found in range %s..%s (try --include-meta)", from, to)
				}
				return err
			}

			fmt.Print(rendered)
			return nil
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "release version for the generated heading (required)")
	cmd.Flags().StringVar(&from, "from", "", "starting revision (default: latest semver tag reachable from --to)")
	cmd.Flags().StringVar(&to, "to", "HEAD", "ending revision (default: HEAD)")
	cmd.Flags().StringVar(&releaseDate, "date", time.Now().Format("2006-01-02"), "release date in YYYY-MM-DD format")
	cmd.Flags().BoolVar(&includeMeta, "include-meta", false, "include docs/test/ci/chore commits in the output")

	return cmd
}

var changelogCmd = newChangelogCmd()

func init() {
	rootCmd.AddCommand(changelogCmd)
}

func changelogRepoStartDir() (string, error) {
	if baseDirOverride != nil {
		return *baseDirOverride, nil
	}
	if workDirFlag != "" {
		return normalizeWorkDir(workDirFlag), nil
	}
	if envDir := os.Getenv("TD_WORK_DIR"); envDir != "" {
		return normalizeWorkDir(envDir), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine working directory: %w", err)
	}
	return cwd, nil
}
