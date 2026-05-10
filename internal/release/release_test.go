package release

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/git"
)

func TestBuildClassifiesCommitsIntoSections(t *testing.T) {
	commits := []git.Commit{
		{ShortSHA: "aaa1111", Subject: "feat: add release notes command", Files: []string{"cmd/release_notes.go"}},
		{ShortSHA: "bbb2222", Subject: "fix: handle empty ranges", Files: []string{"internal/release/release.go"}},
		{ShortSHA: "ccc3333", Subject: "docs: update release guide", Files: []string{"docs/guides/releasing-new-version.md"}},
		{ShortSHA: "ddd4444", Subject: "test: cover release notes command", Files: []string{"cmd/release_notes_test.go"}},
		{ShortSHA: "eee5555", Subject: "Rename helper", Files: []string{"internal/release/render.go"}},
	}

	draft := Build(commits, &git.DiffStats{FilesChanged: 5, Additions: 25, Deletions: 3}, Options{
		RevisionRange: "v0.1.0..HEAD",
	})

	if len(draft.Sections) != 5 {
		t.Fatalf("len(sections) = %d, want 5", len(draft.Sections))
	}

	if got := draft.Sections[0].Title; got != SectionFeatures {
		t.Fatalf("sections[0].Title = %q, want %q", got, SectionFeatures)
	}
	if got := draft.Sections[1].Title; got != SectionBugFixes {
		t.Fatalf("sections[1].Title = %q, want %q", got, SectionBugFixes)
	}
	if got := draft.Sections[4].Title; got != SectionUncategorized {
		t.Fatalf("sections[4].Title = %q, want %q", got, SectionUncategorized)
	}
}

func TestRenderMarkdownIncludesFilesAndStats(t *testing.T) {
	draft := Build([]git.Commit{
		{
			ShortSHA: "abc1234",
			Subject:  "feat: add release notes command",
			Files:    []string{"cmd/release_notes.go", "internal/release/release.go"},
		},
	}, &git.DiffStats{FilesChanged: 2, Additions: 18, Deletions: 4}, Options{
		Title:         "v0.2.0 Draft",
		RevisionRange: "v0.1.0..HEAD",
	})

	rendered := RenderMarkdown(draft, true, true)
	for _, want := range []string{
		"# v0.2.0 Draft",
		"_Range: `v0.1.0..HEAD`_",
		"2 files changed, 18 insertions(+), 4 deletions(-)",
		"## Features",
		"- Add release notes command (`abc1234`)",
		"Files: `cmd/release_notes.go`, `internal/release/release.go`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, rendered)
		}
	}
}

func TestBuildOmitsEmptySectionsByDefault(t *testing.T) {
	draft := Build([]git.Commit{
		{ShortSHA: "abc1234", Subject: "docs: update release guide", Files: []string{"README.md"}},
	}, nil, Options{})

	if len(draft.Sections) != 1 {
		t.Fatalf("len(sections) = %d, want 1", len(draft.Sections))
	}
	if draft.Sections[0].Title != SectionDocumentation {
		t.Fatalf("section title = %q, want %q", draft.Sections[0].Title, SectionDocumentation)
	}
}
