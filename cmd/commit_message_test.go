package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

func runCommitMessageCommand(t *testing.T, dir string, args []string, issueFlag, typeFlag, fileFlag string) (string, error) {
	t.Helper()

	saveAndRestoreGlobals(t)

	baseDir := dir
	baseDirOverride = &baseDir

	_ = commitMessageCmd.Flags().Set("issue", "")
	_ = commitMessageCmd.Flags().Set("type", "")
	_ = commitMessageCmd.Flags().Set("file", "")

	if issueFlag != "" {
		_ = commitMessageCmd.Flags().Set("issue", issueFlag)
	}
	if typeFlag != "" {
		_ = commitMessageCmd.Flags().Set("type", typeFlag)
	}
	if fileFlag != "" {
		_ = commitMessageCmd.Flags().Set("file", fileFlag)
	}

	var output bytes.Buffer
	commitMessageCmd.SetOut(&output)

	runErr := commitMessageCmd.RunE(commitMessageCmd, args)
	return strings.TrimSpace(output.String()), runErr
}

func TestCommitMessageCommandPrintsNormalizedSubject(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Normalize commit hook docs",
		Type:  models.TypeFeature,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	got, err := runCommitMessageCommand(t, dir, []string{"Normalize commit hook docs"}, "", "", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "feat: Normalize commit hook docs (" + issue.ID + ")"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandUsesSubjectSuffixIssue(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Fix retry regression",
		Type:  models.TypeBug,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	got, err := runCommitMessageCommand(t, dir, []string{"Fix retry regression (" + strings.ToUpper(issue.ID) + ")"}, "", "", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "fix: Fix retry regression (" + issue.ID + ")"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandAllowsTypedNoIssueSubjectWithoutFocus(t *testing.T) {
	dir := t.TempDir()

	got, err := runCommitMessageCommand(t, dir, []string{" DoCs :   Update changelog for v0.43.0  "}, "", "", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "docs: Update changelog for v0.43.0"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandRejectsNoIssueFeatureAndFixSubjectsWithoutFocus(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		typeFlag string
	}{
		{name: "typed feat subject", args: []string{"feat: add release notes command"}},
		{name: "typed fix subject", args: []string{"fix: patch nil pointer in sync loop"}},
		{name: "explicit feat override", args: []string{"Add release notes command"}, typeFlag: "feat"},
		{name: "explicit fix override", args: []string{"Patch nil pointer in sync loop"}, typeFlag: "fix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			_, err := runCommitMessageCommand(t, dir, tt.args, "", tt.typeFlag, "")
			if err == nil {
				t.Fatal("expected missing issue error")
			}
			if !strings.Contains(err.Error(), "requires a td issue") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCommitMessageCommandAllowsExplicitTypeWithoutIssueEvenWhenFocused(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Normalize commit hook docs",
		Type:  models.TypeFeature,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	got, err := runCommitMessageCommand(t, dir, []string{"Update changelog for v0.43.0"}, "", "docs", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "docs: Update changelog for v0.43.0"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandUsesFocusedIssueWithExplicitFixType(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Fix retry regression",
		Type:  models.TypeTask,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	got, err := runCommitMessageCommand(t, dir, []string{"Patch retry regression"}, "", "fix", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "fix: Patch retry regression (" + issue.ID + ")"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandNormalizesHumanAuthoredMergeSubject(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Merge support docs",
		Type:  models.TypeFeature,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	got, err := runCommitMessageCommand(t, dir, []string{"Merge support docs"}, "", "", "")
	if err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	want := "feat: Merge support docs (" + issue.ID + ")"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestCommitMessageCommandRewritesFileInPlace(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Fix retry regression",
		Type:  models.TypeBug,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	messagePath := filepath.Join(dir, "COMMIT_EDITMSG")
	initial := "  Fix :   Fix retry regression  (" + strings.ToUpper(issue.ID) + ")  \n\nBody line\n\nNightshift-Task: commit-normalize\n"
	if err := os.WriteFile(messagePath, []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := runCommitMessageCommand(t, dir, nil, "", "", messagePath); err != nil {
		t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
	}

	got, err := os.ReadFile(messagePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	want := "fix: Fix retry regression (" + issue.ID + ")\n\nBody line\n\nNightshift-Task: commit-normalize\n"
	if string(got) != want {
		t.Fatalf("commit message = %q, want %q", string(got), want)
	}
}

func TestCommitMessageCommandFileRewriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Normalize commit hook docs",
		Type:  models.TypeTask,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	messagePath := filepath.Join(dir, "COMMIT_EDITMSG")
	want := "chore: Normalize commit hook docs (" + issue.ID + ")\n\nBody line\n"
	if err := os.WriteFile(messagePath, []byte(want), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if _, err := runCommitMessageCommand(t, dir, nil, "", "", messagePath); err != nil {
		t.Fatalf("first run returned error: %v", err)
	}
	if _, err := runCommitMessageCommand(t, dir, nil, "", "", messagePath); err != nil {
		t.Fatalf("second run returned error: %v", err)
	}

	got, err := os.ReadFile(messagePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != want {
		t.Fatalf("commit message = %q, want %q", string(got), want)
	}
}

func TestCommitMessageCommandSkipsSpecialGitSubjectsInFileMode(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "fixup autosquash subject",
			message: "fixup! feat: normalize commit hook docs (td-a1b2)\n\nbody\n",
		},
		{
			name:    "merge subject",
			message: "Merge branch 'feat/commit-message-normalizer'\n\nbody\n",
		},
		{
			name:    "revert subject",
			message: "Revert \"feat: add commit message normalizer (td-a1b2)\"\n\nThis reverts commit 0123456789abcdef.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			messagePath := filepath.Join(dir, "COMMIT_EDITMSG")
			if err := os.WriteFile(messagePath, []byte(tt.message), 0o644); err != nil {
				t.Fatalf("WriteFile failed: %v", err)
			}

			if _, err := runCommitMessageCommand(t, dir, nil, "", "", messagePath); err != nil {
				t.Fatalf("commitMessageCmd.RunE returned error: %v", err)
			}

			got, err := os.ReadFile(messagePath)
			if err != nil {
				t.Fatalf("ReadFile failed: %v", err)
			}
			if string(got) != tt.message {
				t.Fatalf("commit message = %q, want %q", string(got), tt.message)
			}
		})
	}
}

func TestCommitMessageCommandReturnsClearErrorsForMalformedInput(t *testing.T) {
	dir := t.TempDir()

	database, err := db.Initialize(dir)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer database.Close()

	issue := &models.Issue{
		Title: "Normalize commit hook docs",
		Type:  models.TypeTask,
	}
	if err := database.CreateIssue(issue); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
	if err := config.SetFocus(dir, issue.ID); err != nil {
		t.Fatalf("SetFocus failed: %v", err)
	}

	_, err = runCommitMessageCommand(t, dir, nil, "", "", "")
	if err == nil {
		t.Fatal("expected missing summary error")
	}
	if !strings.Contains(err.Error(), "summary required") {
		t.Fatalf("unexpected missing summary error: %v", err)
	}

	_, err = runCommitMessageCommand(t, dir, []string{"build: ship release"}, "", "", "")
	if err == nil {
		t.Fatal("expected unsupported prefix error")
	}
	if !strings.Contains(err.Error(), `unsupported commit type "build"`) {
		t.Fatalf("unexpected malformed input error: %v", err)
	}

	_, err = runCommitMessageCommand(t, dir, []string{"Normalize commit hook docs"}, ".", "", "")
	if err != nil {
		t.Fatalf("expected --issue . to resolve focused issue: %v", err)
	}
}

func TestCommitMessageCommandIssueDotWithoutFocusReturnsClearError(t *testing.T) {
	dir := t.TempDir()

	_, err := runCommitMessageCommand(t, dir, []string{"Normalize commit hook docs"}, ".", "", "")
	if err == nil {
		t.Fatal("expected no focus error")
	}
	if !strings.Contains(err.Error(), "no focused issue available for --issue .") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallHooksUsesSharedRepoRootInLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not available")
	}

	repo := t.TempDir()
	copyRepoFile(t, "Makefile", filepath.Join(repo, "Makefile"))
	copyRepoFile(t, "scripts/pre-commit.sh", filepath.Join(repo, "scripts", "pre-commit.sh"))
	copyRepoFile(t, "scripts/commit-msg.sh", filepath.Join(repo, "scripts", "commit-msg.sh"))

	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "add", "Makefile", "scripts/pre-commit.sh", "scripts/commit-msg.sh")
	runGit(t, repo, "commit", "-m", "fixture")

	wtPath := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", wtPath, "-b", "feature/hooks")

	cmd := exec.Command("make", "install-hooks")
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make install-hooks failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	hooksDir := strings.TrimSpace(runGitOutput(t, wtPath, "rev-parse", "--git-path", "hooks"))
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(wtPath, hooksDir)
	}

	preCommitTarget, err := os.Readlink(filepath.Join(hooksDir, "pre-commit"))
	if err != nil {
		t.Fatalf("Readlink pre-commit failed: %v", err)
	}
	commitMsgTarget, err := os.Readlink(filepath.Join(hooksDir, "commit-msg"))
	if err != nil {
		t.Fatalf("Readlink commit-msg failed: %v", err)
	}

	wantPreCommit := filepath.Join(repo, "scripts", "pre-commit.sh")
	wantCommitMsg := filepath.Join(repo, "scripts", "commit-msg.sh")
	assertSamePath(t, wantPreCommit, preCommitTarget)
	assertSamePath(t, wantCommitMsg, commitMsgTarget)
	if strings.Contains(preCommitTarget, wtPath) || strings.Contains(commitMsgTarget, wtPath) {
		t.Fatalf("hook targets should not point at worktree path %q", wtPath)
	}

	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("RemoveAll worktree failed: %v", err)
	}

	resolvedPreCommit, err := filepath.EvalSymlinks(filepath.Join(hooksDir, "pre-commit"))
	if err != nil {
		t.Fatalf("EvalSymlinks pre-commit failed after removing worktree: %v", err)
	}
	resolvedCommitMsg, err := filepath.EvalSymlinks(filepath.Join(hooksDir, "commit-msg"))
	if err != nil {
		t.Fatalf("EvalSymlinks commit-msg failed after removing worktree: %v", err)
	}
	assertSamePath(t, wantPreCommit, resolvedPreCommit)
	assertSamePath(t, wantCommitMsg, resolvedCommitMsg)
}

func copyRepoFile(t *testing.T, repoRelativePath, dst string) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(thisFile))
	src := filepath.Join(repoRoot, repoRelativePath)

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile %s failed: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatalf("WriteFile %s failed: %v", dst, err)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}
