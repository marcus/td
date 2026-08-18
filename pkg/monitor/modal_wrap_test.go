package monitor

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// wrapTestIssue carries a log message long enough to wrap several times, which
// is where a content width wider than the box could hold used to show up: Lip
// Gloss re-wrapped the overrun onto its own unindented line.
func wrapTestIssue() ModalEntry {
	now := time.Now()
	return ModalEntry{
		IssueID: "td-8fe2bc",
		Issue: &models.Issue{
			ID: "td-8fe2bc", Title: "sync: an unappliable remote event wedges a peer permanently",
			Type: models.TypeBug, Priority: models.PriorityP1, Status: models.StatusOpen,
			CreatedAt: now, Labels: []string{"sync"},
		},
		Logs: []models.Log{{
			Timestamp: now, SessionID: "ses_4d607a",
			Message: "Fails identically on v0.58.0, so this is pre-existing, not a v0.59.0 " +
				"regression. Surfaced by CI run 32174327383 (random seed), which is why it " +
				"only goes red occasionally.",
		}},
	}
}

// assertLogLinesIntact fails when any line the modal wrapped for itself was
// re-wrapped by the box it was rendered into.
func assertLogLinesIntact(t *testing.T, m Model, rendered string) {
	t.Helper()
	entry := m.CurrentModal()
	for _, line := range m.renderLogLines(entry.Logs[0], m.modalContentWidth()) {
		want := strings.TrimRight(ansi.Strip(line), " ")
		if !strings.Contains(ansi.Strip(rendered), want) {
			t.Fatalf("log line was re-wrapped by the modal box: %q\nrendered:\n%s",
				want, ansi.Strip(rendered))
		}
	}
}

func TestStandaloneModalDoesNotRewrapLogLines(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 200, 50
	m.ModalStack = []ModalEntry{wrapTestIssue()}

	assertLogLinesIntact(t, m, m.renderModal())
}

func TestEmbeddedModalDoesNotRewrapLogLines(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 200, 50
	m.ModalStack = []ModalEntry{wrapTestIssue()}
	m.ModalRenderer = func(content string, width, height int, modalType ModalType, depth int) string {
		// Stand in for a host chrome renderer: one border and one padding cell
		// per side, padding or truncating each line to what is left.
		inner := width - 4
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = " " + ansi.Truncate(line, inner, "") + " "
		}
		return strings.Join(lines, "\n")
	}

	assertLogLinesIntact(t, m, m.renderModal())
}

// TestModalGeometryMatchesAcrossHosts pins the geometry contract: a modal
// occupies the same outer box standalone and embedded, and content is wrapped
// to fit the narrower of the two interiors.
func TestModalGeometryMatchesAcrossHosts(t *testing.T) {
	const outer = 100
	if got, want := modalInnerWidth(outer), outer-6; got != want {
		t.Fatalf("modalInnerWidth(%d) = %d, want %d", outer, got, want)
	}
	if got, want := hostContentWidth(outer), outer-4; got != want {
		t.Fatalf("hostContentWidth(%d) = %d, want %d", outer, got, want)
	}
	if hostContentWidth(outer) < modalInnerWidth(outer) {
		t.Fatal("content wrapped for the standalone box does not fit the host interior")
	}
}
