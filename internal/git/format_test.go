package git

import (
	"strings"
	"testing"
)

func TestFormatChangelog(t *testing.T) {
	grouped := map[string][]CommitInfo{
		"feat": {
			{Hash: "abc1234567", Subject: "add changelog command", Scope: "cli"},
			{Hash: "def7890123", Subject: "add search", IsBreaking: true},
		},
		"fix": {
			{Hash: "aaa1111111", Subject: "correct timestamp parsing"},
		},
	}

	out := FormatChangelog("v0.44.0", "2026-03-29", grouped)

	// Header
	if !strings.Contains(out, "## [v0.44.0] - 2026-03-29") {
		t.Errorf("missing version header in:\n%s", out)
	}

	// Features section comes before Bug Fixes
	featIdx := strings.Index(out, "### Features")
	fixIdx := strings.Index(out, "### Bug Fixes")
	if featIdx == -1 || fixIdx == -1 {
		t.Fatalf("missing section headers in:\n%s", out)
	}
	if featIdx > fixIdx {
		t.Errorf("Features should come before Bug Fixes")
	}

	// Scope rendering
	if !strings.Contains(out, "**cli** — add changelog command") {
		t.Errorf("missing scoped commit line in:\n%s", out)
	}

	// Breaking prefix
	if !strings.Contains(out, "**BREAKING** add search") {
		t.Errorf("missing breaking prefix in:\n%s", out)
	}

	// Short hash
	if !strings.Contains(out, "(abc1234)") {
		t.Errorf("hash not truncated to 7 chars in:\n%s", out)
	}

	// Fix section
	if !strings.Contains(out, "- correct timestamp parsing (aaa1111)") {
		t.Errorf("missing fix line in:\n%s", out)
	}
}

func TestFormatChangelogEmptyGroups(t *testing.T) {
	grouped := map[string][]CommitInfo{}
	out := FormatChangelog("v1.0.0", "2026-01-01", grouped)

	if !strings.Contains(out, "## [v1.0.0] - 2026-01-01") {
		t.Errorf("missing header in:\n%s", out)
	}
	// Should only have the header line, no sections
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line for empty changelog, got %d:\n%s", len(lines), out)
	}
}
