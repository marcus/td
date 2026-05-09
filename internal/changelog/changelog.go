// Package changelog renders paste-ready changelog entries from git commits.
package changelog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gitutil "github.com/marcus/td/internal/git"
)

const (
	sectionFeatures      = "Features"
	sectionBugFixes      = "Bug Fixes"
	sectionDocumentation = "Documentation"
	sectionImprovements  = "Improvements"
)

var (
	// ErrNoRelevantCommits indicates the selected range contains no entries
	// after changelog filters are applied.
	ErrNoRelevantCommits = errors.New("no changelog-worthy commits found")

	conventionalCommitPattern = regexp.MustCompile(`(?i)^(feat(?:ure)?|fix|bugfix|bug|docs?|doc|refactor|perf|style|build|ci|chore|test(?:s)?)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	tdRefPrefixPattern        = regexp.MustCompile(`^(?:(?:\[(?:td|task)-[^\]]+\])\s*)+`)
	tdRefSuffixPattern        = regexp.MustCompile(`\s+\((?:td|task)-[^)]+\)\.?$`)
	taskPrefixPattern         = regexp.MustCompile(`(?i)^task(?:\([^)]+\))?:\s*`)

	sectionOrder = []string{
		sectionFeatures,
		sectionBugFixes,
		sectionDocumentation,
		sectionImprovements,
	}
)

// Options configures changelog generation.
type Options struct {
	FromRef string
	ToRef   string
	Version string
	Date    time.Time
}

// Draft is a paste-ready changelog entry.
type Draft struct {
	FromRef  string
	ToRef    string
	Version  string
	Date     time.Time
	Sections []Section
}

// Section is one markdown section in the generated changelog entry.
type Section struct {
	Title   string
	Entries []Entry
}

// Entry is one changelog bullet.
type Entry struct {
	Text     string
	ShortSHA string
}

// Generate builds a changelog draft from committed git history.
func Generate(repoDir string, opts Options) (*Draft, error) {
	toRef := strings.TrimSpace(opts.ToRef)
	if toRef == "" {
		toRef = "HEAD"
	}

	fromRef := strings.TrimSpace(opts.FromRef)
	if fromRef == "" {
		tag, err := gitutil.NearestReachableSemverTag(repoDir, toRef)
		if err != nil {
			return nil, err
		}
		fromRef = tag
	}

	commits, err := gitutil.ListCommitsInRange(repoDir, fromRef, toRef)
	if err != nil {
		return nil, err
	}

	draft, err := Build(commits, Options{
		FromRef: fromRef,
		ToRef:   toRef,
		Version: strings.TrimSpace(opts.Version),
		Date:    opts.Date,
	})
	if err != nil {
		return nil, fmt.Errorf("%w between %s and %s", err, fromRef, toRef)
	}
	return draft, nil
}

// Build renders a changelog draft from already-loaded commits.
func Build(commits []gitutil.Commit, opts Options) (*Draft, error) {
	sectionsByTitle := make(map[string][]Entry)
	for _, commit := range commits {
		entry, ok := classifyCommit(commit)
		if !ok {
			continue
		}
		sectionsByTitle[entry.section] = append(sectionsByTitle[entry.section], Entry{
			Text:     entry.text,
			ShortSHA: shortSHA(commit),
		})
	}

	sections := make([]Section, 0, len(sectionOrder))
	for _, title := range sectionOrder {
		entries := sectionsByTitle[title]
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, Section{Title: title, Entries: entries})
	}
	if len(sections) == 0 {
		return nil, ErrNoRelevantCommits
	}

	return &Draft{
		FromRef:  strings.TrimSpace(opts.FromRef),
		ToRef:    strings.TrimSpace(opts.ToRef),
		Version:  strings.TrimSpace(opts.Version),
		Date:     opts.Date,
		Sections: sections,
	}, nil
}

// Markdown renders the draft as markdown suitable for CHANGELOG.md.
func (d *Draft) Markdown() string {
	var b strings.Builder
	switch {
	case d.Version != "" && !d.Date.IsZero():
		fmt.Fprintf(&b, "## [%s] - %s\n\n", d.Version, d.Date.Format("2006-01-02"))
	case d.Version != "":
		fmt.Fprintf(&b, "## [%s]\n\n", d.Version)
	default:
		b.WriteString("## Unreleased\n\n")
	}

	for _, section := range d.Sections {
		fmt.Fprintf(&b, "### %s\n", section.Title)
		for _, entry := range section.Entries {
			fmt.Fprintf(&b, "- %s (%s)\n", entry.Text, entry.ShortSHA)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

type classifiedEntry struct {
	section string
	text    string
}

func classifyCommit(commit gitutil.Commit) (classifiedEntry, bool) {
	subject := cleanSubject(commit.Subject)
	if subject == "" || isFilteredSubject(subject) {
		return classifiedEntry{}, false
	}

	if matches := conventionalCommitPattern.FindStringSubmatch(subject); len(matches) == 4 {
		kind := strings.ToLower(strings.TrimSpace(matches[1]))
		scope := strings.TrimSpace(matches[2])
		description := strings.TrimSpace(matches[3])
		return classifyConventional(kind, scope, description), true
	}

	section := classifyLoose(subject)
	return classifiedEntry{section: section, text: cleanBulletText(subject)}, true
}

func classifyConventional(kind, scope, description string) classifiedEntry {
	text := strings.TrimSpace(description)
	if scope != "" {
		text = scope + ": " + text
	}
	text = cleanBulletText(text)

	switch kind {
	case "feat", "feature":
		return classifiedEntry{section: sectionFeatures, text: text}
	case "fix", "bugfix", "bug":
		return classifiedEntry{section: sectionBugFixes, text: text}
	case "docs", "doc":
		return classifiedEntry{section: sectionDocumentation, text: text}
	default:
		return classifiedEntry{section: sectionImprovements, text: text}
	}
}

func classifyLoose(subject string) string {
	lower := strings.ToLower(subject)
	candidates := []string{lower}
	if _, tail, ok := strings.Cut(lower, ": "); ok && strings.TrimSpace(tail) != "" {
		candidates = append(candidates, strings.TrimSpace(tail))
	}

	switch {
	case hasLeadingVerb(candidates, "add", "introduce", "support", "enable", "implement", "show"):
		return sectionFeatures
	case hasLeadingVerb(candidates, "fix", "resolve", "correct", "prevent", "stabilize", "restore", "handle"):
		return sectionBugFixes
	case hasLeadingVerb(candidates, "document", "docs", "doc", "readme", "documentation"):
		return sectionDocumentation
	case hasLeadingVerb(candidates, "polish", "clean up", "clarify", "align", "reduce", "increase", "improve", "simplify", "update", "upgrade", "optimize", "refine", "expose", "refactor"):
		return sectionImprovements
	default:
		return sectionImprovements
	}
}

func hasLeadingVerb(subjects []string, verbs ...string) bool {
	for _, subject := range subjects {
		for _, verb := range verbs {
			if subject == verb || strings.HasPrefix(subject, verb+" ") || strings.HasPrefix(subject, verb+":") {
				return true
			}
		}
	}
	return false
}

func cleanSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	subject = tdRefPrefixPattern.ReplaceAllString(subject, "")
	subject = taskPrefixPattern.ReplaceAllString(subject, "")
	subject = tdRefSuffixPattern.ReplaceAllString(subject, "")
	return strings.Join(strings.Fields(subject), " ")
}

func cleanBulletText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return text
	}

	r, size := utf8.DecodeRuneInString(text)
	if r == utf8.RuneError && size == 0 {
		return text
	}
	if unicode.IsLower(r) {
		return string(unicode.ToUpper(r)) + text[size:]
	}
	return text
}

func isFilteredSubject(subject string) bool {
	lower := strings.ToLower(subject)
	return strings.HasPrefix(lower, "merge ") ||
		strings.HasPrefix(lower, "fixup!") ||
		strings.HasPrefix(lower, "squash!")
}

func shortSHA(commit gitutil.Commit) string {
	if commit.ShortSHA != "" {
		return commit.ShortSHA
	}
	if len(commit.SHA) >= 7 {
		return commit.SHA[:7]
	}
	return commit.SHA
}
