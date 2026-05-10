package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initReleaseNotesRepo(t *testing.T) string {
	t.Helper()

	dir := initGitRepo(t)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	commitReleaseNotesFile(t, dir, "README.md", "# Test\n", "chore: initial commit")
	return dir
}

func commitReleaseNotesFile(t *testing.T, dir, path, content, message string) string {
	t.Helper()

	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	runGit(t, dir, "add", path)
	runGit(t, dir, "commit", "-m", message)

	out, err := execGit(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse failed: %v", err)
	}
	return strings.TrimSpace(out)
}

func tagReleaseNotesRepo(t *testing.T, dir, tag string) {
	t.Helper()
	runGit(t, dir, "tag", "-a", tag, "-m", "Release "+tag)
}

func execGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func saveAndRestoreReleaseNotesState(t *testing.T, now time.Time) {
	t.Helper()

	saveAndRestoreGlobals(t)
	origBaseDirOverride := baseDirOverride
	origNow := releaseNotesNow

	releaseNotesNow = func() time.Time { return now }
	t.Cleanup(func() {
		baseDirOverride = origBaseDirOverride
		releaseNotesNow = origNow
	})
}

func runReleaseNotesCommand(t *testing.T, dir string, flagPairs ...string) (string, error) {
	t.Helper()

	baseDir := dir
	baseDirOverride = &baseDir

	_ = releaseNotesCmd.Flags().Set("from", "")
	_ = releaseNotesCmd.Flags().Set("to", "HEAD")
	_ = releaseNotesCmd.Flags().Set("version", "")

	for i := 0; i+1 < len(flagPairs); i += 2 {
		if err := releaseNotesCmd.Flags().Set(flagPairs[i], flagPairs[i+1]); err != nil {
			t.Fatalf("failed to set --%s: %v", flagPairs[i], err)
		}
	}

	var output bytes.Buffer
	releaseNotesCmd.SetOut(&output)

	err := releaseNotesCmd.RunE(releaseNotesCmd, nil)
	return output.String(), err
}

