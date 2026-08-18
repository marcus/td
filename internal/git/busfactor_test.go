package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBlameOutput(t *testing.T) {
	output := `af3d2c4b8e1 1 1 3
author Alice
author-mail <alice@test.com>
author-time 1700000000
author-tz +0000
committer Alice
committer-mail <alice@test.com>
committer-time 1700000000
committer-tz +0000
summary Initial commit
filename main.go
	package main
af3d2c4b8e1 2 2
author Alice
author-mail <alice@test.com>
author-time 1700000000
author-tz +0000
committer Alice
committer-mail <alice@test.com>
committer-time 1700000000
committer-tz +0000
summary Initial commit
filename main.go

bf3d2c4b8e2 3 3 1
author Bob
author-mail <bob@test.com>
author-time 1700001000
author-tz +0000
committer Bob
committer-mail <bob@test.com>
committer-time 1700001000
committer-tz +0000
summary Add func
filename main.go
	func main() {}
`

	authors, total, err := parseBlameOutput(output)
	if err != nil {
		t.Fatalf("parseBlameOutput failed: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected 3 total lines, got %d", total)
	}
	if authors["Alice"] != 2 {
		t.Errorf("Expected Alice=2, got %d", authors["Alice"])
	}
	if authors["Bob"] != 1 {
		t.Errorf("Expected Bob=1, got %d", authors["Bob"])
	}
}

func TestParseBlameOutputEmpty(t *testing.T) {
	authors, total, err := parseBlameOutput("")
	if err != nil {
		t.Fatalf("parseBlameOutput failed: %v", err)
	}
	if total != 0 {
		t.Errorf("Expected 0 total, got %d", total)
	}
	if len(authors) != 0 {
		t.Errorf("Expected empty authors, got %d", len(authors))
	}
}

func TestParseBlameOutputSkipsUncommitted(t *testing.T) {
	output := `0000000000000000000000000000000000000000 1 1 1
author Not Committed Yet
author-mail <not.committed.yet>
author-time 1700000000
author-tz +0000
committer Not Committed Yet
committer-mail <not.committed.yet>
committer-time 1700000000
committer-tz +0000
summary Version of main.go from modified
filename main.go
	new line
`
	authors, total, err := parseBlameOutput(output)
	if err != nil {
		t.Fatalf("parseBlameOutput failed: %v", err)
	}
	if total != 0 {
		t.Errorf("Expected 0 total (uncommitted skipped), got %d", total)
	}
	if len(authors) != 0 {
		t.Errorf("Expected empty authors, got %d", len(authors))
	}
}

