package workdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBaseDir_UsesGitRootTodosFromSubdir(t *testing.T) {
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".todos"), 0755); err != nil {
		t.Fatalf("create .todos: %v", err)
	}

	subdir := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	got := ResolveBaseDir(subdir)
	assertSamePath(t, repo, got)
}

func TestResolveBaseDir_UsesGitRootTdRootFromSubdir(t *testing.T) {
	repo := initGitRepo(t)
	sharedRoot := filepath.Join(t.TempDir(), "shared-root")
	if err := os.MkdirAll(sharedRoot, 0755); err != nil {
		t.Fatalf("create shared root: %v", err)
	}

	tdRootPath := filepath.Join(repo, tdRootFile)
	if err := os.WriteFile(tdRootPath, []byte(sharedRoot+"\n"), 0644); err != nil {
		t.Fatalf("write %s: %v", tdRootFile, err)
	}

	subdir := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	got := ResolveBaseDir(subdir)
	assertSamePath(t, sharedRoot, got)
}

func TestResolveBaseDir_DoesNotJumpToGitRootWithoutMarkers(t *testing.T) {
	repo := initGitRepo(t)
	subdir := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	got := ResolveBaseDir(subdir)
	assertSamePath(t, subdir, got)
}

func TestResolveBaseDir_ResolvesRelativeTdRootPath(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	sharedRoot := filepath.Join(parent, "shared")
	if err := os.MkdirAll(sharedRoot, 0755); err != nil {
		t.Fatalf("create shared root: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, tdRootFile), []byte("../shared"), 0644); err != nil {
		t.Fatalf("write %s: %v", tdRootFile, err)
	}

	got := ResolveBaseDir(repo)
	assertSamePath(t, sharedRoot, got)
}

func TestResolveBaseDir_ExternalWorktreeFindsMainTodos(t *testing.T) {
	repo := initGitRepo(t)
	runCmd(t, repo, "git", "commit", "--allow-empty", "-m", "init")

	if err := os.MkdirAll(filepath.Join(repo, ".todos"), 0755); err != nil {
		t.Fatalf("create .todos: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "wt")
	runCmd(t, repo, "git", "worktree", "add", wtPath, "-b", "test-branch")

	// Worktree should not have its own .td-root
	if _, err := os.Stat(filepath.Join(wtPath, tdRootFile)); err == nil {
		t.Fatal("worktree should not have .td-root")
	}

	got := ResolveBaseDir(wtPath)
	assertSamePath(t, repo, got)
}

func TestResolveBaseDir_ExternalWorktreeFollowsMainTdRoot(t *testing.T) {
	repo := initGitRepo(t)
	runCmd(t, repo, "git", "commit", "--allow-empty", "-m", "init")

	sharedRoot := filepath.Join(t.TempDir(), "shared-root")
	if err := os.MkdirAll(sharedRoot, 0755); err != nil {
		t.Fatalf("create shared root: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, tdRootFile), []byte(sharedRoot+"\n"), 0644); err != nil {
		t.Fatalf("write %s: %v", tdRootFile, err)
	}

	wtPath := filepath.Join(t.TempDir(), "wt")
	runCmd(t, repo, "git", "worktree", "add", wtPath, "-b", "test-branch")

	// Worktree should not have its own .td-root
	if _, err := os.Stat(filepath.Join(wtPath, tdRootFile)); err == nil {
		t.Fatal("worktree should not have .td-root")
	}

	got := ResolveBaseDir(wtPath)
	assertSamePath(t, sharedRoot, got)
}

func TestResolveBaseDir_MainRepoUnchangedWithWorktrees(t *testing.T) {
	repo := initGitRepo(t)
	runCmd(t, repo, "git", "commit", "--allow-empty", "-m", "init")

	if err := os.MkdirAll(filepath.Join(repo, ".todos"), 0755); err != nil {
		t.Fatalf("create .todos: %v", err)
	}

	// Create a worktree so the repo "has worktrees"
	wtPath := filepath.Join(t.TempDir(), "wt")
	runCmd(t, repo, "git", "worktree", "add", wtPath, "-b", "test-branch")

	got := ResolveBaseDir(repo)
	assertSamePath(t, repo, got)
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runCmd(t, dir, "git", "init")
	return dir
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %q failed: %v (%s)", strings.Join(append([]string{name}, args...), " "), err, strings.TrimSpace(string(out)))
	}
}

func assertSamePath(t *testing.T, want string, got string) {
	t.Helper()

	wantResolved, wantErr := filepath.EvalSymlinks(want)
	if wantErr != nil {
		wantResolved = filepath.Clean(want)
	}

	gotResolved, gotErr := filepath.EvalSymlinks(got)
	if gotErr != nil {
		gotResolved = filepath.Clean(got)
	}

	if wantResolved != gotResolved {
		t.Fatalf("expected %q, got %q", wantResolved, gotResolved)
	}
}
