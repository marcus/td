package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo creates a test git repository with an initial commit
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo
	if err := runCmd(dir, "git", "init"); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git for commits
	runCmd(dir, "git", "config", "user.email", "test@test.com")
	runCmd(dir, "git", "config", "user.name", "Test User")

	// Create initial file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	runCmd(dir, "git", "add", ".")
	runCmd(dir, "git", "commit", "-m", "Initial commit")

	return dir
}

func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// TestParseStatOutputBasic tests parsing git diff --stat output
func TestParseStatOutputBasic(t *testing.T) {
	output := ` file1.go | 10 ++++------
 file2.go | 5 +++++`

	changes := parseStatOutput(output)

	if len(changes) != 2 {
		t.Fatalf("Expected 2 file changes, got %d", len(changes))
	}

	if changes[0].Path != "file1.go" {
		t.Errorf("Expected 'file1.go', got %q", changes[0].Path)
	}
	if changes[0].Additions != 4 {
		t.Errorf("Expected 4 additions, got %d", changes[0].Additions)
	}
	if changes[0].Deletions != 6 {
		t.Errorf("Expected 6 deletions, got %d", changes[0].Deletions)
	}
}

// TestParseStatOutputEmpty tests parsing empty output
func TestParseStatOutputEmpty(t *testing.T) {
	changes := parseStatOutput("")
	if len(changes) != 0 {
		t.Errorf("Expected 0 changes from empty output, got %d", len(changes))
	}
}

// TestParseStatOutputWithSummary tests filtering out summary line
func TestParseStatOutputWithSummary(t *testing.T) {
	output := ` file.go | 3 +++
 2 files changed, 10 insertions(+), 5 deletions(-)`

	changes := parseStatOutput(output)

	// Should only get the file line, not the summary
	if len(changes) != 1 {
		t.Fatalf("Expected 1 file change (summary filtered), got %d", len(changes))
	}
}

// TestParseStatOutputAdditionsOnly tests file with only additions
func TestParseStatOutputAdditionsOnly(t *testing.T) {
	output := ` newfile.go | 50 ++++++++++++++++++++`

	changes := parseStatOutput(output)

	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Additions == 0 {
		t.Error("Expected additions > 0")
	}
	if changes[0].Deletions != 0 {
		t.Error("Expected 0 deletions for new file")
	}
}

// TestParseStatOutputDeletionsOnly tests file with only deletions
func TestParseStatOutputDeletionsOnly(t *testing.T) {
	output := ` oldfile.go | 30 ------------------------------`

	changes := parseStatOutput(output)

	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}
	if changes[0].Additions != 0 {
		t.Error("Expected 0 additions for deleted content")
	}
	if changes[0].Deletions == 0 {
		t.Error("Expected deletions > 0")
	}
}

// TestGetStateInRepo tests GetState in a git repository
func TestGetStateInRepo(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	state, err := GetState()
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	if state.CommitSHA == "" {
		t.Error("CommitSHA should not be empty")
	}
	if len(state.CommitSHA) != 40 {
		t.Errorf("CommitSHA should be 40 chars, got %d", len(state.CommitSHA))
	}
	if state.Branch == "" {
		t.Error("Branch should not be empty")
	}
	if !state.IsClean {
		t.Error("Fresh repo should be clean")
	}
}

// TestGetStateWithModifiedFiles tests GetState with uncommitted changes
func TestGetStateWithModifiedFiles(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify a file
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644)

	state, err := GetState()
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	if state.IsClean {
		t.Error("Repo with modified files should not be clean")
	}
	if state.Modified != 1 {
		t.Errorf("Expected 1 modified file, got %d", state.Modified)
	}
}

// TestGetStateWithUntrackedFiles tests GetState with untracked files
func TestGetStateWithUntrackedFiles(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Create untracked file
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked"), 0644)

	state, err := GetState()
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	if state.IsClean {
		t.Error("Repo with untracked files should not be clean")
	}
	if state.Untracked != 1 {
		t.Errorf("Expected 1 untracked file, got %d", state.Untracked)
	}
	if state.DirtyFiles != 1 {
		t.Errorf("Expected 1 dirty file, got %d", state.DirtyFiles)
	}
}

