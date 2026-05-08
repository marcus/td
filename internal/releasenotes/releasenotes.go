package releasenotes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	SectionBreaking     = "breaking_changes"
	SectionFeatures     = "features"
	SectionFixes        = "bug_fixes"
	SectionPerformance  = "performance"
	SectionImprovements = "improvements"
	SectionDocs         = "documentation"
	SectionMaintenance  = "maintenance"
)

var (
	ErrNotGitRepo = errors.New("not a git repository")

	conventionalSubjectRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*)(?:\(([^)]+)\))?(!)?:\s*(.+)$`)
	breakingFooterRe      = regexp.MustCompile(`(?im)^BREAKING[ -]CHANGE:\s*(.+)$`)
)

type Options struct {
	RepoDir         string
	From            string
	To              string
	Version         string
	Date            string
	IncludeInternal bool
}

type Commit struct {
	Hash      string
	ShortHash string
	Subject   string
	Body      string
}

type ParsedSubject struct {
	Type         string
	Scope        string
	Description  string
	Conventional bool
	Breaking     bool
}

type Item struct {
	SHA          string `json:"sha"`
	Subject      string `json:"subject"`
	Type         string `json:"type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Breaking     bool   `json:"breaking,omitempty"`
	BreakingNote string `json:"breaking_note,omitempty"`
	Internal     bool   `json:"internal,omitempty"`
}

type Section struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items"`
}

