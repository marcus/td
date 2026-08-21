package monitor

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

// A resize that does not move the width the markdown was wrapped at has nothing
// to re-render. This matters beyond the wasted work: an embedding host that
// announces its geometry once per frame instead of once per resize turns an
// unconditional re-render into a message loop, because the rendered-markdown
// message is itself a frame. Sidecar's pane frame did exactly that, and an open
// issue modal re-rendered its description ~150 times a second (td-fcb03a).
func TestWindowSizeRerendersModalMarkdownOnlyOnWidthChange(t *testing.T) {
	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("db init: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	m := NewModel(database, "ses-resize", 10*time.Second, "", baseDir)
	m.Embedded = true
	m.Width, m.Height = 200, 46
	m.updatePanelBounds()
	m.ModalStack = []ModalEntry{{
		IssueID: "td-1",
		Issue:   &models.Issue{ID: "td-1", Description: "# Body\n\nSome text."},
	}}

	resize := func(width, height int) tea.Cmd {
		t.Helper()
		updated, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = updated.(Model)
		return cmd
	}

	if cmd := resize(200, 46); cmd != nil {
		t.Fatal("a resize to the size already held re-rendered the modal markdown")
	}
	if cmd := resize(200, 20); cmd != nil {
		t.Fatal("a height-only resize re-rendered markdown whose wrap width did not move")
	}
	// 200 and 160 both clamp to the same capped modal width, so the wrap width
	// is unchanged and so is the answer.
	if cmd := resize(160, 20); cmd != nil {
		t.Fatal("a width change that clamps to the same content width re-rendered markdown")
	}
	if cmd := resize(90, 20); cmd == nil {
		t.Fatal("a narrower terminal did not re-wrap the modal markdown")
	}
	if m.Width != 90 || m.Height != 20 {
		t.Fatalf("model size = %dx%d, want the last resize to have been applied", m.Width, m.Height)
	}
	if bounds := m.PanelBounds[PanelTaskList]; bounds.W != 90 {
		t.Fatalf("task list bounds width = %d, want panel bounds recomputed at 90", bounds.W)
	}
}
