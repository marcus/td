package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	gitutil "github.com/marcus/td/internal/git"
	"github.com/marcus/td/internal/releasenotes"
	"github.com/spf13/cobra"
)

var releaseNotesNow = time.Now

var releaseNotesCmd = &cobra.Command{
	Use:     "release-notes",
	Short:   "Draft release notes from committed git history",
	GroupID: "system",
	Args:    cobra.NoArgs,
	Long: `Draft release notes from committed git history.

By default, td uses the latest reachable semver tag through HEAD as the
baseline and prints a markdown block you can review before updating
CHANGELOG.md manually.`,
	Example: `  td release-notes
  td release-notes --version v0.44.0
  td release-notes --from v0.43.0 --to HEAD --version v0.44.0`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fromRef, _ := cmd.Flags().GetString("from")
		toRef, _ := cmd.Flags().GetString("to")
		versionLabel, _ := cmd.Flags().GetString("version")

		draft, err := releasenotes.Generate(releaseNotesRepoDir(), releasenotes.Options{
			FromRef: fromRef,
			ToRef:   toRef,
			Version: versionLabel,
			Date:    releaseNotesNow(),
		})
		if err != nil {
			switch {
			case errors.Is(err, gitutil.ErrNotRepository):
				return fmt.Errorf("release notes require a git repository")
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

// releaseNotesRepoDir uses the active worktree instead of td's resolved
// database root so repo-scoped git history is drafted from the branch the user
// is actually on.
func releaseNotesRepoDir() string {
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
	rootCmd.AddCommand(releaseNotesCmd)
	releaseNotesCmd.Flags().String("from", "", "Start the range at this git ref or tag")
	releaseNotesCmd.Flags().String("to", "HEAD", "End the range at this git ref")
	releaseNotesCmd.Flags().String("version", "", "Version label for the markdown header")
}
