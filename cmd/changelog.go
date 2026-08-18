package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:     "changelog",
	Short:   "Generate changelog from git commits",
	Long:    "Read git commits between two refs, parse conventional commit prefixes, and render a grouped markdown changelog.",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		fromRef, _ := cmd.Flags().GetString("from")
		toRef, _ := cmd.Flags().GetString("to")
		version, _ := cmd.Flags().GetString("version")
		prepend, _ := cmd.Flags().GetBool("prepend")

		if version == "" {
			return fmt.Errorf("--version is required (e.g. v0.44.0)")
		}

		// Default --from to latest tag
		if fromRef == "" {
			tag, err := git.GetLatestTag()
			if err != nil {
				return fmt.Errorf("finding latest tag: %w", err)
			}
			fromRef = tag // may be "" if no tags exist
		}

		commits, err := git.GetCommitLog(fromRef, toRef)
		if err != nil {
			return fmt.Errorf("reading commits: %w", err)
		}

		if len(commits) == 0 {
			fmt.Fprintln(os.Stderr, "No commits found in range.")
			return nil
		}

		grouped := git.GroupCommitsByType(commits)
		date := time.Now().Format("2006-01-02")
		changelog := git.FormatChangelog(version, date, grouped)

		if prepend {
			return prependToChangelog(changelog)
		}

		fmt.Print(changelog)
		return nil
	},
}

func prependToChangelog(entry string) error {
	const path = "CHANGELOG.md"

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var b strings.Builder
	if len(existing) == 0 {
		// New file
		b.WriteString("# Changelog\n\nAll notable changes to td are documented in this file.\n\n")
		b.WriteString(entry)
	} else {
		content := string(existing)
		// Insert after the header block (first blank line after opening lines)
		idx := strings.Index(content, "\n\n## ")
		if idx == -1 {
			// No existing version sections — append after header
			b.WriteString(strings.TrimRight(content, "\n"))
			b.WriteString("\n\n")
			b.WriteString(entry)
		} else {
			b.WriteString(content[:idx+2]) // include the \n\n
			b.WriteString(entry)
			b.WriteString("\n")
			b.WriteString(content[idx+2:])
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	fmt.Fprintf(os.Stderr, "Prepended changelog entry to %s\n", path)
	return nil
}

func init() {
	changelogCmd.Flags().String("from", "", "Start ref (default: latest tag)")
	changelogCmd.Flags().String("to", "HEAD", "End ref (default: HEAD)")
	changelogCmd.Flags().String("version", "", "Version string for the header (required, e.g. v0.44.0)")
	changelogCmd.Flags().Bool("prepend", false, "Prepend output to CHANGELOG.md instead of printing to stdout")
	rootCmd.AddCommand(changelogCmd)
}