// TestGetStateWithMixedChanges tests GetState with both modified and untracked
func TestGetStateWithMixedChanges(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Modify existing and create new
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644)

	state, err := GetState()
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}

	if state.Modified != 1 {
		t.Errorf("Expected 1 modified, got %d", state.Modified)
	}
	if state.Untracked != 1 {
		t.Errorf("Expected 1 untracked, got %d", state.Untracked)
	}
	if state.DirtyFiles != 2 {
		t.Errorf("Expected 2 dirty files, got %d", state.DirtyFiles)
	}
}

// TestIsRepoTrue tests IsRepo in a git repository
func TestIsRepoTrue(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if !IsRepo() {
		t.Error("IsRepo should return true in git repo")
	}
}

// TestIsRepoFalse tests IsRepo outside a git repository
func TestIsRepoFalse(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	if IsRepo() {
		t.Error("IsRepo should return false outside git repo")
	}
}

// TestGetRootDir tests getting repository root
func TestGetRootDir(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	root, err := GetRootDir()
	if err != nil {
		t.Fatalf("GetRootDir failed: %v", err)
	}

	// The returned path might be canonical/resolved differently
	if root == "" {
		t.Error("Root dir should not be empty")
	}
}

// TestGetCommitsSince tests counting commits
func TestGetCommitsSince(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Get initial commit SHA
	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create additional commits
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("1"), 0644)
	runCmd(dir, "git", "add", ".")
	runCmd(dir, "git", "commit", "-m", "Second commit")

	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("2"), 0644)
	runCmd(dir, "git", "add", ".")
	runCmd(dir, "git", "commit", "-m", "Third commit")

	count, err := GetCommitsSince(initialSHA)
	if err != nil {
		t.Fatalf("GetCommitsSince failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 commits since initial, got %d", count)
	}
}

// TestGetCommitsSinceSameCommit tests counting with same SHA (should be 0)
func TestGetCommitsSinceSameCommit(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	state, _ := GetState()
	count, err := GetCommitsSince(state.CommitSHA)
	if err != nil {
		t.Fatalf("GetCommitsSince failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 commits since HEAD, got %d", count)
	}
}

// TestGetDiffStatsSince tests diff statistics
func TestGetDiffStatsSince(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create commit with changes
	os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("line1\nline2\nline3\n"), 0644)
	runCmd(dir, "git", "add", ".")
	runCmd(dir, "git", "commit", "-m", "Add new file")

	stats, err := GetDiffStatsSince(initialSHA)
	if err != nil {
		t.Fatalf("GetDiffStatsSince failed: %v", err)
	}

	if stats.FilesChanged != 1 {
		t.Errorf("Expected 1 file changed, got %d", stats.FilesChanged)
	}
	if stats.Additions == 0 {
		t.Error("Expected additions > 0")
	}
}

// TestGetDiffStatsSinceNoChanges tests stats with no changes
func TestGetDiffStatsSinceNoChanges(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	state, _ := GetState()
	stats, err := GetDiffStatsSince(state.CommitSHA)
	if err != nil {
		t.Fatalf("GetDiffStatsSince failed: %v", err)
	}

	if stats.FilesChanged != 0 {
		t.Errorf("Expected 0 files changed, got %d", stats.FilesChanged)
	}
	if stats.Additions != 0 {
		t.Errorf("Expected 0 additions, got %d", stats.Additions)
	}
	if stats.Deletions != 0 {
		t.Errorf("Expected 0 deletions, got %d", stats.Deletions)
	}
}

// TestGetChangedFilesSince tests getting list of changed files
func TestGetChangedFilesSince(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create commit with new file
	os.WriteFile(filepath.Join(dir, "newfile.go"), []byte("package main\n"), 0644)
	runCmd(dir, "git", "add", ".")
	runCmd(dir, "git", "commit", "-m", "Add go file")

	files, err := GetChangedFilesSince(initialSHA)
	if err != nil {
		t.Fatalf("GetChangedFilesSince failed: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("Expected 1 changed file, got %d", len(files))
	}
	if files[0].Path != "newfile.go" {
		t.Errorf("Expected 'newfile.go', got %q", files[0].Path)
	}
}

// TestStateBranchName tests that branch name is captured correctly
func TestStateBranchName(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Default branch could be main or master
	state, _ := GetState()
	if state.Branch == "" {
		t.Error("Branch should not be empty")
	}
	// The branch should be either 'main', 'master', or some default
	if state.Branch != "main" && state.Branch != "master" && state.Branch != "HEAD" {
		t.Logf("Branch name is %q (expected main/master/HEAD)", state.Branch)
	}
}
