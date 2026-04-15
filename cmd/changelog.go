package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	changelogpkg "github.com/marcus/td/internal/changelog"
	gitutil "github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var (
	changelogNow          = time.Now
	releaseVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

var changelogCmd = &cobra.Command{
	Use:     "changelog",
	Short:   "Draft a CHANGELOG.md entry from committed git history",
	GroupID: "system",
	Args:    cobra.NoArgs,
	Long: `Draft a paste-ready CHANGELOG.md entry from committed git history.

By default, td uses the latest reachable semver tag through HEAD as the
baseline, filters out documentation/test/CI/chore commits, and prints a
markdown block you can review before updating CHANGELOG.md manually.`,
	Example: `  td changelog --version v0.44.0
  td changelog --version v0.44.0 --date 2026-04-15
  td changelog --version v0.44.0 --from v0.43.0 --to HEAD
  td changelog --version v0.44.0 --to v0.44.0 --include-meta`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fromRef, _ := cmd.Flags().GetString("from")
		toRef, _ := cmd.Flags().GetString("to")
		versionLabel, _ := cmd.Flags().GetString("version")
		dateValue, _ := cmd.Flags().GetString("date")
		includeMeta, _ := cmd.Flags().GetBool("include-meta")

		versionLabel = strings.TrimSpace(versionLabel)
		if versionLabel == "" {
			return errors.New("--version is required")
		}
		if !releaseVersionPattern.MatchString(versionLabel) {
			return fmt.Errorf("invalid --version %q: expected semver like v1.2.3", versionLabel)
		}

		releaseDate := changelogNow()
		if strings.TrimSpace(dateValue) != "" {
			parsedDate, err := time.Parse("2006-01-02", dateValue)
			if err != nil {
				return fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", dateValue)
			}
			releaseDate = parsedDate
		}

		draft, err := changelogpkg.Generate(gitHistoryRepoDir(), changelogpkg.Options{
			FromRef:     fromRef,
			ToRef:       toRef,
			Version:     versionLabel,
			Date:        releaseDate,
			IncludeMeta: includeMeta,
		})
		if err != nil {
			switch {
			case errors.Is(err, gitutil.ErrNotRepository):
				return fmt.Errorf("changelog requires a git repository")
			case errors.Is(err, gitutil.ErrNoSemverTag):
				return fmt.Errorf("no reachable semver tag found; use --from to set the starting ref")
			default:
				return err
			}
		}

		_, err = fmt.Fprint(cmd.OutOrStdout(), draft.Markdown())
		return err
	},
}

func init() {
	rootCmd.AddCommand(changelogCmd)
	changelogCmd.Flags().String("from", "", "Start the range at this git ref or tag")
	changelogCmd.Flags().String("to", "HEAD", "End the range at this git ref")
	changelogCmd.Flags().String("version", "", "Version label for the markdown header (required)")
	changelogCmd.Flags().String("date", "", "Release date for the markdown header (YYYY-MM-DD, default: today)")
	changelogCmd.Flags().Bool("include-meta", false, "Include documentation, test, CI, and chore commits")
}
