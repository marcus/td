package monitor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/td/internal/config"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/keymap"
)

// newTestKeymap creates a keymap with default bindings for testing
func newTestKeymap() *keymap.Registry {
	km := keymap.NewRegistry()
	keymap.RegisterDefaults(km)
	return km
}

// defaultPaneHeights returns the default pane height ratios for testing
func defaultPaneHeights() [3]float64 {
	return config.DefaultPaneHeights()
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
		PaneHeights:  defaultPaneHeights(),
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
		PaneHeights:  defaultPaneHeights(),
	}
	for i := 0; i < 5; i++ {
		m.TaskListRows = append(m.TaskListRows, TaskListRow{Issue: models.Issue{ID: "tl"}})
	}

	m.Cursor[PanelTaskList] = 0
	m.ScrollOffset[PanelTaskList] = 10 // invalid: cursor would be offscreen

	// Tab cycles from PanelCurrentWork (0) to PanelTaskList (1)
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if m2.ActivePanel != PanelTaskList {
		t.Fatalf("active panel after Tab = %v, want %v", m2.ActivePanel, PanelTaskList)
	}
	if m2.ScrollOffset[PanelTaskList] != 0 {
		t.Fatalf("offset after panel switch = %d, want %d", m2.ScrollOffset[PanelTaskList], 0)
	}
}

func TestEscapeClearsSearchAndExitsSearchMode(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		SearchMode:   true,
		SearchQuery:  "some query",
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
	}

	// Press Escape in search mode
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if m2.SearchMode {
		t.Fatal("SearchMode should be false after Escape")
	}
	if m2.SearchQuery != "" {
		t.Fatalf("SearchQuery should be empty after Escape, got %q", m2.SearchQuery)
	}
}

func TestEscapeClearsSearchFilterFromMainView(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		SearchMode:   false, // Not in search mode
		SearchQuery:  "active filter", // But filter is active
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
	}

	// Press Escape in main view with active filter
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if m2.SearchQuery != "" {
		t.Fatalf("SearchQuery should be cleared by Escape in main view, got %q", m2.SearchQuery)
	}
}

func TestEscapeDoesNothingWithNoFilter(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		SearchMode:   false,
		SearchQuery:  "", // No filter
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
	}

	// Press Escape with no filter - should return nil cmd (no fetch)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Fatal("Escape with no filter should not trigger fetch")
	}
}

func TestRestoreCursorsEnsuresCursorVisible(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		PaneHeights:  defaultPaneHeights(),
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

func TestTaskListLinesFromOffset(t *testing.T) {
	tests := []struct {
		name     string
		rows     []TaskListRow
		offset   int
		expected int
	}{
		{
			name:     "empty list",
			rows:     nil,
			offset:   0,
			expected: 0,
		},
		{
			name: "single category from start",
			rows: []TaskListRow{
				{Category: CategoryReady},
				{Category: CategoryReady},
				{Category: CategoryReady},
			},
			offset:   0,
			expected: 4, // 1 header + 3 rows
		},
		{
			name: "single category from middle",
			rows: []TaskListRow{
				{Category: CategoryReady},
				{Category: CategoryReady},
				{Category: CategoryReady},
			},
			offset:   1,
			expected: 2, // 2 rows (no header since same category as before)
		},
		{
			name: "two categories from start",
			rows: []TaskListRow{
				{Category: CategoryReviewable},
				{Category: CategoryReviewable},
				{Category: CategoryBlocked},
				{Category: CategoryBlocked},
			},
			offset:   0,
			expected: 7, // 1 header + 2 rows + 1 blank + 1 header + 2 rows
		},
		{
			name: "start at category transition",
			rows: []TaskListRow{
				{Category: CategoryReviewable},
				{Category: CategoryBlocked},
				{Category: CategoryBlocked},
			},
			offset:   1,
			expected: 4, // 1 blank + 1 header + 2 rows (transition from Reviewable)
		},
		{
			name: "offset past end",
			rows: []TaskListRow{
				{Category: CategoryReady},
			},
			offset:   5,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{TaskListRows: tt.rows}
			got := m.taskListLinesFromOffset(tt.offset)
			if got != tt.expected {
				t.Errorf("taskListLinesFromOffset(%d) = %d, want %d",
					tt.offset, got, tt.expected)
			}
		})
	}
}

func TestMaxScrollOffsetTaskList(t *testing.T) {
	tests := []struct {
		name          string
		rows          []TaskListRow
		height        int
		expectedMax   int
		description   string
	}{
		{
			name: "content fits - no scroll needed",
			rows: []TaskListRow{
				{Category: CategoryReady},
				{Category: CategoryReady},
			},
			height:      30, // visibleHeight = (30/3) - 5 = 4
			expectedMax: 0,
			description: "2 rows + 1 header = 3 lines fits in 4 visible lines",
		},
		{
			name: "content exceeds - limited scroll",
			rows: []TaskListRow{
				{Category: CategoryReviewable},
				{Category: CategoryReviewable},
				{Category: CategoryReviewable},
				{Category: CategoryReady},
				{Category: CategoryReady},
				{Category: CategoryReady},
				{Category: CategoryBlocked},
				{Category: CategoryBlocked},
			},
			height:      30, // visibleHeight = (30/3) - 5 = 4
			expectedMax: 6,  // At offset 6: 4 lines fit in 4 visible height (shows rows 6-7 with header)
			description: "8 rows with 3 categories - maxOffset=6 shows last rows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Height:       tt.height,
				TaskListRows: tt.rows,
				PaneHeights:  defaultPaneHeights(),
			}
			got := m.maxScrollOffset(PanelTaskList)
			if got != tt.expectedMax {
				t.Errorf("maxScrollOffset() = %d, want %d (%s)",
					got, tt.expectedMax, tt.description)
			}
		})
	}
}

