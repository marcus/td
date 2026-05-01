package changelog

import (
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/git"
)

func testCommit(subject string) git.Commit {
	return git.Commit{
		SHA:     "1234567890abcdef",
		Subject: subject,
		Date:    time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestRenderConventionalCommitGrouping(t *testing.T) {
	commits := []git.Commit{
		testCommit("feat: add changelog command"),
		testCommit("fix(cli): handle empty range"),
		testCommit("docs: update release guide"),
		testCommit("refactor: simplify renderer"),
	}

	got, err := Render(commits, Options{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	wantParts := []string{
		"## Unreleased",
		"### Features\n- Add changelog command (1234567)",
		"### Bug Fixes\n- Handle empty range (1234567)",
		"### Documentation\n- Update release guide (1234567)",
		"### Improvements\n- Simplify renderer (1234567)",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("expected output to contain %q:\n%s", part, got)
		}
	}
}

func TestRenderLooseSubjectGrouping(t *testing.T) {
	commits := []git.Commit{
		testCommit("Add release notes preview"),
		testCommit("Fix release note typo"),
		testCommit("Document release workflow"),
		testCommit("Polish command help"),
	}

	got, err := Render(commits, Options{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	for _, part := range []string{
		"### Features\n- Add release notes preview",
		"### Bug Fixes\n- Fix release note typo",
		"### Documentation\n- Document release workflow",
		"### Improvements\n- Polish command help",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("expected output to contain %q:\n%s", part, got)
		}
	}
}

func TestRenderFiltersMergeAndAutosquashCommits(t *testing.T) {
	commits := []git.Commit{
		testCommit("Merge branch 'main'"),
		testCommit("fixup! feat: add changelog"),
		testCommit("squash! fix: handle range"),
		testCommit("feat: add changelog"),
	}

	got, err := Render(commits, Options{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if strings.Contains(got, "Merge branch") || strings.Contains(got, "fixup!") || strings.Contains(got, "squash!") {
		t.Fatalf("filtered commits appeared in output:\n%s", got)
	}
	if !strings.Contains(got, "- Add changelog") {
		t.Fatalf("expected relevant commit in output:\n%s", got)
	}
}

func TestRenderCapitalizationAndNormalization(t *testing.T) {
	got, err := Render([]git.Commit{testCommit("feat: add preview.")}, Options{
		Version: "v1.2.3",
		Date:    "2026-05-01",
	})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	if !strings.Contains(got, "## v1.2.3 - 2026-05-01") {
		t.Fatalf("expected version/date heading:\n%s", got)
	}
	if !strings.Contains(got, "- Add preview (1234567)") {
		t.Fatalf("expected normalized item:\n%s", got)
	}
}

func TestRenderFeatureFixPrecedenceOverDocumentationKeywords(t *testing.T) {
	got, err := Render([]git.Commit{
		testCommit("Fix README rendering"),
		testCommit("Add documentation export"),
	}, Options{})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	fixIndex := strings.Index(got, "### Bug Fixes")
	featureIndex := strings.Index(got, "### Features")
	docIndex := strings.Index(got, "### Documentation")
	if fixIndex == -1 || featureIndex == -1 {
		t.Fatalf("expected fix and feature sections:\n%s", got)
	}
	if docIndex != -1 {
		t.Fatalf("expected README/docs keyword commits to keep feature/fix categories:\n%s", got)
	}
}

func TestRenderNoRelevantCommits(t *testing.T) {
	_, err := Render([]git.Commit{testCommit("Merge branch 'main'")}, Options{})
	if err == nil {
		t.Fatal("expected error for no relevant commits")
	}
}
