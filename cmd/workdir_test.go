package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertSamePath compares two paths after resolving symlinks (macOS /tmp → /private/tmp).
func assertSamePath(t *testing.T, want, got string) {
	t.Helper()
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		wantResolved = filepath.Clean(want)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		gotResolved = filepath.Clean(got)
	}
	if wantResolved != gotResolved {
		t.Fatalf("expected %q, got %q", wantResolved, gotResolved)
	}
}

// initGitRepo creates a temporary git repo and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	return dir
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// saveAndRestoreGlobals saves baseDir and workDirFlag and restores them on cleanup.
func saveAndRestoreGlobals(t *testing.T) {
	t.Helper()
	origBaseDir := baseDir
	origWorkDirFlag := workDirFlag
	t.Cleanup(func() {
		baseDir = origBaseDir
		workDirFlag = origWorkDirFlag
	})
}

// --- normalizeWorkDir tests ---

func TestNormalizeWorkDir_StripsTodosSuffix(t *testing.T) {
	got := normalizeWorkDir("/some/path/.todos")
	if got != "/some/path" {
		t.Fatalf("expected /some/path, got %q", got)
	}
}

func TestNormalizeWorkDir_AbsolutePathPassthrough(t *testing.T) {
	dir := t.TempDir()
	got := normalizeWorkDir(dir)
	assertSamePath(t, dir, got)
}

func TestNormalizeWorkDir_RelativePathMadeAbsolute(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got := normalizeWorkDir("some/relative/path")
	want := filepath.Join(cwd, "some/relative/path")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// --- initBaseDir tests ---

func TestInitBaseDir_WorkDirFlagWithTdRoot(t *testing.T) {
	saveAndRestoreGlobals(t)

	// Create a dir with .td-root pointing to a shared root
	dir := t.TempDir()
	sharedRoot := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(sharedRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".td-root"), []byte(sharedRoot+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	workDirFlag = dir
	initBaseDir()
	assertSamePath(t, sharedRoot, baseDir)
}

func TestInitBaseDir_WorkDirFlagWithTodosDir(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}

	workDirFlag = dir
	initBaseDir()
	assertSamePath(t, dir, baseDir)
}

func TestInitBaseDir_WorkDirFlagPointingToTodosDir(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	todosPath := filepath.Join(dir, ".todos")
	if err := os.MkdirAll(todosPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Point to the .todos dir itself — normalizeWorkDir should strip it
	workDirFlag = todosPath
	initBaseDir()
	assertSamePath(t, dir, baseDir)
}

func TestInitBaseDir_EnvVarWorkDir(t *testing.T) {
	saveAndRestoreGlobals(t)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}

	workDirFlag = "" // Ensure flag is not set
	t.Setenv("TD_WORK_DIR", dir)
	initBaseDir()
	assertSamePath(t, dir, baseDir)
}

func TestInitBaseDir_FlagTakesPrecedenceOverEnv(t *testing.T) {
	saveAndRestoreGlobals(t)

	flagDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(flagDir, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}
	envDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(envDir, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}

	workDirFlag = flagDir
	t.Setenv("TD_WORK_DIR", envDir)
	initBaseDir()
	assertSamePath(t, flagDir, baseDir)
}

func TestInitBaseDir_WorkDirWithGitWorktree(t *testing.T) {
	saveAndRestoreGlobals(t)

	// Create main repo with a commit and .todos
	repo := initGitRepo(t)
	runGit(t, repo, "commit", "--allow-empty", "-m", "init")
	if err := os.MkdirAll(filepath.Join(repo, ".todos"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a worktree
	wtPath := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", wtPath, "-b", "test-branch")

	// Set flag to the worktree — should resolve to main repo via .todos
	workDirFlag = wtPath
	initBaseDir()
	assertSamePath(t, repo, baseDir)
}

func TestInitBaseDir_WorkDirNoMarkers(t *testing.T) {
	saveAndRestoreGlobals(t)

	// A bare directory with no .todos, .td-root, or git
	dir := t.TempDir()

	workDirFlag = dir
	initBaseDir()
	assertSamePath(t, dir, baseDir)
}
