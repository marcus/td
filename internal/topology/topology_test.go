package topology

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a temp git repo with the given files.
func initGitRepo(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	for _, f := range files {
		fullPath := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("package main\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	run("add", ".")
	run("commit", "-m", "init")

	return dir
}

func TestScan_BasicTree(t *testing.T) {
	dir := initGitRepo(t, []string{
		"main.go",
		"cmd/root.go",
		"cmd/serve.go",
		"internal/db/db.go",
	})

	root, err := Scan(ScanOptions{RootDir: dir})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if root.Name != "." {
		t.Errorf("root name = %q, want %q", root.Name, ".")
	}

	// Should have: cmd/, internal/, main.go
	if len(root.Children) != 3 {
		t.Fatalf("root children = %d, want 3", len(root.Children))
	}

	// Directories come first (sorted)
	if root.Children[0].Name != "cmd" || !root.Children[0].IsDir {
		t.Errorf("first child = %q (dir=%v), want cmd/ dir", root.Children[0].Name, root.Children[0].IsDir)
	}
	if root.Children[1].Name != "internal" || !root.Children[1].IsDir {
		t.Errorf("second child = %q (dir=%v), want internal/ dir", root.Children[1].Name, root.Children[1].IsDir)
	}
	if root.Children[2].Name != "main.go" || root.Children[2].IsDir {
		t.Errorf("third child = %q (dir=%v), want main.go file", root.Children[2].Name, root.Children[2].IsDir)
	}
}

func TestScan_DepthLimit(t *testing.T) {
	dir := initGitRepo(t, []string{
		"main.go",
		"cmd/root.go",
		"internal/db/db.go",
	})

	root, err := Scan(ScanOptions{RootDir: dir, MaxDepth: 1})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Depth 1 means only top-level files
	if len(root.Children) != 1 {
		t.Fatalf("root children = %d, want 1 (only main.go)", len(root.Children))
	}
	if root.Children[0].Name != "main.go" {
		t.Errorf("child = %q, want main.go", root.Children[0].Name)
	}
}

func TestScan_Filter(t *testing.T) {
	dir := initGitRepo(t, []string{
		"main.go",
		"README.md",
		"cmd/root.go",
		"docs/guide.md",
	})

	root, err := Scan(ScanOptions{RootDir: dir, Filter: "*.go"})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Should only have Go files
	var files []string
	collectFiles(root, &files)
	for _, f := range files {
		if filepath.Ext(f) != ".go" {
			t.Errorf("unexpected non-Go file: %s", f)
		}
	}
	if len(files) != 2 {
		t.Errorf("file count = %d, want 2", len(files))
	}
}

func TestScan_FileCount(t *testing.T) {
	dir := initGitRepo(t, []string{
		"main.go",
		"cmd/root.go",
		"cmd/serve.go",
	})

	root, err := Scan(ScanOptions{RootDir: dir})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if root.FileCount != 3 {
		t.Errorf("root file count = %d, want 3", root.FileCount)
	}

	// cmd/ should have 2 files
	for _, child := range root.Children {
		if child.Name == "cmd" {
			if child.FileCount != 2 {
				t.Errorf("cmd file count = %d, want 2", child.FileCount)
			}
		}
	}
}

func TestAnnotateIssues(t *testing.T) {
	root := &Node{
		Name:  ".",
		IsDir: true,
		Children: []*Node{
			{Name: "cmd", Path: "cmd", IsDir: true, Children: []*Node{
				{Name: "root.go", Path: "cmd/root.go"},
			}},
			{Name: "main.go", Path: "main.go"},
		},
	}

	fileIssues := map[string][]string{
		"cmd/root.go": {"td-abc123", "td-def456"},
		"main.go":     {"td-ghi789"},
	}

	AnnotateIssues(root, fileIssues)

	if len(root.Children[0].Children[0].LinkedIssues) != 2 {
		t.Errorf("root.go issues = %d, want 2", len(root.Children[0].Children[0].LinkedIssues))
	}
	if len(root.Children[1].LinkedIssues) != 1 {
		t.Errorf("main.go issues = %d, want 1", len(root.Children[1].LinkedIssues))
	}
}

func TestScan_WithStats(t *testing.T) {
	dir := initGitRepo(t, []string{"main.go"})

	root, err := Scan(ScanOptions{RootDir: dir, WithStats: true})
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// main.go should have at least 1 commit
	if len(root.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(root.Children))
	}
	if root.Children[0].Commits < 1 {
		t.Errorf("commits = %d, want >= 1", root.Children[0].Commits)
	}
	if root.Children[0].LastModified == "" {
		t.Error("last_modified should not be empty")
	}
}

func TestRender_Basic(t *testing.T) {
	root := &Node{
		Name:  ".",
		IsDir: true,
		Children: []*Node{
			{Name: "cmd", Path: "cmd", IsDir: true, FileCount: 2, Children: []*Node{
				{Name: "root.go", Path: "cmd/root.go"},
				{Name: "serve.go", Path: "cmd/serve.go"},
			}},
			{Name: "main.go", Path: "main.go"},
		},
		FileCount: 3,
	}

	result := Render(root, RenderOptions{})

	if result == "" {
		t.Fatal("render returned empty string")
	}

	// Check that key elements are present
	if !containsStr(result, "cmd/") {
		t.Error("missing cmd/ directory")
	}
	if !containsStr(result, "main.go") {
		t.Error("missing main.go")
	}
	if !containsStr(result, "root.go") {
		t.Error("missing root.go")
	}
}

func collectFiles(node *Node, files *[]string) {
	if !node.IsDir {
		*files = append(*files, node.Path)
		return
	}
	for _, child := range node.Children {
		collectFiles(child, files)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
