package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFileProducesIdenticalCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.txt")
	dst := filepath.Join(dir, "dest.txt")

	content := []byte("hello world — test data with UTF-8: é€\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestSyncEntityValidatorAcceptsAllEntities(t *testing.T) {
	// Every entity type that the sync engine can produce must be accepted
	entities := []string{
		"issues", "logs", "comments", "handoffs", "boards",
		"work_sessions", "board_issue_positions",
		"issue_dependencies", "issue_files",
		"work_session_issues", // must not be missing
	}
	for _, entity := range entities {
		if !syncEntityValidator(entity) {
			t.Errorf("syncEntityValidator rejected %q — entity will never sync", entity)
		}
	}
}

func TestCopyFileNonexistentSourceReturnsNil(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "dest"))
	if err != nil {
		t.Errorf("expected nil for nonexistent source, got: %v", err)
	}
}
