package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitMsgHookNormalizesAndRejects(t *testing.T) {
	t.Parallel()

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tests := []struct {
		name       string
		input      string
		want       string
		wantErr    bool
		wantStderr string
	}{
		{
			name:  "accepts task linked subject",
			input: "feat: normalize commit messages (td-a1b2)\n",
			want:  "feat: normalize commit messages (td-a1b2)\n",
		},
		{
			name:  "accepts automation subject without task id",
			input: "chore: bump Homebrew formula to v1.2.3\n",
			want:  "chore: bump Homebrew formula to v1.2.3\n",
		},
		{
			name:  "normalizes casing and spacing",
			input: "  Feat   normalize commit messages   (td-a1b2)  \n\nBody line\nNightshift-Task: commit-normalize\n",
			want:  "feat: normalize commit messages (td-a1b2)\n\nBody line\nNightshift-Task: commit-normalize\n",
		},
		{
			name:       "rejects invalid trailing parenthetical suffix",
			input:      "feat: normalize commit messages (jira-123)\n",
			want:       "feat: normalize commit messages (jira-123)\n",
			wantErr:    true,
			wantStderr: "only allowed trailing parenthetical suffix",
		},
		{
			name:       "rejects invalid suffix after normalizing prefix",
			input:      "Feat normalize commit messages (foo)\n",
			want:       "Feat normalize commit messages (foo)\n",
			wantErr:    true,
			wantStderr: "only allowed trailing parenthetical suffix",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			messageFile := filepath.Join(tempDir, "COMMIT_EDITMSG")
			if err := os.WriteFile(messageFile, []byte(tt.input), 0o644); err != nil {
				t.Fatalf("write message file: %v", err)
			}

			cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts/commit-msg.sh"), messageFile)
			cmd.Dir = repoRoot
			output, err := cmd.CombinedOutput()
			got := string(output)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got success with output: %s", got)
				}
				if !strings.Contains(got, tt.wantStderr) {
					t.Fatalf("expected output to contain %q, got %q", tt.wantStderr, got)
				}
			} else if err != nil {
				t.Fatalf("hook failed: %v\n%s", err, got)
			}

			contents, readErr := os.ReadFile(messageFile)
			if readErr != nil {
				t.Fatalf("read message file: %v", readErr)
			}
			if string(contents) != tt.want {
				t.Fatalf("unexpected message file contents:\nwant: %q\ngot:  %q", tt.want, string(contents))
			}
		})
	}
}

func TestInstallHooksWorksInGitWorktree(t *testing.T) {
	t.Parallel()

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	sourceRepo := filepath.Join(tempDir, "source")
	worktreeDir := filepath.Join(tempDir, "worktree")

	if err := os.MkdirAll(filepath.Join(sourceRepo, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}

	copyFile := func(src, dst string, mode os.FileMode) {
		t.Helper()
		data, readErr := os.ReadFile(src)
		if readErr != nil {
			t.Fatalf("read %s: %v", src, readErr)
		}
		if writeErr := os.WriteFile(dst, data, mode); writeErr != nil {
			t.Fatalf("write %s: %v", dst, writeErr)
		}
	}

	copyFile(filepath.Join(repoRoot, "Makefile"), filepath.Join(sourceRepo, "Makefile"), 0o644)
	copyFile(filepath.Join(repoRoot, "scripts/pre-commit.sh"), filepath.Join(sourceRepo, "scripts/pre-commit.sh"), 0o755)
	copyFile(filepath.Join(repoRoot, "scripts/commit-msg.sh"), filepath.Join(sourceRepo, "scripts/commit-msg.sh"), 0o755)

	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), runErr, output)
		}
		return string(output)
	}

	run(sourceRepo, "git", "init", "-b", "main")
	run(sourceRepo, "git", "config", "user.name", "Test User")
	run(sourceRepo, "git", "config", "user.email", "test@example.com")
	run(sourceRepo, "git", "add", "Makefile", "scripts/pre-commit.sh", "scripts/commit-msg.sh")
	run(sourceRepo, "git", "commit", "-m", "chore: seed worktree fixture")
	run(sourceRepo, "git", "worktree", "add", "-b", "feature/test-hooks", worktreeDir)

	run(worktreeDir, "make", "install-hooks")

	hooksDir := strings.TrimSpace(run(worktreeDir, "git", "rev-parse", "--git-path", "hooks"))
	preCommitTarget := strings.TrimSpace(run(worktreeDir, "readlink", filepath.Join(hooksDir, "pre-commit")))
	commitMsgTarget := strings.TrimSpace(run(worktreeDir, "readlink", filepath.Join(hooksDir, "commit-msg")))

	wantPreCommit, err := filepath.EvalSymlinks(filepath.Join(worktreeDir, "scripts/pre-commit.sh"))
	if err != nil {
		t.Fatalf("eval symlink pre-commit target: %v", err)
	}
	wantCommitMsg, err := filepath.EvalSymlinks(filepath.Join(worktreeDir, "scripts/commit-msg.sh"))
	if err != nil {
		t.Fatalf("eval symlink commit-msg target: %v", err)
	}
	preCommitTarget, err = filepath.EvalSymlinks(preCommitTarget)
	if err != nil {
		t.Fatalf("eval installed pre-commit target: %v", err)
	}
	commitMsgTarget, err = filepath.EvalSymlinks(commitMsgTarget)
	if err != nil {
		t.Fatalf("eval installed commit-msg target: %v", err)
	}

	if preCommitTarget != wantPreCommit {
		t.Fatalf("unexpected pre-commit target: want %q, got %q", wantPreCommit, preCommitTarget)
	}
	if commitMsgTarget != wantCommitMsg {
		t.Fatalf("unexpected commit-msg target: want %q, got %q", wantCommitMsg, commitMsgTarget)
	}
}
