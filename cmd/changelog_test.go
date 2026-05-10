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

	writeAndCommit(t, dir, "README.md", "# Test\n", "chore: initial commit")

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

func writeAndCommit(t *testing.T, dir, fileName, contents, subject string, body ...string) string {
	t.Helper()

	fullPath := filepath.Join(dir, fileName)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir for %s failed: %v", fileName, err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s failed: %v", fileName, err)
	}
	runGitCommand(t, dir, "add", fileName)

	args := []string{"commit", "-m", subject}
	for _, paragraph := range body {
		args = append(args, "-m", paragraph)
	}
	runGitCommand(t, dir, args...)

	return runGitCommand(t, dir, "rev-parse", "HEAD")
}

func currentBranchName(t *testing.T, dir string) string {
	t.Helper()
	return runGitCommand(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

func runChangelogCommand(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	saveAndRestoreGlobals(t)
	baseDir := dir
	baseDirOverride = &baseDir

	command := newChangelogCmd()
	command.SilenceUsage = true
	command.SetArgs(args)

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := command.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	return output.String(), runErr
}

func TestChangelogCommandOutputsMarkdown(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	writeAndCommit(t, dir, "feature.txt", "feature\n", "feat: add changelog command")
	writeAndCommit(t, dir, "fix.txt", "fix\n", "fix: handle explicit range overrides (#70)")
	writeAndCommit(t, dir, "docs.txt", "docs\n", "docs: document release workflow")

	out, err := runChangelogCommand(t, dir,
		"--version", "v0.2.0",
		"--date", "2026-04-06",
	)
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}

	wantSnippets := []string{
		"## [v0.2.0] - 2026-04-06",
		"### Features",
		"- Add changelog command",
		"### Bug Fixes",
		"- Handle explicit range overrides (#70)",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(out, snippet) {
			t.Fatalf("expected output to contain %q\nfull output:\n%s", snippet, out)
		}
	}

	if strings.Contains(out, "document release workflow") {
		t.Fatalf("expected docs commit to be filtered by default:\n%s", out)
	}
}

func TestChangelogCommandExplicitRangeOverride(t *testing.T) {
	dir := initChangelogRepo(t)
	base := runGitCommand(t, dir, "rev-parse", "HEAD")
	first := writeAndCommit(t, dir, "feature.txt", "feature\n", "feat: add scoped entry")
	writeAndCommit(t, dir, "fix.txt", "fix\n", "fix: should stay out of explicit range")

	out, err := runChangelogCommand(t, dir,
		"--version", "v0.2.0",
		"--date", "2026-04-06",
		"--from", base,
		"--to", first,
	)
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}

	if !strings.Contains(out, "Add scoped entry") {
		t.Fatalf("expected explicit range commit in output:\n%s", out)
	}
	if strings.Contains(out, "should stay out of explicit range") {
		t.Fatalf("did not expect later commit in output:\n%s", out)
	}
}

func TestChangelogCommandDefaultsFromLatestReachableSemverTag(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")

	writeAndCommit(t, dir, "feature-a.txt", "feature a\n", "feat: add commit on main before release branch")

	branch := currentBranchName(t, dir)
	runGitCommand(t, dir, "checkout", "-b", "release/v0.2.0")
	writeAndCommit(t, dir, "release.txt", "release\n", "fix: release-branch-only patch")
	runGitCommand(t, dir, "tag", "v0.2.0")
	runGitCommand(t, dir, "checkout", branch)

	writeAndCommit(t, dir, "feature-b.txt", "feature b\n", "fix: add post-release bug fix")

	out, err := runChangelogCommand(t, dir,
		"--version", "v0.3.0",
		"--date", "2026-04-06",
	)
	if err != nil {
		t.Fatalf("command returned error: %v", err)
	}

	wantSnippets := []string{
		"- Add commit on main before release branch",
		"- Add post-release bug fix",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(out, snippet) {
			t.Fatalf("expected output to contain %q\nfull output:\n%s", snippet, out)
		}
	}

	if strings.Contains(out, "release-branch-only patch") {
		t.Fatalf("did not expect release branch commit in output:\n%s", out)
	}
}

func TestChangelogCommandErrorsWithoutVersion(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	writeAndCommit(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "--date", "2026-04-06")
	if err == nil || !strings.Contains(err.Error(), "--version is required") {
		t.Fatalf("expected missing version error, got %v", err)
	}
}

func TestChangelogCommandErrorsOnEmptyRange(t *testing.T) {
	dir := initChangelogRepo(t)

	_, err := runChangelogCommand(t, dir,
		"--version", "v0.2.0",
		"--date", "2026-04-06",
		"--from", "HEAD",
		"--to", "HEAD",
	)
	if err == nil || !strings.Contains(err.Error(), "no commits found in range HEAD..HEAD") {
		t.Fatalf("expected empty range error, got %v", err)
	}
}

func TestChangelogCommandErrorsWithoutTagsWhenFromUnset(t *testing.T) {
	dir := initChangelogRepo(t)
	writeAndCommit(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir,
		"--version", "v0.2.0",
		"--date", "2026-04-06",
	)
	if err == nil || !strings.Contains(err.Error(), "no semver tags found; use --from to specify a starting revision") {
		t.Fatalf("expected no tags error, got %v", err)
	}
}

func TestChangelogCommandFiltersUppercaseMetaByDefault(t *testing.T) {
	dir := initChangelogRepo(t)
	runGitCommand(t, dir, "tag", "v0.1.0")
	writeAndCommit(t, dir, ".github/workflows/ci.yml", "name: ci\n", "CI: fix pipeline")

	_, err := runChangelogCommand(t, dir,
		"--version", "v0.2.0",
		"--date", "2026-04-06",
	)
	if err == nil || !strings.Contains(err.Error(), "no changelog-worthy commits found in range v0.1.0..HEAD") {
		t.Fatalf("expected filtered-meta error, got %v", err)
	}
}
