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

func saveAndRestoreChangelogState(t *testing.T, now time.Time) {
	t.Helper()
	saveAndRestoreGlobals(t)

	origBaseDirOverride := baseDirOverride
	origNow := changelogNow
	t.Cleanup(func() {
		baseDirOverride = origBaseDirOverride
		changelogNow = origNow
		changelogCmd.SetOut(nil)
	})

	changelogNow = func() time.Time { return now }
}

func resetChangelogFlags(t *testing.T) {
	t.Helper()
	defaults := map[string]string{
		"from":    "",
		"to":      "HEAD",
		"version": "",
		"date":    "",
	}
	for name, value := range defaults {
		if err := changelogCmd.Flags().Set(name, value); err != nil {
			t.Fatalf("failed to reset --%s: %v", name, err)
		}
		changelogCmd.Flags().Lookup(name).Changed = false
	}
}

func runChangelogCommand(t *testing.T, dir string, flagPairs ...string) (string, error) {
	t.Helper()
	resetChangelogFlags(t)

	baseDir := dir
	baseDirOverride = &baseDir

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

func initChangelogRepo(t *testing.T) string {
	t.Helper()
	dir := initGitRepo(t)
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test User")
	commitChangelogFile(t, dir, "README.md", "# Test\n", "Initial commit")
	return dir
}

func commitChangelogFile(t *testing.T, dir, path, content, subject string) string {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGit(t, dir, "add", path)
	runGit(t, dir, "commit", "-m", subject)

	var sha bytes.Buffer
	cmd := execCommand(t, dir, "git", "rev-parse", "HEAD")
	cmd.Stdout = &sha
	if err := cmd.Run(); err != nil {
		t.Fatalf("rev-parse HEAD failed: %v", err)
	}
	return strings.TrimSpace(sha.String())
}

func tagChangelogHead(t *testing.T, dir, tag string) {
	t.Helper()
	runGit(t, dir, "tag", "-a", tag, "-m", "Release "+tag)
}

func execCommand(t *testing.T, dir, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd
}

func TestChangelogCommandDefaultsToNearestReachableTag(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)

	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "old.txt", "old\n", "feat: add old feature")
	tagChangelogHead(t, dir, "v0.2.0")
	commitChangelogFile(t, dir, "fix.txt", "fix\n", "fix: patch release")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.1")
	if err != nil {
		t.Fatalf("changelog command failed: %v", err)
	}
	if strings.Contains(output, "Add old feature") {
		t.Fatalf("expected latest reachable tag range, got:\n%s", output)
	}
	if !strings.Contains(output, "Patch release") {
		t.Fatalf("expected patch release entry, got:\n%s", output)
	}
}

func TestChangelogCommandUsesExplicitFromTo(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)

	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add feature")
	mid := commitChangelogFile(t, dir, "fix.txt", "fix\n", "fix: patch feature")
	commitChangelogFile(t, dir, "later.txt", "later\n", "feat: add later work")

	output, err := runChangelogCommand(t, dir, "from", "v0.1.0", "to", mid, "version", "v0.2.0")
	if err != nil {
		t.Fatalf("changelog command failed: %v", err)
	}
	if !strings.Contains(output, "Add feature") || !strings.Contains(output, "Patch feature") {
		t.Fatalf("expected explicit range entries, got:\n%s", output)
	}
	if strings.Contains(output, "Add later work") {
		t.Fatalf("explicit --to should exclude later work, got:\n%s", output)
	}
}

func TestChangelogCommandVersionDateOutput(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)

	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	output, err := runChangelogCommand(t, dir, "version", "v0.2.0", "date", "2026-05-08")
	if err != nil {
		t.Fatalf("changelog command failed: %v", err)
	}
	if !strings.HasPrefix(output, "## [v0.2.0] - 2026-05-08\n\n") {
		t.Fatalf("unexpected heading:\n%s", output)
	}
}

func TestChangelogCommandShowsNoTagGuidance(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir)
	if err == nil || !strings.Contains(err.Error(), "no reachable semver tag found for HEAD; pass --from") {
		t.Fatalf("expected no-tag guidance, got %v", err)
	}
}

func TestChangelogCommandEmptyRangeReturnsUsefulError(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	tagChangelogHead(t, dir, "v0.1.0")

	_, err := runChangelogCommand(t, dir, "from", "HEAD", "to", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "no changelog-worthy commits found between HEAD and HEAD") {
		t.Fatalf("expected empty-range error, got %v", err)
	}
}

func TestChangelogCommandRejectsInvalidDate(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "version", "v0.2.0", "date", "2026/05/09")
	if err == nil || !strings.Contains(err.Error(), `invalid --date "2026/05/09"`) {
		t.Fatalf("expected invalid date error, got %v", err)
	}
}

func TestChangelogCommandRequiresVersionWhenDateSupplied(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "date", "2026-05-09")
	if err == nil || !strings.Contains(err.Error(), "--version is required when --date is supplied") {
		t.Fatalf("expected missing version error, got %v", err)
	}
}

func TestChangelogCommandRejectsInvalidRefs(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	tagChangelogHead(t, dir, "v0.1.0")
	commitChangelogFile(t, dir, "feature.txt", "feature\n", "feat: add changelog command")

	_, err := runChangelogCommand(t, dir, "from", "missing-ref")
	if err == nil || !strings.Contains(err.Error(), `invalid git ref "missing-ref"`) {
		t.Fatalf("expected invalid ref error, got %v", err)
	}
	_, err = runChangelogCommand(t, dir, "to", "")
	if err == nil || !strings.Contains(err.Error(), "--to cannot be empty") {
		t.Fatalf("expected empty --to error, got %v", err)
	}
}

func TestChangelogCommandDefaultRangeUsesTargetRef(t *testing.T) {
	saveAndRestoreChangelogState(t, time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	dir := initChangelogRepo(t)
	initial := strings.TrimSpace(func() string {
		var out bytes.Buffer
		cmd := execCommand(t, dir, "git", "rev-parse", "HEAD")
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("rev-parse HEAD failed: %v", err)
		}
		return out.String()
	}())

	runGit(t, dir, "checkout", "-b", "target", initial)
	commitChangelogFile(t, dir, "target.txt", "target\n", "feat: add target feature")

	runGit(t, dir, "checkout", "-B", "main", initial)
	commitChangelogFile(t, dir, "main.txt", "main\n", "feat: add main feature")
	tagChangelogHead(t, dir, "v0.1.0")

	_, err := runChangelogCommand(t, dir, "to", "target")
	if err == nil || !strings.Contains(err.Error(), "no reachable semver tag found for target") {
		t.Fatalf("expected target-ref no-tag error, got %v", err)
	}
}
