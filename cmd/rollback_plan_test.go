package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func newFakeGit(t *testing.T) (*fakeGit, func()) {
	t.Helper()
	prev := gitRunner
	f := &fakeGit{responses: map[string]gitResponse{}}
	gitRunner = f.run
	return f, func() { gitRunner = prev }
}

type gitResponse struct {
	out string
	err error
}

type fakeGit struct {
	responses map[string]gitResponse
	calls     [][]string
}

func (f *fakeGit) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	key := args[0]
	if r, ok := f.responses[key]; ok {
		return r.out, r.err
	}
	return "", fmt.Errorf("no fake response for git %s", strings.Join(args, " "))
}

func TestBuildRollbackPlan_NoCommits(t *testing.T) {
	f, restore := newFakeGit(t)
	defer restore()
	f.responses["rev-parse"] = gitResponse{out: "deadbeef\n"}
	f.responses["log"] = gitResponse{out: ""}

	plan, err := buildRollbackPlan("main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Commits) != 0 {
		t.Errorf("expected 0 commits, got %d", len(plan.Commits))
	}
	if plan.Head != "deadbeef" {
		t.Errorf("expected HEAD=deadbeef, got %s", plan.Head)
	}
	foundNote := false
	for _, n := range plan.Notes {
		if strings.Contains(n, "nothing to roll back") {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("expected 'nothing to roll back' note, got %v", plan.Notes)
	}
}

func TestBuildRollbackPlan_EnumeratesCommits(t *testing.T) {
	const sep = "\x1f"
	const rec = "\x1e"
	logOut := strings.Join([]string{"abc123def", "abc123d", "Alice", "2026-01-01T00:00:00Z", "feat: add thing", "parent1"}, sep) + rec +
		strings.Join([]string{"fff222aaa", "fff222a", "Bob", "2026-01-02T00:00:00Z", "Merge pull request", "parent1 parent2"}, sep) + rec

	f, restore := newFakeGit(t)
	defer restore()
	f.responses["rev-parse"] = gitResponse{out: "abc123def\n"}
	f.responses["log"] = gitResponse{out: logOut}
	f.responses["show"] = gitResponse{out: "cmd/foo.go\ngo.mod\n"}

	plan, err := buildRollbackPlan("main", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(plan.Commits))
	}
	if plan.Commits[0].IsMerge {
		t.Errorf("first commit should not be merge")
	}
	if !plan.Commits[1].IsMerge {
		t.Errorf("second commit should be merge")
	}
	// Revert command for non-merge:
	if plan.RevertCommands[0] != "git revert --no-edit abc123d" {
		t.Errorf("unexpected revert cmd: %s", plan.RevertCommands[0])
	}
	// Revert command for merge:
	if plan.RevertCommands[1] != "git revert -m 1 --no-edit fff222a" {
		t.Errorf("unexpected merge revert cmd: %s", plan.RevertCommands[1])
	}
	// Files deduplicated and sorted:
	wantFiles := []string{"cmd/foo.go", "go.mod"}
	if len(plan.FilesTouched) != len(wantFiles) {
		t.Fatalf("expected %d files, got %d (%v)", len(wantFiles), len(plan.FilesTouched), plan.FilesTouched)
	}
	for i, f := range wantFiles {
		if plan.FilesTouched[i] != f {
			t.Errorf("files[%d]=%s want %s", i, plan.FilesTouched[i], f)
		}
	}
	// go.mod triggers a dependency note.
	foundDep := false
	foundMerge := false
	for _, n := range plan.Notes {
		if strings.Contains(n, "Dependency manifest") {
			foundDep = true
		}
		if strings.Contains(n, "is a merge") {
			foundMerge = true
		}
	}
	if !foundDep {
		t.Errorf("expected dependency note in %v", plan.Notes)
	}
	if !foundMerge {
		t.Errorf("expected merge note in %v", plan.Notes)
	}
}

func TestRenderRollbackPlan_Formats(t *testing.T) {
	plan := &RollbackPlan{
		Base: "main",
		Head: "abc123def",
		Commits: []CommitInfo{{
			SHA:     "abc123def",
			Short:   "abc123d",
			Author:  "Alice",
			Date:    "2026-01-01T00:00:00Z",
			Subject: "feat: add thing | with pipe",
			Files:   []string{"cmd/foo.go"},
		}},
		FilesTouched:   []string{"cmd/foo.go"},
		RevertCommands: []string{"git revert --no-edit abc123d"},
		Notes:          []string{"check things"},
	}

	text, err := renderRollbackPlan(plan, "text")
	if err != nil {
		t.Fatalf("text render: %v", err)
	}
	if !strings.Contains(text, "abc123d") || !strings.Contains(text, "git revert --no-edit abc123d") {
		t.Errorf("text output missing data: %s", text)
	}

	md, err := renderRollbackPlan(plan, "markdown")
	if err != nil {
		t.Fatalf("markdown render: %v", err)
	}
	if !strings.Contains(md, "# Rollback Plan") || !strings.Contains(md, "| `abc123d` |") {
		t.Errorf("markdown output unexpected: %s", md)
	}
	if !strings.Contains(md, "with pipe") || !strings.Contains(md, "\\|") {
		t.Errorf("markdown pipe escaping missing: %s", md)
	}

	jsonOut, err := renderRollbackPlan(plan, "json")
	if err != nil {
		t.Fatalf("json render: %v", err)
	}
	var round RollbackPlan
	if err := json.Unmarshal([]byte(jsonOut), &round); err != nil {
		t.Fatalf("json invalid: %v\n%s", err, jsonOut)
	}
	if round.Head != "abc123def" || len(round.Commits) != 1 {
		t.Errorf("json roundtrip mismatch: %+v", round)
	}

	if _, err := renderRollbackPlan(plan, "bogus"); err == nil {
		t.Errorf("expected error for unknown format")
	}
}

func TestParseCommitLog_TrailingWhitespace(t *testing.T) {
	const sep = "\x1f"
	const rec = "\x1e"
	raw := strings.Join([]string{"abc", "a", "Alice", "2026-01-01", "msg", "p1"}, sep) + rec + "\n  \n"
	commits, err := parseCommitLog(raw, sep, rec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 1 {
		t.Errorf("expected 1 commit, got %d", len(commits))
	}
}

func TestDeriveNotes_ConfigAndMigration(t *testing.T) {
	files := []string{"db/migrations/0001_init.sql", "deploy/config.yaml", "src/index.js"}
	notes := deriveNotes(files, nil)
	var hasMigration, hasConfig bool
	for _, n := range notes {
		if strings.Contains(n, "Database migration") {
			hasMigration = true
		}
		if strings.Contains(n, "Config file") {
			hasConfig = true
		}
	}
	if !hasMigration {
		t.Errorf("expected migration note: %v", notes)
	}
	if !hasConfig {
		t.Errorf("expected config note: %v", notes)
	}
}
