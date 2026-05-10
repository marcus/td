package release

import (
	"strings"
)

func RenderMarkdown(draft Draft, includeFiles bool, includeStats bool) string {
	var b strings.Builder

	b.WriteString("# ")
	b.WriteString(draft.Title)
	b.WriteString("\n\n")

	if draft.RevisionRange != "" {
		b.WriteString("_Range: `")
		b.WriteString(draft.RevisionRange)
		b.WriteString("`_")
		b.WriteString("\n\n")
	}

	if includeStats {
		if summary := draft.SummaryLine(); summary != "" {
			b.WriteString(summary)
			b.WriteString("\n\n")
		}
	}

	for _, section := range draft.Sections {
		b.WriteString("## ")
		b.WriteString(section.Title)
		b.WriteString("\n")
		if len(section.Entries) == 0 {
			b.WriteString("- None\n\n")
			continue
		}

		for _, entry := range section.Entries {
			b.WriteString("- ")
			b.WriteString(entry.Summary)
			if entry.Commit.ShortSHA != "" {
				b.WriteString(" (`")
				b.WriteString(entry.Commit.ShortSHA)
				b.WriteString("`)")
			}
			b.WriteString("\n")

			if includeFiles && len(entry.Files) > 0 {
				b.WriteString("  Files: ")
				for i, file := range entry.Files {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString("`")
					b.WriteString(file)
					b.WriteString("`")
				}
				b.WriteString("\n")
			}
		}

		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String()) + "\n"
}
