package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	tdgit "github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/releasenotes"
	"github.com/spf13/cobra"
)

var releaseNotesCmd = &cobra.Command{
	Use:     "release-notes",
	Short:   "Draft markdown release notes from git commits",
	GroupID: "system",
	Args:    cobra.NoArgs,
	Example: `  td release-notes
  td release-notes --from v0.4.0 --to HEAD
  td release-notes --version v0.5.0 --date 2026-04-29`,
	RunE: func(cmd *cobra.Command, args []string) error {
		from, _ := cmd.Flags().GetString("from")
		to, _ := cmd.Flags().GetString("to")
		version, _ := cmd.Flags().GetString("version")
		date, _ := cmd.Flags().GetString("date")

		if strings.TrimSpace(to) == "" {
			return fmt.Errorf("--to cannot be empty")
		}
		if date != "" {
			if version == "" {
				return fmt.Errorf("--date requires --version")
			}
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return fmt.Errorf("--date must use YYYY-MM-DD format")
			}
		}

		repoDir := getBaseDir()
		if repoDir == "" {
			var err error
			repoDir, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		if _, err := tdgit.ResolveRef(repoDir, to); err != nil {
			return err
		}

		if strings.TrimSpace(from) == "" {
			tag, err := tdgit.NearestSemverTag(repoDir, to)
			if err != nil {
				return fmt.Errorf("%w; pass --from <ref> to choose a release-note range explicitly", err)
			}
			from = tag
		} else if _, err := tdgit.ResolveRef(repoDir, from); err != nil {
			return err
		}

		commits, err := tdgit.ListCommits(repoDir, from, to)
		if err != nil {
			return fmt.Errorf("failed to list commits for %s..%s: %w", from, to, err)
		}
		if len(commits) == 0 {
			return fmt.Errorf("no commits found in %s..%s", from, to)
		}

		if err := releasenotes.Render(cmd.OutOrStdout(), commits, releasenotes.Options{
			Version: version,
			Date:    date,
		}); err != nil {
			return fmt.Errorf("%w in %s..%s", err, from, to)
		}

		return nil
	},
}

func init() {
	releaseNotesCmd.Flags().String("from", "", "start ref for the release-note range (defaults to nearest reachable semver tag)")
	releaseNotesCmd.Flags().String("to", "HEAD", "end ref for the release-note range")
	releaseNotesCmd.Flags().String("version", "", "version heading to render, such as v0.5.0")
	releaseNotesCmd.Flags().String("date", "", "release date heading in YYYY-MM-DD format (requires --version)")
	rootCmd.AddCommand(releaseNotesCmd)
}
