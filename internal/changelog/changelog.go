package changelog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marcus/td/internal/git"
)

var (
	// ErrNoEntries indicates there were commits in the range, but none survived filtering.
	ErrNoEntries = errors.New("no changelog entries")

	conventionalPrefixPattern = regexp.MustCompile(`^(?P<type>[a-z]+)(?:\([^)]+\))?(?:!)?:\s*(?P<rest>.+)$`)
	internalPrefixPattern     = regexp.MustCompile(`^(?:\[[^\]]+\]\s*)+`)
	internalSuffixPattern     = regexp.MustCompile(`\s+\(td-[^)]+\)$`)
	releaseHousekeeping       = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^docs:\s+update changelog for v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^chore(?:\([^)]+\))?:\s+(?:prepare|cut|publish|release)\s+v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^release\s+v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^bump version to v\d+\.\d+\.\d+`),
	}
)

type section string

const (
	sectionFeatures      section = "Features"
	sectionBugFixes      section = "Bug Fixes"
	sectionImprovements  section = "Improvements"
	sectionDocumentation section = "Documentation"
	sectionOtherChanges  section = "Other Changes"
)

var sectionOrder = []section{
	sectionFeatures,
	sectionBugFixes,
	sectionImprovements,
	sectionDocumentation,
	sectionOtherChanges,
}

// Options controls changelog rendering.
type Options struct {
	Version     string
	Date        time.Time
	IncludeMeta bool
}

// Render converts git commits into a paste-ready markdown changelog section.
func Render(commits []git.Commit, opts Options) (string, error) {
	if strings.TrimSpace(opts.Version) == "" {
		return "", errors.New("version is required")
	}
	if opts.Date.IsZero() {
		return "", errors.New("date is required")
	}

	grouped := map[section][]string{}
	entryCount := 0

	for _, commit := range commits {
		entry, ok := classifyCommit(commit, opts.IncludeMeta)
		if !ok {
			continue
		}
		grouped[entry.section] = append(grouped[entry.section], entry.text)
		entryCount++
	}

	if entryCount == 0 {
		return "", ErrNoEntries
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n\n", opts.Version, opts.Date.Format("2006-01-02"))

	for _, sec := range sectionOrder {
		items := grouped[sec]
		if len(items) == 0 {
			continue
		}

		fmt.Fprintf(&b, "### %s\n", sec)
		for _, item := range items {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

type entry struct {
	section section
	text    string
}

func classifyCommit(commit git.Commit, includeMeta bool) (entry, bool) {
	subject := strings.TrimSpace(commit.Subject)
	if subject == "" {
		return entry{}, false
	}
	if isMergeCommit(subject) || isReleaseHousekeeping(subject) {
		return entry{}, false
	}

	subject = cleanSubject(subject)
	if subject == "" {
		return entry{}, false
	}

	kind, remainder := conventionalKind(subject)
	switch kind {
	case "feat":
		return entry{section: sectionFeatures, text: cleanBulletText(remainder)}, true
	case "fix":
		return entry{section: sectionBugFixes, text: cleanBulletText(remainder)}, true
	case "docs":
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionDocumentation, text: cleanBulletText(remainder)}, true
	case "perf", "refactor", "build", "style", "revert":
		return entry{section: sectionImprovements, text: cleanBulletText(remainder)}, true
	case "test", "ci", "chore":
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionImprovements, text: cleanBulletText(remainder)}, true
	}

	guessedSection, ok := classifyFreeform(subject, includeMeta)
	if !ok {
		return entry{}, false
	}

	return entry{section: guessedSection, text: cleanBulletText(subject)}, true
}

func isMergeCommit(subject string) bool {
	return strings.HasPrefix(subject, "Merge ")
}

func isReleaseHousekeeping(subject string) bool {
	for _, pattern := range releaseHousekeeping {
		if pattern.MatchString(subject) {
			return true
		}
	}
	return false
}

func cleanSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	subject = internalPrefixPattern.ReplaceAllString(subject, "")
	subject = internalSuffixPattern.ReplaceAllString(subject, "")
	return strings.TrimSpace(subject)
}

func conventionalKind(subject string) (string, string) {
	matches := conventionalPrefixPattern.FindStringSubmatch(subject)
	if len(matches) != 3 {
		return "", ""
	}
	return strings.ToLower(matches[1]), strings.TrimSpace(matches[2])
}

func classifyFreeform(subject string, includeMeta bool) (section, bool) {
	lower := strings.ToLower(subject)
	candidates := []string{lower}
	if _, tail, ok := strings.Cut(lower, ": "); ok && strings.TrimSpace(tail) != "" {
		candidates = append(candidates, strings.TrimSpace(tail))
	}

	switch {
	case hasLeadingVerb(candidates, "add", "introduce", "support", "enable", "implement", "show"):
		return sectionFeatures, true
	case hasLeadingVerb(candidates, "fix", "resolve", "correct", "prevent", "stabilize", "restore"):
		return sectionBugFixes, true
	case hasLeadingVerb(candidates, "document", "docs", "readme"):
		if !includeMeta {
			return "", false
		}
		return sectionDocumentation, true
	case hasLeadingVerb(candidates, "clean up", "clarify", "align", "reduce", "increase", "improve", "simplify", "update", "expose", "refine"):
		return sectionImprovements, true
	default:
		return sectionOtherChanges, true
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

func cleanBulletText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
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
