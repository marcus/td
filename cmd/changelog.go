package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	changelogpkg "github.com/marcus/td/internal/changelog"
	gitutil "github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:     "changelog",
	Short:   "Generate changelog markdown from git commits",
	GroupID: "system",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := changelogCommandOptionsFromFlags(cmd)
		if err != nil {
			return err
		}

		baseDir := getBaseDir()
		from, to, err := resolveChangelogRange(baseDir, opts.from, opts.to)
		if err != nil {
			return err
		}

		commits, err := gitutil.ListCommitsInRangeInDir(baseDir, from, to)
		if err != nil {
			return fmt.Errorf("list commits for %s..%s: %w", from, to, err)
		}

		markdown, err := changelogpkg.RenderMarkdown(commits, changelogpkg.Options{
			Version: opts.version,
			Date:    opts.date,
		})
		if err != nil {
			if errors.Is(err, changelogpkg.ErrNoRelevantCommits) {
				return fmt.Errorf("no commits found in range %s..%s", from, to)
			}
			return err
		}

		fmt.Print(markdown)
		return nil
	},
}

type changelogCommandOptions struct {
	from    string
	to      string
	version string
	date    string
}

func changelogCommandOptionsFromFlags(cmd *cobra.Command) (changelogCommandOptions, error) {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	version, _ := cmd.Flags().GetString("version")
	date, _ := cmd.Flags().GetString("date")

	opts := changelogCommandOptions{
		from:    strings.TrimSpace(from),
		to:      strings.TrimSpace(to),
		version: strings.TrimSpace(version),
		date:    strings.TrimSpace(date),
	}

	if opts.date != "" && opts.version == "" {
		return changelogCommandOptions{}, fmt.Errorf("--date requires --version")
	}
	if opts.date != "" {
		if _, err := time.Parse("2006-01-02", opts.date); err != nil {
			return changelogCommandOptions{}, fmt.Errorf("invalid --date %q: expected YYYY-MM-DD", opts.date)
		}
	}

	return opts, nil
}

func resolveChangelogRange(dir, from, to string) (string, string, error) {
	if to == "" {
		to = "HEAD"
	}

	if from == "" {
		tag, err := gitutil.GetLatestReleaseTagInDir(dir)
		if err != nil {
			return "", "", fmt.Errorf("could not determine default changelog range: %w (use --from <ref>)", err)
		}
		from = tag
	}

	if _, err := gitutil.ResolveRefInDir(dir, from); err != nil {
		return "", "", fmt.Errorf("invalid --from ref %q: %w", from, err)
	}
	if _, err := gitutil.ResolveRefInDir(dir, to); err != nil {
		return "", "", fmt.Errorf("invalid --to ref %q: %w", to, err)
	}

	return from, to, nil
}

func init() {
	changelogCmd.Flags().String("from", "", "start ref for the changelog range (defaults to latest semver tag)")
	changelogCmd.Flags().String("to", "HEAD", "end ref for the changelog range")
	changelogCmd.Flags().String("version", "", "release version to use in the markdown heading")
	changelogCmd.Flags().String("date", "", "release date for the markdown heading (YYYY-MM-DD)")

	rootCmd.AddCommand(changelogCmd)
}
