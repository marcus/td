package mouse

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRectContains(t *testing.T) {
	r := Rect{X: 10, Y: 10, W: 20, H: 10}

	cases := []struct {
		x, y     int
		expected bool
	}{
		{10, 10, true},  // Top-left corner
		{29, 10, true},  // Top-right edge (exclusive width)
		{10, 19, true},  // Bottom-left edge (exclusive height)
		{29, 19, true},  // Bottom-right corner
		{15, 15, true},  // Center
		{9, 10, false},  // Just left
		{30, 10, false}, // Just right (exclusive)
		{10, 9, false},  // Just above
		{10, 20, false}, // Just below (exclusive)
	}

	for _, tc := range cases {
		got := r.Contains(tc.x, tc.y)
		if got != tc.expected {
			t.Errorf("Rect(%+v).Contains(%d, %d) = %v, want %v", r, tc.x, tc.y, got, tc.expected)
		}
	}
}

func TestHitMapBasic(t *testing.T) {
	hm := NewHitMap()

	hm.AddRect("region1", 0, 0, 50, 50, "data1")
	hm.AddRect("region2", 60, 0, 50, 50, "data2")

	// Test hit on region1
	r := hm.Test(25, 25)
	if r == nil || r.ID != "region1" {
		t.Errorf("expected hit on region1, got %v", r)
	}

	// Test hit on region2
	r = hm.Test(85, 25)
	if r == nil || r.ID != "region2" {
		t.Errorf("expected hit on region2, got %v", r)
	}

	// Test miss
	r = hm.Test(55, 25)
	if r != nil {
		t.Errorf("expected no hit, got %v", r)
	}
}

func TestHitMapPriority(t *testing.T) {
	hm := NewHitMap()

	// Add overlapping regions - later ones have higher priority
	hm.AddRect("background", 0, 0, 100, 100, nil)
	hm.AddRect("panel", 10, 10, 80, 80, nil)
	hm.AddRect("button", 40, 40, 20, 20, nil)

	// Test at button location - should hit button (highest priority)
	r := hm.Test(50, 50)
	if r == nil || r.ID != "button" {
		t.Errorf("expected hit on button, got %v", r)
	}

	// Test at panel location (not button)
	r = hm.Test(15, 15)
	if r == nil || r.ID != "panel" {
		t.Errorf("expected hit on panel, got %v", r)
	}

	// Test at background location (not panel)
	r = hm.Test(5, 5)
	if r == nil || r.ID != "background" {
		t.Errorf("expected hit on background, got %v", r)
	}
}

func TestHitMapClear(t *testing.T) {
	hm := NewHitMap()

	hm.AddRect("region1", 0, 0, 50, 50, nil)
	hm.AddRect("region2", 60, 0, 50, 50, nil)

	if len(hm.Regions()) != 2 {
		t.Errorf("expected 2 regions, got %d", len(hm.Regions()))
	}

	hm.Clear()

	if len(hm.Regions()) != 0 {
		t.Errorf("expected 0 regions after clear, got %d", len(hm.Regions()))
	}
}

func TestHandlerClick(t *testing.T) {
	h := NewHandler()
	h.HitMap.AddRect("button", 10, 10, 30, 10, nil)

	result := h.HandleClick(20, 15)
	if result.Region == nil || result.Region.ID != "button" {
		t.Errorf("expected click on button, got %v", result.Region)
	}
	if result.IsDoubleClick {
		t.Error("first click should not be double-click")
	}

	// Miss click
	result = h.HandleClick(5, 5)
	if result.Region != nil {
		t.Errorf("expected no region on miss, got %v", result.Region)
	}
}

func TestHandlerDoubleClick(t *testing.T) {
	h := NewHandler()
	h.HitMap.AddRect("button", 10, 10, 30, 10, nil)

	// First click
	result := h.HandleClick(20, 15)
	if result.IsDoubleClick {
		t.Error("first click should not be double-click")
	}

	// Second quick click on same region
	result = h.HandleClick(20, 15)
	if !result.IsDoubleClick {
		t.Error("second quick click should be double-click")
	}

	// Third click should not be double-click (reset after double)
	result = h.HandleClick(20, 15)
	if result.IsDoubleClick {
		t.Error("third click should not be double-click")
	}
}