func TestCursorClampsAtBottom(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "r1"}, Category: CategoryReviewable},
			{Issue: models.Issue{ID: "r2"}, Category: CategoryReviewable},
			{Issue: models.Issue{ID: "b1"}, Category: CategoryBlocked},
		},
	}

	// Start at last item
	m.Cursor[PanelTaskList] = 2

	// Press j - should stay at 2 (clamped)
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.Cursor[PanelTaskList] != 2 {
		t.Errorf("cursor after j at bottom = %d, want 2 (should clamp)", m2.Cursor[PanelTaskList])
	}

	// Press j again - should still be 2
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m3 := updated.(Model)

	if m3.Cursor[PanelTaskList] != 2 {
		t.Errorf("cursor after second j at bottom = %d, want 2", m3.Cursor[PanelTaskList])
	}
}

func TestBlockedItemsSelectable(t *testing.T) {
	m := Model{
		Height:       30,
		ActivePanel:  PanelTaskList,
		Cursor:       make(map[Panel]int),
		SelectedID:   make(map[Panel]string),
		ScrollOffset: make(map[Panel]int),
		Keymap:       newTestKeymap(),
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "r1"}, Category: CategoryReviewable},
			{Issue: models.Issue{ID: "b1"}, Category: CategoryBlocked},
			{Issue: models.Issue{ID: "b2"}, Category: CategoryBlocked},
		},
	}

	// Navigate to blocked item
	m.Cursor[PanelTaskList] = 1

	// Should be able to get the blocked issue ID
	selectedID := m.SelectedIssueID(PanelTaskList)
	if selectedID != "b1" {
		t.Errorf("SelectedIssueID for blocked item = %q, want 'b1'", selectedID)
	}

	// Navigate to last blocked item
	m.Cursor[PanelTaskList] = 2
	selectedID = m.SelectedIssueID(PanelTaskList)
	if selectedID != "b2" {
		t.Errorf("SelectedIssueID for last blocked item = %q, want 'b2'", selectedID)
	}
}

// Tests for modal stack functionality

func TestModalStackEmpty(t *testing.T) {
	m := Model{}

	if m.ModalOpen() {
		t.Error("ModalOpen() should be false for empty stack")
	}
	if m.ModalDepth() != 0 {
		t.Errorf("ModalDepth() = %d, want 0", m.ModalDepth())
	}
	if m.CurrentModal() != nil {
		t.Error("CurrentModal() should be nil for empty stack")
	}
	if m.ModalBreadcrumb() != "" {
		t.Errorf("ModalBreadcrumb() = %q, want empty", m.ModalBreadcrumb())
	}
}

func TestModalStackPush(t *testing.T) {
	m := Model{
		ModalStack: []ModalEntry{},
	}

	// Push first modal
	m.ModalStack = append(m.ModalStack, ModalEntry{
		IssueID:     "td-001",
		SourcePanel: PanelTaskList,
		Loading:     true,
	})

	if !m.ModalOpen() {
		t.Error("ModalOpen() should be true after push")
	}
	if m.ModalDepth() != 1 {
		t.Errorf("ModalDepth() = %d, want 1", m.ModalDepth())
	}

	modal := m.CurrentModal()
	if modal == nil {
		t.Fatal("CurrentModal() should not be nil")
	}
	if modal.IssueID != "td-001" {
		t.Errorf("CurrentModal().IssueID = %q, want %q", modal.IssueID, "td-001")
	}

	// Push second modal
	m.ModalStack = append(m.ModalStack, ModalEntry{
		IssueID: "td-002",
		Loading: true,
	})

	if m.ModalDepth() != 2 {
		t.Errorf("ModalDepth() = %d, want 2", m.ModalDepth())
	}

	modal = m.CurrentModal()
	if modal.IssueID != "td-002" {
		t.Errorf("CurrentModal().IssueID = %q, want %q", modal.IssueID, "td-002")
	}
}

func TestModalStackPop(t *testing.T) {
	m := Model{
		ModalStack: []ModalEntry{
			{IssueID: "td-001", SourcePanel: PanelTaskList},
			{IssueID: "td-002"},
		},
	}

	// Pop second modal
	m.closeModal()

	if m.ModalDepth() != 1 {
		t.Errorf("ModalDepth() after pop = %d, want 1", m.ModalDepth())
	}
	if m.CurrentModal().IssueID != "td-001" {
		t.Errorf("CurrentModal().IssueID = %q, want %q", m.CurrentModal().IssueID, "td-001")
	}

	// Pop first modal
	m.closeModal()

	if m.ModalOpen() {
		t.Error("ModalOpen() should be false after popping all modals")
	}
	if m.ModalDepth() != 0 {
		t.Errorf("ModalDepth() = %d, want 0", m.ModalDepth())
	}
}

func TestModalSourcePanel(t *testing.T) {
	m := Model{
		ModalStack: []ModalEntry{
			{IssueID: "td-001", SourcePanel: PanelActivity},
			{IssueID: "td-002"},
			{IssueID: "td-003"},
		},
	}

	// Source panel should always return the base modal's source panel
	if m.ModalSourcePanel() != PanelActivity {
		t.Errorf("ModalSourcePanel() = %v, want %v", m.ModalSourcePanel(), PanelActivity)
	}
}

