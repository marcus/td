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
	if err := runCmd(dir, "git", "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}
	if err := runCmd(dir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create initial file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "Initial commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Modify a file
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Create untracked file
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Modify existing and create new
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Modified"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if !IsRepo() {
		t.Error("IsRepo should return true in git repo")
	}
}

// TestIsRepoFalse tests IsRepo outside a git repository
func TestIsRepoFalse(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if IsRepo() {
		t.Error("IsRepo should return false outside git repo")
	}
}

// TestGetRootDir tests getting repository root
func TestGetRootDir(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	root, err := GetRootDir()
	if err != nil {
		t.Fatalf("GetRootDir failed: %v", err)
	}

	// The returned path might be canonical/resolved differently
	if root == "" {
		t.Error("Root dir should not be empty")
	}
}

func TestGetRootDirInDir(t *testing.T) {
	dir := initTestRepo(t)
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0755); err != nil {
		t.Fatalf("Failed to create nested dir: %v", err)
	}

	root, err := GetRootDirInDir(nested)
	if err != nil {
		t.Fatalf("GetRootDirInDir failed: %v", err)
	}
	if root == "" {
		t.Fatal("Root dir should not be empty")
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	got, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", root, err)
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestGetCommitsSince tests counting commits
func TestGetCommitsSince(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Get initial commit SHA
	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create additional commits
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("1"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "Second commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("2"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "Third commit"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create commit with changes
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "Add new file"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	initialState, _ := GetState()
	initialSHA := initialState.CommitSHA

	// Create commit with new file
	if err := os.WriteFile(filepath.Join(dir, "newfile.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "Add go file"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

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
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

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

func TestResolveRefInRepoDir(t *testing.T) {
	dir := initTestRepo(t)

	sha, err := ResolveRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("ResolveRef failed: %v", err)
	}
	if len(sha) != 40 {
		t.Fatalf("expected full SHA, got %q", sha)
	}

	if _, err := ResolveRef(dir, "missing-ref"); err == nil {
		t.Fatal("expected missing ref error")
	}
}

func TestNearestSemverTag(t *testing.T) {
	dir := initTestRepo(t)
	if err := runCmd(dir, "git", "tag", "v1.0.0"); err != nil {
		t.Fatalf("Failed to tag v1.0.0: %v", err)
	}

	commitFile(t, dir, "one.txt", "one", "feat: first change")
	if err := runCmd(dir, "git", "tag", "not-a-version"); err != nil {
		t.Fatalf("Failed to tag non-semver: %v", err)
	}

	commitFile(t, dir, "two.txt", "two", "fix: second change")
	if err := runCmd(dir, "git", "tag", "v1.1.0"); err != nil {
		t.Fatalf("Failed to tag v1.1.0: %v", err)
	}

	commitFile(t, dir, "three.txt", "three", "docs: third change")

	tag, err := NearestSemverTag(dir, "HEAD")
	if err != nil {
		t.Fatalf("NearestSemverTag failed: %v", err)
	}
	if tag != "v1.1.0" {
		t.Fatalf("expected v1.1.0, got %q", tag)
	}
}

func TestNearestSemverTagNotFound(t *testing.T) {
	dir := initTestRepo(t)
	if err := runCmd(dir, "git", "tag", "latest"); err != nil {
		t.Fatalf("Failed to tag latest: %v", err)
	}

	if _, err := NearestSemverTag(dir, "HEAD"); err == nil {
		t.Fatal("expected no reachable semver tag error")
	}
}

func TestListCommitsOldestFirstWithBody(t *testing.T) {
	dir := initTestRepo(t)
	if err := runCmd(dir, "git", "tag", "v1.0.0"); err != nil {
		t.Fatalf("Failed to tag v1.0.0: %v", err)
	}

	commitFile(t, dir, "feature.txt", "feature", "feat: add feature")
	if err := os.WriteFile(filepath.Join(dir, "bug.txt"), []byte("bug"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "fix: repair bug", "-m", "Body details"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	commits, err := ListCommits(dir, "v1.0.0", "HEAD")
	if err != nil {
		t.Fatalf("ListCommits failed: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "feat: add feature" {
		t.Fatalf("expected oldest commit first, got %q", commits[0].Subject)
	}
	if commits[1].Subject != "fix: repair bug" {
		t.Fatalf("expected second commit subject, got %q", commits[1].Subject)
	}
	if commits[1].Body != "Body details" {
		t.Fatalf("expected body details, got %q", commits[1].Body)
	}
	if commits[0].SHA == "" || commits[0].ShortSHA == "" || commits[0].Date == "" {
		t.Fatalf("expected commit metadata: %#v", commits[0])
	}
}

func commitFile(t *testing.T, dir, name, content, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := runCmd(dir, "git", "commit", "-m", subject); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}
}
