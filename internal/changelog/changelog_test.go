package changelog

import (
	"errors"
	"strings"
	"testing"
	"time"

	gitutil "github.com/marcus/td/internal/git"
)

func testCommit(sha, subject string) gitutil.Commit {
	return gitutil.Commit{
		SHA:      sha,
		ShortSHA: sha[:7],
		Subject:  subject,
	}
}

func TestBuildGroupsConventionalCommits(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("1111111111111111111111111111111111111111", "feat: add changelog command"),
		testCommit("2222222222222222222222222222222222222222", "fix(parser): handle empty ranges."),
		testCommit("3333333333333333333333333333333333333333", "docs: document release flow"),
		testCommit("4444444444444444444444444444444444444444", "refactor: simplify rendering"),
	}, Options{
		FromRef: "v0.1.0",
		ToRef:   "HEAD",
		Version: "v0.2.0",
		Date:    time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	gotSections := make([]string, 0, len(draft.Sections))
	for _, section := range draft.Sections {
		gotSections = append(gotSections, section.Title)
	}
	wantSections := []string{sectionFeatures, sectionBugFixes, sectionDocumentation, sectionImprovements}
	if strings.Join(gotSections, ",") != strings.Join(wantSections, ",") {
		t.Fatalf("sections = %v, want %v", gotSections, wantSections)
	}

	if got := draft.Sections[0].Entries[0].Text; got != "Add changelog command" {
		t.Fatalf("unexpected feature text: %q", got)
	}
	if got := draft.Sections[1].Entries[0].Text; got != "Parser: handle empty ranges" {
		t.Fatalf("unexpected fix text: %q", got)
	}
}

func TestBuildClassifiesLooseSubjects(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("1111111111111111111111111111111111111111", "Add changelog docs export"),
		testCommit("2222222222222222222222222222222222222222", "Fix README rendering"),
		testCommit("3333333333333333333333333333333333333333", "Document command reference"),
		testCommit("4444444444444444444444444444444444444444", "Polish release preview"),
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if got := draft.Sections[0].Title; got != sectionFeatures {
		t.Fatalf("first section = %q, want features", got)
	}
	if got := draft.Sections[0].Entries[0].Text; got != "Add changelog docs export" {
		t.Fatalf("feature entry = %q", got)
	}
	if got := draft.Sections[1].Title; got != sectionBugFixes {
		t.Fatalf("second section = %q, want bug fixes", got)
	}
	if got := draft.Sections[1].Entries[0].Text; got != "Fix README rendering" {
		t.Fatalf("fix entry = %q", got)
	}
	if got := draft.Sections[2].Title; got != sectionDocumentation {
		t.Fatalf("third section = %q, want documentation", got)
	}
	if got := draft.Sections[3].Title; got != sectionImprovements {
		t.Fatalf("fourth section = %q, want improvements", got)
	}
}

func TestBuildFiltersMergeAndAutosquashCommits(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("1111111111111111111111111111111111111111", "Merge pull request #1 from branch"),
		testCommit("2222222222222222222222222222222222222222", "fixup! feat: add changelog command"),
		testCommit("3333333333333333333333333333333333333333", "squash! fix: patch range"),
		testCommit("4444444444444444444444444444444444444444", "feat: add changelog command"),
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if len(draft.Sections) != 1 || len(draft.Sections[0].Entries) != 1 {
		t.Fatalf("expected only one entry, got %+v", draft.Sections)
	}
	if got := draft.Sections[0].Entries[0].Text; got != "Add changelog command" {
		t.Fatalf("unexpected remaining entry: %q", got)
	}
}

func TestBuildVersionDateHeadingAndShortSHA(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("abcdef1234567890abcdef1234567890abcdef12", "feat: add changelog command."),
	}, Options{
		Version: "v0.2.0",
		Date:    time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	expected := `## [v0.2.0] - 2026-05-09

### Features
- Add changelog command (abcdef1)
`
	if got := draft.Markdown(); got != expected {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestBuildUnreleasedHeading(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("abcdef1234567890abcdef1234567890abcdef12", "fix: handle changelog range"),
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if !strings.HasPrefix(draft.Markdown(), "## Unreleased\n\n") {
		t.Fatalf("expected unreleased heading, got:\n%s", draft.Markdown())
	}
}

func TestBuildReturnsNoRelevantCommitError(t *testing.T) {
	_, err := Build([]gitutil.Commit{
		testCommit("1111111111111111111111111111111111111111", "fixup! feat: add changelog command"),
		testCommit("2222222222222222222222222222222222222222", "squash! fix: patch range"),
	}, Options{})
	if !errors.Is(err, ErrNoRelevantCommits) {
		t.Fatalf("expected ErrNoRelevantCommits, got %v", err)
	}
}

func TestCleanSubjectRemovesTaskRefs(t *testing.T) {
	draft, err := Build([]gitutil.Commit{
		testCommit("abcdef1234567890abcdef1234567890abcdef12", "[td-a7ff5e] task: feat: add rich text input (td-a7ff5e)."),
	}, Options{})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if got := draft.Sections[0].Entries[0].Text; got != "Add rich text input" {
		t.Fatalf("unexpected cleaned entry: %q", got)
	}
}
