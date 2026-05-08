package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcus/td/internal/releasenotes"
)

func TestReleaseNotesDefaultRangeUsesLatestVersionTag(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")
	runGit(t, repo, "tag", "v0.1.0")
	featureSHA := commitFile(t, repo, "feature.txt", "feature\n", "feat(cli): add release notes")

	out, err := runReleaseNotesCommand(t, repo, "--version", "v0.2.0")
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}

	for _, want := range []string{
		"## v0.2.0",
		"_Range: `v0.1.0..HEAD` (1 commits)_",
		"### Features",
		"- add release notes (" + featureSHA + ")",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "initial release") {
		t.Fatalf("default range should exclude tagged commit:\n%s", out)
	}
}

func TestReleaseNotesExplicitFromTo(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")
	runGit(t, repo, "tag", "v0.1.0")
	featureSHA := commitFile(t, repo, "feature.txt", "feature\n", "feat: add board export")
	fixSHA := commitFile(t, repo, "fix.txt", "fix\n", "fix: handle missing board")
	commitFile(t, repo, "docs.txt", "docs\n", "docs: explain board export")

	out, err := runReleaseNotesCommand(t, repo, "--from", "v0.1.0", "--to", fixSHA)
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}

	for _, want := range []string{
		"_Range: `v0.1.0.." + fixSHA + "` (2 commits)_",
		"- add board export (" + featureSHA + ")",
		"- handle missing board (" + fixSHA + ")",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "explain board export") {
		t.Fatalf("explicit --to should exclude later commit:\n%s", out)
	}
}

func TestReleaseNotesJSONOutput(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")
	runGit(t, repo, "tag", "v0.1.0")
	commitFile(t, repo, "fix.txt", "fix\n", "fix(api): handle empty release range")

	out, err := runReleaseNotesCommand(t, repo, "--from", "v0.1.0", "--json", "--version", "v0.2.0")
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}

	var draft releasenotes.Draft
	if err := json.Unmarshal([]byte(out), &draft); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if draft.Version != "v0.2.0" || draft.From != "v0.1.0" || draft.To != "HEAD" {
		t.Fatalf("unexpected draft metadata: %+v", draft)
	}
	if draft.CommitCount != 1 {
		t.Fatalf("commit count = %d, want 1", draft.CommitCount)
	}
	items := releaseNotesSectionItems(draft, releasenotes.SectionFixes)
	if len(items) != 1 {
		t.Fatalf("bug fix items = %d, want 1", len(items))
	}
	if items[0].Subject != "handle empty release range" || items[0].Scope != "api" {
		t.Fatalf("unexpected bug fix item: %+v", items[0])
	}
}

func TestReleaseNotesEmptyRange(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")

	out, err := runReleaseNotesCommand(t, repo, "--from", "HEAD", "--to", "HEAD")
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}
	if !strings.Contains(out, "_Range: `HEAD..HEAD` (0 commits)_") {
		t.Fatalf("missing empty range metadata:\n%s", out)
	}
	if !strings.Contains(out, "_No release note entries found._") {
		t.Fatalf("missing empty draft message:\n%s", out)
	}
}

func TestReleaseNotesNoTagsRequiresFrom(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")

	_, err := runReleaseNotesCommand(t, repo)
	if err == nil {
		t.Fatal("expected error without v* tags or --from")
	}
	if !strings.Contains(err.Error(), "no v* tags found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseNotesInvalidDate(t *testing.T) {
	repo := newReleaseNotesRepo(t)
	commitFile(t, repo, "README.md", "initial\n", "feat: initial release")

	_, err := runReleaseNotesCommand(t, repo, "--from", "HEAD", "--to", "HEAD", "--date", "05/08/2026")
	if err == nil {
		t.Fatal("expected invalid date error")
	}
	if !strings.Contains(err.Error(), "invalid --date") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runReleaseNotesCommand(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	saveAndRestoreGlobals(t)
	workDirFlag = ""
	t.Setenv("TD_WORK_DIR", "")
	t.Chdir(repo)

	cmd := newReleaseNotesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func newReleaseNotesRepo(t *testing.T) string {
	t.Helper()
	repo := initGitRepo(t)
	runGit(t, repo, "config", "user.email", "td@example.com")
	runGit(t, repo, "config", "user.name", "td tests")
	return repo
}

func commitFile(t *testing.T, repo, name, content, subject string) string {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", name)
	runGit(t, repo, "commit", "-m", subject)
	return gitOutput(t, repo, "rev-parse", "--short", "HEAD")
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

func releaseNotesSectionItems(draft releasenotes.Draft, id string) []releasenotes.Item {
	for _, section := range draft.Sections {
		if section.ID == id {
			return section.Items
		}
	}
	return nil
}
