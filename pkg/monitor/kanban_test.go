package monitor

import (
	"fmt"
	"testing"

	"github.com/marcus/td/internal/models"
)

func TestKanbanColumnIssues(t *testing.T) {
	data := TaskListData{
		Reviewable:    []models.Issue{{ID: "r1"}},
		NeedsRework:   []models.Issue{{ID: "nw1"}, {ID: "nw2"}},
		InProgress:    []models.Issue{{ID: "ip1"}},
		Ready:         []models.Issue{{ID: "rd1"}, {ID: "rd2"}, {ID: "rd3"}},
		PendingReview: []models.Issue{{ID: "pr1"}, {ID: "pr2"}},
		Blocked:       nil,
		Closed:        []models.Issue{{ID: "c1"}},
	}

	tests := []struct {
		cat      TaskListCategory
		expected int
	}{
		{CategoryReviewable, 1},
		{CategoryNeedsRework, 2},
		{CategoryInProgress, 1},
		{CategoryReady, 3},
		{CategoryPendingReview, 2},
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

// newKanbanTestModel creates a Model with sensible defaults for kanban tests.
func newKanbanTestModel(data TaskListData) Model {
	return Model{
		KanbanOpen:       true,
		KanbanCol:        0,
		KanbanRow:        0,
		KanbanColScrolls: make([]int, len(kanbanColumnOrder)),
		Width:            120,
		Height:           40,
		BoardMode: BoardMode{
			SwimlaneData: data,
		},
	}
}

func TestKanbanNavigation(t *testing.T) {
	// Column order: 0=Review, 1=Rework, 2=InProgress, 3=Ready, 4=PendingReview, 5=Blocked, 6=Closed
	m := newKanbanTestModel(TaskListData{
		Reviewable:    []models.Issue{{ID: "r1"}, {ID: "r2"}},
		NeedsRework:   nil,
		InProgress:    []models.Issue{{ID: "ip1"}},
		Ready:         []models.Issue{{ID: "rd1"}},
		PendingReview: nil,
		Blocked:       nil,
		Closed:        []models.Issue{{ID: "c1"}, {ID: "c2"}, {ID: "c3"}},
	})

	// Test move down within column (Reviewable has 2 items)
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

	// Test move right to col 1 (NeedsRework - empty)
	m.kanbanMoveRight()
	if m.KanbanCol != 1 {
		t.Errorf("after moveRight: col = %d, want 1", m.KanbanCol)
	}
	if m.KanbanRow != 0 {
		t.Errorf("after moveRight to empty col: row = %d, want 0", m.KanbanRow)
	}

	// Move right to InProgress (col 2)
	m.kanbanMoveRight()
	if m.KanbanCol != 2 {
		t.Errorf("after second moveRight: col = %d, want 2", m.KanbanCol)
	}

	// Move all the way to Closed (col 6)
	m.kanbanMoveRight() // col 3 (Ready)
	m.kanbanMoveRight() // col 4 (PendingReview)
	m.kanbanMoveRight() // col 5 (Blocked)
	m.kanbanMoveRight() // col 6 (Closed)
	if m.KanbanCol != 6 {
		t.Errorf("col should be 6, got %d", m.KanbanCol)
	}

	// Move right at rightmost column (should not move)
	m.kanbanMoveRight()
	if m.KanbanCol != 6 {
		t.Errorf("after moveRight at rightmost: col = %d, want 6", m.KanbanCol)
	}

	// Col 6 (Closed) has 3 items - move down to row 2
	m.kanbanMoveDown()
	m.kanbanMoveDown()
	if m.KanbanRow != 2 {
		t.Errorf("after moving down in Closed: row = %d, want 2", m.KanbanRow)
	}

	// Move left to Blocked (col 5, empty) - row should clamp to 0
	m.kanbanMoveLeft()
	if m.KanbanCol != 5 {
		t.Errorf("after moveLeft: col = %d, want 5", m.KanbanCol)
	}
	if m.KanbanRow != 0 {
		t.Errorf("after moveLeft to empty col: row = %d, want 0", m.KanbanRow)
	}

	// Move left to col 0
	m.kanbanMoveLeft() // col 4
	m.kanbanMoveLeft() // col 3
	m.kanbanMoveLeft() // col 2
	m.kanbanMoveLeft() // col 1
	m.kanbanMoveLeft() // col 0
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
	m := newKanbanTestModel(TaskListData{
		Reviewable: []models.Issue{{ID: "r1"}, {ID: "r2"}},
	})
	m.KanbanRow = 5 // out of range

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
		if color == nil {
			t.Errorf("kanbanColumnColor(%s) returned empty string", cat)
		}
	}
}

func TestKanbanPerColumnScroll(t *testing.T) {
	// Create a model with a small viewport so scrolling is needed.
	// Height=20: modalHeight = 20*85/100 = 17, availableCardHeight = 17-8 = 9,
	// maxVisibleCards = 9/3 = 3
	m := Model{
		KanbanOpen:       true,
		KanbanCol:        0,
		KanbanRow:        0,
		KanbanColScrolls: make([]int, len(kanbanColumnOrder)),
		Width:            120,
		Height:           20,
		BoardMode: BoardMode{
			SwimlaneData: TaskListData{
				Reviewable: makeIssues("r", 10),
				Closed:     makeIssues("c", 8),
			},
		},
	}

	_, _, _, maxVisible := m.kanbanDimensions()
	if maxVisible < 1 {
		t.Fatalf("maxVisibleCards = %d, expected > 0", maxVisible)
	}

	// Move down past visible area
	for i := 0; i < maxVisible+2; i++ {
		m.kanbanMoveDown()
	}

	// KanbanRow should be at maxVisible+2 (or capped to column length-1)
	expectedRow := maxVisible + 2
	if expectedRow > 9 {
		expectedRow = 9
	}
	if m.KanbanRow != expectedRow {
		t.Errorf("after scrolling down: row = %d, want %d", m.KanbanRow, expectedRow)
	}

	// Scroll for column 0 should be > 0
	if m.KanbanColScrolls[0] <= 0 {
		t.Errorf("column 0 scroll should be > 0, got %d", m.KanbanColScrolls[0])
	}

	// Record column 0 scroll
	col0Scroll := m.KanbanColScrolls[0]

	// Move to Closed column (col 6)
	m.KanbanCol = 6
	m.clampKanbanRow()
	m.ensureKanbanCursorVisible()

	// Column 0 scroll should be preserved
	if m.KanbanColScrolls[0] != col0Scroll {
		t.Errorf("column 0 scroll changed after switching: got %d, want %d", m.KanbanColScrolls[0], col0Scroll)
	}

	// Scroll down in Closed column
	for i := 0; i < maxVisible+1; i++ {
		m.kanbanMoveDown()
	}

	// Column 6 scroll should be > 0 (if there are enough items)
	if len(m.BoardMode.SwimlaneData.Closed) > maxVisible {
		if m.KanbanColScrolls[6] <= 0 {
			t.Errorf("column 6 scroll should be > 0, got %d", m.KanbanColScrolls[6])
		}
	}

	// Column 0 scroll should still be preserved
	if m.KanbanColScrolls[0] != col0Scroll {
		t.Errorf("column 0 scroll changed after scrolling col 6: got %d, want %d", m.KanbanColScrolls[0], col0Scroll)
	}
}

func TestKanbanScrollUpPreservesPosition(t *testing.T) {
	m := Model{
		KanbanOpen:       true,
		KanbanCol:        0,
		KanbanRow:        0,
		KanbanColScrolls: make([]int, len(kanbanColumnOrder)),
		Width:            120,
		Height:           20,
		BoardMode: BoardMode{
			SwimlaneData: TaskListData{
				Reviewable: makeIssues("r", 10),
			},
		},
	}

	_, _, _, maxVisible := m.kanbanDimensions()

	// Scroll down then up
	for i := 0; i < maxVisible+3; i++ {
		m.kanbanMoveDown()
	}
	scrollAfterDown := m.KanbanColScrolls[0]

	m.kanbanMoveUp()
	// Scroll should stay the same (cursor moved but still in view)
	if m.KanbanColScrolls[0] != scrollAfterDown {
		t.Errorf("scroll changed on moveUp when cursor still visible: got %d, want %d",
			m.KanbanColScrolls[0], scrollAfterDown)
	}

	// Move up enough to scroll up
	for i := 0; i < maxVisible; i++ {
		m.kanbanMoveUp()
	}
	if m.KanbanColScrolls[0] >= scrollAfterDown {
		t.Errorf("scroll should decrease after moving up past visible area: got %d, was %d",
			m.KanbanColScrolls[0], scrollAfterDown)
	}
}

func TestKanbanFullscreenToggle(t *testing.T) {
	m := newKanbanTestModel(TaskListData{
		Reviewable: []models.Issue{{ID: "r1"}},
	})

	// Initially not fullscreen
	if m.KanbanFullscreen {
		t.Error("kanban should not be fullscreen by default")
	}

	// Toggle fullscreen on
	m.KanbanFullscreen = !m.KanbanFullscreen
	if !m.KanbanFullscreen {
		t.Error("kanban should be fullscreen after toggle")
	}

	// Dimensions should use full viewport
	modalWidth, modalHeight, _, _ := m.kanbanDimensions()
	if modalWidth != m.Width-2 {
		t.Errorf("fullscreen modalWidth = %d, want %d", modalWidth, m.Width-2)
	}
	if modalHeight != m.Height {
		t.Errorf("fullscreen modalHeight = %d, want %d", modalHeight, m.Height)
	}

	// Toggle back to overlay
	m.KanbanFullscreen = !m.KanbanFullscreen
	if m.KanbanFullscreen {
		t.Error("kanban should not be fullscreen after second toggle")
	}

	// Dimensions should be smaller in overlay mode
	overlayWidth, overlayHeight, _, _ := m.kanbanDimensions()
	if overlayWidth >= m.Width-2 {
		t.Errorf("overlay width %d should be less than fullscreen width %d", overlayWidth, m.Width-2)
	}
	if overlayHeight >= m.Height {
		t.Errorf("overlay height %d should be less than fullscreen height %d", overlayHeight, m.Height)
	}
}

func TestKanbanFullscreenMaxVisibleCards(t *testing.T) {
	m := newKanbanTestModel(TaskListData{
		Reviewable: makeIssues("r", 20),
	})

	_, _, _, overlayVisible := m.kanbanDimensions()

	m.KanbanFullscreen = true
	_, _, _, fsVisible := m.kanbanDimensions()

	// Fullscreen should show more (or equal) cards than overlay
	if fsVisible < overlayVisible {
		t.Errorf("fullscreen visible cards (%d) should be >= overlay visible cards (%d)",
			fsVisible, overlayVisible)
	}
}

func TestKanbanDimensions(t *testing.T) {
	m := Model{
		Width:  120,
		Height: 40,
	}

	modalW, modalH, colW, maxCards := m.kanbanDimensions()

	if modalW <= 0 {
		t.Errorf("modalWidth should be positive, got %d", modalW)
	}
	if modalH <= 0 {
		t.Errorf("modalHeight should be positive, got %d", modalH)
	}
	if colW < minKanbanColWidth {
		t.Errorf("colWidth = %d, should be >= %d", colW, minKanbanColWidth)
	}
	if maxCards < 1 {
		t.Errorf("maxVisibleCards should be >= 1, got %d", maxCards)
	}
}

func TestKanbanCloseResetsState(t *testing.T) {
	m := newKanbanTestModel(TaskListData{
		Reviewable: makeIssues("r", 5),
	})
	m.KanbanCol = 3
	m.KanbanRow = 2
	m.KanbanFullscreen = true
	m.KanbanColScrolls[0] = 5

	m.closeKanbanView()

	if m.KanbanOpen {
		t.Error("KanbanOpen should be false after close")
	}
	if m.KanbanCol != 0 {
		t.Errorf("KanbanCol should be 0 after close, got %d", m.KanbanCol)
	}
	if m.KanbanRow != 0 {
		t.Errorf("KanbanRow should be 0 after close, got %d", m.KanbanRow)
	}
	if m.KanbanFullscreen {
		t.Error("KanbanFullscreen should be false after close")
	}
	if m.KanbanColScrolls != nil {
		t.Error("KanbanColScrolls should be nil after close")
	}
}

func TestKanbanEnsureCursorVisibleNilScrolls(t *testing.T) {
	// Should not panic when KanbanColScrolls is nil
	m := Model{
		KanbanOpen: true,
		KanbanCol:  0,
		KanbanRow:  0,
		Width:      120,
		Height:     40,
	}
	m.ensureKanbanCursorVisible() // should not panic
}

// makeIssues creates n test issues with the given prefix.
func makeIssues(prefix string, n int) []models.Issue {
	issues := make([]models.Issue, n)
	for i := range issues {
		issues[i] = models.Issue{ID: fmt.Sprintf("%s%d", prefix, i)}
	}
	return issues
}
