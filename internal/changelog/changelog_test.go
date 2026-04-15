package changelog

import (
	"strings"
	"testing"
	"time"

	gitutil "github.com/marcus/td/internal/git"
)

func TestBuildDraftClassifiesConventionalCommits(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "feat: add changelog command"},
		{Subject: "fix(parser): handle empty range"},
		{Subject: "refactor: simplify release-note rendering"},
		{Subject: "misc polish for release output"},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	if len(draft.Sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(draft.Sections))
	}

	if draft.Sections[0].Title != sectionFeatures || draft.Sections[0].Entries[0] != "Add changelog command" {
		t.Fatalf("unexpected features section: %+v", draft.Sections[0])
	}
	if draft.Sections[1].Title != sectionBugFixes || draft.Sections[1].Entries[0] != "Parser: handle empty range" {
		t.Fatalf("unexpected bug fixes section: %+v", draft.Sections[1])
	}
	if draft.Sections[2].Title != sectionImprovements || draft.Sections[2].Entries[0] != "Simplify release-note rendering" {
		t.Fatalf("unexpected improvements section: %+v", draft.Sections[2])
	}
	if draft.Sections[3].Title != sectionOtherChanges || draft.Sections[3].Entries[0] != "Misc polish for release output" {
		t.Fatalf("unexpected other changes section: %+v", draft.Sections[3])
	}
}

func TestBuildDraftUsesFreeformVerbHeuristics(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "[td-79fac9] Show colored status line at top of td issue detail modal"},
		{Subject: "Fix closed_at timestamp to use current time on approve/close (#55)"},
		{Subject: "[td-377aae] Review commands: clarify stale concurrent transitions"},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	if got := draft.Sections[0].Entries[0]; got != "Show colored status line at top of td issue detail modal" {
		t.Fatalf("unexpected feature entry: %q", got)
	}
	if got := draft.Sections[1].Entries[0]; got != "Fix closed_at timestamp to use current time on approve/close (#55)" {
		t.Fatalf("unexpected bug fix entry: %q", got)
	}
	if got := draft.Sections[2].Entries[0]; got != "Review commands: clarify stale concurrent transitions" {
		t.Fatalf("unexpected improvement entry: %q", got)
	}
}

func TestBuildDraftExcludesMergeAndReleaseHousekeeping(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "Merge pull request #91 from marcus/dispatch/td-527bd4-0006"},
		{Subject: "docs: Update changelog for v0.43.0"},
		{Subject: "chore: release v0.44.0"},
		{Subject: "feat: add changelog command"},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	if len(draft.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(draft.Sections))
	}
	if draft.Sections[0].Title != sectionFeatures || draft.Sections[0].Entries[0] != "Add changelog command" {
		t.Fatalf("unexpected remaining section: %+v", draft.Sections[0])
	}
}

func TestBuildDraftPreservesSectionOrder(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "chore: tidy release helper"},
		{Subject: "docs: explain changelog flow"},
		{Subject: "refactor: simplify parser"},
		{Subject: "fix: handle tagged releases"},
		{Subject: "feat: add changelog command"},
	}, draftOptions{
		RepoRoot:    "/tmp/repo",
		FromRef:     "v0.1.0",
		ToRef:       "HEAD",
		Version:     "v0.2.0",
		Date:        time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
		IncludeMeta: true,
	})

	got := []string{}
	for _, section := range draft.Sections {
		got = append(got, section.Title)
	}

	want := []string{
		sectionFeatures,
		sectionBugFixes,
		sectionImprovements,
		sectionDocumentation,
		sectionOtherChanges,
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected section order: got %v want %v", got, want)
	}
}

func TestBuildDraftCleansTaskPrefixesAndBulletText(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "[td-a7ff5e] task: feat: add rich text file input for issues (td-a7ff5e)."},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	if got := draft.Sections[0].Entries[0]; got != "Add rich text file input for issues" {
		t.Fatalf("unexpected cleaned bullet: %q", got)
	}
}

func TestBuildDraftFiltersMetaByDefault(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "docs: document release workflow"},
		{Subject: "test: cover tagged release range"},
		{Subject: "CI: fix pipeline", Files: []string{".github/workflows/release.yml"}},
		{Subject: "chore: tidy generated files"},
	}, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	if len(draft.Sections) != 0 {
		t.Fatalf("expected no visible sections, got %+v", draft.Sections)
	}

	expected := "## [v0.2.0] - 2026-04-15\n\n_No changelog-worthy changes found between v0.1.0 and HEAD. Re-run with --include-meta to include documentation, test, CI, and chore commits._\n"
	if got := draft.Markdown(); got != expected {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestBuildDraftIncludesMetaWhenRequested(t *testing.T) {
	draft := buildDraft([]gitutil.Commit{
		{Subject: "docs: explain changelog flow"},
		{Subject: "test: cover tagged release range"},
		{Subject: "CI: fix pipeline", Files: []string{".github/workflows/release.yml"}},
		{Subject: "chore: tidy generated files"},
	}, draftOptions{
		RepoRoot:    "/tmp/repo",
		FromRef:     "v0.1.0",
		ToRef:       "HEAD",
		Version:     "v0.2.0",
		Date:        time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
		IncludeMeta: true,
	})

	if len(draft.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(draft.Sections))
	}
	if draft.Sections[0].Title != sectionDocumentation || draft.Sections[0].Entries[0] != "Explain changelog flow" {
		t.Fatalf("unexpected documentation section: %+v", draft.Sections[0])
	}
	if draft.Sections[1].Title != sectionOtherChanges {
		t.Fatalf("unexpected other changes section: %+v", draft.Sections[1])
	}
	if got := strings.Join(draft.Sections[1].Entries, "\n"); !strings.Contains(got, "Cover tagged release range") || !strings.Contains(got, "Fix pipeline") || !strings.Contains(got, "Tidy generated files") {
		t.Fatalf("unexpected other changes entries: %s", got)
	}
}

func TestDraftMarkdownHandlesEmptyRange(t *testing.T) {
	draft := buildDraft(nil, draftOptions{
		RepoRoot: "/tmp/repo",
		FromRef:  "v0.1.0",
		ToRef:    "HEAD",
		Version:  "v0.2.0",
		Date:     time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC),
	})

	expected := "## [v0.2.0] - 2026-04-15\n\n_No committed changes found between v0.1.0 and HEAD._\n"
	if got := draft.Markdown(); got != expected {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestMatchingSemverTagRefOnlyMatchesExplicitTagRefs(t *testing.T) {
	tags := []string{"v0.2.1", "v0.2.0"}

	if got := matchingSemverTagRef("v0.2.1", tags); got != "v0.2.1" {
		t.Fatalf("expected direct tag match, got %q", got)
	}
	if got := matchingSemverTagRef("refs/tags/v0.2.0", tags); got != "v0.2.0" {
		t.Fatalf("expected refs/tags match, got %q", got)
	}
	if got := matchingSemverTagRef("deadbeef", tags); got != "" {
		t.Fatalf("expected commit-like ref to not match, got %q", got)
	}
}
