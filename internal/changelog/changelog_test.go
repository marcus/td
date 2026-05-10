package changelog

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marcus/td/internal/git"
)

func TestRenderGroupsAndCleansCommits(t *testing.T) {
	commits := []git.Commit{
		{Subject: "feat: add rich text file input for issues"},
		{Subject: "fix: add missing -l shorthand for multivalue flags (#70)"},
		{Subject: "[td-377aae] Review commands: clarify stale concurrent transitions"},
		{Subject: "Merge pull request #91 from marcus/dispatch/td-527bd4-0006"},
		{Subject: "docs: Update changelog for v0.43.0"},
		{Subject: "[td-79fac9] Show colored status line at top of td issue detail modal"},
	}

	got, err := Render(commits, Options{
		Version: "v0.44.0",
		Date:    time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	wantSnippets := []string{
		"## [v0.44.0] - 2026-04-06",
		"### Features",
		"- Add rich text file input for issues",
		"- Show colored status line at top of td issue detail modal",
		"### Bug Fixes",
		"- Add missing -l shorthand for multivalue flags (#70)",
		"### Improvements",
		"- Review commands: clarify stale concurrent transitions",
	}

	for _, snippet := range wantSnippets {
		if !strings.Contains(got, snippet) {
			t.Fatalf("expected output to contain %q\nfull output:\n%s", snippet, got)
		}
	}

	unwanted := []string{
		"Merge pull request",
		"Update changelog for v0.43.0",
	}
	for _, snippet := range unwanted {
		if strings.Contains(got, snippet) {
			t.Fatalf("did not expect output to contain %q\nfull output:\n%s", snippet, got)
		}
	}
}

func TestRenderIncludesMetaWhenRequested(t *testing.T) {
	commits := []git.Commit{
		{Subject: "docs: document release workflow caveats"},
		{Subject: "test: cover explicit range overrides"},
		{Subject: "CI: fix release pipeline"},
	}

	got, err := Render(commits, Options{
		Version:     "v0.44.0",
		Date:        time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
		IncludeMeta: true,
	})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(got, "### Documentation") {
		t.Fatalf("expected documentation section in output:\n%s", got)
	}
	if !strings.Contains(got, "- Document release workflow caveats") {
		t.Fatalf("expected docs bullet in output:\n%s", got)
	}
	if !strings.Contains(got, "- Cover explicit range overrides") {
		t.Fatalf("expected test bullet in output:\n%s", got)
	}
	if !strings.Contains(got, "- Fix release pipeline") {
		t.Fatalf("expected CI bullet in output:\n%s", got)
	}
}

func TestRenderFiltersUppercaseMetaByDefault(t *testing.T) {
	commits := []git.Commit{
		{Subject: "CI: fix release pipeline"},
	}

	_, err := Render(commits, Options{
		Version: "v0.44.0",
		Date:    time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrNoEntries) {
		t.Fatalf("expected ErrNoEntries, got %v", err)
	}
}

func TestRenderReturnsErrNoEntriesWhenEverythingFiltered(t *testing.T) {
	commits := []git.Commit{
		{Subject: "docs: Update changelog for v0.43.0"},
		{Subject: "Merge pull request #91 from marcus/dispatch/td-527bd4-0006"},
		{Subject: "test: exercise explicit range overrides"},
	}

	_, err := Render(commits, Options{
		Version: "v0.44.0",
		Date:    time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrNoEntries) {
		t.Fatalf("expected ErrNoEntries, got %v", err)
	}
}