func runReleaseNotesCommandFromCWD(t *testing.T, cwd string, flagPairs ...string) (string, error) {
	t.Helper()

	baseDirOverride = nil

	_ = releaseNotesCmd.Flags().Set("from", "")
	_ = releaseNotesCmd.Flags().Set("to", "HEAD")
	_ = releaseNotesCmd.Flags().Set("version", "")

	for i := 0; i+1 < len(flagPairs); i += 2 {
		if err := releaseNotesCmd.Flags().Set(flagPairs[i], flagPairs[i+1]); err != nil {
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
	releaseNotesCmd.SetOut(&output)

	err = releaseNotesCmd.RunE(releaseNotesCmd, nil)
	return output.String(), err
}

func TestReleaseNotesCommandFormatsMarkdownWithOrderedSections(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "cmd/release_notes.go", "package cmd\n", "feat: add release notes command")
	commitReleaseNotesFile(t, dir, "internal/releasenotes/draft.go", "package releasenotes\n", "fix(parser): handle empty release range")
	commitReleaseNotesFile(t, dir, "docs/guides/releasing-new-version.md", "# Release\n", "refresh release guide")
	commitReleaseNotesFile(t, dir, "cmd/release_notes_test.go", "package cmd\n", "test: cover release notes command")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"from", "v0.1.0",
		"to", "HEAD",
		"version", "v0.2.0",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	expected := `## [v0.2.0] - 2026-04-10

### Features
- add release notes command

### Bug Fixes
- parser: handle empty release range

### Documentation
- refresh release guide

### Other Changes
- cover release notes command
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestReleaseNotesCommandDefaultsToLatestTag(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add old feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runReleaseNotesCommand(t, dir, "version", "v0.2.1")
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if strings.Contains(output, "add old feature") {
		t.Fatalf("expected output to use latest tag only, got:\n%s", output)
	}
	if !strings.Contains(output, "patch release") {
		t.Fatalf("expected output to include latest fix, got:\n%s", output)
	}
}

func TestReleaseNotesCommandHonorsToFlag(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	featureSHA := commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add release notes command")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"from", "v0.1.0",
		"to", featureSHA,
		"version", "v0.2.0",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "add release notes command") {
		t.Fatalf("expected output to include feature commit, got:\n%s", output)
	}
	if strings.Contains(output, "patch release") {
		t.Fatalf("expected output to stop at --to commit, got:\n%s", output)
	}
}

func TestReleaseNotesCommandDefaultsFromPreviousTagWhenToIsReleaseTag(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add release notes command")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"to", "v0.2.0",
		"version", "v0.2.0",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "add release notes command") {
		t.Fatalf("expected output to include tagged release commit, got:\n%s", output)
	}
	if strings.Contains(output, "patch release") {
		t.Fatalf("expected output to stop at the requested release tag, got:\n%s", output)
	}
}

func TestReleaseNotesCommandDefaultsToLatestRetaggedVersion(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add initial feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	tagReleaseNotesRepo(t, dir, "v0.2.1")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runReleaseNotesCommand(t, dir, "version", "v0.2.2")
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if strings.Contains(output, "add initial feature") {
		t.Fatalf("expected default range to start from the latest retagged version, got:\n%s", output)
	}
	if !strings.Contains(output, "patch release") {
		t.Fatalf("expected output to include unreleased patch, got:\n%s", output)
	}
}

func TestReleaseNotesCommandTreatsRetaggedReleaseAsEmptyRange(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add initial feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	tagReleaseNotesRepo(t, dir, "v0.2.1")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"to", "v0.2.1",
		"version", "v0.2.1",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	expected := `## [v0.2.1] - 2026-04-10

_No committed changes found between v0.2.0 and v0.2.1._
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestReleaseNotesCommandDoesNotTreatTaggedCommitSHAAsReleaseTag(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	releaseSHA := commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add initial feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"to", releaseSHA,
		"version", "vTEST",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	expected := "## [vTEST] - 2026-04-10\n\n_No committed changes found between v0.2.0 and " + releaseSHA + "._\n"
	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestReleaseNotesCommandShowsEmptyRangeFallback(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")

	output, err := runReleaseNotesCommand(t, dir, "from", "v0.1.0")
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	expected := `## [Unreleased] - 2026-04-10

_No committed changes found between v0.1.0 and HEAD._
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestReleaseNotesCommandPrefersCurrentWorktreeOverEnvVar(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")

	wtPath := filepath.Join(t.TempDir(), "wt")
	runGit(t, dir, "worktree", "add", wtPath, "-b", "release-notes-worktree")
	commitReleaseNotesFile(t, wtPath, "feature.txt", "feature\n", "feat: add worktree feature")

	t.Setenv("TD_WORK_DIR", dir)

	output, err := runReleaseNotesCommandFromCWD(t, wtPath, "version", "v0.2.0")
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "add worktree feature") {
		t.Fatalf("expected output to use cwd worktree history, got:\n%s", output)
	}
	if strings.Contains(output, "_No committed changes found") {
		t.Fatalf("expected non-empty worktree draft, got:\n%s", output)
	}
}

func TestReleaseNotesCommandFallsBackToEnvVarWhenCWDIsNotRepo(t *testing.T) {
	saveAndRestoreReleaseNotesState(t, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC))
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add env-backed release notes")

	t.Setenv("TD_WORK_DIR", dir)

	output, err := runReleaseNotesCommandFromCWD(t, t.TempDir(), "version", "v0.2.0")
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if !strings.Contains(output, "add env-backed release notes") {
		t.Fatalf("expected output to use TD_WORK_DIR fallback repo, got:\n%s", output)
	}
	if strings.Contains(output, "_No committed changes found") {
		t.Fatalf("expected non-empty env fallback draft, got:\n%s", output)
	}
}