func TestHandlerDrag(t *testing.T) {
	h := NewHandler()

	h.StartDrag(100, 100, "sidebar", 250)

	if !h.IsDragging() {
		t.Error("expected dragging to be true")
	}
	if h.DragRegion() != "sidebar" {
		t.Errorf("expected drag region 'sidebar', got %q", h.DragRegion())
	}
	if h.DragStartValue() != 250 {
		t.Errorf("expected drag start value 250, got %d", h.DragStartValue())
	}

	dx, dy := h.DragDelta(150, 120)
	if dx != 50 || dy != 20 {
		t.Errorf("expected delta (50, 20), got (%d, %d)", dx, dy)
	}

	h.EndDrag()

	if h.IsDragging() {
		t.Error("expected dragging to be false after EndDrag")
	}
}

func TestHandleMouseActions(t *testing.T) {
	h := NewHandler()
	h.HitMap.AddRect("button", 10, 10, 30, 10, nil)

	// Test click
	action := h.HandleMouse(tea.MouseClickMsg{X: 20, Y: 15, Button: tea.MouseLeft})
	if action.Type != ActionClick {
		t.Errorf("expected ActionClick, got %v", action.Type)
	}
	if action.Region == nil || action.Region.ID != "button" {
		t.Errorf("expected region 'button', got %v", action.Region)
	}

	// Test hover
	action = h.HandleMouse(tea.MouseMotionMsg{X: 25, Y: 15})
	if action.Type != ActionHover {
		t.Errorf("expected ActionHover, got %v", action.Type)
	}

	// Test scroll down
	action = h.HandleMouse(tea.MouseWheelMsg{X: 20, Y: 15, Button: tea.MouseWheelDown})
	if action.Type != ActionScrollDown {
		t.Errorf("expected ActionScrollDown, got %v", action.Type)
	}

	// Test scroll up
	action = h.HandleMouse(tea.MouseWheelMsg{X: 20, Y: 15, Button: tea.MouseWheelUp})
	if action.Type != ActionScrollUp {
		t.Errorf("expected ActionScrollUp, got %v", action.Type)
	}
}

func TestHandleMouseShiftScroll(t *testing.T) {
	h := NewHandler()

	// Shift+scroll up = scroll left
	action := h.HandleMouse(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp, Mod: tea.ModShift})
	if action.Type != ActionScrollLeft {
		t.Errorf("expected ActionScrollLeft, got %v", action.Type)
	}

	// Shift+scroll down = scroll right
	action = h.HandleMouse(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown, Mod: tea.ModShift})
	if action.Type != ActionScrollRight {
		t.Errorf("expected ActionScrollRight, got %v", action.Type)
	}
}

func TestHandleMouseDragMotion(t *testing.T) {
	h := NewHandler()

	// Start a drag
	h.StartDrag(100, 100, "divider", 50)

	// Motion while dragging
	action := h.HandleMouse(tea.MouseMotionMsg{X: 150, Y: 110})
	if action.Type != ActionDrag {
		t.Errorf("expected ActionDrag, got %v", action.Type)
	}
	if action.DragDX != 50 || action.DragDY != 10 {
		t.Errorf("expected drag delta (50, 10), got (%d, %d)", action.DragDX, action.DragDY)
	}

	// Release
	action = h.HandleMouse(tea.MouseReleaseMsg{X: 150, Y: 110})
	if action.Type != ActionDragEnd {
		t.Errorf("expected ActionDragEnd, got %v", action.Type)
	}

	if h.IsDragging() {
		t.Error("expected dragging to be false after release")
	}
}

func TestHandlerClear(t *testing.T) {
	h := NewHandler()
	h.HitMap.AddRect("button", 10, 10, 30, 10, nil)

	h.Clear()

	if len(h.HitMap.Regions()) != 0 {
		t.Errorf("expected 0 regions after Clear, got %d", len(h.HitMap.Regions()))
	}
}
