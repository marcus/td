package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseNotesDefaultRangeSelection(t *testing.T) {
	repo := initReleaseNotesRepo(t)
	gitCmd(t, repo, "tag", "v1.0.0")
	commitReleaseNoteFile(t, repo, "feature.txt", "feat: add release notes")

	out, err := runReleaseNotesCommand(t, repo, nil)
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}
	if !strings.Contains(out, "### Features\n- Add release notes") {
		t.Fatalf("expected feature markdown, got:\n%s", out)
	}
}

func TestReleaseNotesExplicitRange(t *testing.T) {
	repo := initReleaseNotesRepo(t)
	gitCmd(t, repo, "tag", "v1.0.0")
	commitReleaseNoteFile(t, repo, "feature.txt", "feat: add release notes")
	gitCmd(t, repo, "tag", "v1.1.0")
	commitReleaseNoteFile(t, repo, "fix.txt", "fix: repair later bug")

	out, err := runReleaseNotesCommand(t, repo, map[string]string{
		"from": "v1.0.0",
		"to":   "v1.1.0",
	})
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}
	if !strings.Contains(out, "Add release notes") {
		t.Fatalf("expected first range commit, got:\n%s", out)
	}
	if strings.Contains(out, "Repair later bug") {
		t.Fatalf("did not expect commit after --to ref, got:\n%s", out)
	}
}

func TestReleaseNotesValidationFailures(t *testing.T) {
	repo := initReleaseNotesRepo(t)
	commitReleaseNoteFile(t, repo, "feature.txt", "feat: add release notes")

	tests := []struct {
		name    string
		flags   map[string]string
		wantErr string
	}{
		{
			name:    "date requires version",
			flags:   map[string]string{"date": "2026-04-29"},
			wantErr: "--date requires --version",
		},
		{
			name:    "date format",
			flags:   map[string]string{"version": "v1.0.0", "date": "04-29-2026"},
			wantErr: "--date must use YYYY-MM-DD format",
		},
		{
			name:    "missing default tag",
			flags:   nil,
			wantErr: "pass --from <ref>",
		},
		{
			name:    "invalid from ref",
			flags:   map[string]string{"from": "missing-ref"},
			wantErr: "invalid git ref",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runReleaseNotesCommand(t, repo, tt.flags)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestReleaseNotesStdoutMarkdownWithHeading(t *testing.T) {
	repo := initReleaseNotesRepo(t)
	gitCmd(t, repo, "tag", "v1.0.0")
	commitReleaseNoteFile(t, repo, "fix.txt", "fix: repair output")

	out, err := runReleaseNotesCommand(t, repo, map[string]string{
		"version": "v1.1.0",
		"date":    "2026-04-29",
	})
	if err != nil {
		t.Fatalf("release-notes failed: %v", err)
	}

	if !strings.HasPrefix(out, "## v1.1.0 - 2026-04-29\n\n") {
		t.Fatalf("expected version/date heading, got:\n%s", out)
	}
	if !strings.Contains(out, "### Bug Fixes\n- Repair output") {
		t.Fatalf("expected bug fix section, got:\n%s", out)
	}
}

func TestReleaseNotesNoRelevantCommits(t *testing.T) {
	repo := initReleaseNotesRepo(t)
	gitCmd(t, repo, "tag", "v1.0.0")
	commitReleaseNoteFile(t, repo, "merge.txt", "fixup! feat: add hidden note")

	_, err := runReleaseNotesCommand(t, repo, nil)
	if err == nil {
		t.Fatal("expected no relevant commits error")
	}
	if !strings.Contains(err.Error(), "no release-note-worthy commits") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runReleaseNotesCommand(t *testing.T, repo string, flags map[string]string) (string, error) {
	t.Helper()
	saveAndRestoreCommandFlags(t, releaseNotesCmd, "from", "to", "version", "date")

	oldBaseDirOverride := baseDirOverride
	baseDirOverride = &repo
	t.Cleanup(func() {
		baseDirOverride = oldBaseDirOverride
	})

	var out bytes.Buffer
	releaseNotesCmd.SetOut(&out)
	t.Cleanup(func() {
		releaseNotesCmd.SetOut(nil)
	})

	for name, value := range flags {
		if err := releaseNotesCmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set flag %s=%s: %v", name, value, err)
		}
	}

	err := releaseNotesCmd.RunE(releaseNotesCmd, nil)
	return out.String(), err
}

func initReleaseNotesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "Initial commit")
	return dir
}

func commitReleaseNoteFile(t *testing.T, repo, name, subject string) {
	t.Helper()
	path := filepath.Join(repo, name)
	content := fmt.Sprintf("%s\n", subject)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", subject)
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}
