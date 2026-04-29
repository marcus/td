package releasenotes

import (
	"bytes"
	"strings"
	"testing"

	"github.com/marcus/td/internal/git"
)

func TestRenderConventionalCommits(t *testing.T) {
	commits := []git.Commit{
		commit("a1b2c3d", "feat(cli): add release notes"),
		commit("b2c3d4e", "fix: repair default range"),
		commit("c3d4e5f", "docs: update release guide"),
		commit("d4e5f6a", "refactor: simplify command setup"),
	}

	got := renderString(t, commits, Options{})
	want := `## Release Notes

### Features
- Add release notes (a1b2c3d)

### Bug Fixes
- Repair default range (b2c3d4e)

### Documentation
- Update release guide (c3d4e5f)

### Improvements
- Simplify command setup (d4e5f6a)
`
	if got != want {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestRenderLooseCommitSubjects(t *testing.T) {
	commits := []git.Commit{
		commit("a1b2c3d", "Add parser helper"),
		commit("b2c3d4e", "Fix empty output"),
		commit("c3d4e5f", "Document release workflow"),
		commit("d4e5f6a", "Tidy tests"),
	}

	got := renderString(t, commits, Options{})
	for _, want := range []string{
		"### Features\n- Add parser helper",
		"### Bug Fixes\n- Fix empty output",
		"### Documentation\n- Document release workflow",
		"### Improvements\n- Tidy tests",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in:\n%s", want, got)
		}
	}
}

func TestRenderBreakingChangeMarkers(t *testing.T) {
	commits := []git.Commit{
		commit("a1b2c3d", "feat!: remove old config"),
		{ShortSHA: "b2c3d4e", Subject: "fix: change storage", Body: "BREAKING CHANGE: database is migrated"},
	}

	got := renderString(t, commits, Options{})
	want := `## Release Notes

### Breaking Changes
- Remove old config (a1b2c3d)
- Change storage (b2c3d4e)
`
	if got != want {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestDraftFiltersNoisyCommits(t *testing.T) {
	commits := []git.Commit{
		commit("a1b2c3d", "Merge pull request #1 from branch"),
		commit("b2c3d4e", "fixup! feat: add release notes"),
		commit("c3d4e5f", "squash! fix: repair output"),
		commit("d4e5f6a", "feat: keep this"),
	}

	sections := Draft(commits)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if got := sections[0].Entries[0].Text; got != "Keep this" {
		t.Fatalf("expected only non-noisy commit, got %q", got)
	}
}

func TestRenderEmptyRelevantRange(t *testing.T) {
	var out bytes.Buffer
	err := Render(&out, []git.Commit{
		commit("a1b2c3d", "Merge branch main"),
		commit("b2c3d4e", "fixup! typo"),
	}, Options{})
	if err == nil {
		t.Fatal("expected empty relevant range error")
	}
	if !strings.Contains(err.Error(), "no release-note-worthy commits") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderVersionAndDateHeading(t *testing.T) {
	got := renderString(t, []git.Commit{commit("a1b2c3d", "feat: add notes")}, Options{
		Version: "v1.2.3",
		Date:    "2026-04-29",
	})
	if !strings.HasPrefix(got, "## v1.2.3 - 2026-04-29\n\n") {
		t.Fatalf("unexpected heading:\n%s", got)
	}
}

func TestDraftSectionOrderIsDeterministic(t *testing.T) {
	commits := []git.Commit{
		commit("d4e5f6a", "chore: improve tests"),
		commit("c3d4e5f", "docs: update docs"),
		commit("b2c3d4e", "fix: repair bug"),
		commit("a1b2c3d", "feat: add feature"),
		commit("e5f6a7b", "feat!: break config"),
	}

	got := renderString(t, commits, Options{})
	wantOrder := []string{
		"### Breaking Changes",
		"### Features",
		"### Bug Fixes",
		"### Documentation",
		"### Improvements",
	}
	last := -1
	for _, marker := range wantOrder {
		idx := strings.Index(got, marker)
		if idx == -1 {
			t.Fatalf("missing marker %q in:\n%s", marker, got)
		}
		if idx <= last {
			t.Fatalf("marker %q out of order in:\n%s", marker, got)
		}
		last = idx
	}
}

func renderString(t *testing.T, commits []git.Commit, opts Options) string {
	t.Helper()
	var out bytes.Buffer
	if err := Render(&out, commits, opts); err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	return out.String()
}

func commit(shortSHA, subject string) git.Commit {
	return git.Commit{ShortSHA: shortSHA, Subject: subject}
}
