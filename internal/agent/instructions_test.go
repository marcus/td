package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstructionTextContract(t *testing.T) {
	for _, want := range []string{
		InstructionStartMarker,
		InstructionVersionMarker,
		InstructionEndMarker,
		"td usage --new-session -q",
		"Use your judgment",
		"td approve <id> --self-review --reason",
		"td <command> --help",
	} {
		if !strings.Contains(InstructionText, want) {
			t.Errorf("InstructionText missing %q", want)
		}
	}

	for _, avoid := range []string{"MANDATORY", "Do NOT", "You cannot review your own"} {
		if strings.Contains(InstructionText, avoid) {
			t.Errorf("InstructionText contains legacy wording %q", avoid)
		}
	}

	if words := len(strings.Fields(InstructionBody)); words > 110 {
		t.Errorf("InstructionBody has %d words, want at most 110", words)
	}

	if version, ok := markedInstructionsVersion(InstructionText); !ok || version != InstructionVersion {
		t.Errorf("InstructionText version = %d, %v; want %d, true", version, ok, InstructionVersion)
	}
}

func TestInstallInstructionsReplacesMarkedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	old := "# Project\n\n" +
		InstructionStartMarker + "\n" +
		"<!-- td-agent-instructions:version=1 -->\n\n" +
		"Old td guidance.\n\n" +
		InstructionEndMarker +
		"\n\nKeep this project-specific guidance.\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	if err := InstallInstructions(path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if strings.Count(text, InstructionStartMarker) != 1 {
		t.Fatalf("start marker count = %d, want 1", strings.Count(text, InstructionStartMarker))
	}
	if !strings.Contains(text, InstructionVersionMarker) {
		t.Fatal("updated guidance is missing current version marker")
	}
	if strings.Contains(text, "Old td guidance.") {
		t.Fatal("old marked guidance was not replaced")
	}
	if !strings.Contains(text, "Keep this project-specific guidance.") {
		t.Fatal("project-specific guidance was not preserved")
	}
}

func TestOutdatedMarkedInstructionsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	old := InstructionStartMarker + "\n" +
		"<!-- td-agent-instructions:version=1 -->\n" +
		"Old guidance\n" +
		InstructionEndMarker + "\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	if got := OutdatedMarkedInstructionsFile(dir); got != path {
		t.Fatalf("OutdatedMarkedInstructionsFile = %q, want %q", got, path)
	}

	if err := InstallInstructions(path); err != nil {
		t.Fatal(err)
	}
	if got := OutdatedMarkedInstructionsFile(dir); got != "" {
		t.Fatalf("updated guidance still reported as outdated: %q", got)
	}

	future := InstructionStartMarker + "\n" +
		"<!-- td-agent-instructions:version=3 -->\n" +
		"Future guidance\n" +
		InstructionEndMarker + "\n"
	if err := os.WriteFile(path, []byte(future), 0644); err != nil {
		t.Fatal(err)
	}
	if got := OutdatedMarkedInstructionsFile(dir); got != "" {
		t.Fatalf("newer guidance must not be offered as an update: %q", got)
	}
	if err := InstallInstructions(path); err == nil {
		t.Fatal("installing over newer guidance should return an error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != future {
		t.Fatal("installing over newer guidance changed its contents")
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
		os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0644)
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("DetectAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("finds GEMINI.md", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644)

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("DetectAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("finds CLAUDE.local.md", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CLAUDE.local.md"), []byte("# Local"), 0644)

		got := DetectAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.local.md" {
			t.Errorf("DetectAgentFile = %q, want CLAUDE.local.md", got)
		}
	})

	t.Run("finds CODEX.md", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("# Codex"), 0644)

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
		os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0644)
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "AGENTS.md" {
			t.Errorf("PreferredAgentFile = %q, want AGENTS.md", got)
		}
	})

	t.Run("uses CLAUDE.md when AGENTS.md missing", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "CLAUDE.md" {
			t.Errorf("PreferredAgentFile = %q, want CLAUDE.md", got)
		}
	})

	t.Run("uses GEMINI.md when AGENTS.md and CLAUDE.md missing", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644)

		got := PreferredAgentFile(dir)
		if filepath.Base(got) != "GEMINI.md" {
			t.Errorf("PreferredAgentFile = %q, want GEMINI.md", got)
		}
	})

	t.Run("uses CODEX.md when higher-priority files missing", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("# Codex"), 0644)

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
		os.WriteFile(path, []byte("Run td usage --new-session"), 0644)

		if !HasTDInstructions(path) {
			t.Error("HasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when file has no td usage", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		os.WriteFile(path, []byte("# Claude instructions"), 0644)

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
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Run td usage --new-session"), 0644)

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when GEMINI.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("Use td usage -q"), 0644)

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CLAUDE.local.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CLAUDE.local.md"), []byte("td usage"), 0644)

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns true when CODEX.md has instructions", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CODEX.md"), []byte("td usage --new-session"), 0644)

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true")
		}
	})

	t.Run("returns false when files exist but no instructions", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)
		os.WriteFile(filepath.Join(dir, "GEMINI.md"), []byte("# Gemini"), 0644)

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
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0644)
		// GEMINI.local.md has instructions
		os.WriteFile(filepath.Join(dir, "GEMINI.local.md"), []byte("td usage"), 0644)

		if !AnyFileHasTDInstructions(dir) {
			t.Error("AnyFileHasTDInstructions = false, want true (found in GEMINI.local.md)")
		}
	})
}
