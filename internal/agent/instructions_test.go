package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestKnownAgentFiles(t *testing.T) {
	expected := []string{
		"AGENTS.md",
		"CLAUDE.md",
		"CLAUDE.local.md",
		"GEMINI.md",
		"GEMINI.local.md",
		"CODEX.md",
		"COPILOT.md",
		"CURSOR.md",
		".github/copilot-instructions.md",
	}
	if len(KnownAgentFiles) != len(expected) {
		t.Fatalf("KnownAgentFiles has %d entries, want %d", len(KnownAgentFiles), len(expected))
	}
	for i, name := range expected {
		if KnownAgentFiles[i] != name {
			t.Errorf("KnownAgentFiles[%d] = %q, want %q", i, KnownAgentFiles[i], name)
		}
	}
}

func TestDetectAgentFile(t *testing.T) {
	t.Run("finds AGENTS.md first", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "AGENTS.md"), "# Agents")
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude")

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("DetectAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("finds GEMINI.md", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "GEMINI.md"), "# Gemini")

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("DetectAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("finds CLAUDE.local.md", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.local.md"), "# Local")

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.local.md" {
			t.Errorf("DetectAgentFile = %q, want CLAUDE.local.md", got)
		}
	})

	t.Run("finds CODEX.md", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CODEX.md"), "# Codex")

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "CODEX.md" {
			t.Errorf("DetectAgentFile = %q, want CODEX.md", got)
		}
	})

	t.Run("returns empty when no files exist", func(t *testing.T) {
		dir := t.TempDir()

		got := DetectAgentFile(dir)
		if got != "" {
			t.Errorf("DetectAgentFile = %q, want empty", got)
		}
	})
}

func TestPreferredAgentFile(t *testing.T) {
	t.Run("prefers AGENTS.md when it exists", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "AGENTS.md"), "# Agents")
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude")

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("PreferredAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("uses CLAUDE.md when AGENTS.md missing", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude")

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.md" {
			t.Errorf("PreferredAgentFile = %q, want CLAUDE.md", got)
		}
	})

	t.Run("uses GEMINI.md when AGENTS.md and CLAUDE.md missing", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "GEMINI.md"), "# Gemini")

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("PreferredAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("uses CODEX.md when higher-priority files missing", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CODEX.md"), "# Codex")

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "CODEX.md" {
			t.Errorf("PreferredAgentFile = %q, want CODEX.md", got)
		}
	})

	t.Run("defaults to AGENTS.md when nothing exists", func(t *testing.T) {
		dir := t.TempDir()

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("PreferredAgentFile = %q, want AGENTS.md", got)
		}
	})
}

func TestHasTDInstructions(t *testing.T) {
	t.Run("returns true when file contains td usage", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		mustWriteFile(t, path, "Run td usage --new-session")

		if !HasTDInstructions(path) {
			t.Error("HasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when file has no td usage", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		mustWriteFile(t, path, "# Claude instructions")

		if HasTDInstructions(path) {
			t.Error("HasTDInstructions = true, want false")
		}
	})

	t.Run("returns false for missing file", func(t *testing.T) {
		if HasTDInstructions("/nonexistent/file.md") {
			t.Error("HasTDInstructions = true, want false for missing file")
		}
	})
}

func TestAnyFileHasTDInstructions(t *testing.T) {
	t.Run("returns true when CLAUDE.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "Run td usage --new-session")

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when GEMINI.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "GEMINI.md"), "Use td usage -q")

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CLAUDE.local.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.local.md"), "td usage")

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CODEX.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CODEX.md"), "td usage --new-session")

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when files exist but no instructions", func(t *testing.T) {
		dir := t.TempDir()
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude")
		mustWriteFile(t, filepath.Join(dir, "GEMINI.md"), "# Gemini")

		if AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = true, want false")
		}
	})

	t.Run("returns false when no files exist", func(t *testing.T) {
		dir := t.TempDir()

		if AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = true, want false")
		}
	})

	t.Run("finds instructions in non-primary file", func(t *testing.T) {
		dir := t.TempDir()
		// CLAUDE.md exists but has no instructions
		mustWriteFile(t, filepath.Join(dir, "CLAUDE.md"), "# Claude")
		// GEMINI.local.md has instructions
		mustWriteFile(t, filepath.Join(dir, "GEMINI.local.md"), "td usage")

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true (found in GEMINI.local.md)")
		}
	})
}