func TestModalBreadcrumb(t *testing.T) {
	tests := []struct {
		name     string
		stack    []ModalEntry
		expected string
	}{
		{
			name:     "empty stack",
			stack:    nil,
			expected: "",
		},
		{
			name: "single modal",
			stack: []ModalEntry{
				{IssueID: "td-001"},
			},
			expected: "", // No breadcrumb for depth 1
		},
		{
			name: "two modals with types",
			stack: []ModalEntry{
				{IssueID: "td-001", Issue: &models.Issue{Type: models.TypeEpic}},
				{IssueID: "td-002", Issue: &models.Issue{Type: models.TypeTask}},
			},
			expected: "epic: td-001 > task: td-002",
		},
		{
			name: "three modals",
			stack: []ModalEntry{
				{IssueID: "td-001", Issue: &models.Issue{Type: models.TypeEpic}},
				{IssueID: "td-002", Issue: &models.Issue{Type: models.TypeTask}},
				{IssueID: "td-003", Issue: &models.Issue{Type: models.TypeBug}},
			},
			expected: "epic: td-001 > task: td-002 > bug: td-003",
		},
		{
			name: "modals without issue loaded",
			stack: []ModalEntry{
				{IssueID: "td-001"},
				{IssueID: "td-002"},
			},
			expected: "td-001 > td-002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{ModalStack: tt.stack}
			got := m.ModalBreadcrumb()
			if got != tt.expected {
				t.Errorf("ModalBreadcrumb() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEpicTasksCursor(t *testing.T) {
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: "td-001",
				Issue:   &models.Issue{ID: "td-001", Type: models.TypeEpic},
				EpicTasks: []models.Issue{
					{ID: "td-002"},
					{ID: "td-003"},
					{ID: "td-004"},
				},
				TaskSectionFocused: true,
				EpicTasksCursor:    0,
			},
		},
	}

	// Move cursor down
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.CurrentModal().EpicTasksCursor != 1 {
		t.Errorf("EpicTasksCursor after j = %d, want 1", m2.CurrentModal().EpicTasksCursor)
	}

	// Move cursor down again
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m3 := updated.(Model)

	if m3.CurrentModal().EpicTasksCursor != 2 {
		t.Errorf("EpicTasksCursor after j = %d, want 2", m3.CurrentModal().EpicTasksCursor)
	}

	// Move cursor down at bottom (should stay at 2)
	updated, _ = m3.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m4 := updated.(Model)

	if m4.CurrentModal().EpicTasksCursor != 2 {
		t.Errorf("EpicTasksCursor at bottom after j = %d, want 2", m4.CurrentModal().EpicTasksCursor)
	}

	// Move cursor up
	updated, _ = m4.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m5 := updated.(Model)

	if m5.CurrentModal().EpicTasksCursor != 1 {
		t.Errorf("EpicTasksCursor after k = %d, want 1", m5.CurrentModal().EpicTasksCursor)
	}
}

func TestToggleTaskSectionFocus(t *testing.T) {
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: "td-001",
				Issue:   &models.Issue{ID: "td-001", Type: models.TypeEpic},
				EpicTasks: []models.Issue{
					{ID: "td-002"},
				},
				TaskSectionFocused: false,
			},
		},
	}

	// Toggle focus on
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if !m2.CurrentModal().TaskSectionFocused {
		t.Error("TaskSectionFocused should be true after Tab")
	}

	// Toggle focus off
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m3 := updated.(Model)

	if m3.CurrentModal().TaskSectionFocused {
		t.Error("TaskSectionFocused should be false after Tab")
	}
}

func TestBlockedByCursorNavigation(t *testing.T) {
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: "td-001",
				Issue:   &models.Issue{ID: "td-001"},
				BlockedBy: []models.Issue{
					{ID: "td-002", Status: models.StatusOpen},
					{ID: "td-003", Status: models.StatusOpen},
					{ID: "td-004", Status: models.StatusOpen},
				},
				BlockedBySectionFocused: true,
				BlockedByCursor:         0,
			},
		},
	}

	// Move cursor down
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.CurrentModal().BlockedByCursor != 1 {
		t.Errorf("BlockedByCursor after j = %d, want 1", m2.CurrentModal().BlockedByCursor)
	}

	// Move cursor down again
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m3 := updated.(Model)

	if m3.CurrentModal().BlockedByCursor != 2 {
		t.Errorf("BlockedByCursor after j = %d, want 2", m3.CurrentModal().BlockedByCursor)
	}

	// Move cursor down at bottom (should stay at 2)
	updated, _ = m3.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m4 := updated.(Model)

	if m4.CurrentModal().BlockedByCursor != 2 {
		t.Errorf("BlockedByCursor at bottom after j = %d, want 2", m4.CurrentModal().BlockedByCursor)
	}

	// Move cursor up
	updated, _ = m4.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m5 := updated.(Model)

	if m5.CurrentModal().BlockedByCursor != 1 {
		t.Errorf("BlockedByCursor after k = %d, want 1", m5.CurrentModal().BlockedByCursor)
	}
}

func TestBlocksSectionNavigation(t *testing.T) {
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: "td-001",
				Issue:   &models.Issue{ID: "td-001"},
				Blocks: []models.Issue{
					{ID: "td-002"},
					{ID: "td-003"},
				},
				BlocksSectionFocused: true,
				BlocksCursor:         0,
			},
		},
	}

	// Move cursor down
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.CurrentModal().BlocksCursor != 1 {
		t.Errorf("BlocksCursor after j = %d, want 1", m2.CurrentModal().BlocksCursor)
	}

	// Move cursor down at bottom (should stay at 1)
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m3 := updated.(Model)

	if m3.CurrentModal().BlocksCursor != 1 {
		t.Errorf("BlocksCursor at bottom after j = %d, want 1", m3.CurrentModal().BlocksCursor)
	}

	// Move cursor up
	updated, _ = m3.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m4 := updated.(Model)

	if m4.CurrentModal().BlocksCursor != 0 {
		t.Errorf("BlocksCursor after k = %d, want 0", m4.CurrentModal().BlocksCursor)
	}
}

