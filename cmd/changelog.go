package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcus/td/internal/changelog"
	"github.com/marcus/td/internal/git"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:     "changelog",
	Short:   "Draft release notes from git commits",
	Long:    `Parses git commits between tags, groups them by conventional-commit type, and formats release notes matching CHANGELOG.md style.`,
	GroupID: "system",
	RunE:    runChangelog,
}

func init() {
	changelogCmd.Flags().String("from", "", "start ref (exclusive); defaults to latest tag")
	changelogCmd.Flags().String("to", "", "end ref (inclusive); defaults to HEAD")
	changelogCmd.Flags().String("version", "", "version for the heading (e.g. v0.44.0)")
	changelogCmd.Flags().Bool("write", false, "prepend output to CHANGELOG.md")
	rootCmd.AddCommand(changelogCmd)
}

func runChangelog(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	version, _ := cmd.Flags().GetString("version")
	write, _ := cmd.Flags().GetBool("write")

	// Default "from" to the latest tag
	if from == "" {
		tag, err := git.LatestTag()
		if err != nil {
			// No tags — include all commits
			from = ""
		} else {
			from = tag
			// If no version specified, try to derive from the tag
			if version == "" {
				version = bumpPatch(tag)
			}
		}
	}

	if version == "" {
		version = "vX.Y.Z"
	}

	commits, err := git.ListCommitsBetween(from, to)
	if err != nil {
		return fmt.Errorf("listing commits: %w", err)
	}

	if len(commits) == 0 {
		fmt.Fprintln(os.Stderr, "No commits found in range.")
		return nil
	}

	// Parse and group
	var parsed []changelog.ParsedCommit
	for _, c := range commits {
		pc := changelog.ParseCommit(changelog.Commit{
			Hash:    c.Hash,
			Subject: c.Subject,
			Body:    c.Body,
			PR:      changelog.ExtractPR(c.Subject),
		})
		parsed = append(parsed, pc)
	}

	categories := changelog.GroupCommits(parsed)
	output := changelog.FormatMarkdown(version, time.Now(), categories)

	if write {
		if err := prependToChangelog(output); err != nil {
			return fmt.Errorf("writing CHANGELOG.md: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Prepended %s release notes to CHANGELOG.md\n", version)
		return nil
	}

	fmt.Print(output)
	return nil
}

// prependToChangelog inserts the release notes after the file header.
func prependToChangelog(notes string) error {
	path := "CHANGELOG.md"
	existing, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist — create with header
		content := "# Changelog\n\nAll notable changes to td are documented in this file.\n\n" + notes
		return os.WriteFile(path, []byte(content), 0644)
	}

	content := string(existing)

	// Find insertion point: after the header block (first blank line after "# Changelog")
	insertIdx := 0
	headerEnd := strings.Index(content, "\n\n## [")
	if headerEnd != -1 {
		insertIdx = headerEnd + 1 // after the first newline of the blank line
	} else {
		// Fallback: after first blank line
		blankLine := strings.Index(content, "\n\n")
		if blankLine != -1 {
			insertIdx = blankLine + 1
		}
	}

	var b strings.Builder
	b.WriteString(content[:insertIdx])
	b.WriteString("\n")
	b.WriteString(notes)
	b.WriteString(content[insertIdx:])

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// bumpPatch increments the patch version of a semver tag (e.g. v0.43.0 → v0.44.0).
// Falls back to appending "-next" on parse failure.
func bumpPatch(tag string) string {
	t := strings.TrimPrefix(tag, "v")
	parts := strings.SplitN(t, ".", 3)
	if len(parts) == 3 {
		// Bump minor (project convention: minor bumps per release)
		minor := 0
		fmt.Sscanf(parts[1], "%d", &minor)
		return fmt.Sprintf("v%s.%d.0", parts[0], minor+1)
	}
	return tag + "-next"
}
