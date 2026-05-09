package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func runCmdOutput(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func commitGitFile(t *testing.T, dir, path, content, subject string, body ...string) string {
	t.Helper()

	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("Failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	if err := runCmd(dir, "git", "add", path); err != nil {
		t.Fatalf("Failed to git add %s: %v", path, err)
	}

	args := []string{"commit", "-m", subject}
	for _, paragraph := range body {
		args = append(args, "-m", paragraph)
	}
	if out, err := runCmdOutput(dir, "git", args...); err != nil {
		t.Fatalf("Failed to commit %q: %v\n%s", subject, err, out)
	}

	sha, err := runCmdOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Failed to resolve HEAD: %v", err)
	}
	return sha
}

func tagHead(t *testing.T, dir, tag string) {
	t.Helper()
	if out, err := runCmdOutput(dir, "git", "tag", "-a", tag, "-m", "Release "+tag); err != nil {
		t.Fatalf("Failed to tag HEAD as %s: %v\n%s", tag, err, out)
	}
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

func TestGetRootDirFromReturnsErrNotRepository(t *testing.T) {
	_, err := GetRootDirFrom(t.TempDir())
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("expected ErrNotRepository, got %v", err)
	}
}

func TestNearestReachableSemverTagReturnsLatestReachableTag(t *testing.T) {
	dir := initTestRepo(t)

	tagHead(t, dir, "v0.1.0")
	commitGitFile(t, dir, "feature.txt", "feature\n", "feat: add feature")
	tagHead(t, dir, "v0.2.0")
	commitGitFile(t, dir, "fix.txt", "fix\n", "fix: patch release")
	if out, err := runCmdOutput(dir, "git", "tag", "-a", "release-candidate", "-m", "not semver"); err != nil {
		t.Fatalf("Failed to tag non-semver: %v\n%s", err, out)
	}

	tag, err := NearestReachableSemverTag(dir, "HEAD")
	if err != nil {
		t.Fatalf("NearestReachableSemverTag failed: %v", err)
	}
	if tag != "v0.2.0" {
		t.Fatalf("expected v0.2.0, got %q", tag)
	}
}

func TestNearestReachableSemverTagReturnsErrNoSemverTag(t *testing.T) {
	dir := initTestRepo(t)

	_, err := NearestReachableSemverTag(dir, "HEAD")
	if !errors.Is(err, ErrNoSemverTag) {
		t.Fatalf("expected ErrNoSemverTag, got %v", err)
	}
}

func TestNearestReachableSemverTagExcludesUnreachableSideBranchTags(t *testing.T) {
	dir := initTestRepo(t)

	tagHead(t, dir, "v0.1.0")
	mainHead, err := runCmdOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Failed to resolve main HEAD: %v", err)
	}
	if out, err := runCmdOutput(dir, "git", "checkout", "-b", "side"); err != nil {
		t.Fatalf("Failed to create side branch: %v\n%s", err, out)
	}
	commitGitFile(t, dir, "side.txt", "side\n", "feat: side-only feature")
	tagHead(t, dir, "v9.9.9")
	if out, err := runCmdOutput(dir, "git", "checkout", "-B", "main", mainHead); err != nil {
		t.Fatalf("Failed to return to main: %v\n%s", err, out)
	}
	commitGitFile(t, dir, "main.txt", "main\n", "fix: main patch")

	tag, err := NearestReachableSemverTag(dir, "HEAD")
	if err != nil {
		t.Fatalf("NearestReachableSemverTag failed: %v", err)
	}
	if tag != "v0.1.0" {
		t.Fatalf("expected v0.1.0, got %q", tag)
	}
}

func TestResolveRefRejectsEmptyAndInvalidRefs(t *testing.T) {
	dir := initTestRepo(t)

	if _, err := ResolveRef(dir, " "); err == nil || !strings.Contains(err.Error(), "git ref is required") {
		t.Fatalf("expected empty ref error, got %v", err)
	}
	if _, err := ResolveRef(dir, "missing-ref"); err == nil || !strings.Contains(err.Error(), "invalid git ref") {
		t.Fatalf("expected invalid ref error, got %v", err)
	}
}

func TestListCommitsInRangeReturnsOldestFirst(t *testing.T) {
	dir := initTestRepo(t)
	base, err := runCmdOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Failed to resolve base: %v", err)
	}

	commitGitFile(t, dir, "one.txt", "one\n", "feat: add one")
	commitGitFile(t, dir, "two.txt", "two\n", "fix: add two")

	commits, err := ListCommitsInRange(dir, base, "HEAD")
	if err != nil {
		t.Fatalf("ListCommitsInRange failed: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].Subject != "feat: add one" || commits[1].Subject != "fix: add two" {
		t.Fatalf("commits not oldest-first: %+v", commits)
	}
}

func TestListCommitsInRangeReturnsEmptyRange(t *testing.T) {
	dir := initTestRepo(t)

	commits, err := ListCommitsInRange(dir, "HEAD", "HEAD")
	if err != nil {
		t.Fatalf("ListCommitsInRange failed: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected empty range, got %+v", commits)
	}
}

func TestListCommitsInRangeParsesSubjectBodyAndDate(t *testing.T) {
	dir := initTestRepo(t)
	base, err := runCmdOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Failed to resolve base: %v", err)
	}

	sha := commitGitFile(t, dir, "feature.txt", "feature\n", "feat(parser): add parsing", "Body line one\nBody line two")

	commits, err := ListCommitsInRange(dir, base, "HEAD")
	if err != nil {
		t.Fatalf("ListCommitsInRange failed: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	commit := commits[0]
	if commit.SHA != sha {
		t.Fatalf("SHA = %q, want %q", commit.SHA, sha)
	}
	if commit.ShortSHA != sha[:7] {
		t.Fatalf("ShortSHA = %q, want %q", commit.ShortSHA, sha[:7])
	}
	if commit.Subject != "feat(parser): add parsing" {
		t.Fatalf("Subject = %q", commit.Subject)
	}
	if !strings.Contains(commit.Body, "Body line one\nBody line two") {
		t.Fatalf("Body not parsed correctly: %q", commit.Body)
	}
	if commit.Date.IsZero() {
		t.Fatal("Date should not be zero")
	}
}

func TestListCommitsInRangeUsesNULDelimiters(t *testing.T) {
	dir := initTestRepo(t)
	base, err := runCmdOutput(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("Failed to resolve base: %v", err)
	}

	subject := "feat: add parser | with pipes and %% markers"
	body := "Body with newlines\n---\nand punctuation | that should stay intact"
	commitGitFile(t, dir, "feature.txt", "feature\n", subject, body)

	commits, err := ListCommitsInRange(dir, base, "HEAD")
	if err != nil {
		t.Fatalf("ListCommitsInRange failed: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}
	if commits[0].Subject != subject {
		t.Fatalf("Subject = %q, want %q", commits[0].Subject, subject)
	}
	if commits[0].Body != body {
		t.Fatalf("Body = %q, want %q", commits[0].Body, body)
	}
}
