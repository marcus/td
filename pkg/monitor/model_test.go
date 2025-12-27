package monitor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/keymap"
)

// newTestKeymap creates a keymap with default bindings for testing
func newTestKeymap() *keymap.Registry {
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)
	return km
}

func TestRowCount(t *testing.T) {
	m := Model{
		CurrentWorkRows: []string{"id1", "id2"},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "td-1"}},
			{Issue: models.Issue{ID: "td-2"}},
			{Issue: models.Issue{ID: "td-3"}},
		},
		Activity: []ActivityItem{{}, {}},
	}

	tests := []struct {
		panel    Panel
		expected int
	}{
		{PanelCurrentWork, 2},
		{PanelTaskList, 3},
		{PanelActivity, 2},
	}

	for _, tt := range tests {
		got := m.rowCount(tt.panel)
		if got != tt.expected {
			t.Errorf("rowCount(%d) = %d, want %d", tt.panel, got, tt.expected)
		}
	}
}

func TestClampCursor(t *testing.T) {
	tests := []struct {
		name        string
		rowCount    int
		cursorStart int
		expected    int
	}{
		{"empty list", 0, 5, 0},
		{"cursor beyond end", 3, 5, 2},
		{"cursor at end", 3, 2, 2},
		{"cursor in range", 3, 1, 1},
		{"negative cursor", 3, -1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Cursor:          make(map[Panel]int),
				CurrentWorkRows: make([]string, tt.rowCount),
			}
			m.Cursor[PanelCurrentWork] = tt.cursorStart
			m.clampCursor(PanelCurrentWork)
			if m.Cursor[PanelCurrentWork] != tt.expected {
				t.Errorf("clampCursor: got %d, want %d", m.Cursor[PanelCurrentWork], tt.expected)
			}
		})
	}
}

func TestMoveCursor(t *testing.T) {
	tests := []struct {
		name        string
		rowCount    int
		cursorStart int
		delta       int
		expected    int
	}{
		{"move down", 5, 2, 1, 3},
		{"move up", 5, 2, -1, 1},
		{"clamp at bottom", 5, 4, 1, 4},
		{"clamp at top", 5, 0, -1, 0},
		{"empty list", 0, 0, 1, 0},
		{"move multiple down", 5, 0, 3, 3},
		{"move multiple up", 5, 4, -3, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Cursor:          make(map[Panel]int),
				SelectedID:      make(map[Panel]string),
				CurrentWorkRows: make([]string, tt.rowCount),
				ActivePanel:     PanelCurrentWork,
			}
			// Fill with dummy IDs
			for i := range m.CurrentWorkRows {
				m.CurrentWorkRows[i] = "id-" + string(rune('a'+i))
			}
			m.Cursor[PanelCurrentWork] = tt.cursorStart
			m.moveCursor(tt.delta)
			if m.Cursor[PanelCurrentWork] != tt.expected {
				t.Errorf("moveCursor(%d): got %d, want %d", tt.delta, m.Cursor[PanelCurrentWork], tt.expected)
			}
		})
	}
}

func TestSelectedIssueID(t *testing.T) {
	m := Model{
		Cursor: map[Panel]int{
			PanelCurrentWork: 1,
			PanelTaskList:    0,
			PanelActivity:    2,
		},
		CurrentWorkRows: []string{"cw-1", "cw-2", "cw-3"},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "tl-1"}},
			{Issue: models.Issue{ID: "tl-2"}},
		},
		Activity: []ActivityItem{
			{IssueID: "act-1"},
			{IssueID: "act-2"},
			{IssueID: "act-3"},
		},
	}

	tests := []struct {
		panel    Panel
		expected string
	}{
		{PanelCurrentWork, "cw-2"},
		{PanelTaskList, "tl-1"},
		{PanelActivity, "act-3"},
	}

	for _, tt := range tests {
		got := m.SelectedIssueID(tt.panel)
		if got != tt.expected {
			t.Errorf("SelectedIssueID(%d) = %q, want %q", tt.panel, got, tt.expected)
		}
	}
}

func TestSelectedIssueIDEmptyLists(t *testing.T) {
	m := Model{
		Cursor: map[Panel]int{
			PanelCurrentWork: 0,
			PanelTaskList:    0,
			PanelActivity:    0,
		},
		CurrentWorkRows: []string{},
		TaskListRows:    []TaskListRow{},
		Activity:        []ActivityItem{},
	}

	for _, panel := range []Panel{PanelCurrentWork, PanelTaskList, PanelActivity} {
		got := m.SelectedIssueID(panel)
		if got != "" {
			t.Errorf("SelectedIssueID(%d) for empty list = %q, want empty", panel, got)
		}
	}
}

func TestBuildTaskListRows(t *testing.T) {
	m := Model{
		TaskList: TaskListData{
			Reviewable: []models.Issue{{ID: "r1"}, {ID: "r2"}},
			Ready:      []models.Issue{{ID: "rd1"}},
			Blocked:    []models.Issue{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}},
		},
	}

	m.buildTaskListRows()

	// Order should be: Reviewable, Ready, Blocked
	expected := []struct {
		id       string
		category TaskListCategory
	}{
		{"r1", CategoryReviewable},
		{"r2", CategoryReviewable},
		{"rd1", CategoryReady},
		{"b1", CategoryBlocked},
		{"b2", CategoryBlocked},
		{"b3", CategoryBlocked},
	}

	if len(m.TaskListRows) != len(expected) {
		t.Fatalf("TaskListRows length = %d, want %d", len(m.TaskListRows), len(expected))
	}

	for i, exp := range expected {
		row := m.TaskListRows[i]
		if row.Issue.ID != exp.id {
			t.Errorf("TaskListRows[%d].ID = %q, want %q", i, row.Issue.ID, exp.id)
		}
		if row.Category != exp.category {
			t.Errorf("TaskListRows[%d].Category = %q, want %q", i, row.Category, exp.category)
		}
	}
}

