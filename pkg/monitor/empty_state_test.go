package monitor

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

func contentRows(rendered string) []string {
	var rows []string
	for _, line := range strings.Split(rendered, "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "│") {
			rows = append(rows, plain)
		}
	}
	return rows
}

func interior(plain string) string {
	runes := []rune(plain)
	start, end := -1, -1
	for i, r := range runes {
		if r == '│' {
			if start < 0 {
				start = i
			}
			end = i
		}
	}
	if start < 0 || end <= start {
		return ""
	}
	return string(runes[start+1 : end])
}

func firstLetterCol(plain string) int {
	for i, r := range []rune(plain) {
		if unicode.IsLetter(r) {
			return i
		}
	}
	return -1
}

func assertEmptyStateLayout(t *testing.T, rendered, titleNeedle, bodyNeedle string) {
	t.Helper()
	rows := contentRows(rendered)
	if len(rows) < 3 {
		t.Fatalf("want at least 3 inner rows, got %d in %q", len(rows), ansi.Strip(rendered))
	}
	if !strings.Contains(rows[0], titleNeedle) {
		t.Fatalf("title row %q does not contain %q", rows[0], titleNeedle)
	}
	if strings.TrimSpace(interior(rows[1])) != "" {
		t.Fatalf("expected blank line below header, got %q", rows[1])
	}
	bodyIdx := -1
	for i := 2; i < len(rows); i++ {
		if strings.Contains(rows[i], bodyNeedle) {
			bodyIdx = i
			break
		}
	}
	if bodyIdx < 0 {
		t.Fatalf("body %q not found in %q", bodyNeedle, strings.Join(rows, "\n"))
	}
	titleCol := firstLetterCol(rows[0])
	bodyCol := firstLetterCol(rows[bodyIdx])
	if titleCol < 0 || bodyCol < 0 {
		t.Fatalf("missing title/body letters: title=%d body=%d", titleCol, bodyCol)
	}
	if titleCol != bodyCol {
		t.Fatalf("empty-state text col %d does not align with title col %d\ntitle: %q\nbody:  %q",
			bodyCol, titleCol, rows[0], rows[bodyIdx])
	}
}

func TestEmptyStateCurrentWorkCopyAndAlignment(t *testing.T) {
	m := newTestModel()
	m.Width = 80

	out := m.renderCurrentWorkPanel(10)
	if strings.Contains(out, "No current work") {
		t.Fatal("legacy 'No current work' still rendered")
	}
	assertEmptyStateLayout(t, out, "CURRENT WORK", "Ask an agent")
	if strings.Contains(ansi.Strip(out), "Workspaces") {
		t.Fatal("standalone monitor must not mention Sidecar Workspaces")
	}

	m.Embedded = true
	embedded := m.renderCurrentWorkPanel(12)
	plain := ansi.Strip(embedded)
	if !strings.Contains(plain, "Press [3] for Workspaces") {
		t.Fatalf("embedded empty current work missing next-step, got %q", plain)
	}
	assertEmptyStateLayout(t, embedded, "CURRENT WORK", "Ask an agent")
}

func TestEmptyStateActivityCopyAndAlignment(t *testing.T) {
	m := newTestModel()
	m.Width = 80
	out := m.renderActivityPanel(8)
	assertEmptyStateLayout(t, out, "ACTIVITY LOG", "No recent activity")
	if strings.Contains(ansi.Strip(out), "Workspaces") {
		t.Fatal("activity empty state must not mention Workspaces")
	}

	m.Embedded = true
	embedded := m.renderActivityPanel(8)
	if strings.Contains(ansi.Strip(embedded), "Workspaces") {
		t.Fatal("activity empty state must not mention Workspaces even when embedded")
	}
}

func TestEmptyStateBoardZeroIssuesVsFiltered(t *testing.T) {
	m := newTestModel()
	m.Width = 90
	m.BoardMode.Board = &models.Board{Name: "Main"}

	zero := m.renderTaskListBoardView(12)
	if !strings.Contains(ansi.Strip(zero), emptyBoardNoTasksMsg) {
		t.Fatalf("zero-issue board missing no-tasks copy: %q", ansi.Strip(zero))
	}
	if strings.Contains(zero, "No issues match the board query") {
		t.Fatal("zero-issue board used filter-mismatch copy")
	}
	assertEmptyStateLayout(t, zero, "BOARD", "No tasks yet")
	if strings.Contains(ansi.Strip(zero), "Workspaces") {
		t.Fatal("standalone board empty state mentioned Workspaces")
	}

	m.HasIssues = true
	filtered := m.renderTaskListBoardView(12)
	plain := ansi.Strip(filtered)
	if !strings.Contains(plain, emptyBoardFilteredMsg) {
		t.Fatalf("filtered board missing mismatch copy: %q", plain)
	}
	if strings.Contains(plain, emptyBoardNoTasksMsg) {
		t.Fatal("filtered board used zero-issue copy")
	}
	assertEmptyStateLayout(t, filtered, "BOARD", "No issues match")

	m.HasIssues = false
	m.Embedded = true
	embedded := m.renderTaskListBoardView(14)
	if !strings.Contains(ansi.Strip(embedded), "Press [3] for Workspaces") {
		t.Fatalf("embedded zero-issue board missing next-step: %q", ansi.Strip(embedded))
	}

	m.HasIssues = true
	embeddedFiltered := m.renderTaskListBoardView(14)
	if strings.Contains(ansi.Strip(embeddedFiltered), "Workspaces") {
		t.Fatal("filter-mismatch board must not show workspace next-step")
	}
}

func TestEmptyStateBoardSwimlanesMatchesBacklog(t *testing.T) {
	m := newTestModel()
	m.Width = 90
	m.BoardMode.Board = &models.Board{Name: "Main"}

	zero := m.renderBoardSwimlanesView(12)
	assertEmptyStateLayout(t, zero, "BOARD", "No tasks yet")

	m.HasIssues = true
	filtered := m.renderBoardSwimlanesView(12)
	assertEmptyStateLayout(t, filtered, "BOARD", "No issues match")
}

func TestEmptyStateEmbeddedNextStepDisappearsWhenWorkExists(t *testing.T) {
	m := newTestModel()
	m.Width = 90
	m.Embedded = true
	m.CurrentWorkRows = []string{"td-abc"}
	m.FocusedIssue = &models.Issue{ID: "td-abc", Title: "Doing it", Status: models.StatusInProgress, Type: models.TypeTask, Priority: models.PriorityP2}
	out := m.renderCurrentWorkPanel(10)
	if strings.Contains(ansi.Strip(out), "Workspaces") {
		t.Fatal("next-step must disappear once current work exists")
	}
}
