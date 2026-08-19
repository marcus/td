package monitor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// runeIndex reports the display column of sub within s (rune-based, so
// multi-byte glyphs like the caret do not skew alignment checks).
func runeIndex(s, sub string) int {
	idx := strings.Index(s, sub)
	if idx < 0 {
		return -1
	}
	return len([]rune(s[:idx]))
}

func rowsTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width = 100
	m.Height = 40
	m.ActivePanel = PanelTaskList
	m.Cursor[PanelTaskList] = 1
	m.TaskListRows = []TaskListRow{
		{
			Issue: models.Issue{
				ID: "td-1a2b3c", Title: "First in progress", Type: models.TypeTask,
				Priority: models.PriorityP1, Status: models.StatusInProgress,
			},
			Category: CategoryInProgress,
		},
		{
			Issue: models.Issue{
				ID: "td-longer-key", Title: "Second ready item", Type: models.TypeFeature,
				Priority: models.PriorityP2, Status: models.StatusOpen,
			},
			Category: CategoryReady,
		},
	}
	m.TaskList.InProgress = []models.Issue{m.TaskListRows[0].Issue}
	m.TaskList.Ready = []models.Issue{m.TaskListRows[1].Issue}
	return m
}

func TestIssueKeyColumnWidth(t *testing.T) {
	if got := issueKeyColumnWidth([]string{"td-1a2b"}); got != issueKeyMinWidth {
		t.Errorf("short keys = %d, want min %d", got, issueKeyMinWidth)
	}
	if got := issueKeyColumnWidth([]string{"td-1a2b", "td-longer-key"}); got != len("td-longer-key") {
		t.Errorf("mixed keys = %d, want %d", got, len("td-longer-key"))
	}
	long := strings.Repeat("x", 40)
	if got := issueKeyColumnWidth([]string{long}); got != issueKeyMaxWidth {
		t.Errorf("overlong key = %d, want max %d", got, issueKeyMaxWidth)
	}
}

func TestFormatIssueRowColumnOrderAndGutter(t *testing.T) {
	m := rowsTestModel(t)
	issue := m.TaskListRows[0].Issue

	plain := ansi.Strip(m.formatIssueRow(&issue, false, 12, 80, ""))
	if !strings.HasPrefix(plain, strings.Repeat(" ", rowGutterWidth)+issue.ID) {
		t.Fatalf("unselected row should start with a blank gutter then the key: %q", plain)
	}
	// Columns: key, type symbol, priority, title.
	rest := strings.TrimSpace(plain[rowGutterWidth+12:])
	fields := strings.Fields(rest)
	if len(fields) < 3 || fields[1] != "P1" {
		t.Fatalf("expected <type> P1 <title...>, got %q", rest)
	}
	if !strings.Contains(plain, issue.Title) {
		t.Errorf("row missing title: %q", plain)
	}

	selected := ansi.Strip(m.formatIssueRow(&issue, true, 12, 80, ""))
	if !strings.HasPrefix(selected, rowCaretGlyph) {
		t.Errorf("selected row should start with the caret: %q", selected)
	}
	if lipgloss.Width(selected) != lipgloss.Width(plain) {
		t.Errorf("caret shifted the row: selected %q vs plain %q", selected, plain)
	}
}

func TestTaskListRowsAlignAndDropRedundantStatusTag(t *testing.T) {
	m := rowsTestModel(t)
	out := ansi.Strip(m.renderTaskListPanel(12))

	for tag := range categoryTagLabels {
		label := categoryTagLabels[tag]
		if strings.Contains(out, label) {
			t.Errorf("row status tag %q should not be repeated inside the section: %s", label, out)
		}
	}

	var rowLines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "td-1a2b3c") || strings.Contains(line, "td-longer-key") {
			rowLines = append(rowLines, line)
		}
	}
	if len(rowLines) != 2 {
		t.Fatalf("expected 2 issue rows, got %d in:\n%s", len(rowLines), out)
	}
	first := runeIndex(rowLines[0], "P1")
	second := runeIndex(rowLines[1], "P2")
	if first != second {
		t.Errorf("priority column not aligned: %d vs %d\n%q\n%q", first, second, rowLines[0], rowLines[1])
	}
}

func TestSectionHeadersAreFlushLeftAndRowsIndented(t *testing.T) {
	m := rowsTestModel(t)
	out := ansi.Strip(m.renderTaskListPanel(12))

	for _, line := range strings.Split(out, "\n") {
		// Strip the panel border + padding to get the pane's content column.
		inner := line
		if idx := strings.IndexAny(inner, "│"); idx >= 0 {
			inner = inner[idx+len("│"):]
		}
		if strings.HasPrefix(strings.TrimSpace(inner), "IN PROGRESS") ||
			strings.HasPrefix(strings.TrimSpace(inner), "READY") {
			if strings.HasPrefix(inner, " ") && strings.HasPrefix(inner[1:], " ") {
				t.Errorf("section header should be flush left, got %q", inner)
			}
		}
		if strings.Contains(inner, "td-1a2b3c") && !strings.HasPrefix(inner, strings.Repeat(" ", rowGutterWidth)) {
			t.Errorf("row should be indented under its header, got %q", inner)
		}
	}
}

func TestSectionHeaderColorsComeFromTheme(t *testing.T) {
	m := rowsTestModel(t)
	styles := m.renderStyles()

	inProgress := m.formatCategoryHeader(CategoryInProgress)
	ready := m.formatCategoryHeader(CategoryReady)

	if !strings.Contains(inProgress, "IN PROGRESS") || !strings.Contains(ready, "READY") {
		t.Fatalf("headers missing labels: %q %q", inProgress, ready)
	}
	if !strings.Contains(inProgress, styles.category[CategoryInProgress].Bold(true).Render("IN PROGRESS")) {
		t.Errorf("IN PROGRESS header not painted with its themed category color: %q", inProgress)
	}
	if !strings.Contains(ready, styles.category[CategoryReady].Bold(true).Render("READY")) {
		t.Errorf("READY header not painted with its themed category color: %q", ready)
	}
	if inProgress == ready {
		t.Error("section headers should use distinct colors per section")
	}
	// Count is carried on the header, right-aligned after the rule.
	if !strings.HasSuffix(ansi.Strip(inProgress), "1") {
		t.Errorf("header should end with its item count: %q", ansi.Strip(inProgress))
	}
}

func TestSectionHeadersFollowThemeChanges(t *testing.T) {
	m := rowsTestModel(t)
	before := m.formatCategoryHeader(CategoryReady)
	if err := m.SetTheme(Theme{Success: "#00ff00", Border: "#123456"}); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	after := m.formatCategoryHeader(CategoryReady)
	if before == after {
		t.Error("section header did not re-color when the theme changed")
	}
}
