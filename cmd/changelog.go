package cmd

import (
	"fmt"
	"time"

	"github.com/marcus/td/internal/changelog"
	"github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

func newChangelogCmd() *cobra.Command {
	var fromRef string
	var toRef string
	var version string
	var date string

	cmd := &cobra.Command{
		Use:     "changelog",
		Short:   "Generate markdown changelog from git commits",
		GroupID: "system",
		RunE: func(cmd *cobra.Command, args []string) error {
			if date != "" {
				if version == "" {
					return fmt.Errorf("--date requires --version")
				}
				if _, err := time.Parse("2006-01-02", date); err != nil {
					return fmt.Errorf("--date must use YYYY-MM-DD format")
				}
			}

			if _, err := git.ResolveRef(toRef); err != nil {
				return err
			}
			if fromRef == "" {
				tag, err := git.NearestReachableSemverTag(toRef)
				if err != nil {
					return fmt.Errorf("cannot determine default --from: %w; pass --from explicitly", err)
				}
				fromRef = tag
			}
			if _, err := git.ResolveRef(fromRef); err != nil {
				return err
			}

			commits, err := git.ListCommits(fromRef, toRef)
			if err != nil {
				return err
			}
			markdown, err := changelog.Render(commits, changelog.Options{
				Version: version,
				Date:    date,
			})
			if err != nil {
				return fmt.Errorf("%w in range %s..%s", err, fromRef, toRef)
			}
			fmt.Fprint(cmd.OutOrStdout(), markdown)
			return nil
		},
	}

	cmd.Flags().StringVar(&fromRef, "from", "", "Start ref (defaults to nearest reachable semver tag)")
	cmd.Flags().StringVar(&toRef, "to", "HEAD", "End ref")
	cmd.Flags().StringVar(&version, "version", "", "Release version heading")
	cmd.Flags().StringVar(&date, "date", "", "Release date in YYYY-MM-DD format (requires --version)")

	return cmd
}

var changelogCmd = newChangelogCmd()

func init() {
	rootCmd.AddCommand(changelogCmd)
}