func TestBlockedByContextDetection(t *testing.T) {
	tests := []struct {
		name     string
		model    Model
		expected keymap.Context
	}{
		{
			name: "blocked-by focused context",
			model: Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{
						IssueID:                 "td-001",
						Issue:                   &models.Issue{ID: "td-001"},
						BlockedBySectionFocused: true,
					},
				},
			},
			expected: keymap.ContextBlockedByFocused,
		},
		{
			name: "blocks focused context",
			model: Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{
						IssueID:              "td-001",
						Issue:                &models.Issue{ID: "td-001"},
						BlocksSectionFocused: true,
					},
				},
			},
			expected: keymap.ContextBlocksFocused,
		},
		{
			name: "modal context when not focused",
			model: Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{
						IssueID: "td-001",
						Issue:   &models.Issue{ID: "td-001"},
					},
				},
			},
			expected: keymap.ContextModal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.model.currentContext()
			if got != tt.expected {
				t.Errorf("currentContext() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTabCyclesThroughSections(t *testing.T) {
	// Test cycling through: scroll -> blocked-by -> blocks -> scroll
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID: "td-001",
				Issue:   &models.Issue{ID: "td-001"},
				BlockedBy: []models.Issue{
					{ID: "td-002", Status: models.StatusOpen},
				},
				Blocks: []models.Issue{
					{ID: "td-003"},
				},
			},
		},
	}

	// Start in scroll mode
	if m.CurrentModal().BlockedBySectionFocused || m.CurrentModal().BlocksSectionFocused {
		t.Error("Should start in scroll mode")
	}

	// Tab to blocked-by section
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m2 := updated.(Model)

	if !m2.CurrentModal().BlockedBySectionFocused {
		t.Error("Tab should focus blocked-by section first")
	}

	// Tab to blocks section
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m3 := updated.(Model)

	if !m3.CurrentModal().BlocksSectionFocused {
		t.Error("Tab should focus blocks section next")
	}
	if m3.CurrentModal().BlockedBySectionFocused {
		t.Error("BlockedBySectionFocused should be false")
	}

	// Tab back to scroll mode
	updated, _ = m3.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m4 := updated.(Model)

	if m4.CurrentModal().BlockedBySectionFocused || m4.CurrentModal().BlocksSectionFocused {
		t.Error("Tab should return to scroll mode")
	}
}

func TestContextEpicTasks(t *testing.T) {
	tests := []struct {
		name     string
		model    Model
		expected keymap.Context
	}{
		{
			name: "main context",
			model: Model{
				Keymap: newTestKeymap(),
			},
			expected: keymap.ContextMain,
		},
		{
			name: "modal context",
			model: Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{IssueID: "td-001", TaskSectionFocused: false},
				},
			},
			expected: keymap.ContextModal,
		},
		{
			name: "epic tasks context",
			model: Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{IssueID: "td-001", TaskSectionFocused: true},
				},
			},
			expected: keymap.ContextEpicTasks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.model.currentContext()
			if got != tt.expected {
				t.Errorf("currentContext() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNavigateModal(t *testing.T) {
	tests := []struct {
		name        string
		model       Model
		delta       int
		expectIssue string // empty means no change
	}{
		{
			name: "navigate next in task list",
			model: Model{
				Keymap:     newTestKeymap(),
				Cursor:     map[Panel]int{PanelTaskList: 0},
				SelectedID: map[Panel]string{},
				TaskListRows: []TaskListRow{
					{Issue: models.Issue{ID: "td-001"}},
					{Issue: models.Issue{ID: "td-002"}},
					{Issue: models.Issue{ID: "td-003"}},
				},
				ModalStack: []ModalEntry{
					{IssueID: "td-001", SourcePanel: PanelTaskList},
				},
			},
			delta:       1,
			expectIssue: "td-002",
		},
		{
			name: "navigate prev in task list",
			model: Model{
				Keymap:     newTestKeymap(),
				Cursor:     map[Panel]int{PanelTaskList: 1},
				SelectedID: map[Panel]string{},
				TaskListRows: []TaskListRow{
					{Issue: models.Issue{ID: "td-001"}},
					{Issue: models.Issue{ID: "td-002"}},
					{Issue: models.Issue{ID: "td-003"}},
				},
				ModalStack: []ModalEntry{
					{IssueID: "td-002", SourcePanel: PanelTaskList},
				},
			},
			delta:       -1,
			expectIssue: "td-001",
		},
		{
			name: "navigate at boundary stays at edge",
			model: Model{
				Keymap:     newTestKeymap(),
				Cursor:     map[Panel]int{PanelTaskList: 1},
				SelectedID: map[Panel]string{},
				TaskListRows: []TaskListRow{
					{Issue: models.Issue{ID: "td-001"}},
					{Issue: models.Issue{ID: "td-002"}},
				},
				ModalStack: []ModalEntry{
					{IssueID: "td-002", SourcePanel: PanelTaskList},
				},
			},
			delta:       1,
			expectIssue: "td-002", // stays at last
		},
		{
			name: "no navigation at depth > 1",
			model: Model{
				Keymap:     newTestKeymap(),
				Cursor:     map[Panel]int{PanelTaskList: 0},
				SelectedID: map[Panel]string{},
				TaskListRows: []TaskListRow{
					{Issue: models.Issue{ID: "td-001"}},
					{Issue: models.Issue{ID: "td-002"}},
				},
				ModalStack: []ModalEntry{
					{IssueID: "td-001", SourcePanel: PanelTaskList},
					{IssueID: "td-002", SourcePanel: PanelTaskList},
				},
			},
			delta:       1,
			expectIssue: "td-002", // no change (depth 2)
		},
		{
			name: "no modal returns no change",
			model: Model{
				Keymap:     newTestKeymap(),
				Cursor:     map[Panel]int{},
				SelectedID: map[Panel]string{},
				ModalStack: []ModalEntry{},
			},
			delta:       1,
			expectIssue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := tt.model.navigateModal(tt.delta)
			m := result.(Model)

			if tt.expectIssue == "" {
				if m.ModalOpen() {
					t.Error("expected no modal, but modal is open")
				}
				return
			}

			if !m.ModalOpen() {
				t.Fatal("expected modal to be open")
			}
			if m.CurrentModal().IssueID != tt.expectIssue {
				t.Errorf("modal issue = %q, want %q", m.CurrentModal().IssueID, tt.expectIssue)
			}
		})
	}
}

