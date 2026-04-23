package changelog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	gitutil "github.com/marcus/td/internal/git"
)

// ErrNoRelevantCommits is returned when a commit range contains no changelog-worthy commits.
var ErrNoRelevantCommits = errors.New("no relevant commits")

// Options configures markdown rendering.
type Options struct {
	Version string
	Date    string
}

type section string

const (
	sectionFeatures      section = "Features"
	sectionBugFixes      section = "Bug Fixes"
	sectionDocumentation section = "Documentation"
	sectionImprovements  section = "Improvements"
)

var (
	conventionalPrefixPattern = regexp.MustCompile(`^(?i)(build|chore|ci|docs?|feat|fix|perf|refactor|revert|style|test)(\([^)]+\))?(!)?:\s*`)
	fixupPrefixPattern        = regexp.MustCompile(`^(?i)(fixup!|squash!)\s*`)
	pullRequestSuffixPattern  = regexp.MustCompile(`\s+\(#\d+\)$`)
	spacePattern              = regexp.MustCompile(`\s+`)
	orderedSections           = []section{
		sectionFeatures,
		sectionBugFixes,
		sectionDocumentation,
		sectionImprovements,
	}
)

// RenderMarkdown converts commits into grouped changelog markdown.
func RenderMarkdown(commits []gitutil.Commit, opts Options) (string, error) {
	grouped := make(map[section][]string)
	total := 0

	for _, commit := range commits {
		section, summary, ok := classifyCommit(commit)
		if !ok {
			continue
		}
		grouped[section] = append(grouped[section], summary)
		total++
	}

	if total == 0 {
		return "", ErrNoRelevantCommits
	}

	var b strings.Builder
	b.WriteString(renderHeading(opts))
	b.WriteString("\n\n")

	wroteSection := false
	for _, current := range orderedSections {
		items := grouped[current]
		if len(items) == 0 {
			continue
		}
		if wroteSection {
			b.WriteString("\n")
		}
		b.WriteString("### ")
		b.WriteString(string(current))
		b.WriteString("\n")
		for _, item := range items {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
		wroteSection = true
	}

	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func renderHeading(opts Options) string {
	version := strings.TrimSpace(opts.Version)
	date := strings.TrimSpace(opts.Date)

	if version == "" {
		return "## Unreleased"
	}
	if date == "" {
		return fmt.Sprintf("## [%s]", version)
	}
	return fmt.Sprintf("## [%s] - %s", version, date)
}

func classifyCommit(commit gitutil.Commit) (section, string, bool) {
	normalized := normalizeSubject(commit.Subject)
	if normalized == "" {
		return "", "", false
	}

	if commitType := conventionalType(commit.Subject); commitType != "" {
		switch commitType {
		case "feat":
			return sectionFeatures, normalized, true
		case "fix":
			return sectionBugFixes, normalized, true
		case "doc", "docs":
			return sectionDocumentation, normalized, true
		default:
			return sectionImprovements, normalized, true
		}
	}

	lower := strings.ToLower(normalized)
	switch {
	case isDocumentationSummary(lower):
		return sectionDocumentation, normalized, true
	case isFeatureSummary(lower):
		return sectionFeatures, normalized, true
	case isBugFixSummary(lower):
		return sectionBugFixes, normalized, true
	default:
		return sectionImprovements, normalized, true
	}
}

func conventionalType(subject string) string {
	subject = strings.TrimSpace(subject)
	matches := conventionalPrefixPattern.FindStringSubmatch(subject)
	if len(matches) < 2 {
		return ""
	}
	return strings.ToLower(matches[1])
}

func normalizeSubject(subject string) string {
	normalized := strings.TrimSpace(subject)
	if normalized == "" || isMergeNoise(normalized) {
		return ""
	}

	normalized = fixupPrefixPattern.ReplaceAllString(normalized, "")
	normalized = conventionalPrefixPattern.ReplaceAllString(normalized, "")
	normalized = pullRequestSuffixPattern.ReplaceAllString(normalized, "")
	normalized = strings.TrimSuffix(strings.TrimSpace(normalized), ".")
	normalized = spacePattern.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}

	return upperFirst(normalized)
}

func isMergeNoise(subject string) bool {
	lower := strings.ToLower(strings.TrimSpace(subject))
	return strings.HasPrefix(lower, "merge ")
}

func isDocumentationSummary(subject string) bool {
	docPrefixes := []string{"document ", "docs ", "readme", "guide ", "comment ", "commentary "}
	docContains := []string{"documentation", "readme", "changelog", "command reference", "release guide", "docs/"}
	return hasPrefix(subject, docPrefixes...) || containsAny(subject, docContains...)
}

func isFeatureSummary(subject string) bool {
	featurePrefixes := []string{"add ", "allow ", "create ", "enable ", "generate ", "implement ", "introduce ", "support "}
	return hasPrefix(subject, featurePrefixes...)
}

func isBugFixSummary(subject string) bool {
	bugPrefixes := []string{"fix ", "avoid ", "correct ", "handle ", "prevent ", "resolve "}
	bugContains := []string{" bug", "panic", "crash", "regression"}
	return hasPrefix(subject, bugPrefixes...) || containsAny(subject, bugContains...)
}

func hasPrefix(subject string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(subject, prefix) {
			return true
		}
	}
	return false
}

func containsAny(subject string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(subject, part) {
			return true
		}
	}
	return false
}

func upperFirst(subject string) string {
	runes := []rune(subject)
	if len(runes) == 0 {
		return subject
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