type Draft struct {
	Version     string    `json:"version,omitempty"`
	Date        string    `json:"date,omitempty"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Repository  string    `json:"repository,omitempty"`
	CommitCount int       `json:"commit_count"`
	Sections    []Section `json:"sections"`
}

type sectionDef struct {
	id    string
	title string
}

var orderedSections = []sectionDef{
	{SectionBreaking, "Breaking Changes"},
	{SectionFeatures, "Features"},
	{SectionFixes, "Bug Fixes"},
	{SectionPerformance, "Performance"},
	{SectionImprovements, "Improvements"},
	{SectionDocs, "Documentation"},
	{SectionMaintenance, "Maintenance"},
}

func DraftFromGit(ctx context.Context, opts Options) (*Draft, error) {
	if err := ValidateDate(opts.Date); err != nil {
		return nil, err
	}
	if strings.TrimSpace(opts.To) == "" {
		opts.To = "HEAD"
	}

	repo, err := GitRoot(ctx, opts.RepoDir)
	if err != nil {
		return nil, err
	}

	if err := VerifyCommitRef(ctx, repo, opts.To); err != nil {
		return nil, fmt.Errorf("invalid --to ref %q: %w", opts.To, err)
	}

	if strings.TrimSpace(opts.From) == "" {
		tag, err := DefaultBaseRef(ctx, repo, opts.To)
		if err != nil {
			return nil, err
		}
		opts.From = tag
	}

	if err := VerifyCommitRef(ctx, repo, opts.From); err != nil {
		return nil, fmt.Errorf("invalid --from ref %q: %w", opts.From, err)
	}

	commits, err := CollectCommits(ctx, repo, opts.From, opts.To)
	if err != nil {
		return nil, err
	}

	return BuildDraft(commits, Draft{
		Version:    strings.TrimSpace(opts.Version),
		Date:       strings.TrimSpace(opts.Date),
		From:       opts.From,
		To:         opts.To,
		Repository: repo,
	}, opts.IncludeInternal), nil
}

func ValidateDate(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return fmt.Errorf("invalid --date %q: use YYYY-MM-DD", value)
	}
	return nil
}

func GitRoot(ctx context.Context, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotGitRepo, dir)
	}
	return strings.TrimSpace(string(out)), nil
}

func DefaultBaseRef(ctx context.Context, repoDir, toRef string) (string, error) {
	describeRef := toRef
	if pointsAtVersionTag(ctx, repoDir, toRef) {
		describeRef = toRef + "^"
	}

	out, err := runGit(ctx, repoDir, "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", describeRef)
	if err != nil {
		return "", fmt.Errorf("no v* tags found; pass --from to choose a base ref")
	}
	return strings.TrimSpace(string(out)), nil
}

func pointsAtVersionTag(ctx context.Context, repoDir, ref string) bool {
	out, err := runGit(ctx, repoDir, "describe", "--exact-match", "--tags", "--match", "v[0-9]*", ref)
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func VerifyCommitRef(ctx context.Context, repoDir, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("empty ref")
	}
	_, err := runGit(ctx, repoDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return errors.New("ref does not resolve to a commit")
	}
	return nil
}

func CollectCommits(ctx context.Context, repoDir, fromRef, toRef string) ([]Commit, error) {
	raw, err := runGit(ctx, repoDir, "log", "--no-merges", "--format=%x1e%H%x1f%h%x1f%s%x1f%B", fromRef+".."+toRef)
	if err != nil {
		return nil, fmt.Errorf("failed to read git commits for %s..%s: %w", fromRef, toRef, err)
	}
	commits, err := parseGitLog(raw)
	if err != nil {
		return nil, err
	}

	// git log is newest-first; reverse for changelog-style oldest-first output.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

func BuildDraft(commits []Commit, base Draft, includeInternal bool) *Draft {
	sections := make([]Section, 0, len(orderedSections))
	sectionIndex := make(map[string]int, len(orderedSections))
	for _, def := range orderedSections {
		sectionIndex[def.id] = len(sections)
		sections = append(sections, Section{ID: def.id, Title: def.title, Items: []Item{}})
	}

	commitCount := 0
	for _, commit := range commits {
		item, sectionID, ok := Classify(commit)
		if !ok {
			continue
		}
		if item.Internal && !item.Breaking && !includeInternal {
			continue
		}
		idx := sectionIndex[sectionID]
		sections[idx].Items = append(sections[idx].Items, item)
		commitCount++
	}

	base.CommitCount = commitCount
	base.Sections = sections
	return &base
}

func ParseSubject(subject string) ParsedSubject {
	subject = strings.TrimSpace(subject)
	matches := conventionalSubjectRe.FindStringSubmatch(subject)
	if matches == nil {
		return ParsedSubject{
			Description:  subject,
			Conventional: false,
		}
	}
	return ParsedSubject{
		Type:         strings.ToLower(matches[1]),
		Scope:        matches[2],
		Breaking:     matches[3] == "!",
		Description:  strings.TrimSpace(matches[4]),
		Conventional: true,
	}
}

func Classify(commit Commit) (Item, string, bool) {
	if shouldSkipSubject(commit.Subject) {
		return Item{}, "", false
	}

	parsed := ParseSubject(commit.Subject)
	breakingNote := breakingNote(commit.Body)
	breaking := parsed.Breaking || breakingNote != ""
	sectionID, internal := sectionForType(parsed.Type, parsed.Conventional)
	if breaking {
		sectionID = SectionBreaking
	}

	subject := parsed.Description
	if strings.TrimSpace(subject) == "" {
		subject = strings.TrimSpace(commit.Subject)
	}
	if subject == "" {
		subject = "(no subject)"
	}

	return Item{
		SHA:          commit.ShortHash,
		Subject:      subject,
		Type:         parsed.Type,
		Scope:        parsed.Scope,
		Breaking:     breaking,
		BreakingNote: breakingNote,
		Internal:     internal,
	}, sectionID, true
}

func RenderMarkdown(draft *Draft) string {
	var b strings.Builder
	header := "## Release Notes"
	if draft.Version != "" {
		header = "## " + draft.Version
	}
	if draft.Date != "" {
		header += " - " + draft.Date
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	if draft.From != "" || draft.To != "" {
		b.WriteString(fmt.Sprintf("_Range: `%s..%s` (%d commits)_\n\n", draft.From, draft.To, draft.CommitCount))
	}

	hasItems := false
	for _, section := range draft.Sections {
		if len(section.Items) == 0 {
			continue
		}
		hasItems = true
		b.WriteString("### ")
		b.WriteString(section.Title)
		b.WriteString("\n\n")
		for _, item := range section.Items {
			b.WriteString("- ")
			b.WriteString(item.Subject)
			if item.SHA != "" {
				b.WriteString(" (")
				b.WriteString(item.SHA)
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if !hasItems {
		b.WriteString("_No release note entries found._\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func SectionTitles() []string {
	titles := make([]string, 0, len(orderedSections))
	for _, section := range orderedSections {
		titles = append(titles, section.title)
	}
	return titles
}

func parseGitLog(raw []byte) ([]Commit, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	records := bytes.Split(raw, []byte{0x1e})
	commits := make([]Commit, 0, len(records))
	for _, record := range records {
		record = bytes.Trim(record, "\n")
		if len(bytes.TrimSpace(record)) == 0 {
			continue
		}

		fields := bytes.SplitN(record, []byte{0x1f}, 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("malformed commit log record: expected 4 fields, got %d", len(fields))
		}

		commit := Commit{
			Hash:      strings.TrimSpace(string(fields[0])),
			ShortHash: strings.TrimSpace(string(fields[1])),
			Subject:   strings.TrimSpace(string(fields[2])),
			Body:      strings.TrimSpace(string(fields[3])),
		}
		if commit.Hash == "" || commit.ShortHash == "" {
			return nil, errors.New("malformed commit log record: missing commit hash")
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

func shouldSkipSubject(subject string) bool {
	lower := strings.ToLower(strings.TrimSpace(subject))
	if lower == "" {
		return false
	}
	for _, prefix := range []string{"fixup!", "squash!", "amend!"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.HasPrefix(lower, "merge ")
}

func sectionForType(commitType string, conventional bool) (string, bool) {
	if !conventional {
		return SectionImprovements, false
	}

	switch commitType {
	case "feat", "feature":
		return SectionFeatures, false
	case "fix", "bugfix":
		return SectionFixes, false
	case "perf":
		return SectionPerformance, false
	case "docs", "doc":
		return SectionDocs, false
	case "refactor", "improve", "improvement", "enhance", "enhancement":
		return SectionImprovements, false
	case "build", "chore", "ci", "internal", "style", "test":
		return SectionMaintenance, true
	default:
		return SectionImprovements, false
	}
}

func breakingNote(body string) string {
	matches := breakingFooterRe.FindStringSubmatch(body)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func runGit(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoDir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}
