package git

import (
	"strings"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestNormalizeCommitSubjectRawSummary(t *testing.T) {
	got, err := NormalizeCommitSubject("  Normalize commit hook docs  ", CommitMessageOptions{
		IssueID:   "td-a1b2",
		IssueType: models.TypeFeature,
	})
	if err != nil {
		t.Fatalf("NormalizeCommitSubject returned error: %v", err)
	}

	want := "feat: Normalize commit hook docs (td-a1b2)"
	if got != want {
		t.Fatalf("NormalizeCommitSubject = %q, want %q", got, want)
	}
}

func TestNormalizeCommitSubjectMixedCasePrefix(t *testing.T) {
	got, err := NormalizeCommitSubject(" FeAt :   Normalize commit hook docs  ", CommitMessageOptions{
		IssueID: "td-a1b2",
	})
	if err != nil {
		t.Fatalf("NormalizeCommitSubject returned error: %v", err)
	}

	want := "feat: Normalize commit hook docs (td-a1b2)"
	if got != want {
		t.Fatalf("NormalizeCommitSubject = %q, want %q", got, want)
	}
}

func TestNormalizeCommitSubjectAlreadyNormalizedIsIdempotent(t *testing.T) {
	want := "chore: Normalize commit hook docs (td-a1b2)"

	got, err := NormalizeCommitSubject(want, CommitMessageOptions{})
	if err != nil {
		t.Fatalf("NormalizeCommitSubject returned error: %v", err)
	}
	if got != want {
		t.Fatalf("NormalizeCommitSubject = %q, want %q", got, want)
	}
}

func TestNormalizeCommitSubjectDuplicateTaskSuffixes(t *testing.T) {
	got, err := NormalizeCommitSubject("fix: normalize commit hook docs (TD-A1B2) (td-a1b2)", CommitMessageOptions{})
	if err != nil {
		t.Fatalf("NormalizeCommitSubject returned error: %v", err)
	}

	want := "fix: normalize commit hook docs (td-a1b2)"
	if got != want {
		t.Fatalf("NormalizeCommitSubject = %q, want %q", got, want)
	}
}

func TestNormalizeCommitSubjectMissingSummary(t *testing.T) {
	_, err := NormalizeCommitSubject("feat:   (td-a1b2)", CommitMessageOptions{})
	if err == nil {
		t.Fatal("expected missing summary error")
	}
	if !strings.Contains(err.Error(), "missing commit summary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCommitSubjectUnsupportedPrefix(t *testing.T) {
	_, err := NormalizeCommitSubject("docs: update README (td-a1b2)", CommitMessageOptions{})
	if err == nil {
		t.Fatal("expected unsupported prefix error")
	}
	if !strings.Contains(err.Error(), `unsupported commit type "docs"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCommitSubjectInvalidIssueIDSuffix(t *testing.T) {
	_, err := NormalizeCommitSubject("feat: update README (td-nothex)", CommitMessageOptions{})
	if err == nil {
		t.Fatal("expected invalid issue ID error")
	}
	if !strings.Contains(err.Error(), `invalid issue ID "td-nothex"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeCommitMessagePreservesBodyAndTrailers(t *testing.T) {
	message := "  normalize commit hook docs  \n\nBody line 1\nBody line 2\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n"

	got, err := NormalizeCommitMessage(message, CommitMessageOptions{
		IssueID:   "td-a1b2",
		IssueType: models.TypeTask,
	})
	if err != nil {
		t.Fatalf("NormalizeCommitMessage returned error: %v", err)
	}

	want := "chore: normalize commit hook docs (td-a1b2)\n\nBody line 1\nBody line 2\n\nNightshift-Task: commit-normalize\nNightshift-Ref: https://github.com/marcus/nightshift\n"
	if got != want {
		t.Fatalf("NormalizeCommitMessage = %q, want %q", got, want)
	}
}

func TestNormalizeCommitMessageSkipsSpecialGitSubjects(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "fixup autosquash commit",
			message: "fixup! feat: normalize commit hook docs (td-a1b2)\n\nbody\n",
		},
		{
			name:    "squash autosquash commit",
			message: "squash! feat: normalize commit hook docs (td-a1b2)\n",
		},
		{
			name:    "merge commit",
			message: "Merge branch 'feat/commit-message-normalizer'\n\n# Conflicts:\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCommitMessage(tt.message, CommitMessageOptions{
				IssueID:   "td-a1b2",
				IssueType: models.TypeFeature,
			})
			if err != nil {
				t.Fatalf("NormalizeCommitMessage returned error: %v", err)
			}
			if got != tt.message {
				t.Fatalf("NormalizeCommitMessage = %q, want %q", got, tt.message)
			}
		})
	}
}

func TestDefaultCommitTypeFromIssueMetadata(t *testing.T) {
	tests := []struct {
		name      string
		issueType models.Type
		want      CommitType
	}{
		{name: "feature maps to feat", issueType: models.TypeFeature, want: CommitTypeFeat},
		{name: "bug maps to fix", issueType: models.TypeBug, want: CommitTypeFix},
		{name: "task maps to chore", issueType: models.TypeTask, want: CommitTypeChore},
		{name: "chore maps to chore", issueType: models.TypeChore, want: CommitTypeChore},
		{name: "epic maps to chore", issueType: models.TypeEpic, want: CommitTypeChore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DefaultCommitType(tt.issueType)
			if err != nil {
				t.Fatalf("DefaultCommitType returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("DefaultCommitType(%q) = %q, want %q", tt.issueType, got, tt.want)
			}
		})
	}
}
