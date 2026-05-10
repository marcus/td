package releasenotes

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	gitutil "github.com/marcus/td/internal/git"
)

const (
	sectionFeatures      = "Features"
	sectionBugFixes      = "Bug Fixes"
	sectionDocumentation = "Documentation"
	sectionOtherChanges  = "Other Changes"
)

var (
	conventionalCommitPattern = regexp.MustCompile(`(?i)^(feat(?:ure)?|fix|bugfix|bug|docs?|doc|test(?:s)?|refactor|perf|chore|build|ci|style|revert)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	tdRefPrefixPattern        = regexp.MustCompile(`^\[td-[0-9a-f]+\]\s*`)
	tdRefSuffixPattern        = regexp.MustCompile(`\s+\(td-[0-9a-f]+\)$`)
	prSuffixPattern           = regexp.MustCompile(`\s+\(#\d+\)$`)
	sectionOrder              = []string{
		sectionFeatures,
		sectionBugFixes,
		sectionDocumentation,
		sectionOtherChanges,
	}
)

// Options configures release-note generation.
type Options struct {
	FromRef string
	ToRef   string
	Version string
	Date    time.Time
}

// Draft is changelog-ready release note content plus range metadata.
type Draft struct {
	RepoRoot string
	FromRef  string
	ToRef    string
	Version  string
	Date     time.Time
	Sections []Section
}

// Section is one markdown section in the generated draft.
type Section struct {
	Title   string
	Entries []string
}

// Generate drafts release notes from committed git history.
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
		usePreviousTag := rawToRef != "" && !strings.EqualFold(toRef, "HEAD")
		if usePreviousTag {
			taggedReleaseTarget, err := gitutil.RefPointsToSemverTag(root, toRef)
			if err != nil {
				return nil, err
			}
			if taggedReleaseTarget {
				fromRef, err = gitutil.GetPreviousSemverTag(root, toRef)
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
		RepoRoot: root,
		FromRef:  fromRef,
		ToRef:    toRef,
		Version:  opts.Version,
		Date:     date,
	}), nil
}

// Markdown renders the draft as a CHANGELOG-ready markdown block.
func (d *Draft) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n\n", d.Version, d.Date.Format("2006-01-02"))

	if len(d.Sections) == 0 {
		fmt.Fprintf(&b, "_No committed changes found between %s and %s._\n", d.FromRef, d.ToRef)
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
	RepoRoot string
	FromRef  string
	ToRef    string
	Version  string
	Date     time.Time
}

func buildDraft(commits []gitutil.Commit, opts draftOptions) *Draft {
	sectionsByTitle := make(map[string][]string)
	for _, commit := range commits {
		title, entry := classifyCommit(commit)
		sectionsByTitle[title] = append(sectionsByTitle[title], entry)
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
		RepoRoot: opts.RepoRoot,
		FromRef:  opts.FromRef,
		ToRef:    opts.ToRef,
		Version:  version,
		Date:     opts.Date,
		Sections: sections,
	}
}

func classifyCommit(commit gitutil.Commit) (string, string) {
	subject := cleanSubject(commit.Subject)
	if subject == "" {
		shortSHA := commit.SHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		return sectionOtherChanges, fmt.Sprintf("commit %s", shortSHA)
	}

	if matches := conventionalCommitPattern.FindStringSubmatch(subject); len(matches) == 4 {
		title := sectionForPrefix(strings.ToLower(matches[1]))
		scope := strings.TrimSpace(matches[2])
		description := strings.TrimSpace(matches[3])
		if scope != "" {
			return title, fmt.Sprintf("%s: %s", scope, description)
		}
		return title, description
	}

	if documentationOnly(commit.Files) {
		return sectionDocumentation, subject
	}

	return sectionOtherChanges, subject
}

func sectionForPrefix(prefix string) string {
	switch prefix {
	case "feat", "feature":
		return sectionFeatures
	case "fix", "bugfix", "bug":
		return sectionBugFixes
	case "docs", "doc":
		return sectionDocumentation
	default:
		return sectionOtherChanges
	}
}

func cleanSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	subject = tdRefPrefixPattern.ReplaceAllString(subject, "")
	subject = prSuffixPattern.ReplaceAllString(subject, "")
	subject = tdRefSuffixPattern.ReplaceAllString(subject, "")
	subject = strings.Join(strings.Fields(subject), " ")
	return subject
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

func isDocumentationFile(path string) bool {
	return strings.HasPrefix(path, "docs/") ||
		strings.HasPrefix(path, "website/docs/") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".mdx") ||
		path == "README" ||
		path == "README.md" ||
		path == "CHANGELOG.md"
}
