package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func initReleaseNotesRepo(t *testing.T) string {
	t.Helper()

	dir := initGitRepo(t)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	commitReleaseNotesFile(t, dir, "README.md", "# Test\n", "chore: initial commit")
	return dir
}

func commitReleaseNotesFile(t *testing.T, dir, path, content, message string) {
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
}

func tagReleaseNotesRepo(t *testing.T, dir, tag string) {
	t.Helper()
	runGit(t, dir, "tag", tag)
}

func runReleaseNotesCommand(t *testing.T, dir string, flagPairs ...string) (string, error) {
	t.Helper()

	saveAndRestoreGlobals(t)
	baseDir := dir
	baseDirOverride = &baseDir

	_ = releaseNotesCmd.Flags().Set("from", "")
	_ = releaseNotesCmd.Flags().Set("to", "HEAD")
	_ = releaseNotesCmd.Flags().Set("version", "")
	_ = releaseNotesCmd.Flags().Set("date", "")

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

func TestReleaseNotesCommandFormatsStableMarkdown(t *testing.T) {
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "cmd/release_notes.go", "package cmd\n", "feat: add release notes command")
	commitReleaseNotesFile(t, dir, "internal/git/git.go", "package git\n", "fix: handle empty release range")
	commitReleaseNotesFile(t, dir, "docs/guides/releasing.md", "# Release\n", "refresh release guide")
	commitReleaseNotesFile(t, dir, "cmd/release_notes_test.go", "package cmd\n", "test: cover release notes command")
	commitReleaseNotesFile(t, dir, "internal/git/release.go", "package git\n", "refactor: simplify range parsing")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"from", "v0.1.0",
		"to", "HEAD",
		"version", "v1.2.3",
		"date", "2026-04-02",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	expected := `## [v1.2.3] - 2026-04-02

### Features
- Add release notes command

### Bug Fixes
- Handle empty release range

### Improvements
- Simplify range parsing

### Documentation
- Refresh release guide

### Testing
- Cover release notes command
`

	if output != expected {
		t.Fatalf("unexpected markdown output:\n%s", output)
	}
}

func TestReleaseNotesCommandDefaultsToLatestTag(t *testing.T) {
	dir := initReleaseNotesRepo(t)

	tagReleaseNotesRepo(t, dir, "v0.1.0")
	commitReleaseNotesFile(t, dir, "feature.txt", "feature\n", "feat: add old feature")
	tagReleaseNotesRepo(t, dir, "v0.2.0")
	commitReleaseNotesFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runReleaseNotesCommand(
		t,
		dir,
		"version", "v0.2.1",
		"date", "2026-04-02",
	)
	if err != nil {
		t.Fatalf("releaseNotesCmd.RunE returned error: %v", err)
	}

	if strings.Contains(output, "Add old feature") {
		t.Fatalf("expected output to use latest tag only, got:\n%s", output)
	}
	if !strings.Contains(output, "Patch release") {
		t.Fatalf("expected output to include latest fix, got:\n%s", output)
	}
}

func TestReleaseNotesCommandReturnsErrorForEmptyRange(t *testing.T) {
	dir := initReleaseNotesRepo(t)
	tagReleaseNotesRepo(t, dir, "v0.1.0")

	_, err := runReleaseNotesCommand(t, dir, "date", "2026-04-02")
	if err == nil {
		t.Fatal("expected empty range error")
	}
	if !strings.Contains(err.Error(), "no commits found in range v0.1.0..HEAD") {
		t.Fatalf("unexpected error: %v", err)
	}
}
