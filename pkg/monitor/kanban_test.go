package monitor

import (
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestKanbanColumnIssues(t *testing.T) {
	data := TaskListData{
		Reviewable:  []models.Issue{{ID: "r1"}},
		NeedsRework: []models.Issue{{ID: "nw1"}, {ID: "nw2"}},
		Ready:       []models.Issue{{ID: "rd1"}, {ID: "rd2"}, {ID: "rd3"}},
		Blocked:     nil,
		Closed:      []models.Issue{{ID: "c1"}},
	}

	tests := []struct {
		cat      TaskListCategory
		expected int
	}{
		{CategoryReviewable, 1},
		{CategoryNeedsRework, 2},
		{CategoryReady, 3},
		{CategoryBlocked, 0},
		{CategoryClosed, 1},
	}

	for _, tt := range tests {
		issues := kanbanColumnIssues(data, tt.cat)
		if len(issues) != tt.expected {
			t.Errorf("kanbanColumnIssues(%s) = %d issues, want %d", tt.cat, len(issues), tt.expected)
		}
	}
}

func TestKanbanNavigation(t *testing.T) {
	m := Model{
		KanbanOpen: true,
		KanbanCol:  0,
		KanbanRow:  0,
		BoardMode: BoardMode{
			SwimlaneData: TaskListData{
				Reviewable:  []models.Issue{{ID: "r1"}, {ID: "r2"}},
				NeedsRework: nil,
				Ready:       []models.Issue{{ID: "rd1"}},
				Blocked:     nil,
				Closed:      []models.Issue{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}},
			},
		},
	}

	// Test move down within column
	m.kanbanMoveDown()
	if m.KanbanRow != 1 {
		t.Errorf("after moveDown: row = %d, want 1", m.KanbanRow)
	}

	// Test move down at bottom of column (should not move)
	m.kanbanMoveDown()
	if m.KanbanRow != 1 {
		t.Errorf("after moveDown at bottom: row = %d, want 1", m.KanbanRow)
	}

	// Test move up
	m.kanbanMoveUp()
	if m.KanbanRow != 0 {
		t.Errorf("after moveUp: row = %d, want 0", m.KanbanRow)
	}

	// Test move up at top (should not move)
	m.kanbanMoveUp()
	if m.KanbanRow != 0 {
		t.Errorf("after moveUp at top: row = %d, want 0", m.KanbanRow)
	}

	// Test move right
	m.kanbanMoveRight()
	if m.KanbanCol != 1 {
		t.Errorf("after moveRight: col = %d, want 1", m.KanbanCol)
	}

	// Column 1 (NeedsRework) is empty - row should clamp to 0
	if m.KanbanRow != 0 {
		t.Errorf("after moveRight to empty col: row = %d, want 0", m.KanbanRow)
	}

	// Move right again to Ready (col 2)
	m.kanbanMoveRight()
	if m.KanbanCol != 2 {
		t.Errorf("after second moveRight: col = %d, want 2", m.KanbanCol)
	}

	// Move right to Blocked (col 3), then Closed (col 4)
	m.kanbanMoveRight()
	m.kanbanMoveRight()
	if m.KanbanCol != 4 {
		t.Errorf("col should be 4, got %d", m.KanbanCol)
	}

	// Move right at rightmost column (should not move)
	m.kanbanMoveRight()
	if m.KanbanCol != 4 {
		t.Errorf("after moveRight at rightmost: col = %d, want 4", m.KanbanCol)
	}

	// Col 4 (Closed) has 3 items - move down to row 2
	m.kanbanMoveDown()
	m.kanbanMoveDown()
	if m.KanbanRow != 2 {
		t.Errorf("after moving down in Closed: row = %d, want 2", m.KanbanRow)
	}

	// Move left to Blocked (empty) - row should clamp to 0
	m.kanbanMoveLeft()
	if m.KanbanCol != 3 {
		t.Errorf("after moveLeft: col = %d, want 3", m.KanbanCol)
	}
	if m.KanbanRow != 0 {
		t.Errorf("after moveLeft to empty col: row = %d, want 0", m.KanbanRow)
	}

	// Move left to col 0
	m.kanbanMoveLeft()
	m.kanbanMoveLeft()
	m.kanbanMoveLeft()
	if m.KanbanCol != 0 {
		t.Errorf("col should be 0, got %d", m.KanbanCol)
	}

	// Move left at leftmost (should not move)
	m.kanbanMoveLeft()
	if m.KanbanCol != 0 {
		t.Errorf("after moveLeft at leftmost: col = %d, want 0", m.KanbanCol)
	}
}

func TestKanbanClampRow(t *testing.T) {
	m := Model{
		KanbanOpen: true,
		KanbanCol:  0,
		KanbanRow:  5, // out of range
		BoardMode: BoardMode{
			SwimlaneData: TaskListData{
				Reviewable: []models.Issue{{ID: "r1"}, {ID: "r2"}},
			},
		},
	}

	m.clampKanbanRow()
	if m.KanbanRow != 1 {
		t.Errorf("clampKanbanRow: row = %d, want 1", m.KanbanRow)
	}

	// Empty column
	m.KanbanCol = 1 // NeedsRework is empty
	m.KanbanRow = 5
	m.clampKanbanRow()
	if m.KanbanRow != 0 {
		t.Errorf("clampKanbanRow on empty col: row = %d, want 0", m.KanbanRow)
	}
}

func TestKanbanColumnLabelsAndColors(t *testing.T) {
	// Verify all columns have labels and colors
	for _, cat := range kanbanColumnOrder {
		label := kanbanColumnLabel(cat)
		if label == "" {
			t.Errorf("kanbanColumnLabel(%s) returned empty string", cat)
		}

		color := kanbanColumnColor(cat)
		if color == "" {
			t.Errorf("kanbanColumnColor(%s) returned empty string", cat)
		}
	}
}
