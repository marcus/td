package monitor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcus/td/internal/config"
)

// TestCheckFirstRun_SuppressedAfterSeen verifies the Getting Started modal is
// shown only on the first open of a project and not on subsequent launches,
// even when td instructions were never installed.
func TestCheckFirstRun_SuppressedAfterSeen(t *testing.T) {
	t.Run("first open with no instructions shows modal", func(t *testing.T) {
		dir := t.TempDir()
		m := newTestModel()
		m.BaseDir = dir

		msg := m.checkFirstRun()().(FirstRunCheckMsg)
		if !msg.IsFirstRun {
			t.Error("expected IsFirstRun=true on first open of a project without instructions")
		}
	})

	t.Run("seen flag suppresses modal even without instructions", func(t *testing.T) {
		dir := t.TempDir()
		if err := config.SetGettingStartedSeen(dir, true); err != nil {
			t.Fatalf("SetGettingStartedSeen failed: %v", err)
		}
		m := newTestModel()
		m.BaseDir = dir

		msg := m.checkFirstRun()().(FirstRunCheckMsg)
		if msg.IsFirstRun {
			t.Error("expected IsFirstRun=false after the modal has been seen")
		}
	})

	t.Run("existing instructions suppress modal even when unseen", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Project\n\nRun td usage to start.\n"), 0644); err != nil {
			t.Fatalf("write AGENTS.md failed: %v", err)
		}
		m := newTestModel()
		m.BaseDir = dir

		msg := m.checkFirstRun()().(FirstRunCheckMsg)
		if msg.IsFirstRun {
			t.Error("expected IsFirstRun=false when instructions already installed")
		}
		if !msg.HasInstructions {
			t.Error("expected HasInstructions=true when AGENTS.md contains td usage")
		}
	})
}

// TestFirstRunCheckMsg_MarksSeen verifies that showing the modal records the
// seen flag so it is not re-shown on the next launch.
func TestFirstRunCheckMsg_MarksSeen(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel()
	m.BaseDir = dir

	result, cmd := m.Update(FirstRunCheckMsg{IsFirstRun: true})
	if !result.(Model).GettingStartedOpen {
		t.Fatal("expected GettingStartedOpen=true")
	}
	if cmd == nil {
		t.Fatal("expected a command to persist the seen flag")
	}

	// Execute the returned command (markGettingStartedSeen is fire-and-forget).
	cmd()

	seen, err := config.GetGettingStartedSeen(dir)
	if err != nil {
		t.Fatalf("GetGettingStartedSeen failed: %v", err)
	}
	if !seen {
		t.Error("expected getting_started_seen to be persisted after the modal is shown")
	}
}
