// Package changelog parses conventional commits and formats release notes.
package changelog

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Commit represents a parsed git commit.
type Commit struct {
	Hash    string
	Subject string
	Body    string
	PR      string // PR number if present (e.g. "69")
}

// ParsedCommit is a commit with its conventional-commit parts extracted.
type ParsedCommit struct {
	Commit
	Type        string // feat, fix, docs, test, refactor, chore, etc.
	Scope       string // optional scope
	Description string // the description after type(scope):
	Breaking    bool
}

// Category maps conventional-commit types to human-readable headings.
type Category struct {
	Heading string
	Commits []ParsedCommit
}

var categoryOrder = []struct {
	Type    string
	Heading string
}{
	{"feat", "Features"},
	{"fix", "Bug Fixes"},
	{"docs", "Documentation"},
	{"refactor", "Improvements"},
	{"perf", "Performance"},
	{"test", "Testing"},
	{"chore", "Chores"},
}

// conventionalRe matches "type(scope): description" or "type: description" or "type!: description"
var conventionalRe = regexp.MustCompile(`^(\w+)(?:\(([^)]*)\))?(!)?:\s*(.+)`)

// prRe matches (#123) at the end of a subject line.
var prRe = regexp.MustCompile(`\(#(\d+)\)\s*$`)

// ParseCommit extracts conventional-commit fields from a commit.
func ParseCommit(c Commit) ParsedCommit {
	pc := ParsedCommit{Commit: c}

	m := conventionalRe.FindStringSubmatch(c.Subject)
	if m == nil {
		// Not a conventional commit — treat as uncategorized
		pc.Type = ""
		pc.Description = c.Subject
		return pc
	}

	pc.Type = strings.ToLower(m[1])
	pc.Scope = m[2]
	pc.Breaking = m[3] == "!"
	pc.Description = m[4]

	return pc
}

// ExtractPR pulls a PR number from the subject if present.
func ExtractPR(subject string) string {
	m := prRe.FindStringSubmatch(subject)
	if m != nil {
		return m[1]
	}
	return ""
}

// GroupCommits organizes parsed commits into ordered categories.
// Commits that don't match any known type go into "Other Changes".
func GroupCommits(commits []ParsedCommit) []Category {
	buckets := make(map[string][]ParsedCommit)

	for _, c := range commits {
		buckets[c.Type] = append(buckets[c.Type], c)
	}

	var categories []Category
	for _, co := range categoryOrder {
		if cs, ok := buckets[co.Type]; ok {
			categories = append(categories, Category{Heading: co.Heading, Commits: cs})
			delete(buckets, co.Type)
		}
	}

	// Collect remaining non-empty types into "Other Changes"
	var other []ParsedCommit
	for typ, cs := range buckets {
		if typ == "" {
			other = append(other, cs...)
		} else {
			other = append(other, cs...)
		}
	}
	if len(other) > 0 {
		categories = append(categories, Category{Heading: "Other Changes", Commits: other})
	}

	return categories
}

// FormatMarkdown renders grouped commits as a changelog section matching the
// project's CHANGELOG.md style.
func FormatMarkdown(version string, date time.Time, categories []Category) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## [%s] - %s\n", version, date.Format("2006-01-02")))

	for _, cat := range categories {
		b.WriteString(fmt.Sprintf("\n### %s\n", cat.Heading))
		for _, c := range cat.Commits {
			line := formatCommitLine(c)
			b.WriteString(fmt.Sprintf("- %s\n", line))
		}
	}

	return b.String()
}

// formatCommitLine formats a single commit as a changelog bullet.
func formatCommitLine(c ParsedCommit) string {
	desc := c.Description
	// Strip trailing PR reference from description if we'll add it ourselves
	desc = prRe.ReplaceAllString(desc, "")
	desc = strings.TrimSpace(desc)

	var parts []string
	if c.Scope != "" {
		parts = append(parts, fmt.Sprintf("**%s** — %s", c.Scope, desc))
	} else {
		parts = append(parts, desc)
	}

	pr := c.PR
	if pr == "" {
		pr = ExtractPR(c.Subject)
	}
	if pr != "" {
		return fmt.Sprintf("%s (#%s)", parts[0], pr)
	}
	return parts[0]
}
