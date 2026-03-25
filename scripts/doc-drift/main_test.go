package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"cmd/foo.go", false},
		{"#anchor", false},
	}
	for _, tt := range tests {
		if got := isURL(tt.input); got != tt.want {
			t.Errorf("isURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsPathLike(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"cmd/foo.go", true},
		{"internal/db/", true},
		{"schema.go", false},  // bare filename, too ambiguous
		{"config.json", false}, // bare filename, too ambiguous
		{"some-word", false},
		{"tea.Cmd", false},         // Go type, not a path
		{"tea.Batch", false},       // Go type, not a path
		{"saveFilterState()", false}, // function call
		{"foo bar", false},
		{"https://example.com", false},
		{"v0.3.0", false}, // version string
		{"cmd/", true},
		{"docs/modal-system.md", true},
		{"sidecar/internal/plugins/tdmonitor/plugin.go:241-250", true},
		{".todos/issues.db", true},
	}
	for _, tt := range tests {
		if got := isPathLike(tt.input); got != tt.want {
			t.Errorf("isPathLike(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestStripLineRef(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"foo.go:42", "foo.go"},
		{"plugin.go:241-250", "plugin.go"},
		{"cmd/foo.go", "cmd/foo.go"},
		{"internal/db/", "internal/db/"},
	}
	for _, tt := range tests {
		if got := stripLineRef(tt.input); got != tt.want {
			t.Errorf("stripLineRef(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractRefs(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "test.md")
	content := `# Test Doc

See [modal guide](docs/modal-system.md) for details.

Code lives in ` + "`cmd/foo.go`" + ` and ` + "`internal/db/`" + `.

` + "```bash" + `
go build -o td .
` + "```" + `

[External](https://example.com) links are skipped.
`
	if err := os.WriteFile(md, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	refs, err := extractRefs(dir, md)
	if err != nil {
		t.Fatal(err)
	}

	// Should find: docs/modal-system.md (link), cmd/foo.go (backtick), internal/db/ (backtick)
	// Should NOT find: https://example.com, "go build -o td ."
	wantPaths := map[string]string{
		"docs/modal-system.md": "link",
		"cmd/foo.go":           "backtick",
		"internal/db/":         "backtick",
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.path] = r.kind
	}
	for path, kind := range wantPaths {
		if got[path] != kind {
			t.Errorf("expected ref %q (kind=%s), got kind=%q", path, kind, got[path])
		}
	}
	if _, ok := got["https://example.com"]; ok {
		t.Error("should not extract URL as ref")
	}
}

func TestValidateRefs(t *testing.T) {
	root := t.TempDir()

	// Create some real files/dirs
	os.MkdirAll(filepath.Join(root, "cmd"), 0755)
	os.WriteFile(filepath.Join(root, "cmd", "foo.go"), []byte("package cmd"), 0644)
	os.MkdirAll(filepath.Join(root, "internal", "db"), 0755)

	refs := []ref{
		{file: "README.md", line: 1, path: "cmd/foo.go", kind: "backtick"},
		{file: "README.md", line: 2, path: "internal/db/", kind: "backtick"},
		{file: "README.md", line: 3, path: "docs/missing.md", kind: "link"},
		{file: "README.md", line: 4, path: "nonexistent/path/", kind: "backtick"},
	}

	broken := validateRefs(root, refs)
	if len(broken) != 2 {
		t.Fatalf("expected 2 broken refs, got %d: %+v", len(broken), broken)
	}
	if broken[0].path != "docs/missing.md" {
		t.Errorf("expected first broken ref to be docs/missing.md, got %s", broken[0].path)
	}
	if broken[1].path != "nonexistent/path/" {
		t.Errorf("expected second broken ref to be nonexistent/path/, got %s", broken[1].path)
	}
}

func TestValidateRefsWithLineNumbers(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "cmd"), 0755)
	os.WriteFile(filepath.Join(root, "cmd", "foo.go"), []byte("package cmd"), 0644)

	refs := []ref{
		{file: "README.md", line: 1, path: "cmd/foo.go:42", kind: "backtick"},
		{file: "README.md", line: 2, path: "cmd/foo.go:10-20", kind: "backtick"},
	}

	broken := validateRefs(root, refs)
	if len(broken) != 0 {
		t.Fatalf("expected 0 broken refs for line-numbered paths, got %d: %+v", len(broken), broken)
	}
}
