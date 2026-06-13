package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCommitMsgHook(t *testing.T, initial string) (string, string, error) {
	t.Helper()

	tmpDir := t.TempDir()
	msgFile := filepath.Join(tmpDir, "COMMIT_EDITMSG")
	if err := os.WriteFile(msgFile, []byte(initial), 0o644); err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	cmd := exec.Command("bash", "scripts/commit-msg.sh", msgFile)
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()

	finalContent, readErr := os.ReadFile(msgFile)
	if readErr != nil {
		t.Fatalf("read commit message: %v", readErr)
	}

	return string(finalContent), string(output), err
}

func TestCommitMsgHookNormalizesCanonicalSubjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		initial  string
		expected string
	}{
		{
			name: "task linked subject",
			initial: "Feat : normalize commit messages (td-2b41b2)\n\nNightshift-Task: commit-normalize\n" +
				"Nightshift-Ref: https://github.com/marcus/nightshift\n",
			expected: "feat: normalize commit messages (td-2b41b2)\n\nNightshift-Task: commit-normalize\n" +
				"Nightshift-Ref: https://github.com/marcus/nightshift\n",
		},
		{
			name:     "automation subject without td suffix",
			initial:  "Docs:update changelog for v0.40.0\n",
			expected: "docs: update changelog for v0.40.0\n",
		},
		{
			name:     "internal parentheses remain valid",
			initial:  "Fix: handle foo (bar) safely (td-2b41b2)\n",
			expected: "fix: handle foo (bar) safely (td-2b41b2)\n",
		},
		{
			name:     "git workflow subjects bypass normalization",
			initial:  "fixup! feat: normalize commit messages (td-2b41b2)\n",
			expected: "fixup! feat: normalize commit messages (td-2b41b2)\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			finalContent, output, err := runCommitMsgHook(t, tt.initial)
			if err != nil {
				t.Fatalf("hook failed: %v\noutput:\n%s", err, output)
			}
			if finalContent != tt.expected {
				t.Fatalf("unexpected commit message\nwant:\n%s\ngot:\n%s", tt.expected, finalContent)
			}
		})
	}
}

func TestCommitMsgHookRejectsNonCanonicalSubjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial string
	}{
		{
			name:    "missing colon is rejected instead of inferred",
			initial: "feat normalize commit messages (td-2b41b2)\n",
		},
		{
			name:    "non td suffix is rejected",
			initial: "feat: normalize commit messages (jira-123)\n",
		},
		{
			name:    "extra trailing parenthetical is rejected",
			initial: "feat: normalize commit messages (td-2b41b2) (extra)\n",
		},
		{
			name:    "empty summary is rejected",
			initial: "feat:\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			finalContent, output, err := runCommitMsgHook(t, tt.initial)
			if err == nil {
				t.Fatalf("expected hook to fail, output:\n%s", output)
			}
			if finalContent != tt.initial {
				t.Fatalf("hook should leave rejected message unchanged\nwant:\n%s\ngot:\n%s", tt.initial, finalContent)
			}
			if !strings.Contains(output, "type: summary") {
				t.Fatalf("expected remediation output, got:\n%s", output)
			}
		})
	}
}
