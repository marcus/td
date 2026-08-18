package git

import (
	"fmt"
	"strings"
)

// typeOrder defines display order for changelog sections.
var typeOrder = []string{"feat", "fix", "perf", "refactor", "docs", "test", "chore", "ci", "build", "other"}

// FormatChangelog renders grouped commits into markdown matching the existing
// CHANGELOG.md style:
//
//	## [vX.Y.Z] - YYYY-MM-DD
//	### Features
//	- description (hash)
func FormatChangelog(version, date string, grouped map[string][]CommitInfo) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## [%s] - %s\n", version, date)

	for _, typ := range typeOrder {
		commits, ok := grouped[typ]
		if !ok || len(commits) == 0 {
			continue
		}

		title := typeTitles[typ]
		fmt.Fprintf(&b, "\n### %s\n", title)

		for _, c := range commits {
			short := c.Hash
			if len(short) > 7 {
				short = short[:7]
			}

			prefix := ""
			if c.IsBreaking {
				prefix = "**BREAKING** "
			}

			scope := ""
			if c.Scope != "" {
				scope = fmt.Sprintf("**%s** — ", c.Scope)
			}

			fmt.Fprintf(&b, "- %s%s%s (%s)\n", prefix, scope, c.Subject, short)
		}
	}

	return b.String()
}
