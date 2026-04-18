package agent

import (
	"os"
	"path/filepath"
	"testing"
)

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
		must(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0644))
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644))

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("DetectAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("finds GEMINI.md", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644))

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("DetectAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("finds CLAUDE.local.md", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.local.md"), []byte("# Local"), 0644))

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.local.md" {
			t.Errorf("DetectAgentFile = %q, want CLAUDE.local.md", got)
		}
	})

	t.Run("finds CODEX.md", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("# Codex"), 0644))

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
		must(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0644))
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644))

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("PreferredAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("uses CLAUDE.md when AGENTS.md missing", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644))

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.md" {
			t.Errorf("PreferredAgentFile = %q, want CLAUDE.md", got)
		}
	})

	t.Run("uses GEMINI.md when AGENTS.md and CLAUDE.md missing", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644))

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("PreferredAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("uses CODEX.md when higher-priority files missing", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("# Codex"), 0644))

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
		must(t, os.WriteFile(path, []byte("Run td usage --new-session"), 0644))

		if !HasTDInstructions(path) {
			t.Error("HasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when file has no td usage", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		must(t, os.WriteFile(path, []byte("# Claude instructions"), 0644))

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
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Run td usage --new-session"), 0644))

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when GEMINI.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("Use td usage -q"), 0644))

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CLAUDE.local.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.local.md"), []byte("td usage"), 0644))

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CODEX.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("td usage --new-session"), 0644))

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when files exist but no instructions", func(t *testing.T) {
		dir := t.TempDir()
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644))
		must(t, os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644))

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
		must(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644))
		// GEMINI.local.md has instructions
		must(t, os.WriteFile(filepath.Join(dir, "GEMINI.local.md"), []byte("td usage"), 0644))

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true (found in GEMINI.local.md)")
		}
	})
}
