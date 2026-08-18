package changelog

import (
	"strings"
	"testing"
	"time"
)

func TestParseCommit(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		wantTyp string
		scope   string
		desc    string
		brk     bool
	}{
		{
			name:    "feat with scope",
			subject: "feat(cli): add changelog command",
			wantTyp: "feat",
			scope:   "cli",
			desc:    "add changelog command",
		},
		{
			name:    "fix without scope",
			subject: "fix: resolve nil pointer in sync",
			wantTyp: "fix",
			desc:    "resolve nil pointer in sync",
		},
		{
			name:    "breaking change",
			subject: "refactor!: rename API endpoints",
			wantTyp: "refactor",
			desc:    "rename API endpoints",
			brk:     true,
		},
		{
			name:    "with PR reference",
			subject: "fix: resolve crash (#42)",
			wantTyp: "fix",
			desc:    "resolve crash (#42)",
		},
		{
			name:    "non-conventional commit",
			subject: "Update README",
			wantTyp: "",
			desc:    "Update README",
		},
		{
			name:    "docs type",
			subject: "docs: update API reference",
			wantTyp: "docs",
			desc:    "update API reference",
		},
		{
			name:    "test type",
			subject: "test: add integration tests for sync",
			wantTyp: "test",
			desc:    "add integration tests for sync",
		},
		{
			name:    "chore type",
			subject: "chore: bump dependencies",
			wantTyp: "chore",
			desc:    "bump dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Commit{Hash: "abc123", Subject: tt.subject}
			pc := ParseCommit(c)

			if pc.Type != tt.wantTyp {
				t.Errorf("Type = %q, want %q", pc.Type, tt.wantTyp)
			}
			if pc.Scope != tt.scope {
				t.Errorf("Scope = %q, want %q", pc.Scope, tt.scope)
			}
			if pc.Description != tt.desc {
				t.Errorf("Description = %q, want %q", pc.Description, tt.desc)
			}
			if pc.Breaking != tt.brk {
				t.Errorf("Breaking = %v, want %v", pc.Breaking, tt.brk)
			}
		})
	}
}

func TestExtractPR(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"fix: resolve crash (#42)", "42"},
		{"feat: add feature (#123)", "123"},
		{"no pr here", ""},
		{"fix: thing (not a #pr)", ""},
	}

	for _, tt := range tests {
		got := ExtractPR(tt.subject)
		if got != tt.want {
			t.Errorf("ExtractPR(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

func TestGroupCommits(t *testing.T) {
	commits := []ParsedCommit{
		{Commit: Commit{Subject: "feat: a"}, Type: "feat", Description: "a"},
		{Commit: Commit{Subject: "fix: b"}, Type: "fix", Description: "b"},
		{Commit: Commit{Subject: "feat: c"}, Type: "feat", Description: "c"},
		{Commit: Commit{Subject: "docs: d"}, Type: "docs", Description: "d"},
		{Commit: Commit{Subject: "refactor: e"}, Type: "refactor", Description: "e"},
		{Commit: Commit{Subject: "misc"}, Type: "", Description: "misc"},
	}

	categories := GroupCommits(commits)

	// Should have: Features, Bug Fixes, Documentation, Improvements, Other Changes
	if len(categories) != 5 {
		t.Fatalf("got %d categories, want 5", len(categories))
	}

	// First should be Features with 2 commits
	if categories[0].Heading != "Features" {
		t.Errorf("first category = %q, want Features", categories[0].Heading)
	}
	if len(categories[0].Commits) != 2 {
		t.Errorf("Features has %d commits, want 2", len(categories[0].Commits))
	}

	// Bug Fixes should have 1
	if categories[1].Heading != "Bug Fixes" {
		t.Errorf("second category = %q, want Bug Fixes", categories[1].Heading)
	}

	// Last should be Other Changes
	if categories[4].Heading != "Other Changes" {
		t.Errorf("last category = %q, want Other Changes", categories[4].Heading)
	}
}

func TestGroupCommits_empty(t *testing.T) {
	categories := GroupCommits(nil)
	if len(categories) != 0 {
		t.Errorf("got %d categories for nil input, want 0", len(categories))
	}
}

func TestFormatMarkdown(t *testing.T) {
	categories := []Category{
		{
			Heading: "Features",
			Commits: []ParsedCommit{
				{
					Commit:      Commit{Hash: "abc", Subject: "feat(cli): add changelog (#10)"},
					Type:        "feat",
					Scope:       "cli",
					Description: "add changelog (#10)",
				},
			},
		},
		{
			Heading: "Bug Fixes",
			Commits: []ParsedCommit{
				{
					Commit:      Commit{Hash: "def", Subject: "fix: resolve crash"},
					Type:        "fix",
					Description: "resolve crash",
				},
			},
		},
	}

	date := time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)
	output := FormatMarkdown("v0.44.0", date, categories)

	if !strings.Contains(output, "## [v0.44.0] - 2026-03-25") {
		t.Error("missing version heading")
	}
	if !strings.Contains(output, "### Features") {
		t.Error("missing Features section")
	}
	if !strings.Contains(output, "**cli** — add changelog (#10)") {
		t.Error("missing scoped commit line")
	}
	if !strings.Contains(output, "### Bug Fixes") {
		t.Error("missing Bug Fixes section")
	}
	if !strings.Contains(output, "- resolve crash") {
		t.Error("missing unscoped commit line")
	}
}

func TestFormatCommitLine_prDedup(t *testing.T) {
	// PR in both subject and explicit PR field shouldn't duplicate
	c := ParsedCommit{
		Commit:      Commit{Hash: "abc", Subject: "fix: thing (#5)", PR: "5"},
		Type:        "fix",
		Description: "thing (#5)",
	}

	line := formatCommitLine(c)
	// Should have exactly one (#5)
	if strings.Count(line, "(#5)") != 1 {
		t.Errorf("PR duplicated in line: %q", line)
	}
}
