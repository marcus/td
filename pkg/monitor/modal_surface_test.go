package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/ansifill"
)

// splotchTestIssue is an epic whose detail modal mixes styled and unstyled
// runs on the same line - the combination that used to leave the surface
// showing through behind text.
func splotchTestIssue() ModalEntry {
	now := time.Now()
	return ModalEntry{
		IssueID: "td-2930ee",
		Issue: &models.Issue{
			ID: "td-2930ee", Title: "Full Sidecar theme parity for embedded td monitor",
			Type: models.TypeEpic, Priority: models.PriorityP1, Status: models.StatusOpen,
			CreatedAt: now, Labels: []string{"theme", "sidecar", "monitor"},
			Description: "Execute the plan.", Acceptance: "Every render path is themed.",
		},
		EpicTasks: []models.Issue{
			{ID: "td-c64888", Title: "Phase 1", Type: models.TypeTask, Status: models.StatusClosed},
		},
	}
}

// assertSurfaceUnbroken fails when any cell of a modal line renders outside
// the surface background: a nested style that reset without the surface being
// restored, or padding the line never received.
func assertSurfaceUnbroken(t *testing.T, rendered, surface string, wantWidth int) {
	t.Helper()
	fill := ansifill.Code(surface)
	for i, line := range strings.Split(rendered, "\n") {
		if strings.TrimSpace(ansi.Strip(line)) == "" && !strings.Contains(line, fill) {
			continue // blank filler line outside the painted body
		}
		if !strings.HasPrefix(line, fill) {
			t.Fatalf("line %d does not open on the surface: %q", i, line)
		}
		if width := ansi.StringWidth(line); width != wantWidth {
			t.Fatalf("line %d width = %d, want %d: %q", i, width, wantWidth, line)
		}
		for _, segment := range strings.Split(line, "\x1b[m")[1:] {
			if segment != "" && !strings.HasPrefix(segment, fill) && !strings.HasPrefix(segment, "\x1b[0m") {
				t.Fatalf("line %d drops the surface after a nested reset: %q", i, line)
			}
		}
	}
}

func TestEmbeddedIssueModalKeepsSurfaceSolid(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 120, 40
	theme := phase2TestTheme()
	if err := m.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	var hostWidth int
	m.ModalRenderer = func(content string, width, height int, modalType ModalType, depth int) string {
		hostWidth = width
		return content
	}
	m.ModalStack = []ModalEntry{splotchTestIssue()}

	rendered := m.renderModal()
	// The host owns border and padding: one cell per side each.
	assertSurfaceUnbroken(t, rendered, theme.Surface, hostWidth-4)
}

func TestStandaloneIssueModalKeepsSurfaceSolid(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 120, 40
	theme := phase2TestTheme()
	if err := m.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	m.ModalStack = []ModalEntry{splotchTestIssue()}

	rendered := m.renderModal()
	fill := ansifill.Code(theme.Surface)
	for i, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, fill) {
			continue // border-only lines
		}
		body := line[strings.Index(line, fill):]
		for _, segment := range strings.Split(body, "\x1b[m")[1:] {
			if segment != "" && !strings.HasPrefix(segment, fill) && !strings.HasPrefix(segment, "\x1b[0m") &&
				!strings.HasPrefix(segment, "\x1b[38") {
				t.Fatalf("line %d drops the surface after a nested reset: %q", i, line)
			}
		}
	}
}
