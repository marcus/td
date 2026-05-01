// Package changelog renders markdown changelogs from git commits.
package changelog

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcus/td/internal/git"
)

// Options controls changelog markdown rendering.
type Options struct {
	Version string
	Date    string
}

type section struct {
	title string
	items []string
}

var conventionalPattern = regexp.MustCompile(`^([a-zA-Z]+)(?:\([^)]+\))?!?:\s*(.+)$`)

// Render turns commits into markdown sections.
func Render(commits []git.Commit, opts Options) (string, error) {
	sections := []section{
		{title: "Features"},
		{title: "Bug Fixes"},
		{title: "Documentation"},
		{title: "Improvements"},
	}
	sectionIndex := map[string]int{
		"feature":       0,
		"fix":           1,
		"documentation": 2,
		"improvement":   3,
	}

	for _, commit := range commits {
		category, text, ok := classify(commit.Subject)
		if !ok {
			continue
		}
		shortSHA := commit.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		item := fmt.Sprintf("- %s (%s)", normalizeText(text), shortSHA)
		sections[sectionIndex[category]].items = append(sections[sectionIndex[category]].items, item)
	}

	hasItems := false
	for _, section := range sections {
		if len(section.items) > 0 {
			hasItems = true
			break
		}
	}
	if !hasItems {
		return "", fmt.Errorf("no relevant commits found for changelog")
	}

	var b strings.Builder
	heading := "Unreleased"
	if opts.Version != "" {
		heading = opts.Version
		if opts.Date != "" {
			heading += " - " + opts.Date
		}
	}
	fmt.Fprintf(&b, "## %s\n", heading)

	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n", section.title)
		for _, item := range section.items {
			fmt.Fprintf(&b, "%s\n", item)
		}
	}

	return b.String(), nil
}

func classify(subject string) (category string, text string, ok bool) {
	subject = strings.TrimSpace(subject)
	lower := strings.ToLower(subject)
	if subject == "" || strings.HasPrefix(lower, "merge ") ||
		strings.HasPrefix(lower, "fixup!") || strings.HasPrefix(lower, "squash!") {
		return "", "", false
	}

	if matches := conventionalPattern.FindStringSubmatch(subject); len(matches) == 3 {
		switch strings.ToLower(matches[1]) {
		case "feat", "feature":
			return "feature", matches[2], true
		case "fix", "bugfix":
			return "fix", matches[2], true
		case "doc", "docs":
			return "documentation", matches[2], true
		default:
			return "improvement", matches[2], true
		}
	}

	words := strings.Fields(lower)
	if len(words) == 0 {
		return "", "", false
	}
	first := strings.Trim(words[0], ":,.")
	switch first {
	case "add", "adds", "added", "create", "creates", "created", "implement", "implements", "implemented", "introduce", "introduces", "introduced", "new", "support", "supports":
		return "feature", subject, true
	case "fix", "fixes", "fixed", "repair", "repairs", "repaired", "resolve", "resolves", "resolved", "address", "addresses", "addressed", "handle", "handles", "handled", "prevent", "prevents", "prevented", "correct", "corrects", "corrected":
		return "fix", subject, true
	case "doc", "docs", "document", "documents", "documented", "readme":
		return "documentation", subject, true
	default:
		if strings.Contains(lower, "documentation") || strings.Contains(lower, "readme") {
			return "documentation", subject, true
		}
		return "improvement", subject, true
	}
}

func normalizeText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
	if text == "" {
		return text
	}

	r, size := utf8.DecodeRuneInString(text)
	if r == utf8.RuneError {
		return text
	}
	return string(unicode.ToUpper(r)) + text[size:]
}
