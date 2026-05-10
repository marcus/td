package releasenotes

import (
	"testing"
	"time"

	gitutil "github.com/marcus/td/internal/git"
)

func TestBuildDraftClassifiesAndOrdersSections(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{SHA: "aaaaaaa", Subject: "feat: add release notes command", Files: []string{"cmd/release_notes.go"}},
		{SHA: "bbbbbbb", Subject: "fix(parser): handle empty release range", Files: []string{"internal/releasenotes/draft.go"}},
		{SHA: "ccccccc", Subject: "refresh release guide", Files: []string{"docs/guides/releasing-new-version.md"}},
		{SHA: "ddddddd", Subject: "test: cover release notes command", Files: []string{"cmd/release_notes_test.go"}},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 10, 9, 30, 0, 0, time.UTC),
	})

	if len(draft.Sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(draft.Sections))
	}

	if draft.Sections[0].Title != sectionFeatures || draft.Sections[0].Entries[0] != "add release notes command" {
		t.Fatalf("unexpected features section: %+v", draft.Sections[0])
	}
	if draft.Sections[1].Title != sectionBugFixes || draft.Sections[1].Entries[0] != "parser: handle empty release range" {
		t.Fatalf("unexpected bug fixes section: %+v", draft.Sections[1])
	}
	if draft.Sections[2].Title != sectionDocumentation || draft.Sections[2].Entries[0] != "refresh release guide" {
		t.Fatalf("unexpected documentation section: %+v", draft.Sections[2])
	}
	if draft.Sections[3].Title != sectionOtherChanges || draft.Sections[3].Entries[0] != "cover release notes command" {
		t.Fatalf("unexpected other changes section: %+v", draft.Sections[3])
	}
}

func TestClassifyCommitStripsCommonNoise(t *testing.T) {
	title, entry := classifyCommit(gitutil.Commit{
		SHA:     "abcdef1",
		Subject: "[td-527bd4] refresh release guide (td-527bd4) (#91)",
		Files:   []string{"docs/guides/releasing-new-version.md"},
	})

	if title != sectionDocumentation {
		t.Fatalf("expected documentation section, got %q", title)
	}
	if entry != "refresh release guide" {
		t.Fatalf("expected cleaned entry, got %q", entry)
	}
}

func TestDraftMarkdownHandlesEmptyRange(t *testing.T) {
	draft := buildDraft(nil, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Date:     time.Date(2026, 4, 10, 9, 30, 0, 0, time.UTC),
	})

	expected := `## [Unreleased] - 2026-04-10

_No committed changes found between v0.1.0 and HEAD._
`

	if got := draft.Markdown(); got != expected {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}
