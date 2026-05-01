package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initChangelogTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")
	changelogCommit(t, dir, "README.md", "# Test\n", "chore: initial")
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func changelogCommit(t *testing.T, dir, file, content, subject string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-m", subject)
	return runGitCmd(t, dir, "rev-parse", "HEAD")
}

func runChangelogCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cmd := newChangelogCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), err
}

func TestChangelogDefaultLatestTagRange(t *testing.T) {
	dir := initChangelogTestRepo(t)
	runGitCmd(t, dir, "tag", "v1.0.0")
	changelogCommit(t, dir, "one.txt", "one", "feat: first feature")
	runGitCmd(t, dir, "tag", "v1.1.0")
	changelogCommit(t, dir, "two.txt", "two", "fix: second fix")

	out, err := runChangelogCmd(t, dir)
	if err != nil {
		t.Fatalf("changelog failed: %v", err)
	}
	if strings.Contains(out, "First feature") {
		t.Fatalf("default range included commit before latest tag:\n%s", out)
	}
	if !strings.Contains(out, "Second fix") {
		t.Fatalf("default range missed commit after latest tag:\n%s", out)
	}
}

func TestChangelogExplicitFromToRange(t *testing.T) {
	dir := initChangelogTestRepo(t)
	runGitCmd(t, dir, "tag", "v1.0.0")
	changelogCommit(t, dir, "one.txt", "one", "feat: first feature")
	runGitCmd(t, dir, "tag", "v1.1.0")
	changelogCommit(t, dir, "two.txt", "two", "fix: second fix")

	out, err := runChangelogCmd(t, dir, "--from", "v1.0.0", "--to", "v1.1.0")
	if err != nil {
		t.Fatalf("changelog failed: %v", err)
	}
	if !strings.Contains(out, "First feature") || strings.Contains(out, "Second fix") {
		t.Fatalf("explicit range output wrong:\n%s", out)
	}
}

func TestChangelogNearestReachableTagSelection(t *testing.T) {
	dir := initChangelogTestRepo(t)
	mainBranch := runGitCmd(t, dir, "branch", "--show-current")
	runGitCmd(t, dir, "tag", "v1.0.0")
	runGitCmd(t, dir, "checkout", "-b", "side")
	changelogCommit(t, dir, "side.txt", "side", "feat: side feature")
	runGitCmd(t, dir, "tag", "v2.0.0")
	runGitCmd(t, dir, "checkout", mainBranch)
	changelogCommit(t, dir, "main.txt", "main", "feat: main feature")

	out, err := runChangelogCmd(t, dir)
	if err != nil {
		t.Fatalf("changelog failed: %v", err)
	}
	if !strings.Contains(out, "Main feature") {
		t.Fatalf("expected main feature from reachable tag:\n%s", out)
	}
	if strings.Contains(out, "Side feature") {
		t.Fatalf("included commit from unreachable tag:\n%s", out)
	}
}

func TestChangelogVersionDateHeading(t *testing.T) {
	dir := initChangelogTestRepo(t)
	runGitCmd(t, dir, "tag", "v1.0.0")
	changelogCommit(t, dir, "one.txt", "one", "feat: release notes")

	out, err := runChangelogCmd(t, dir, "--version", "v1.1.0", "--date", "2026-05-01")
	if err != nil {
		t.Fatalf("changelog failed: %v", err)
	}
	if !strings.Contains(out, "## v1.1.0 - 2026-05-01") {
		t.Fatalf("missing version/date heading:\n%s", out)
	}
}

func TestChangelogEmptyRangeError(t *testing.T) {
	dir := initChangelogTestRepo(t)
	runGitCmd(t, dir, "tag", "v1.0.0")

	_, err := runChangelogCmd(t, dir)
	if err == nil || !strings.Contains(err.Error(), "no relevant commits") {
		t.Fatalf("expected empty range error, got %v", err)
	}
}

func TestChangelogNoTagGuidance(t *testing.T) {
	dir := initChangelogTestRepo(t)
	changelogCommit(t, dir, "one.txt", "one", "feat: release notes")

	_, err := runChangelogCmd(t, dir)
	if err == nil || !strings.Contains(err.Error(), "pass --from explicitly") {
		t.Fatalf("expected no-tag guidance, got %v", err)
	}
}

func TestChangelogDateValidation(t *testing.T) {
	dir := initChangelogTestRepo(t)
	runGitCmd(t, dir, "tag", "v1.0.0")
	changelogCommit(t, dir, "one.txt", "one", "feat: release notes")

	_, err := runChangelogCmd(t, dir, "--date", "05-01-2026")
	if err == nil || !strings.Contains(err.Error(), "--date requires --version") {
		t.Fatalf("expected date requires version error, got %v", err)
	}

	_, err = runChangelogCmd(t, dir, "--version", "v1.1.0", "--date", "05-01-2026")
	if err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("expected date format error, got %v", err)
	}
}