func TestPushModal(t *testing.T) {
	m := Model{
		Keymap:     newTestKeymap(),
		ModalStack: []ModalEntry{},
	}

	// Push first modal
	result, cmd := m.pushModal("td-001", PanelTaskList)
	m = result.(Model)

	if m.ModalDepth() != 1 {
		t.Errorf("after first push, depth = %d, want 1", m.ModalDepth())
	}
	if m.CurrentModal().IssueID != "td-001" {
		t.Errorf("first modal issue = %q, want td-001", m.CurrentModal().IssueID)
	}
	if !m.CurrentModal().Loading {
		t.Error("new modal should be loading")
	}
	if cmd == nil {
		t.Error("pushModal should return a fetch command")
	}

	// Push second modal
	result, _ = m.pushModal("td-002", PanelTaskList)
	m = result.(Model)

	if m.ModalDepth() != 2 {
		t.Errorf("after second push, depth = %d, want 2", m.ModalDepth())
	}
	if m.CurrentModal().IssueID != "td-002" {
		t.Errorf("top modal issue = %q, want td-002", m.CurrentModal().IssueID)
	}
}

func TestCloseModalOnEmptyStack(t *testing.T) {
	m := Model{
		Keymap:     newTestKeymap(),
		ModalStack: []ModalEntry{},
	}

	// Closing empty stack should not panic
	m.closeModal()

	if m.ModalDepth() != 0 {
		t.Errorf("after close on empty, depth = %d, want 0", m.ModalDepth())
	}
}

func TestCloseModalPopsStack(t *testing.T) {
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{IssueID: "td-001"},
			{IssueID: "td-002"},
		},
	}

	// Close should pop top
	m.closeModal()

	if m.ModalDepth() != 1 {
		t.Errorf("after first close, depth = %d, want 1", m.ModalDepth())
	}
	if m.CurrentModal().IssueID != "td-001" {
		t.Errorf("remaining modal = %q, want td-001", m.CurrentModal().IssueID)
	}

	// Close again
	m.closeModal()

	if m.ModalDepth() != 0 {
		t.Errorf("after second close, depth = %d, want 0", m.ModalDepth())
	}
	if m.ModalOpen() {
		t.Error("expected modal to be closed")
	}
}

// Tests for parent epic focus navigation

func TestParentEpicFocus_JKeyFocusesEpicWhenScroll0(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic, Title: "Parent Epic"}
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:     "td-story",
				Issue:       &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:  parentEpic,
				Scroll:      0,
				ParentEpicFocused: false,
			},
		},
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if !m2.CurrentModal().ParentEpicFocused {
		t.Error("j key at scroll=0 with parent epic should focus the epic")
	}
}

func TestParentEpicFocus_JKeyUnfocusesAndScrollsPastEpicZone(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Height: 30, // Needed for modal height calculation
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        parentEpic,
				Scroll:            0,
				ParentEpicFocused: true,
				ContentLines:      50, // Enough content to allow scrolling
			},
		},
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.CurrentModal().ParentEpicFocused {
		t.Error("j key when focused on epic should unfocus it")
	}
	if m2.CurrentModal().Scroll != 1 {
		t.Errorf("j key when unfocusing epic should set scroll=1, got %d", m2.CurrentModal().Scroll)
	}

	// Pressing j again should NOT re-focus (it should scroll)
	updated, _ = m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m3 := updated.(Model)

	if m3.CurrentModal().ParentEpicFocused {
		t.Error("j key after unfocusing should scroll, not re-focus epic")
	}
	if m3.CurrentModal().Scroll != 2 {
		t.Errorf("j key should increment scroll, got %d", m3.CurrentModal().Scroll)
	}
}

func TestParentEpicFocus_KKeyAtScroll0FocusesEpic(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        parentEpic,
				Scroll:            0,
				ParentEpicFocused: false,
			},
		},
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m2 := updated.(Model)

	if !m2.CurrentModal().ParentEpicFocused {
		t.Error("k key at scroll=0 with parent epic should focus the epic")
	}
}

func TestParentEpicFocus_EnterOpensEpicModal(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        parentEpic,
				Scroll:            0,
				ParentEpicFocused: true,
				SourcePanel:       PanelTaskList,
			},
		},
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	if m2.ModalDepth() != 2 {
		t.Errorf("Enter on focused epic should push modal, depth = %d, want 2", m2.ModalDepth())
	}
	if m2.CurrentModal().IssueID != "td-epic" {
		t.Errorf("pushed modal should be for epic, got %q", m2.CurrentModal().IssueID)
	}
	if cmd == nil {
		t.Error("Enter on epic should return a fetch command")
	}
}

func TestParentEpicFocus_EscClosesModalDoesNotOpenEpic(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap:      newTestKeymap(),
		ActivePanel: PanelTaskList,
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        parentEpic,
				Scroll:            0,
				ParentEpicFocused: true,
				SourcePanel:       PanelTaskList,
			},
		},
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if m2.ModalOpen() {
		t.Error("ESC when parent epic focused should close modal")
	}
	if m2.ModalDepth() != 0 {
		t.Errorf("modal depth should be 0, got %d", m2.ModalDepth())
	}
}

func TestParentEpicFocus_OrphanStoryNoEpic(t *testing.T) {
	m := Model{
		Height: 30, // Needed for modal height calculation
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        nil, // No parent
				Scroll:            0,
				ParentEpicFocused: false,
				ContentLines:      50, // Enough content to allow scrolling
			},
		},
	}

	// j should scroll, not try to focus a nonexistent epic
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m2 := updated.(Model)

	if m2.CurrentModal().ParentEpicFocused {
		t.Error("j key should not focus epic when there is no parent epic")
	}
	if m2.CurrentModal().Scroll != 1 {
		t.Errorf("j key on orphan story should scroll, got scroll=%d", m2.CurrentModal().Scroll)
	}
}

func TestParentEpicFocus_ContextReturnsParentEpicFocused(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				ParentEpic:        parentEpic,
				ParentEpicFocused: true,
			},
		},
	}

	ctx := m.currentContext()
	if ctx != keymap.ContextParentEpicFocused {
		t.Errorf("context = %q, want %q", ctx, keymap.ContextParentEpicFocused)
	}
}

func TestParentEpicFocus_KKeyStaysOnEpicWhenAlreadyFocused(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        parentEpic,
				Scroll:            0,
				ParentEpicFocused: true,
			},
		},
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m2 := updated.(Model)

	// Should stay focused on epic, not open it or do anything else
	if !m2.CurrentModal().ParentEpicFocused {
		t.Error("k key when already focused on epic should stay focused")
	}
	if m2.ModalDepth() != 1 {
		t.Errorf("k key should not push new modal, depth = %d", m2.ModalDepth())
	}
}

