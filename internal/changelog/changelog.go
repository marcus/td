package changelog

import (
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
	sectionImprovements  = "Improvements"
	sectionDocumentation = "Documentation"
	sectionOtherChanges  = "Other Changes"
)

var (
	conventionalCommitPattern = regexp.MustCompile(`(?i)^(feat(?:ure)?|fix|bugfix|bug|docs?|doc|test(?:s)?|refactor|perf|chore|build|ci|style|revert)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	tdRefPrefixPattern        = regexp.MustCompile(`^(?:(?:\[(?:td|task)-[^\]]+\])\s*)+`)
	tdRefSuffixPattern        = regexp.MustCompile(`\s+\((?:td|task)-[^)]+\)\.?$`)
	taskPrefixPattern         = regexp.MustCompile(`(?i)^task(?:\([^)]+\))?:\s*`)
	releaseHousekeeping       = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^docs:\s+update changelog for v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^update changelog for v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^chore(?:\([^)]+\))?:\s+(?:prepare|cut|publish|release)\s+v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^release\s+v\d+\.\d+\.\d+`),
		regexp.MustCompile(`(?i)^bump version to v\d+\.\d+\.\d+`),
	}
	sectionOrder = []string{
		sectionFeatures,
		sectionBugFixes,
		sectionImprovements,
		sectionDocumentation,
		sectionOtherChanges,
	}
)

// Options configures changelog generation.
type Options struct {
	FromRef     string
	ToRef       string
	Version     string
	Date        time.Time
	IncludeMeta bool
}

// Draft is paste-ready CHANGELOG content plus range metadata.
type Draft struct {
	RepoRoot      string
	FromRef       string
	ToRef         string
	Version       string
	Date          time.Time
	IncludeMeta   bool
	SourceCommits int
	Sections      []Section
}

// Section is one markdown section in the generated draft.
type Section struct {
	Title   string
	Entries []string
}

// Generate drafts a CHANGELOG entry from committed git history.
func Generate(repoDir string, opts Options) (*Draft, error) {
	root, err := gitutil.GetRootDirFrom(repoDir)
	if err != nil {
		return nil, err
	}

	rawToRef := strings.TrimSpace(opts.ToRef)
	toRef := rawToRef
	if toRef == "" {
		toRef = "HEAD"
	}

	fromRef := strings.TrimSpace(opts.FromRef)
	if fromRef == "" {
		if rawToRef != "" && !strings.EqualFold(toRef, "HEAD") {
			tags, err := gitutil.GetSemverTagsPointingAt(root, toRef)
			if err != nil {
				return nil, err
			}
			if releaseTag := matchingSemverTagRef(rawToRef, tags); releaseTag != "" {
				fromRef, err = gitutil.GetPreviousSemverTag(root, releaseTag)
			} else {
				fromRef, err = gitutil.GetLatestSemverTag(root, toRef)
			}
		} else {
			fromRef, err = gitutil.GetLatestSemverTag(root, toRef)
		}
		if err != nil {
			return nil, err
		}
	}

	commits, err := gitutil.ListCommitsInRange(root, fromRef, toRef)
	if err != nil {
		return nil, err
	}

	date := opts.Date
	if date.IsZero() {
		date = time.Now()
	}

	return buildDraft(commits, draftOptions{
		RepoRoot:    root,
		FromRef:     fromRef,
		ToRef:       toRef,
		Version:     opts.Version,
		Date:        date,
		IncludeMeta: opts.IncludeMeta,
	}), nil
}

