package db

import (
	"testing"
)

func TestToRepoRelative_BasicPath(t *testing.T) {
	rel, err := ToRepoRelative("/home/user/project/src/main.go", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "src/main.go" {
		t.Errorf("expected src/main.go, got %s", rel)
	}
}

func TestToRepoRelative_RootFile(t *testing.T) {
	rel, err := ToRepoRelative("/home/user/project/README.md", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "README.md" {
		t.Errorf("expected README.md, got %s", rel)
	}
}

func TestToRepoRelative_OutsideRepo(t *testing.T) {
	_, err := ToRepoRelative("/other/path/file.go", "/home/user/project")
	if err == nil {
		t.Error("expected error for path outside repo")
	}
}

func TestToRepoRelative_SamePath(t *testing.T) {
	rel, err := ToRepoRelative("/home/user/project", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel != "." {
		t.Errorf("expected '.', got %s", rel)
	}
}

func TestIsAbsolutePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/usr/bin/go", true},
		{"/home/user/file.go", true},
		{"src/main.go", false},
		{"./relative/path", false},
		{"relative", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsAbsolutePath(tt.path)
		if got != tt.want {
			t.Errorf("IsAbsolutePath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeFilePathForID(t *testing.T) {
	// Forward slashes should be preserved
	result := NormalizeFilePathForID("src/main.go")
	if result != "src/main.go" {
		t.Errorf("expected src/main.go, got %s", result)
	}

	// Dot segments should be cleaned
	result = NormalizeFilePathForID("src/../pkg/main.go")
	if result != "pkg/main.go" {
		t.Errorf("expected pkg/main.go, got %s", result)
	}
}

func TestIssueFileID_CrossPlatformConsistency(t *testing.T) {
	// Both forward and backslash paths should produce the same ID
	id1 := IssueFileID("td-abc123", "src/main.go")
	id2 := IssueFileID("td-abc123", "src/main.go")
	if id1 != id2 {
		t.Errorf("IDs should match: %s vs %s", id1, id2)
	}

	// Cleaned path should match uncleaned
	id3 := IssueFileID("td-abc123", "src/../src/main.go")
	if id1 != id3 {
		t.Errorf("Cleaned path ID should match: %s vs %s", id1, id3)
	}
}
