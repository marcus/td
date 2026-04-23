package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initChangelogRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGitCommand(t, dir, "init")
	runGitCommand(t, dir, "config", "user.email", "test@example.com")
	runGitCommand(t, dir, "config", "user.name", "Test User")

	writeRepoFile(t, dir, "README.md", "# test\n")
	runGitCommand(t, dir, "add", "README.md")
	runGitCommand(t, dir, "commit", "-m", "Initial commit")

	return dir
}

func runGitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func commitRepoFile(t *testing.T, dir, name, contents, message string) string {
	t.Helper()
	writeRepoFile(t, dir, name, contents)
	runGitCommand(t, dir, "add", name)
	runGitCommand(t, dir, "commit", "-m", message)
	return runGitCommand(t, dir, "rev-parse", "HEAD")
}

func runChangelogCommand(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	saveAndRestoreGlobals(t)
	baseDir := dir
	baseDirOverride = &baseDir

	_ = changelogCmd.Flags().Set("from", "")
	_ = changelogCmd.Flags().Set("to", "HEAD")
	_ = changelogCmd.Flags().Set("version", "")
	_ = changelogCmd.Flags().Set("date", "")

	for i := 0; i < len(args); i += 2 {
		if err := changelogCmd.Flags().Set(strings.TrimPrefix(args[i], "--"), args[i+1]); err != nil {
			t.Fatalf("setting flag %s failed: %v", args[i], err)
		}
	}

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := changelogCmd.RunE(changelogCmd, nil)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	return output.String(), runErr
}

func TestChangelogCommandUsesLatestTagByDefault(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")
	commitRepoFile(t, dir, "fix.txt", "fix\n", "fix: handle empty range")

	output, err := runChangelogCommand(t, dir)
	if err != nil {
		t.Fatalf("runChangelogCommand failed: %v", err)
	}

	if !strings.Contains(output, "## Unreleased") {
		t.Fatalf("expected unreleased heading, got:\n%s", output)
	}
	if !strings.Contains(output, "### Features\n- Add changelog command") {
		t.Fatalf("expected feature section, got:\n%s", output)
	}
	if !strings.Contains(output, "### Bug Fixes\n- Handle empty range") {
		t.Fatalf("expected bug fix section, got:\n%s", output)
	}
	if strings.Contains(output, "Initial commit") {
		t.Fatalf("expected commits before latest tag to be excluded, got:\n%s", output)
	}
}

func TestChangelogCommandSupportsExplicitRange(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	midSHA := commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")
	commitRepoFile(t, dir, "docs.md", "docs\n", "docs: update release guide")
	commitRepoFile(t, dir, "notes.txt", "notes\n", "Improve release messaging")

	output, err := runChangelogCommand(t, dir, "--from", "v0.1.0", "--to", midSHA)
	if err != nil {
		t.Fatalf("runChangelogCommand failed: %v", err)
	}

	if !strings.Contains(output, "- Add changelog command") {
		t.Fatalf("expected explicit range commit, got:\n%s", output)
	}
	if strings.Contains(output, "Update release guide") || strings.Contains(output, "Improve release messaging") {
		t.Fatalf("expected output to stop at explicit --to ref, got:\n%s", output)
	}
}

func TestChangelogCommandUsesNearestReachableTagByDefault(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v2.0.0")
	commitRepoFile(t, dir, "prep.txt", "prep\n", "feat: release prep")
	runGitCommand(t, dir, "tag", "v1.5.0")
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	output, err := runChangelogCommand(t, dir)
	if err != nil {
		t.Fatalf("runChangelogCommand failed: %v", err)
	}

	if !strings.Contains(output, "- Add changelog command") {
		t.Fatalf("expected commit after nearest tag, got:\n%s", output)
	}
	if strings.Contains(output, "Release prep") {
		t.Fatalf("expected commits before nearest tag to be excluded, got:\n%s", output)
	}
}

func TestChangelogCommandHandlesMixedCommitStyles(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat(cli): add changelog command")
	commitRepoFile(t, dir, "docs.md", "docs\n", "Document release workflow")
	commitRepoFile(t, dir, "notes.txt", "notes\n", "Improve command help text")

	output, err := runChangelogCommand(t, dir, "--version", "v0.2.0", "--date", "2026-04-23")
	if err != nil {
		t.Fatalf("runChangelogCommand failed: %v", err)
	}

	wantParts := []string{
		"## [v0.2.0] - 2026-04-23",
		"### Features\n- Add changelog command",
		"### Documentation\n- Document release workflow",
		"### Improvements\n- Improve command help text",
	}
	for _, part := range wantParts {
		if !strings.Contains(output, part) {
			t.Fatalf("expected output to contain %q, got:\n%s", part, output)
		}
	}
}

func TestChangelogCommandIgnoresAutosquashCommits(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add parser")
	commitRepoFile(t, dir, "feature.txt", "feature updated\n", "fixup! feat: add parser")

	output, err := runChangelogCommand(t, dir)
	if err != nil {
		t.Fatalf("runChangelogCommand failed: %v", err)
	}

	if strings.Count(output, "- Add parser\n") != 1 {
		t.Fatalf("expected a single feature bullet, got:\n%s", output)
	}
}

func TestChangelogCommandErrorsOnEmptyRange(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")

	_, err := runChangelogCommand(t, dir)
	if err == nil {
		t.Fatal("expected error for empty range")
	}
	if !strings.Contains(err.Error(), "no commits found in range v0.1.0..HEAD") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChangelogCommandRequiresFromWhenNoTagsExist(t *testing.T) {
	dir := initChangelogRepo(t)
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir)
	if err == nil {
		t.Fatal("expected error when no tags exist")
	}
	if !strings.Contains(err.Error(), "could not determine default changelog range") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "use --from <ref>") {
		t.Fatalf("expected helpful hint, got: %v", err)
	}
}

func TestChangelogCommandRejectsDateWithoutVersion(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	commitRepoFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "--date", "2026-04-23")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "--date requires --version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