func TestNavigateModalClearsParentEpicState(t *testing.T) {
	parentEpic := &models.Issue{ID: "td-epic", Type: models.TypeEpic}
	m := Model{
		Keymap:     newTestKeymap(),
		Cursor:     map[Panel]int{PanelTaskList: 0},
		SelectedID: map[Panel]string{},
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "td-001"}},
			{Issue: models.Issue{ID: "td-002"}},
		},
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-001",
				SourcePanel:       PanelTaskList,
				ParentEpic:        parentEpic,
				ParentEpicFocused: true,
			},
		},
	}

	updated, _ := m.navigateModal(1)
	m2 := updated.(Model)

	if m2.CurrentModal().ParentEpic != nil {
		t.Error("navigateModal should clear ParentEpic")
	}
	if m2.CurrentModal().ParentEpicFocused {
		t.Error("navigateModal should clear ParentEpicFocused")
	}
	if m2.CurrentModal().IssueID != "td-002" {
		t.Errorf("navigateModal should move to td-002, got %s", m2.CurrentModal().IssueID)
	}
}

func TestMouseWheelScrollDownInModal(t *testing.T) {
	m := Model{
		Width:  80,
		Height: 30,
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:      "td-001",
				Issue:        &models.Issue{ID: "td-001", Type: models.TypeTask},
				Scroll:       5,
				ContentLines: 50,
				SourcePanel:  PanelTaskList,
			},
		},
		PaneHeights: defaultPaneHeights(),
	}

	downMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
		X:      40,
		Y:      15,
	}
	updated, _ := m.handleMouse(downMsg)
	m2 := updated.(Model)

	// Scroll should increase by 3 (delta)
	if m2.CurrentModal().Scroll != 8 {
		t.Errorf("Scroll down should increase scroll to 8, got %d", m2.CurrentModal().Scroll)
	}
}

func TestMouseWheelScrollUpInModal(t *testing.T) {
	m := Model{
		Width:  80,
		Height: 30,
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:      "td-001",
				Issue:        &models.Issue{ID: "td-001", Type: models.TypeTask},
				Scroll:       10,
				ContentLines: 50,
				SourcePanel:  PanelTaskList,
			},
		},
		PaneHeights: defaultPaneHeights(),
	}

	upMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		X:      40,
		Y:      15,
	}
	updated, _ := m.handleMouse(upMsg)
	m2 := updated.(Model)

	// Scroll should decrease by 3 (delta)
	if m2.CurrentModal().Scroll != 7 {
		t.Errorf("Scroll up should decrease scroll to 7, got %d", m2.CurrentModal().Scroll)
	}
}

func TestMouseWheelScrollInModalClampsBounds(t *testing.T) {
	m := Model{
		Width:  80,
		Height: 30,
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:      "td-001",
				Issue:        &models.Issue{ID: "td-001", Type: models.TypeTask},
				Scroll:       0,
				ContentLines: 10, // Short content
				SourcePanel:  PanelTaskList,
			},
		},
		PaneHeights: defaultPaneHeights(),
	}

	// Scroll up at top should stay at 0
	upMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		X:      40,
		Y:      15,
	}
	updated, _ := m.handleMouse(upMsg)
	m2 := updated.(Model)

	if m2.CurrentModal().Scroll != 0 {
		t.Errorf("Scroll up at top should stay at 0, got %d", m2.CurrentModal().Scroll)
	}
}

func TestMouseWheelScrollInEpicScrollsContent(t *testing.T) {
	// Mouse wheel in epic modal should scroll content, not task cursor
	// (task cursor is navigated with j/k keys)
	m := Model{
		Width:  80,
		Height: 30,
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:            "td-epic",
				Issue:              &models.Issue{ID: "td-epic", Type: models.TypeEpic},
				EpicTasks:          []models.Issue{{ID: "td-1"}, {ID: "td-2"}, {ID: "td-3"}, {ID: "td-4"}, {ID: "td-5"}},
				TaskSectionFocused: true,
				EpicTasksCursor:    0,
				Scroll:             0,
				ContentLines:       50,
				SourcePanel:        PanelTaskList,
			},
		},
		PaneHeights: defaultPaneHeights(),
	}

	// Scroll down should scroll modal content, not task cursor
	downMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
		X:      40,
		Y:      15,
	}
	updated, _ := m.handleMouse(downMsg)
	m2 := updated.(Model)

	// Task cursor should remain unchanged
	if m2.CurrentModal().EpicTasksCursor != 0 {
		t.Errorf("Mouse wheel should not move task cursor, got %d", m2.CurrentModal().EpicTasksCursor)
	}
	// Modal content should scroll
	if m2.CurrentModal().Scroll == 0 {
		t.Error("Mouse wheel should scroll modal content")
	}
}

func TestModalContentWidth(t *testing.T) {
	tests := []struct {
		name        string
		termWidth   int
		expectWidth int
	}{
		{
			name:        "normal terminal 100 chars",
			termWidth:   100,
			expectWidth: 76, // (100 * 80 / 100) - 4 = 76
		},
		{
			name:        "wide terminal 150 chars",
			termWidth:   150,
			expectWidth: 96, // capped at 100, minus 4 = 96
		},
		{
			name:        "narrow terminal 50 chars",
			termWidth:   50,
			expectWidth: 36, // (50 * 80 / 100) - 4 = 36
		},
		{
			name:        "very narrow terminal 30 chars",
			termWidth:   30,
			expectWidth: 36, // modal min 40, content min 36
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{Width: tt.termWidth}
			got := m.modalContentWidth()
			if got != tt.expectWidth {
				t.Errorf("modalContentWidth() = %d, want %d", got, tt.expectWidth)
			}
		})
	}
}

