// Package releasenotes turns local git commits into deterministic markdown.
package releasenotes

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/marcus/td/internal/git"
)

const (
	SectionBreaking      = "Breaking Changes"
	SectionFeatures      = "Features"
	SectionBugFixes      = "Bug Fixes"
	SectionDocumentation = "Documentation"
	SectionImprovements  = "Improvements"
)

var sectionOrder = []string{
	SectionBreaking,
	SectionFeatures,
	SectionBugFixes,
	SectionDocumentation,
	SectionImprovements,
}

// Options controls the release-note heading.
type Options struct {
	Version string
	Date    string
}

// Entry is one release-note bullet.
type Entry struct {
	Section  string
	Text     string
	ShortSHA string
}

// Section is a rendered release-note section.
type Section struct {
	Title   string
	Entries []Entry
}

// Draft builds stable sections from commits.
func Draft(commits []git.Commit) []Section {
	grouped := make(map[string][]Entry)
	for _, commit := range commits {
		entry, ok := entryFromCommit(commit)
		if !ok {
			continue
		}
		grouped[entry.Section] = append(grouped[entry.Section], entry)
	}

	sections := make([]Section, 0, len(sectionOrder))
	for _, title := range sectionOrder {
		entries := grouped[title]
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, Section{Title: title, Entries: entries})
	}
	return sections
}

// Render writes release notes as markdown.
func Render(w io.Writer, commits []git.Commit, opts Options) error {
	sections := Draft(commits)
	if len(sections) == 0 {
		return fmt.Errorf("no release-note-worthy commits")
	}

	heading := "Release Notes"
	if opts.Version != "" {
		heading = opts.Version
		if opts.Date != "" {
			heading += " - " + opts.Date
		}
	}
	if _, err := fmt.Fprintf(w, "## %s\n\n", heading); err != nil {
		return err
	}

	for i, section := range sections {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "### %s\n", section.Title); err != nil {
			return err
		}
		for _, entry := range section.Entries {
			if entry.ShortSHA != "" {
				if _, err := fmt.Fprintf(w, "- %s (%s)\n", entry.Text, entry.ShortSHA); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(w, "- %s\n", entry.Text); err != nil {
				return err
			}
		}
	}
	return nil
}

func entryFromCommit(commit git.Commit) (Entry, bool) {
	subject := strings.TrimSpace(commit.Subject)
	if subject == "" || isNoisySubject(subject) {
		return Entry{}, false
	}

	parsed := parseSubject(subject)
	if parsed.description == "" {
		return Entry{}, false
	}

	section := sectionForType(parsed.kind)
	if parsed.breaking || hasBreakingFooter(commit.Body) {
		section = SectionBreaking
	}

	return Entry{
		Section:  section,
		Text:     normalizeDescription(parsed.description),
		ShortSHA: commit.ShortSHA,
	}, true
}

func isNoisySubject(subject string) bool {
	lower := strings.ToLower(strings.TrimSpace(subject))
	return strings.HasPrefix(lower, "merge ") ||
		strings.HasPrefix(lower, "fixup!") ||
		strings.HasPrefix(lower, "squash!")
}

type parsedSubject struct {
	kind        string
	description string
	breaking    bool
}

var conventionalPattern = regexp.MustCompile(`^([A-Za-z]+)(?:\([^)]+\))?(!)?:\s+(.+)$`)

func parseSubject(subject string) parsedSubject {
	match := conventionalPattern.FindStringSubmatch(subject)
	if len(match) == 4 {
		return parsedSubject{
			kind:        strings.ToLower(match[1]),
			description: match[3],
			breaking:    match[2] == "!",
		}
	}

	return parsedSubject{
		kind:        inferLooseType(subject),
		description: subject,
	}
}

func inferLooseType(subject string) string {
	lower := strings.ToLower(subject)
	switch {
	case strings.HasPrefix(lower, "fix ") || strings.HasPrefix(lower, "repair ") || strings.HasPrefix(lower, "resolve "):
		return "fix"
	case strings.HasPrefix(lower, "add ") || strings.HasPrefix(lower, "introduce ") || strings.HasPrefix(lower, "create "):
		return "feat"
	case strings.HasPrefix(lower, "document ") || strings.HasPrefix(lower, "docs ") || strings.HasPrefix(lower, "update docs"):
		return "docs"
	default:
		return "chore"
	}
}

func sectionForType(kind string) string {
	switch kind {
	case "feat", "feature":
		return SectionFeatures
	case "fix", "bugfix":
		return SectionBugFixes
	case "docs", "doc":
		return SectionDocumentation
	default:
		return SectionImprovements
	}
}

func normalizeDescription(description string) string {
	description = strings.TrimSpace(description)
	description = strings.TrimSuffix(description, ".")
	if description == "" {
		return description
	}
	return strings.ToUpper(description[:1]) + description[1:]
}

func hasBreakingFooter(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upper, "BREAKING CHANGE:") || strings.HasPrefix(upper, "BREAKING-CHANGE:") {
			return true
		}
	}
	return false
}
