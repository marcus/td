package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/marcus/td/internal/releasenotes"
	"github.com/spf13/cobra"
)

func newReleaseNotesCmd() *cobra.Command {
	var opts releasenotes.Options
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "release-notes",
		Short:   "Draft release notes from git commits",
		GroupID: "system",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoDir, err := releaseNotesStartDir()
			if err != nil {
				return err
			}
			opts.RepoDir = repoDir

			draft, err := releasenotes.DraftFromGit(context.Background(), opts)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if jsonOutput {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(draft)
			}

			_, err = fmt.Fprint(out, releasenotes.RenderMarkdown(draft))
			return err
		},
	}

	cmd.Flags().StringVar(&opts.From, "from", "", "Base git ref (default: latest v* tag before --to)")
	cmd.Flags().StringVar(&opts.To, "to", "HEAD", "Target git ref")
	cmd.Flags().StringVar(&opts.Version, "version", "", "Release version heading, for example v0.5.0")
	cmd.Flags().StringVar(&opts.Date, "date", "", "Release date in YYYY-MM-DD format")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit structured JSON")
	cmd.Flags().BoolVar(&opts.IncludeInternal, "include-internal", false, "Include internal maintenance commits")

	return cmd
}

func releaseNotesStartDir() (string, error) {
	if workDirFlag != "" {
		return normalizeWorkDir(workDirFlag), nil
	}
	if envDir := os.Getenv("TD_WORK_DIR"); envDir != "" {
		return normalizeWorkDir(envDir), nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory: %w", err)
	}
	return dir, nil
}

var releaseNotesCmd = newReleaseNotesCmd()

func init() {
	rootCmd.AddCommand(releaseNotesCmd)
}
