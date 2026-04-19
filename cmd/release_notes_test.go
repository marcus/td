package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initReleaseNotesRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test User"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	writeAndCommitReleaseFile(t, dir, "README.md", "# Test\n", "chore: initial commit")
	tagCmd := exec.Command("git", "tag", "v0.1.0")
	tagCmd.Dir = dir
	if err := tagCmd.Run(); err != nil {
		t.Fatalf("git tag failed: %v", err)
	}

	writeAndCommitReleaseFile(t, dir, "cmd/release_notes.go", "package cmd\n", "feat: add release notes command")
	writeAndCommitReleaseFile(t, dir, "docs/release.md", "# Release\n", "docs: add release docs")
	writeAndCommitReleaseFile(t, dir, "internal/release/release.go", "package release\n", "fix: handle empty release range")

	return dir
}

func writeAndCommitReleaseFile(t *testing.T, dir, path, contents, message string) {
	t.Helper()

	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	addCmd := exec.Command("git", "add", path)
	addCmd.Dir = dir
	if err := addCmd.Run(); err != nil {
		t.Fatalf("git add %s: %v", path, err)
	}

	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = dir
	if err := commitCmd.Run(); err != nil {
		t.Fatalf("git commit %q: %v", message, err)
	}
}

func runReleaseNotesCommand(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	saveAndRestoreGlobals(t)
	baseDir := dir
	baseDirOverride = &baseDir
	_ = releaseNotesCmd.Flags().Set("from", "")
	_ = releaseNotesCmd.Flags().Set("to", "HEAD")
	_ = releaseNotesCmd.Flags().Set("range", "")
	_ = releaseNotesCmd.Flags().Set("output", "markdown")
	_ = releaseNotesCmd.Flags().Set("include-files", "false")
	_ = releaseNotesCmd.Flags().Set("include-stats", "false")
	_ = releaseNotesCmd.Flags().Set("title", "Release Notes Draft")

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := releaseNotesCmd.RunE(releaseNotesCmd, args)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	return output.String(), runErr
}

func TestReleaseNotesCommandOutputsMarkdownDraft(t *testing.T) {
	dir := initReleaseNotesRepo(t)

	output, err := runReleaseNotesCommand(t, dir)
	if err != nil {
		t.Fatalf("RunE error: %v", err)
	}

	for _, want := range []string{
		"# Release Notes Draft",
		"## Features",
		"- Add release notes command",
		"## Bug Fixes",
		"- Handle empty release range",
		"## Documentation",
		"- Add release docs",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestReleaseNotesCommandIncludesFilesAndStats(t *testing.T) {
	dir := initReleaseNotesRepo(t)

	saveAndRestoreGlobals(t)
	baseDir := dir
	baseDirOverride = &baseDir
	_ = releaseNotesCmd.Flags().Set("include-files", "true")
	_ = releaseNotesCmd.Flags().Set("include-stats", "true")
	_ = releaseNotesCmd.Flags().Set("output", "markdown")

	var output bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	runErr := releaseNotesCmd.RunE(releaseNotesCmd, nil)

	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&output, r)

	if runErr != nil {
		t.Fatalf("RunE error: %v", runErr)
	}

	got := output.String()
	if !strings.Contains(got, "Files: `cmd/release_notes.go`") {
		t.Fatalf("expected file list in output:\n%s", got)
	}
	if !strings.Contains(got, "files changed") {
		t.Fatalf("expected diff stats in output:\n%s", got)
	}
}

func TestReleaseNotesCommandErrorsWithoutTags(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test User"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	writeAndCommitReleaseFile(t, dir, "README.md", "# Test\n", "feat: initial release prep")

	_, err := runReleaseNotesCommand(t, dir)
	if err == nil || !strings.Contains(err.Error(), "no tags found") {
		t.Fatalf("expected no-tags error, got %v", err)
	}
}
