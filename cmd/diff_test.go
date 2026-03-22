package cmd

import (
	"testing"

	"github.com/marcus/td/internal/diff"
)

func TestDiffCommand_Registered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "diff" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("diff command not registered on rootCmd")
	}
}

func TestDiffCommand_Flags(t *testing.T) {
	ResetDiffFlags()
	f := GetDiffFlags()
	if f.Staged {
		t.Error("default staged should be false")
	}
	if f.Ref != "HEAD~1" {
		t.Errorf("default ref should be HEAD~1, got %s", f.Ref)
	}
	if f.Issue != "" {
		t.Errorf("default issue should be empty, got %s", f.Issue)
	}
	if f.JSON {
		t.Error("default json should be false")
	}
}

func TestDiffCommand_HasFlags(t *testing.T) {
	cmd := diffCmd
	flags := []string{"staged", "ref", "issue", "json"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s to be defined", name)
		}
	}
}

func TestParseDiffOutput_GoFile(t *testing.T) {
	rawDiff := `diff --git a/main.go b/main.go
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/main.go
@@ -0,0 +1,5 @@
+package main
+
+func Hello() string {
+	return "hi"
+}
`
	summaries := ParseDiffOutput(rawDiff, "HEAD~1", false)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.Path != "main.go" {
		t.Errorf("expected path main.go, got %s", s.Path)
	}
	if s.Language != "go" {
		t.Errorf("expected language go, got %s", s.Language)
	}
	if s.Status != "added" {
		t.Errorf("expected status added, got %s", s.Status)
	}
}

func TestParseDiffOutput_NonGoFile(t *testing.T) {
	rawDiff := `diff --git a/README.md b/README.md
index abc..def 100644
--- a/README.md
+++ b/README.md
@@ -1 +1,2 @@
 # Project
+New content
`
	summaries := ParseDiffOutput(rawDiff, "HEAD~1", false)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	s := summaries[0]
	if s.Language != "markdown" {
		t.Errorf("expected language markdown, got %s", s.Language)
	}
	if s.Category != "documentation" {
		t.Errorf("expected category documentation, got %s", s.Category)
	}
}

func TestStatusToIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"added", "+"},
		{"deleted", "-"},
		{"renamed", "~"},
		{"modified", "*"},
	}
	for _, tt := range tests {
		got := statusToIcon(tt.status)
		if got != tt.want {
			t.Errorf("statusToIcon(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestChangeKindIcon(t *testing.T) {
	tests := []struct {
		kind diff.ChangeKind
		want string
	}{
		{diff.ChangeAdded, "+"},
		{diff.ChangeRemoved, "-"},
		{diff.ChangeModified, "~"},
	}
	for _, tt := range tests {
		got := changeKindIcon(tt.kind)
		if got != tt.want {
			t.Errorf("changeKindIcon(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
