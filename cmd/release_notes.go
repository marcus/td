package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/output"
	"github.com/marcus/td/internal/release"
	"github.com/spf13/cobra"
)

var releaseNotesCmd = &cobra.Command{
	Use:     "release-notes",
	Short:   "Draft release notes from git history",
	Long:    "Build a markdown-first release notes draft from commits since the latest tag, or an explicit git revision range.",
	GroupID: "system",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo := git.NewRepo(getBaseDir())
		if !repo.IsRepo() {
			err := fmt.Errorf("release notes require a git repository")
			output.Error("%v", err)
			return err
		}

		from, _ := cmd.Flags().GetString("from")
		to, _ := cmd.Flags().GetString("to")
		rangeArg, _ := cmd.Flags().GetString("range")
		outputMode, _ := cmd.Flags().GetString("output")
		includeFiles, _ := cmd.Flags().GetBool("include-files")
		includeStats, _ := cmd.Flags().GetBool("include-stats")
		title, _ := cmd.Flags().GetString("title")

		if !cmd.Flags().Changed("from") {
			from = ""
		}
		if !cmd.Flags().Changed("to") {
			to = ""
		}
		if !cmd.Flags().Changed("range") {
			rangeArg = ""
		}

		revisionRange, err := repo.ResolveRevisionRange(from, to, rangeArg)
		if err != nil {
			if errors.Is(err, git.ErrNoTagsFound) {
				err = fmt.Errorf("no tags found; use --from <rev> --to <rev> or --range <expr> to draft release notes without tags")
			}
			output.Error("%v", err)
			return err
		}

		commits, err := repo.ListCommits(revisionRange.Expr)
		if err != nil {
			output.Error("%v", err)
			return err
		}
		if len(commits) == 0 {
			err = fmt.Errorf("no commits found in range %s", revisionRange.Expr)
			output.Error("%v", err)
			return err
		}

		stats, err := repo.GetDiffStats(revisionRange.Expr)
		if err != nil {
			output.Error("%v", err)
			return err
		}

		draft := release.Build(commits, stats, release.Options{
			Title:            title,
			RevisionRange:    revisionRange.Expr,
			From:             revisionRange.From,
			To:               revisionRange.To,
			IncludeFiles:     includeFiles,
			IncludeDiffStats: includeStats,
		})
		markdown := release.RenderMarkdown(draft, includeFiles, includeStats)

		switch strings.ToLower(strings.TrimSpace(outputMode)) {
		case "", "terminal":
			rendered, renderErr := output.RenderMarkdown(markdown)
			if renderErr != nil {
				fmt.Print(markdown)
				return nil
			}
			fmt.Println(rendered)
			return nil
		case "markdown":
			fmt.Print(markdown)
			return nil
		default:
			err := fmt.Errorf("invalid output mode %q (expected terminal or markdown)", outputMode)
			output.Error("%v", err)
			return err
		}
	},
}

func init() {
	releaseNotesCmd.Flags().String("from", "", "starting revision (defaults to latest tag)")
	releaseNotesCmd.Flags().String("to", "HEAD", "ending revision")
	releaseNotesCmd.Flags().String("range", "", "explicit git revision range (for example v0.9.0..HEAD)")
	releaseNotesCmd.Flags().String("output", "terminal", "output mode: terminal or markdown")
	releaseNotesCmd.Flags().Bool("include-files", false, "include changed files under each entry")
	releaseNotesCmd.Flags().Bool("include-stats", false, "include diff summary stats near the top")
	releaseNotesCmd.Flags().String("title", "Release Notes Draft", "document title")

	rootCmd.AddCommand(releaseNotesCmd)
}
