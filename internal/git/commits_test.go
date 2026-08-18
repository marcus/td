package git

import (
	"testing"
)

func TestListCommitsBetween(t *testing.T) {
	// This test runs inside the actual td repo, so we can use real tags.
	tag, err := LatestTag()
	if err != nil {
		t.Skip("no tags found, skipping")
	}

	prev, err := PreviousTag(tag)
	if err != nil {
		t.Skip("no previous tag found, skipping")
	}

	commits, err := ListCommitsBetween(prev, tag)
	if err != nil {
		t.Fatalf("ListCommitsBetween(%s, %s) error: %v", prev, tag, err)
	}

	if len(commits) == 0 {
		t.Fatalf("expected commits between %s and %s, got none", prev, tag)
	}

	// Verify commit structure
	for _, c := range commits {
		if c.Hash == "" {
			t.Error("commit has empty hash")
		}
		if c.Subject == "" {
			t.Error("commit has empty subject")
		}
	}
}

func TestLatestTag(t *testing.T) {
	tag, err := LatestTag()
	if err != nil {
		t.Skip("no tags, skipping")
	}
	if tag == "" {
		t.Error("LatestTag returned empty string without error")
	}
}

func TestListCommitsBetween_emptyRange(t *testing.T) {
	// HEAD..HEAD should return no commits
	commits, err := ListCommitsBetween("HEAD", "HEAD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("expected 0 commits for HEAD..HEAD, got %d", len(commits))
	}
}
