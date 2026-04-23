package changelog

import (
	"errors"
	"strings"
	"testing"
	"time"

	gitutil "github.com/marcus/td/internal/git"
)

func TestRenderMarkdownGroupsConventionalAndLooseSubjects(t *testing.T) {
	commits := []gitutil.Commit{
		{Subject: "feat(cli): add changelog command"},
		{Subject: "fix: handle empty ranges"},
		{Subject: "docs: update release guide"},
		{Subject: "Improve command help text"},
		{Subject: "Merge pull request #42 from feature/changelog"},
	}

	got, err := RenderMarkdown(commits, Options{})
	if err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	wantParts := []string{
		"## Unreleased",
		"### Features\n- Add changelog command",
		"### Bug Fixes\n- Handle empty ranges",
		"### Documentation\n- Update release guide",
		"### Improvements\n- Improve command help text",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("expected output to contain %q, got:\n%s", part, got)
		}
	}
	if strings.Contains(got, "Merge pull request") {
		t.Fatalf("expected merge commit to be ignored, got:\n%s", got)
	}
}

func TestRenderMarkdownWithVersionAndDate(t *testing.T) {
	commits := []gitutil.Commit{
		{
			Subject:    "feat: add changelog command",
			CommitDate: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		},
	}

	got, err := RenderMarkdown(commits, Options{
		Version: "v0.9.0",
		Date:    "2026-04-23",
	})
	if err != nil {
		t.Fatalf("RenderMarkdown failed: %v", err)
	}

	if !strings.HasPrefix(got, "## [v0.9.0] - 2026-04-23\n\n") {
		t.Fatalf("unexpected heading:\n%s", got)
	}
}

func TestRenderMarkdownRequiresRelevantCommits(t *testing.T) {
	_, err := RenderMarkdown([]gitutil.Commit{
		{Subject: "Merge branch 'main' into feature/changelog"},
	}, Options{})
	if !errors.Is(err, ErrNoRelevantCommits) {
		t.Fatalf("expected ErrNoRelevantCommits, got %v", err)
	}
}
