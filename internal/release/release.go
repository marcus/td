package release

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marcus/td/internal/git"
)

const (
	SectionFeatures      = "Features"
	SectionBugFixes      = "Bug Fixes"
	SectionDocumentation = "Documentation"
	SectionInternal      = "Internal"
	SectionUncategorized = "Uncategorized"
)

type Options struct {
	Title            string
	RevisionRange    string
	From             string
	To               string
	IncludeFiles     bool
	IncludeDiffStats bool
	IncludeEmpty     bool
}

type Draft struct {
	Title         string
	RevisionRange string
	From          string
	To            string
	Sections      []Section
	DiffStats     *git.DiffStats
}

type Section struct {
	Title   string
	Entries []Entry
}

type Entry struct {
	Commit  git.Commit
	Summary string
	Files   []string
}

func Build(commits []git.Commit, stats *git.DiffStats, opts Options) Draft {
	ordered := []string{
		SectionFeatures,
		SectionBugFixes,
		SectionDocumentation,
		SectionInternal,
		SectionUncategorized,
	}

	grouped := map[string][]Entry{}
	for _, commit := range commits {
		section := classifyCommit(commit)
		grouped[section] = append(grouped[section], Entry{
			Commit:  commit,
			Summary: humanizeSummary(commit.Subject),
			Files:   cleanFiles(commit.Files),
		})
	}

	sections := make([]Section, 0, len(ordered))
	for _, title := range ordered {
		entries := grouped[title]
		if len(entries) == 0 && !opts.IncludeEmpty {
			continue
		}
		sections = append(sections, Section{Title: title, Entries: entries})
	}

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Release Notes Draft"
	}

	return Draft{
		Title:         title,
		RevisionRange: opts.RevisionRange,
		From:          opts.From,
		To:            opts.To,
		Sections:      sections,
		DiffStats:     stats,
	}
}

func classifyCommit(commit git.Commit) string {
	subject := strings.ToLower(strings.TrimSpace(commit.Subject))
	prefix := conventionalPrefix(subject)

	switch prefix {
	case "feat", "feature":
		return SectionFeatures
	case "fix", "bugfix", "hotfix":
		return SectionBugFixes
	case "docs", "doc":
		return SectionDocumentation
	case "chore", "refactor", "test", "tests", "ci", "build", "perf", "style", "release":
		return SectionInternal
	}

	if hasAnyPrefix(subject, "fix ", "fix:", "bug ", "bug:", "resolve ", "resolved ") {
		return SectionBugFixes
	}
	if hasAnyPrefix(subject, "add ", "adds ", "introduce ", "implement ", "support ", "create ") {
		return SectionFeatures
	}
	if docsOnly(commit.Files) || strings.Contains(subject, "readme") || strings.Contains(subject, "changelog") {
		return SectionDocumentation
	}
	if internalOnly(commit.Files) {
		return SectionInternal
	}

	return SectionUncategorized
}

func conventionalPrefix(subject string) string {
	if subject == "" {
		return ""
	}
	end := strings.Index(subject, ":")
	if end == -1 {
		return ""
	}
	prefix := subject[:end]
	if open := strings.Index(prefix, "("); open != -1 {
		prefix = prefix[:open]
	}
	return strings.TrimSpace(prefix)
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func docsOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isDocPath(file) {
			return false
		}
	}
	return true
}

func internalOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if isDocPath(file) {
			continue
		}
		base := filepath.Base(file)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		if strings.HasPrefix(file, ".github/") || strings.HasPrefix(file, "scripts/") || strings.HasPrefix(file, "deploy/") {
			continue
		}
		return false
	}
	return true
}

func isDocPath(path string) bool {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	return strings.HasPrefix(path, "docs/") ||
		strings.HasPrefix(path, "website/docs/") ||
		strings.EqualFold(base, "README.md") ||
		strings.EqualFold(base, "CHANGELOG.md") ||
		ext == ".md"
}

func humanizeSummary(subject string) string {
	summary := strings.TrimSpace(subject)
	if summary == "" {
		return "Untitled change"
	}

	if prefix := conventionalPrefix(strings.ToLower(summary)); prefix != "" {
		if idx := strings.Index(summary, ":"); idx != -1 && idx+1 < len(summary) {
			summary = strings.TrimSpace(summary[idx+1:])
		}
	}

	summary = strings.TrimSpace(strings.TrimSuffix(summary, "."))
	if summary == "" {
		return "Untitled change"
	}

	first := strings.ToUpper(summary[:1])
	if len(summary) == 1 {
		return first
	}
	return first + summary[1:]
}

func cleanFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		cleaned = append(cleaned, file)
	}
	return cleaned
}

func (d Draft) SummaryLine() string {
	if d.DiffStats == nil {
		return ""
	}
	return fmt.Sprintf("%d files changed, %d insertions(+), %d deletions(-)", d.DiffStats.FilesChanged, d.DiffStats.Additions, d.DiffStats.Deletions)
}