func TestBuildCurrentWorkRows(t *testing.T) {
	focusedIssue := &models.Issue{ID: "focused"}
	m := Model{
		FocusedIssue: focusedIssue,
		InProgress: []models.Issue{
			{ID: "ip1"},
			{ID: "focused"}, // duplicate, should be skipped
			{ID: "ip2"},
		},
	}

	m.buildCurrentWorkRows()

	expected := []string{"focused", "ip1", "ip2"}
	if len(m.CurrentWorkRows) != len(expected) {
		t.Fatalf("CurrentWorkRows length = %d, want %d", len(m.CurrentWorkRows), len(expected))
	}

	for i, exp := range expected {
		if m.CurrentWorkRows[i] != exp {
			t.Errorf("CurrentWorkRows[%d] = %q, want %q", i, m.CurrentWorkRows[i], exp)
		}
	}
}

func TestHandleKey_JMovesCursorAndKeepsVisible(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
	}

	// Fill task list with enough rows to require scrolling.
	for i := 0; i < 20; i++ {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: models.Issue{ID: "tl"}})
	}

	// With Height=30: availableHeight=27, panelHeight=9, visibleHeight=4 (9-5 for title/border/scroll indicators).
	// Put cursor at last visible row (position 3 when offset=0).
	m.Cursor[PanelTaskList] = 3
	m.ScrollOffset[PanelTaskList] = 0

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.Cursor[PanelTaskList] != 4 {
		t.Fatalf("cursor after j = %d, want %d", m2.Cursor[PanelTaskList], 4)
	}
	// Cursor moved past viewport. When transitioning from offset=0 to offset>0,
	// the "▲ more above" indicator appears taking 1 line, so we scroll 1 extra.
	// newOffset = cursor(4) - effectiveHeight(4) + 1 = 1, then +1 for indicator = 2.
	if m2.ScrollOffset[PanelTaskList] != 2 {
		t.Fatalf("offset after j = %d, want %d", m2.ScrollOffset[PanelTaskList], 2)
	}
}

func TestHandleKey_PanelSwitchEnsuresCursorVisible(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelCurrentWork,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
	}
	for i := 0; i < 5; i++ {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: models.Issue{ID: "tl"}})
	}

	m.Cursor[PanelTaskList] = 0
	m.ScrollOffset[PanelTaskList] = 10 // invalid: cursor would be offscreen

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m2 := updated.(Model)

	if m2.ActivePanel != PanelTaskList {
		t.Fatalf("active panel after '2' = %v, want %v", m2.ActivePanel, PanelTaskList)
	}
	if m2.ScrollOffset[PanelTaskList] != 0 {
		t.Fatalf("offset after panel switch = %d, want %d", m2.ScrollOffset[PanelTaskList], 0)
	}
}

func TestRestoreCursorsEnsuresCursorVisible(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
	}
	for i := 0; i < 5; i++ {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: models.Issue{ID: "tl"}})
	}

	m.Cursor[PanelTaskList] = 0
	m.ScrollOffset[PanelTaskList] = 10

	m.restoreCursors()

	if m.ScrollOffset[PanelTaskList] != 0 {
		t.Fatalf("offset after restoreCursors = %d, want %d", m.ScrollOffset[PanelTaskList], 0)
	}
}

func TestCategoryHeaderLinesBetween(t *testing.T) {
	tests := []struct {
		name     string
		rows     []TaskListRow
		start    int
		end      int
		expected int
	}{
		{
			name:     "empty list",
			rows:     nil,
			start:    0,
			end:      5,
			expected: 0,
		},
		{
			name: "same category",
			rows: []TaskListRow{
				{Category: CategoryClosed},
				{Category: CategoryClosed},
				{Category: CategoryClosed},
			},
			start:    0,
			end:      3,
			expected: 1, // first header only
		},
		{
			name: "two categories from start",
			rows: []TaskListRow{
				{Category: CategoryReviewable},
				{Category: CategoryClosed},
				{Category: CategoryClosed},
			},
			start:    0,
			end:      3,
			expected: 3, // first header (1) + blank+header for second category (2)
		},
		{
			name: "category transition mid-range",
			rows: []TaskListRow{
				{Category: CategoryReviewable},
				{Category: CategoryReviewable},
				{Category: CategoryClosed},
				{Category: CategoryClosed},
			},
			start:    1,
			end:      4,
			expected: 2, // blank + header when transitioning to Closed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{TaskListRows: tt.rows}
			got := m.categoryHeaderLinesBetween(tt.start, tt.end)
			if got != tt.expected {
				t.Errorf("categoryHeaderLinesBetween(%d, %d) = %d, want %d",
					tt.start, tt.end, got, tt.expected)
			}
		})
	}
}
