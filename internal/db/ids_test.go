package db

import (
	"strings"
	"testing"
)

func TestNormalizeIssueID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"bare hex gets prefix", "abc123", "td-abc123"},
		{"already prefixed", "td-abc123", "td-abc123"},
		{"prefix-like text without dash is prefixed", "tdabc", "td-tdabc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeIssueID(tt.in); got != tt.want {
				t.Errorf("NormalizeIssueID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerateIDPrefixesAndLengths(t *testing.T) {
	cases := []struct {
		name      string
		gen       func() (string, error)
		prefix    string
		hexDigits int
	}{
		{"generateID", generateID, "td-", 6},
		{"generateWSID", generateWSID, "ws-", 4},
		{"generateBoardID", generateBoardID, "bd-", 8},
		{"generateLogID", generateLogID, "lg-", 8},
		{"generateHandoffID", generateHandoffID, "ho-", 8},
		{"generateCommentID", generateCommentID, "cm-", 8},
		{"generateSnapshotID", generateSnapshotID, "gs-", 8},
		{"generateNoteID", generateNoteID, "nt-", 6},
		{"generateActionID", generateActionID, "al-", 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := c.gen()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(id, c.prefix) {
				t.Errorf("id %q missing prefix %q", id, c.prefix)
			}
			suffix := strings.TrimPrefix(id, c.prefix)
			if len(suffix) != c.hexDigits {
				t.Errorf("id %q suffix length = %d, want %d", id, len(suffix), c.hexDigits)
			}
			if !isHex(suffix) {
				t.Errorf("id %q suffix %q is not lowercase hex", id, suffix)
			}
		})
	}
}

func TestGenerateIDUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id, err := generateID()
		if err != nil {
			t.Fatalf("generateID error: %v", err)
		}
		if _, dup := seen[id]; dup {
			// 6 hex chars = 16.7M space; 100 samples should not collide.
			t.Fatalf("duplicate id within 100 iterations: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestIDGeneratorOverride(t *testing.T) {
	orig := idGenerator
	t.Cleanup(func() { idGenerator = orig })

	idGenerator = func() (string, error) { return "td-fixed1", nil }
	got, err := generateID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "td-fixed1" {
		t.Errorf("generateID() = %q, want td-fixed1", got)
	}
}

func TestDeterministicIDsAreStable(t *testing.T) {
	t.Run("BoardIssuePosID", func(t *testing.T) {
		a := BoardIssuePosID("bd-1", "td-abc")
		b := BoardIssuePosID("bd-1", "td-abc")
		if a != b {
			t.Errorf("not stable: %s vs %s", a, b)
		}
		if !strings.HasPrefix(a, "bip_") {
			t.Errorf("wrong prefix: %s", a)
		}
		if len(strings.TrimPrefix(a, "bip_")) != 16 {
			t.Errorf("expected 16-char hash, got %s", a)
		}
	})

	t.Run("DependencyID", func(t *testing.T) {
		a := DependencyID("td-a", "td-b", "blocks")
		b := DependencyID("td-a", "td-b", "blocks")
		c := DependencyID("td-a", "td-b", "depends_on")
		if a != b {
			t.Errorf("not stable: %s vs %s", a, b)
		}
		if a == c {
			t.Errorf("different relation should differ: %s == %s", a, c)
		}
		if !strings.HasPrefix(a, "dep_") {
			t.Errorf("wrong prefix: %s", a)
		}
	})

	t.Run("IssueFileID normalizes paths", func(t *testing.T) {
		clean := IssueFileID("td-abc", "src/main.go")
		dirty := IssueFileID("td-abc", "src/../src/main.go")
		if clean != dirty {
			t.Errorf("cleaned and uncleaned paths should produce same id: %s vs %s", clean, dirty)
		}
		if !strings.HasPrefix(clean, "ifl_") {
			t.Errorf("wrong prefix: %s", clean)
		}
		if len(strings.TrimPrefix(clean, "ifl_")) != 16 {
			t.Errorf("expected 16-char hash, got %s", clean)
		}
	})

	t.Run("WsiID", func(t *testing.T) {
		a := WsiID("ws-1", "td-x")
		b := WsiID("ws-1", "td-x")
		different := WsiID("ws-1", "td-y")
		if a != b {
			t.Errorf("not stable: %s vs %s", a, b)
		}
		if a == different {
			t.Errorf("different inputs should differ")
		}
		if !strings.HasPrefix(a, "wsi_") {
			t.Errorf("wrong prefix: %s", a)
		}
	})
}

func TestDeterministicIDDistinguishesComponents(t *testing.T) {
	// Ensures the separator prevents naive concatenation collisions
	// like ("ab","cd") vs ("a","bcd").
	a := BoardIssuePosID("ab", "cd")
	b := BoardIssuePosID("a", "bcd")
	if a == b {
		t.Errorf("naive concat collision: %s == %s", a, b)
	}
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return len(s) > 0
}
