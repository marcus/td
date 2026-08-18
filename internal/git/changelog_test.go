package git

import (
	"testing"
)

func TestParseConventionalCommit(t *testing.T) {
	tests := []struct {
		name       string
		subject    string
		body       string
		wantType   string
		wantScope  string
		wantBreak  bool
		wantSubj   string
	}{
		{
			name:      "feat with scope",
			subject:   "feat(cli): add changelog command",
			wantType:  "feat",
			wantScope: "cli",
			wantSubj:  "add changelog command",
		},
		{
			name:     "fix without scope",
			subject:  "fix: correct timestamp parsing",
			wantType: "fix",
			wantSubj: "correct timestamp parsing",
		},
		{
			name:      "breaking with bang",
			subject:   "feat(api)!: remove deprecated endpoint",
			wantType:  "feat",
			wantScope: "api",
			wantBreak: true,
			wantSubj:  "remove deprecated endpoint",
		},
		{
			name:      "breaking in body",
			subject:   "refactor(db): change schema",
			body:      "BREAKING CHANGE: columns renamed",
			wantType:  "refactor",
			wantScope: "db",
			wantBreak: true,
			wantSubj:  "change schema",
		},
		{
			name:     "chore",
			subject:  "chore: update dependencies",
			wantType: "chore",
			wantSubj: "update dependencies",
		},
		{
			name:     "non-conventional",
			subject:  "Update README",
			wantType: "other",
			wantSubj: "Update README",
		},
		{
			name:     "merge commit",
			subject:  "Merge branch 'feature' into main",
			wantType: "other",
			wantSubj: "Merge branch 'feature' into main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci := ParseConventionalCommit(tt.subject, tt.body)
			if ci.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", ci.Type, tt.wantType)
			}
			if ci.Scope != tt.wantScope {
				t.Errorf("Scope = %q, want %q", ci.Scope, tt.wantScope)
			}
			if ci.IsBreaking != tt.wantBreak {
				t.Errorf("IsBreaking = %v, want %v", ci.IsBreaking, tt.wantBreak)
			}
			if ci.Subject != tt.wantSubj {
				t.Errorf("Subject = %q, want %q", ci.Subject, tt.wantSubj)
			}
		})
	}
}

func TestGroupCommitsByType(t *testing.T) {
	commits := []CommitInfo{
		{Type: "feat", Subject: "add A"},
		{Type: "feat", Subject: "add B"},
		{Type: "fix", Subject: "fix C"},
		{Type: "chore", Subject: "update deps"},
		{Type: "other", Subject: "random commit"},
		{Type: "unknown_type", Subject: "weird"},
	}

	grouped := GroupCommitsByType(commits)

	if len(grouped["feat"]) != 2 {
		t.Errorf("feat count = %d, want 2", len(grouped["feat"]))
	}
	if len(grouped["fix"]) != 1 {
		t.Errorf("fix count = %d, want 1", len(grouped["fix"]))
	}
	if len(grouped["chore"]) != 1 {
		t.Errorf("chore count = %d, want 1", len(grouped["chore"]))
	}
	// "unknown_type" should be bucketed under "other"
	if len(grouped["other"]) != 2 {
		t.Errorf("other count = %d, want 2 (including unknown_type)", len(grouped["other"]))
	}
}
