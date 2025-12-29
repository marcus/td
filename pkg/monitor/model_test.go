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
		Keymap: newTestKeymap(),
		ModalStack: []ModalEntry{
			{
				IssueID:           "td-story",
				Issue:             &models.Issue{ID: "td-story", Type: models.TypeTask},
				ParentEpic:        nil, // No parent
				Scroll:            0,
				ParentEpicFocused: false,
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
