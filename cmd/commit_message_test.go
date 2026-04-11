package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := commitMessageCmd.RunE(commitMessageCmd, args)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

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
	if err := os.WriteFile(messagePath, []byte(initial), 0644); err != nil {
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
	if err := os.WriteFile(messagePath, []byte(want), 0644); err != nil {
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

	_, err = runCommitMessageCommand(t, dir, []string{"docs: update README"}, "", "", "")
	if err == nil {
		t.Fatal("expected unsupported prefix error")
	}
	if !strings.Contains(err.Error(), `unsupported commit type "docs"`) {
		t.Fatalf("unexpected malformed input error: %v", err)
	}
}