func TestComputeBusFactor(t *testing.T) {
	tests := []struct {
		name    string
		authors []AuthorLines
		total   int
		want    int
	}{
		{
			name:    "single owner",
			authors: []AuthorLines{{Author: "Alice", Lines: 100}},
			total:   100,
			want:    1,
		},
		{
			name: "two equal owners",
			authors: []AuthorLines{
				{Author: "Alice", Lines: 50},
				{Author: "Bob", Lines: 50},
			},
			total: 100,
			want:  2, // neither alone exceeds 50%
		},
		{
			name: "three owners - two needed",
			authors: []AuthorLines{
				{Author: "Alice", Lines: 40},
				{Author: "Bob", Lines: 35},
				{Author: "Charlie", Lines: 25},
			},
			total: 100,
			want:  2,
		},
		{
			name: "many small owners",
			authors: []AuthorLines{
				{Author: "A", Lines: 20},
				{Author: "B", Lines: 20},
				{Author: "C", Lines: 20},
				{Author: "D", Lines: 20},
				{Author: "E", Lines: 20},
			},
			total: 100,
			want:  3,
		},
		{
			name:    "empty",
			authors: nil,
			total:   0,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBusFactor(tt.authors, tt.total)
			if got != tt.want {
				t.Errorf("computeBusFactor() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTopOwnerPct(t *testing.T) {
	sorted := []AuthorLines{
		{Author: "Alice", Lines: 75},
		{Author: "Bob", Lines: 25},
	}
	pct := topOwnerPct(sorted, 100)
	if pct != 75.0 {
		t.Errorf("Expected 75.0%%, got %.1f%%", pct)
	}
}

func TestTopOwnerPctEmpty(t *testing.T) {
	pct := topOwnerPct(nil, 0)
	if pct != 0 {
		t.Errorf("Expected 0%%, got %.1f%%", pct)
	}
}

func TestDirAtDepth(t *testing.T) {
	tests := []struct {
		path  string
		depth int
		want  string
	}{
		{"cmd/stats.go", 1, "cmd"},
		{"cmd/stats.go", 2, "cmd"},
		{"internal/git/git.go", 1, "internal"},
		{"internal/git/git.go", 2, "internal/git"},
		{"internal/git/git.go", 3, "internal/git"},
		{"main.go", 1, "."},
		{"main.go", 2, "."},
		{"a/b/c/d/e.go", 2, "a/b"},
		{"a/b/c/d/e.go", 3, "a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := dirAtDepth(tt.path, tt.depth)
			if got != tt.want {
				t.Errorf("dirAtDepth(%q, %d) = %q, want %q", tt.path, tt.depth, got, tt.want)
			}
		})
	}
}

func TestSortAuthorMap(t *testing.T) {
	m := map[string]int{
		"Alice":   10,
		"Bob":     50,
		"Charlie": 30,
	}

	sorted := sortAuthorMap(m)
	if len(sorted) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(sorted))
	}
	if sorted[0].Author != "Bob" || sorted[0].Lines != 50 {
		t.Errorf("Expected Bob/50 first, got %s/%d", sorted[0].Author, sorted[0].Lines)
	}
	if sorted[1].Author != "Charlie" || sorted[1].Lines != 30 {
		t.Errorf("Expected Charlie/30 second, got %s/%d", sorted[1].Author, sorted[1].Lines)
	}
	if sorted[2].Author != "Alice" || sorted[2].Lines != 10 {
		t.Errorf("Expected Alice/10 third, got %s/%d", sorted[2].Author, sorted[2].Lines)
	}
}

func TestBuildFileOwnership(t *testing.T) {
	authors := map[string]int{
		"Alice": 80,
		"Bob":   20,
	}

	fo := buildFileOwnership("cmd/main.go", authors, 100)

	if fo.Path != "cmd/main.go" {
		t.Errorf("Expected path cmd/main.go, got %s", fo.Path)
	}
	if fo.TotalLines != 100 {
		t.Errorf("Expected 100 lines, got %d", fo.TotalLines)
	}
	if fo.BusFactor != 1 {
		t.Errorf("Expected bus factor 1, got %d", fo.BusFactor)
	}
	if fo.TopOwnerPct != 80.0 {
		t.Errorf("Expected 80.0%%, got %.1f%%", fo.TopOwnerPct)
	}
	if fo.Contributors != 2 {
		t.Errorf("Expected 2 contributors, got %d", fo.Contributors)
	}
}

// TestAnalyzeBusFactorIntegration runs a real bus-factor analysis on a temp repo.
func TestAnalyzeBusFactorIntegration(t *testing.T) {
	dir := initTestRepo(t)
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Add a file by "Alice" (the configured test user is "Test User")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "add main.go"); err != nil {
		t.Fatal(err)
	}

	// Add file in subdir
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg/lib.go"), []byte("package pkg\n\nfunc Foo() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(dir, "git", "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(dir, "git", "commit", "-m", "add pkg/lib.go"); err != nil {
		t.Fatal(err)
	}

	result, err := AnalyzeBusFactor(BusFactorOptions{Depth: 2})
	if err != nil {
		t.Fatalf("AnalyzeBusFactor failed: %v", err)
	}

	if len(result.Dirs) == 0 {
		t.Fatal("Expected at least one directory result")
	}
	if len(result.Files) == 0 {
		t.Fatal("Expected at least one file result")
	}

	// All files by one author -> bus factor should be 1
	for _, d := range result.Dirs {
		if d.BusFactor != 1 {
			t.Errorf("Dir %s: expected bus factor 1 (single author), got %d", d.Path, d.BusFactor)
		}
		if d.TopOwnerPct != 100.0 {
			t.Errorf("Dir %s: expected 100%% top owner, got %.1f%%", d.Path, d.TopOwnerPct)
		}
	}
}

func TestCountFilesInDir(t *testing.T) {
	files := []FileOwnership{
		{Path: "cmd/main.go"},
		{Path: "cmd/stats.go"},
		{Path: "internal/git/git.go"},
		{Path: "main.go"},
	}

	if got := countFilesInDir(files, "cmd", 1); got != 2 {
		t.Errorf("Expected 2 files in cmd, got %d", got)
	}
	if got := countFilesInDir(files, "internal/git", 2); got != 1 {
		t.Errorf("Expected 1 file in internal/git, got %d", got)
	}
	if got := countFilesInDir(files, ".", 1); got != 1 {
		t.Errorf("Expected 1 file in root, got %d", got)
	}
}