// Markdown renders the draft as a CHANGELOG-ready markdown block.
func (d *Draft) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n\n", d.Version, d.Date.Format("2006-01-02"))

	switch {
	case d.SourceCommits == 0:
		fmt.Fprintf(&b, "_No committed changes found between %s and %s._\n", d.FromRef, d.ToRef)
		return b.String()
	case len(d.Sections) == 0:
		fmt.Fprintf(&b, "_No changelog-worthy changes found between %s and %s", d.FromRef, d.ToRef)
		if !d.IncludeMeta {
			b.WriteString(". Re-run with --include-meta to include documentation, test, CI, and chore commits")
		}
		b.WriteString("._\n")
		return b.String()
	}

	for _, section := range d.Sections {
		fmt.Fprintf(&b, "### %s\n", section.Title)
		for _, entry := range section.Entries {
			fmt.Fprintf(&b, "- %s\n", entry)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

type draftOptions struct {
	RepoRoot    string
	FromRef     string
	ToRef       string
	Version     string
	Date        time.Time
	IncludeMeta bool
}

func buildDraft(commits []gitutil.Commit, opts draftOptions) *Draft {
	sectionsByTitle := make(map[string][]string)
	for _, commit := range commits {
		entry, ok := classifyCommit(commit, opts.IncludeMeta)
		if !ok {
			continue
		}
		sectionsByTitle[entry.section] = append(sectionsByTitle[entry.section], entry.text)
	}

	sections := make([]Section, 0, len(sectionOrder))
	for _, title := range sectionOrder {
		entries := sectionsByTitle[title]
		if len(entries) == 0 {
			continue
		}
		sections = append(sections, Section{
			Title:   title,
			Entries: entries,
		})
	}

	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "Unreleased"
	}

	return &Draft{
		RepoRoot:      opts.RepoRoot,
		FromRef:       opts.FromRef,
		ToRef:         opts.ToRef,
		Version:       version,
		Date:          opts.Date,
		IncludeMeta:   opts.IncludeMeta,
		SourceCommits: len(commits),
		Sections:      sections,
	}
}

type entry struct {
	section string
	text    string
}

func classifyCommit(commit gitutil.Commit, includeMeta bool) (entry, bool) {
	subject := cleanSubject(commit.Subject)
	if subject == "" || isMergeCommit(subject) || isReleaseHousekeeping(subject) {
		return entry{}, false
	}

	if matches := conventionalCommitPattern.FindStringSubmatch(subject); len(matches) == 4 {
		kind := strings.ToLower(strings.TrimSpace(matches[1]))
		scope := strings.TrimSpace(matches[2])
		description := strings.TrimSpace(matches[3])
		return classifyConventional(kind, scope, description, includeMeta)
	}

	if documentationOnly(commit.Files) {
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionDocumentation, text: cleanBulletText(subject)}, true
	}

	if testsOnly(commit.Files) || ciOnly(commit.Files) {
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionOtherChanges, text: cleanBulletText(subject)}, true
	}

	section, isMeta := classifyFreeform(subject)
	if isMeta && !includeMeta {
		return entry{}, false
	}

	return entry{section: section, text: cleanBulletText(subject)}, true
}

func classifyConventional(kind, scope, description string, includeMeta bool) (entry, bool) {
	text := description
	if scope != "" {
		text = scope + ": " + description
	}
	text = cleanBulletText(text)

	switch kind {
	case "feat", "feature":
		return entry{section: sectionFeatures, text: text}, true
	case "fix", "bugfix", "bug":
		return entry{section: sectionBugFixes, text: text}, true
	case "perf", "refactor", "build", "style":
		return entry{section: sectionImprovements, text: text}, true
	case "docs", "doc":
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionDocumentation, text: text}, true
	case "test", "tests", "ci", "chore":
		if !includeMeta {
			return entry{}, false
		}
		return entry{section: sectionOtherChanges, text: text}, true
	default:
		return entry{section: sectionOtherChanges, text: text}, true
	}
}

func classifyFreeform(subject string) (string, bool) {
	lower := strings.ToLower(subject)
	candidates := []string{lower}
	if _, tail, ok := strings.Cut(lower, ": "); ok && strings.TrimSpace(tail) != "" {
		candidates = append(candidates, strings.TrimSpace(tail))
	}

	switch {
	case hasLeadingVerb(candidates, "add", "introduce", "support", "enable", "implement", "show"):
		return sectionFeatures, false
	case hasLeadingVerb(candidates, "fix", "resolve", "correct", "prevent", "stabilize", "restore", "handle"):
		return sectionBugFixes, false
	case hasLeadingVerb(candidates, "clean up", "clarify", "align", "reduce", "increase", "improve", "simplify", "update", "upgrade", "optimize", "refine", "expose"):
		return sectionImprovements, false
	case hasLeadingVerb(candidates, "document", "docs", "doc", "readme", "documentation"):
		return sectionDocumentation, true
	case hasLeadingVerb(candidates, "test", "tests", "ci", "chore"):
		return sectionOtherChanges, true
	default:
		return sectionOtherChanges, false
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
	subject = strings.Join(strings.Fields(subject), " ")
	return subject
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

func matchingSemverTagRef(rawRef string, tags []string) string {
	ref := strings.TrimSpace(rawRef)
	ref = strings.TrimPrefix(ref, "refs/tags/")
	for _, tag := range tags {
		if ref == tag {
			return tag
		}
	}
	return ""
}

func documentationOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isDocumentationFile(file) {
			return false
		}
	}
	return true
}

func testsOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isTestFile(file) {
			return false
		}
	}
	return true
}

func ciOnly(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		if !isCIFile(file) {
			return false
		}
	}
	return true
}

func isDocumentationFile(path string) bool {
	return strings.HasPrefix(path, "docs/") ||
		strings.HasPrefix(path, "website/docs/") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".mdx") ||
		path == "README" ||
		path == "README.md" ||
		path == "CHANGELOG.md"
}

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") ||
		strings.HasPrefix(path, "test/") ||
		strings.HasPrefix(path, "tests/")
}

func isCIFile(path string) bool {
	return strings.HasPrefix(path, ".github/workflows/") ||
		strings.HasPrefix(path, "ci/")
}