// TestEpicAutoFocusTaskSection verifies that epics with tasks auto-focus the task section
// This enables j/k navigation without requiring Tab to enter the task section
func TestEpicAutoFocusTaskSection(t *testing.T) {
	tests := []struct {
		name               string
		issue              *models.Issue
		epicTasks          []models.Issue
		expectFocused      bool
		expectCursor       int
	}{
		{
			name:          "epic with tasks auto-focuses",
			issue:         &models.Issue{ID: "td-001", Type: models.TypeEpic},
			epicTasks:     []models.Issue{{ID: "td-002"}, {ID: "td-003"}},
			expectFocused: true,
			expectCursor:  0,
		},
		{
			name:          "epic without tasks does not auto-focus",
			issue:         &models.Issue{ID: "td-001", Type: models.TypeEpic},
			epicTasks:     []models.Issue{},
			expectFocused: false,
			expectCursor:  0,
		},
		{
			name:          "non-epic does not auto-focus",
			issue:         &models.Issue{ID: "td-001", Type: models.TypeTask},
			epicTasks:     []models.Issue{},
			expectFocused: false,
			expectCursor:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Keymap: newTestKeymap(),
				ModalStack: []ModalEntry{
					{
						IssueID:            tt.issue.ID,
						Loading:            true,
						TaskSectionFocused: false,
					},
				},
			}

			// Simulate IssueDetailsMsg
			msg := IssueDetailsMsg{
				IssueID:   tt.issue.ID,
				Issue:     tt.issue,
				EpicTasks: tt.epicTasks,
			}

			updated, _ := m.Update(msg)
			m2 := updated.(Model)

			if m2.CurrentModal().TaskSectionFocused != tt.expectFocused {
				t.Errorf("TaskSectionFocused = %v, want %v",
					m2.CurrentModal().TaskSectionFocused, tt.expectFocused)
			}
			if m2.CurrentModal().EpicTasksCursor != tt.expectCursor {
				t.Errorf("EpicTasksCursor = %d, want %d",
					m2.CurrentModal().EpicTasksCursor, tt.expectCursor)
			}
		})
	}
}

func TestUpdateQuerySort(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		sortMode SortMode
		expected string
	}{
		{
			name:     "empty query gets sort clause",
			query:    "",
			sortMode: SortByCreatedDesc,
			expected: "sort:-created",
		},
		{
			name:     "query without sort gets sort appended",
			query:    "type=epic",
			sortMode: SortByUpdatedDesc,
			expected: "type=epic sort:-updated",
		},
		{
			name:     "query with existing sort gets replaced",
			query:    "type=epic sort:id",
			sortMode: SortByCreatedDesc,
			expected: "type=epic sort:-created",
		},
		{
			name:     "priority sort mode",
			query:    "status=open",
			sortMode: SortByPriority,
			expected: "status=open sort:priority",
		},
		{
			name:     "multiple words without sort",
			query:    "type=bug status=open",
			sortMode: SortByCreatedDesc,
			expected: "type=bug status=open sort:-created",
		},
		{
			name:     "descending sort replaced",
			query:    "sort:-updated",
			sortMode: SortByCreatedDesc,
			expected: "sort:-created",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateQuerySort(tt.query, tt.sortMode)
			if got != tt.expected {
				t.Errorf("updateQuerySort(%q, %v) = %q, want %q",
					tt.query, tt.sortMode, got, tt.expected)
			}
		})
	}
}

