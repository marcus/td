package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func saveAndRestoreChangelogState(t *testing.T, now time.Time) {
	t.Helper()

	saveAndRestoreGlobals(t)
	origBaseDirOverride := baseDirOverride
	origNow := changelogNow

	changelogNow = func() time.Time { return now }
	t.Cleanup(func() {
		baseDirOverride = origBaseDirOverride
		changelogNow = origNow
	})
}

func runChangelogCommand(t *testing.T, dir string, flagPairs ...string) (string, error) {
	t.Helper()

	baseDir := dir
	baseDirOverride = &baseDir

	_ = changelogCmd.Flags().Set("from", "")
	_ = changelogCmd.Flags().Set("to", "HEAD")
	_ = changelogCmd.Flags().Set("version", "")
	_ = changelogCmd.Flags().Set("date", "")
	_ = changelogCmd.Flags().Set("include-meta", "false")

	for i := 0; i+1 < len(flagPairs); i += 2 {
		if err := changelogCmd.Flags().Set(flagPairs[i], flagPairs[i+1]); err != nil {
			t.Fatalf("failed to set --%s: %v", flagPairs[i], err)
		}
	}

	var output bytes.Buffer
	changelogCmd.SetOut(&output)

	err := changelogCmd.RunE(changelogCmd, nil)
	return output.String(), err
}

func runChangelogCommandFromCWD(t *testing.T, cwd string, flagPairs ...string) (string, error) {
	t.Helper()

	baseDirOverride = nil

	_ = changelogCmd.Flags().Set("from", "")
	_ = changelogCmd.Flags().Set("to", "HEAD")
	_ = changelogCmd.Flags().Set("version", "")
	_ = changelogCmd.Flags().Set("date", "")
	_ = changelogCmd.Flags().Set("include-meta", "false")

	for i := 0; i+1 < len(flagPairs); i += 2 {
		if err := changelogCmd.Flags().Set(flagPairs[i], flagPairs[i+1]); err != nil {
			t.Fatalf("failed to set --%s: %v", flagPairs[i], err)
		}
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	var output bytes.Buffer
	changelogCmd.SetOut(&output)

	err = changelogCmd.RunE(changelogCmd, nil)
	return output.String(), err
}

func TestChangelogCommandFormatsMarkdownWithOrderedSections(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "cmd/changelog.go", "package cmd\n", "feat: add changelog command")
	commitReleaseNotesFile(t, dir, "internal/changelog/changelog.go", "package changelog\n", "fix(parser): handle tagged release range")
	commitReleaseNotesFile(t, dir, "internal/changelog/render.go", "package changelog\n", "refactor: simplify changelog renderer")
	commitReleaseNotesFile(t, dir, "docs/changelog.md", "# Changelog\n", "docs: document changelog workflow")
	commitReleaseNotesFile(t, dir, "cmd/changelog_test.go", "package cmd\n", "test: cover changelog command")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	expected := `## [v0.2.0] - 2026-04-15

### Features
- Add changelog command

### Bug Fixes
- Parser: handle tagged release range

### Improvements
- Simplify changelog renderer
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestChangelogCommandDefaultsToLatestTag(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add old feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.1")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if strings.Contains(output, "Add old feature") {
		t.Fatalf("expected output to use latest tag only, got:\n%s", output)
	}
	if !strings.Contains(output, "Patch release") {
		t.Fatalf("expected output to include latest fix, got:\n%s", output)
	}
}

func TestChangelogCommandDefaultsFromPreviousTagWhenToIsReleaseTag(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runChangelogCommand(t, dir, "to", "v0.2.0", "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "Add changelog command") {
		t.Fatalf("expected output to include tagged release commit, got:\n%s", output)
	}
	if strings.Contains(output, "Patch release") {
		t.Fatalf("expected output to stop at the requested release tag, got:\n%s", output)
	}
}

func TestChangelogCommandUsesCurrentWorktreeForRepoResolution(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")

	wtPath := filepath.Join(t.TempDir(), "wt")
	runGit(t, dir, "worktree", "add", wtPath, "-b", "changelog-worktree")
	commitReleaseNotesFile(t, wtPath, "feature.txt", "feature\n", "feat: add worktree feature")

	t.Setenv("TD_WORK_DIR", dir)

	output, err := runChangelogCommandFromCWD(t, wtPath, "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "Add worktree feature") {
		t.Fatalf("expected output to use cwd worktree history, got:\n%s", output)
	}
}

func TestChangelogCommandUsesWorkDirFlagForRepoResolution(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add flagged work-dir feature")

	workDirFlag = dir

	output, err := runChangelogCommandFromCWD(t, t.TempDir(), "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "Add flagged work-dir feature") {
		t.Fatalf("expected output to use --work-dir repo, got:\n%s", output)
	}
}

func TestChangelogCommandRejectsInvalidVersion(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "version", "1.2.3")
	if err == nil || !strings.Contains(err.Error(), `invalid --version "1.2.3"`) {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestChangelogCommandRejectsInvalidDate(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "version", "v0.2.0", "date", "2026/04/15")
	if err == nil || !strings.Contains(err.Error(), `invalid --date "2026/04/15"`) {
		t.Fatalf("expected invalid date error, got %v", err)
	}
}

func TestChangelogCommandShowsEmptyRangeFallback(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")

	output, err := runChangelogCommand(t, dir, "from", "HEAD", "to", "HEAD", "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	expected := `## [v0.2.0] - 2026-04-15

_No committed changes found between HEAD and HEAD._
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestChangelogCommandShowsNoEntryFallbackByDefault(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "docs/changelog.md", "# Changelog\n", "docs: document changelog workflow")
	commitReleaseNotesFile(t, dir, "cmd/changelog_test.go", "package cmd\n", "test: cover changelog command")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "No changelog-worthy changes found between v0.1.0 and HEAD") {
		t.Fatalf("expected filtered-range message, got:\n%s", output)
	}
	if !strings.Contains(output, "--include-meta") {
		t.Fatalf("expected include-meta hint, got:\n%s", output)
	}
}

func TestChangelogCommandIncludesMetaWhenRequested(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "docs/changelog.md", "# Changelog\n", "docs: document changelog workflow")
	commitReleaseNotesFile(t, dir, "cmd/changelog_test.go", "package cmd\n", "test: cover changelog command")
	commitReleaseNotesFile(t, dir, ".github/workflows/release.yml", "name: release\n", "CI: fix release pipeline")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.0", "include-meta", "true")
	if err != nil {
		t.Fatalf("changelogCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "### Documentation") || !strings.Contains(output, "Document changelog workflow") {
		t.Fatalf("expected documentation section in output, got:\n%s", output)
	}
	if !strings.Contains(output, "### Other Changes") {
		t.Fatalf("expected other changes section in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Cover changelog command") || !strings.Contains(output, "Fix release pipeline") {
		t.Fatalf("expected meta entries in output, got:\n%s", output)
	}
}