func TestSortModeToSortClause(t *testing.T) {
	tests := []struct {
		mode     SortMode
		expected string
	}{
		{SortByPriority, "sort:priority"},
		{SortByCreatedDesc, "sort:-created"},
		{SortByUpdatedDesc, "sort:-updated"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.mode.ToSortClause()
			if got != tt.expected {
				t.Errorf("ToSortClause() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Pane Resize Tests
// =============================================================================

// newResizeTestModel creates a model configured for pane resize testing
func newResizeTestModel(width, height int) Model {
	m := Model{
		Width:            width,
		Height:           height,
		PaneHeights:      [3]float64{0.333, 0.333, 0.334},
		PanelBounds:      make(map[Panel]Rect),
		DividerBounds:    [2]Rect{},
		DraggingDivider:  -1,
		DividerHover:     -1,
		Cursor:           make(map[Panel]int),
		ScrollOffset:     make(map[Panel]int),
		SelectedID:       make(map[Panel]string),
		Keymap:           newTestKeymap(),
	}
	m.updatePanelBounds()
	return m
}

func TestDividerHitTest(t *testing.T) {
	m := newResizeTestModel(80, 30)

	tests := []struct {
		name     string
		x, y     int
		expected int
	}{
		// Divider 0: between pane 0 and 1 (at Y ~= height * 0.333)
		{"divider 0 center", 40, m.DividerBounds[0].Y + 1, 0},
		{"divider 0 left edge", 0, m.DividerBounds[0].Y, 0},
		{"divider 0 right edge", 79, m.DividerBounds[0].Y, 0},

		// Divider 1: between pane 1 and 2
		{"divider 1 center", 40, m.DividerBounds[1].Y + 1, 1},
		{"divider 1 left edge", 0, m.DividerBounds[1].Y, 1},

		// Non-divider areas
		{"middle of pane 0", 40, 3, -1},
		{"middle of pane 1", 40, m.PanelBounds[PanelTaskList].Y + 3, -1},
		{"bottom of pane 2", 40, m.Height - 5, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.HitTestDivider(tt.x, tt.y)
			if got != tt.expected {
				t.Errorf("HitTestDivider(%d, %d) = %d, want %d", tt.x, tt.y, got, tt.expected)
			}
		})
	}
}

func TestDragDividerUpdatesHeights(t *testing.T) {
	m := newResizeTestModel(80, 100) // 100px height for easy math
	m.PaneHeights = [3]float64{0.333, 0.333, 0.334}
	m.updatePanelBounds()

	// Start drag on divider 0 at the actual divider position
	startY := m.DividerBounds[0].Y + 1
	result, _ := m.startDividerDrag(0, startY)
	m = result.(Model)

	if m.DraggingDivider != 0 {
		t.Fatalf("DraggingDivider = %d, want 0", m.DraggingDivider)
	}

	// Drag down 10 pixels (~10% of available height)
	result, _ = m.updateDividerDrag(startY + 10)
	m = result.(Model)

	// Pane 0 should grow, pane 1 should shrink, pane 2 unchanged
	if m.PaneHeights[0] <= 0.333 {
		t.Errorf("Pane 0 should have grown: got %f", m.PaneHeights[0])
	}
	if m.PaneHeights[1] >= 0.333 {
		t.Errorf("Pane 1 should have shrunk: got %f", m.PaneHeights[1])
	}

	// Sum should still be 1.0
	sum := m.PaneHeights[0] + m.PaneHeights[1] + m.PaneHeights[2]
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("Heights don't sum to 1.0: got %f", sum)
	}
}

func TestDragEnforcesMinimumHeights(t *testing.T) {
	m := newResizeTestModel(80, 100)
	m.PaneHeights = [3]float64{0.333, 0.333, 0.334}
	m.updatePanelBounds()

	startY := m.DividerBounds[0].Y + 1
	result, _ := m.startDividerDrag(0, startY)
	m = result.(Model)

	originalHeights := m.PaneHeights

	// Try to drag way down (would make pane 1 < 10%)
	result, _ = m.updateDividerDrag(startY + 60) // Large delta - would violate min
	m = result.(Model)

	// All panes should still be >= 10%
	const minHeight = 0.1
	for i, h := range m.PaneHeights {
		if h < minHeight-0.001 { // Small tolerance for float comparison
			t.Errorf("Pane %d height %f < minimum %f", i, h, minHeight)
		}
	}

	// If constraint couldn't be satisfied, heights should remain unchanged
	if m.PaneHeights[1] < minHeight {
		if m.PaneHeights != originalHeights {
			t.Error("Heights changed despite violating constraints")
		}
	}
}

func TestPaneHeightsPreservedOnWindowResize(t *testing.T) {
	m := newResizeTestModel(80, 100)
	customHeights := [3]float64{0.5, 0.3, 0.2}
	m.PaneHeights = customHeights
	m.updatePanelBounds()

	// Simulate window resize
	m.Width = 120
	m.Height = 60
	m.updatePanelBounds()

	// Ratios should be unchanged
	for i := range customHeights {
		if m.PaneHeights[i] != customHeights[i] {
			t.Errorf("Pane %d height changed after resize: got %f, want %f",
				i, m.PaneHeights[i], customHeights[i])
		}
	}
}

func TestVisibleHeightUsesActualPanelHeight(t *testing.T) {
	tests := []struct {
		name        string
		height      int
		paneHeights [3]float64
		searchMode  bool
		embedded    bool
	}{
		{
			name:        "default heights",
			height:      100,
			paneHeights: [3]float64{0.333, 0.333, 0.334},
			searchMode:  false,
			embedded:    false,
		},
		{
			name:        "custom heights 50/30/20",
			height:      100,
			paneHeights: [3]float64{0.5, 0.3, 0.2},
			searchMode:  false,
			embedded:    false,
		},
		{
			name:        "with search bar",
			height:      100,
			paneHeights: [3]float64{0.333, 0.333, 0.334},
			searchMode:  true,
			embedded:    false,
		},
		{
			name:        "embedded mode (no footer)",
			height:      100,
			paneHeights: [3]float64{0.333, 0.333, 0.334},
			searchMode:  false,
			embedded:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newResizeTestModel(80, tt.height)
			m.PaneHeights = tt.paneHeights
			m.SearchMode = tt.searchMode
			m.Embedded = tt.embedded
			m.updatePanelBounds()

			// Calculate expected available height
			searchBarHeight := 0
			if tt.searchMode {
				searchBarHeight = 2
			}
			footerHeight := 3
			if tt.embedded {
				footerHeight = 0
			}
			availableHeight := tt.height - footerHeight - searchBarHeight

			// Test each panel
			for panel := Panel(0); panel < 3; panel++ {
				expectedPanelHeight := int(float64(availableHeight) * tt.paneHeights[panel])
				// visibleHeight = panelHeight - 5 (title + border + indicators)
				expectedVisible := expectedPanelHeight - 5

				got := m.visibleHeightForPanel(panel)

				// Allow small variance due to rounding
				diff := got - expectedVisible
				if diff < -1 || diff > 1 {
					t.Errorf("visibleHeightForPanel(%d) = %d, want ~%d",
						panel, got, expectedVisible)
				}
			}
		})
	}
}

func TestHitTestTaskListRowBottomIndicator(t *testing.T) {
	// Test that clicks on the bottom scroll indicator return -1
	m := Model{
		Width:  80,
		Height: 30,
		PaneHeights: [3]float64{0.333, 0.333, 0.334},
		ScrollOffset: map[Panel]int{
			PanelTaskList: 2, // Scrolled down, so top indicator is shown
		},
		// Create enough rows to require both scroll indicators
		TaskListRows: []TaskListRow{
			{Issue: models.Issue{ID: "td-1"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-2"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-3"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-4"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-5"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-6"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-7"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-8"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-9"}, Category: CategoryReady},
			{Issue: models.Issue{ID: "td-10"}, Category: CategoryReady},
		},
	}

	// Click on top indicator (relY = 0) should return -1
	if got := m.hitTestTaskListRow(0); got != -1 {
		t.Errorf("hitTestTaskListRow(0) with top indicator = %d, want -1", got)
	}

	// Click on first visible row (relY = 1) should return the offset index (2)
	if got := m.hitTestTaskListRow(1); got != 2 {
		t.Errorf("hitTestTaskListRow(1) = %d, want 2 (first visible row)", got)
	}

	// Calculate where bottom indicator would be
	visibleLines := m.visibleHeightForPanel(PanelTaskList) - 1 // -1 for top indicator

	// Click at bottom indicator position should return -1
	if got := m.hitTestTaskListRow(visibleLines); got != -1 {
		t.Errorf("hitTestTaskListRow(%d) at bottom indicator = %d, want -1", visibleLines, got)
	}
}
