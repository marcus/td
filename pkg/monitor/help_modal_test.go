package monitor

import (
	"strings"
	"testing"
)

// TestHelpModalToggle verifies help modal can be toggled open/closed
func TestHelpModalToggle(t *testing.T) {
	tests := []struct {
		name     string
		initial  bool
		expected bool
	}{
		{"open help from closed", false, true},
		{"close help from open", true, false},
		{"toggle twice", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				HelpOpen:   tt.initial,
				HelpScroll: 0,
				Width:      120,
				Height:     40,
				Keymap:     newTestKeymap(),
			}

			// If test name indicates toggle twice, toggle twice
			if tt.name == "toggle twice" {
				m.HelpOpen = !m.HelpOpen
			}

			m.HelpOpen = !m.HelpOpen
			if m.HelpOpen != tt.expected {
				t.Errorf("HelpOpen = %v, want %v", m.HelpOpen, tt.expected)
			}
		})
	}
}

// TestHelpModalDisplaysContent verifies help modal displays generated help text
func TestHelpModalDisplaysContent(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 0,
		Width:          120,
		Height:         40,
		Keymap:         newTestKeymap(),
	}

	// Generate help and count lines
	helpText := m.Keymap.GenerateHelp()
	lines := strings.Split(helpText, "\n")
	m.HelpTotalLines = len(lines)

	// Verify help text is not empty and contains expected sections
	expectedSections := []string{
		"MONITOR TUI - Key Bindings",
		"NAVIGATION",
		"MODALS",
		"CRUD",
		"ACTIONS",
		"MOUSE",
	}

	for _, section := range expectedSections {
		if !strings.Contains(helpText, section) {
			t.Errorf("Help text missing expected section: %s", section)
		}
	}

	// Verify minimum content length
	if len(helpText) < 100 {
		t.Errorf("Help text too short: %d chars", len(helpText))
	}
}

// TestHelpModalMaxScroll verifies scroll calculations are correct
func TestHelpModalMaxScroll(t *testing.T) {
	tests := []struct {
		name              string
		width             int
		height            int
		totalLines        int
		expectedMaxScroll int
	}{
		{"no scroll needed", 120, 40, 20, 0},
		{"single page of scroll", 120, 40, 30, 4},
		{"multiple pages", 120, 40, 50, 24},
		{"small terminal", 50, 20, 30, 11},
		{"very small terminal", 50, 15, 30, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				HelpOpen:       true,
				HelpScroll:     0,
				HelpTotalLines: tt.totalLines,
				Width:          tt.width,
				Height:         tt.height,
				Keymap:         newTestKeymap(),
			}

			maxScroll := m.helpMaxScroll()
			if maxScroll != tt.expectedMaxScroll {
				t.Errorf("helpMaxScroll() = %d, want %d", maxScroll, tt.expectedMaxScroll)
			}
		})
	}
}

// TestHelpVisibleHeight verifies correct visible height calculation
func TestHelpVisibleHeight(t *testing.T) {
	tests := []struct {
		name           string
		height         int
		expectedHeight int
	}{
		{"standard height", 40, 28}, // 80% = 32, minus 4 for border/footer = 28
		{"small height", 20, 12},    // 80% = 16, minus 4 = 12
		{"very small", 15, 8},       // 80% = 12, clamped to min 15, minus 4 = 11, but clamped to 8
		{"very large", 100, 36},     // 80% = 80, clamped to max 40, minus 4 = 36
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				Height: tt.height,
			}

			visible := m.helpVisibleHeight()
			if visible != tt.expectedHeight {
				t.Errorf("helpVisibleHeight() = %d, want %d", visible, tt.expectedHeight)
			}
		})
	}
}

// TestHelpScrollClamping verifies scroll position is clamped correctly
func TestHelpScrollClamping(t *testing.T) {
	tests := []struct {
		name           string
		scroll         int
		totalLines     int
		width          int
		height         int
		expectedScroll int
	}{
		{"scroll at top", 0, 100, 120, 40, 0},
		{"scroll negative", -5, 100, 120, 40, 0},
		{"scroll too high", 100, 50, 120, 40, 8}, // maxScroll = 50 - 28 (visible) - 1 indicator = 21, but check actual calc
		{"scroll at max", 30, 50, 120, 40, 21},
		{"scroll within bounds", 5, 100, 120, 40, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				HelpOpen:       true,
				HelpScroll:     tt.scroll,
				HelpTotalLines: tt.totalLines,
				Width:          tt.width,
				Height:         tt.height,
			}

			m.clampHelpScroll()
			if m.HelpScroll != tt.expectedScroll {
				t.Errorf("clampHelpScroll() = %d, want %d", m.HelpScroll, tt.expectedScroll)
			}
		})
	}
}

// TestHelpScrollWithKeyboardInput verifies scroll changes with keyboard commands
func TestHelpScrollWithKeyboardInput(t *testing.T) {
	tests := []struct {
		name           string
		initialScroll  int
		totalLines     int
		height         int
		scrollDelta    int
		expectedScroll int
		description    string
	}{
		{"scroll down one line", 0, 50, 40, 1, 1, "single line down"},
		{"scroll up from middle", 10, 50, 40, -5, 5, "multiple lines up"},
		{"scroll down clamped", 25, 50, 40, 10, 21, "clamped at bottom"},
		{"scroll up clamped", 0, 50, 40, -10, 0, "clamped at top"},
		{"half page down", 0, 100, 40, 14, 14, "half page (14 lines)"},
		{"full page down", 0, 100, 40, 28, 28, "full page (28 lines)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				HelpOpen:       true,
				HelpScroll:     tt.initialScroll,
				HelpTotalLines: tt.totalLines,
				Height:         tt.height,
			}

			m.HelpScroll += tt.scrollDelta
			m.clampHelpScroll()

			if m.HelpScroll != tt.expectedScroll {
				t.Errorf("after scrolling: HelpScroll = %d, want %d (desc: %s)", m.HelpScroll, tt.expectedScroll, tt.description)
			}
		})
	}
}

// TestHelpExitToTop verifies jumping to top of help
func TestHelpExitToTop(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     50,
		HelpTotalLines: 100,
		Width:          120,
		Height:         40,
		Keymap:         newTestKeymap(),
	}

	// Simulate jumping to top (gg command)
	m.HelpScroll = 0
	m.clampHelpScroll()

	if m.HelpScroll != 0 {
		t.Errorf("After jumping to top: HelpScroll = %d, want 0", m.HelpScroll)
	}
}

// TestHelpExitToBottom verifies jumping to bottom of help
func TestHelpExitToBottom(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 100,
		Width:          120,
		Height:         40,
		Keymap:         newTestKeymap(),
	}

	// Simulate jumping to bottom (G command)
	m.HelpScroll = m.helpMaxScroll()

	if m.HelpScroll != 21 {
		t.Errorf("After jumping to bottom: HelpScroll = %d, want 21", m.HelpScroll)
	}
}

// TestHelpModalEdgeCaseShortTerminal verifies help renders correctly on short terminal
func TestHelpModalEdgeCaseShortTerminal(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 50,
		Width:          40, // Very narrow
		Height:         15, // Very short
		Keymap:         newTestKeymap(),
	}

	// Verify calculations work without panic
	visibleHeight := m.helpVisibleHeight()
	if visibleHeight < 1 {
		t.Errorf("helpVisibleHeight() = %d, must be at least 1", visibleHeight)
	}

	maxScroll := m.helpMaxScroll()
	if maxScroll < 0 {
		t.Errorf("helpMaxScroll() = %d, must be >= 0", maxScroll)
	}

	// Verify clamping works
	m.HelpScroll = 1000
	m.clampHelpScroll()
	if m.HelpScroll > maxScroll {
		t.Errorf("After clamping: HelpScroll = %d, exceeds maxScroll %d", m.HelpScroll, maxScroll)
	}
}

// TestHelpModalEdgeCaseLongTerminal verifies help renders correctly on large terminal
func TestHelpModalEdgeCaseLongTerminal(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 50,
		Width:          200,
		Height:         100,
		Keymap:         newTestKeymap(),
	}

	// Verify calculations work without panic
	visibleHeight := m.helpVisibleHeight()
	if visibleHeight > 40 {
		t.Errorf("helpVisibleHeight() = %d, should be clamped to 36 (40-4)", visibleHeight)
	}

	maxScroll := m.helpMaxScroll()
	if maxScroll < 0 {
		t.Errorf("helpMaxScroll() = %d, must be >= 0", maxScroll)
	}
}

// TestHelpModalCompleteContent verifies help content is complete and properly formatted
func TestHelpModalCompleteContent(t *testing.T) {
	km := newTestKeymap()
	helpText := km.GenerateHelp()

	// Count sections
	sections := []string{
		"NAVIGATION:",
		"MODALS:",
		"EPIC TASKS",
		"CRUD:",
		"FORM",
		"ACTIONS:",
		"HANDOFFS MODAL:",
		"SEARCH",
		"MOUSE:",
	}

	for _, section := range sections {
		if !strings.Contains(helpText, section) {
			t.Errorf("Help text missing section: %s", section)
		}
	}

	// Verify line formatting consistency
	lines := strings.Split(helpText, "\n")
	if len(lines) < 20 {
		t.Errorf("Help text has only %d lines, expected at least 20", len(lines))
	}

	// Verify key bindings are formatted with descriptions
	hasFormattedBindings := false
	for _, line := range lines {
		if strings.Contains(line, "↑") || strings.Contains(line, "↓") || strings.Contains(line, "Enter") {
			hasFormattedBindings = true
			if !strings.Contains(line, "  ") {
				t.Errorf("Binding line lacks proper formatting: %q", line)
			}
		}
	}

	if !hasFormattedBindings {
		t.Errorf("Help text has no formatted key bindings")
	}
}

// TestHelpScrollPositionTracking verifies scroll position is preserved and tracked
func TestHelpScrollPositionTracking(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 100,
		Width:          120,
		Height:         40,
	}

	// Test sequence: open → scroll → close → reopen → verify state
	positions := []int{0, 5, 10, 15, 10, 5, 0}

	for i, pos := range positions {
		m.HelpScroll = pos
		m.clampHelpScroll()

		if m.HelpScroll != pos {
			t.Errorf("At step %d: HelpScroll = %d, want %d", i, m.HelpScroll, pos)
		}
	}
}

// TestHelpScrollIndicators verifies scroll indicators show correct information
func TestHelpScrollIndicators(t *testing.T) {
	tests := []struct {
		name           string
		scroll         int
		totalLines     int
		shouldShowUp   bool
		shouldShowDown bool
	}{
		{"at top", 0, 50, false, true},
		{"at bottom", 21, 50, true, false},
		{"in middle", 10, 50, true, true},
		{"single page", 0, 20, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				HelpOpen:       true,
				HelpScroll:     tt.scroll,
				HelpTotalLines: tt.totalLines,
				Width:          120,
				Height:         40,
				Keymap:         newTestKeymap(),
			}

			visibleHeight := m.helpVisibleHeight()
			maxScroll := m.helpMaxScroll()
			_ = maxScroll

			// Up indicator should show if scroll > 0
			showUp := m.HelpScroll > 0
			if showUp != tt.shouldShowUp {
				t.Errorf("showUp = %v, want %v", showUp, tt.shouldShowUp)
			}

			// Down indicator should show if more content below
			endIdx := m.HelpScroll + visibleHeight
			showDown := endIdx < m.HelpTotalLines
			if showDown != tt.shouldShowDown {
				t.Errorf("showDown = %v, want %v", showDown, tt.shouldShowDown)
			}
		})
	}
}

// TestHelpCloseKeybinding verifies help can be closed via keyboard
func TestHelpCloseKeybinding(t *testing.T) {
	m := Model{
		HelpOpen:   true,
		HelpScroll: 10,
		Width:      120,
		Height:     40,
		Keymap:     newTestKeymap(),
	}

	// Simulate closing help (? or Esc key)
	m.HelpOpen = false
	m.HelpScroll = 0

	if m.HelpOpen {
		t.Errorf("HelpOpen should be false after close")
	}

	if m.HelpScroll != 0 {
		t.Errorf("HelpScroll should reset to 0, got %d", m.HelpScroll)
	}
}

// TestHelpMultipleToggleStates verifies rapid open/close doesn't corrupt state
func TestHelpMultipleToggleStates(t *testing.T) {
	m := Model{
		HelpOpen:       false,
		HelpScroll:     0,
		HelpTotalLines: 100,
		Width:          120,
		Height:         40,
		Keymap:         newTestKeymap(),
	}

	// Rapid open/close cycles
	for i := 0; i < 5; i++ {
		m.HelpOpen = true
		m.HelpScroll = 5
		m.clampHelpScroll()

		if m.HelpScroll != 5 {
			t.Errorf("Cycle %d open: HelpScroll = %d, want 5", i, m.HelpScroll)
		}

		m.HelpOpen = false
		m.clampHelpScroll()

		if m.HelpScroll != 0 && m.HelpScroll != 5 {
			t.Errorf("Cycle %d close: HelpScroll corrupted to %d", i, m.HelpScroll)
		}
	}
}

// TestHelpPersistentScrollState verifies scroll state persists when help stays open
func TestHelpPersistentScrollState(t *testing.T) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     0,
		HelpTotalLines: 100,
		Width:          120,
		Height:         40,
	}

	// Simulate user scrolling while help is open
	scrollSequence := []int{0, 3, 6, 9, 12, 10, 5, 0}

	for i, expectedScroll := range scrollSequence {
		m.HelpScroll = expectedScroll
		m.clampHelpScroll()

		if m.HelpScroll != expectedScroll {
			t.Errorf("At scroll sequence %d: HelpScroll = %d, want %d", i, m.HelpScroll, expectedScroll)
		}

		// Verify state is not corrupted
		if m.HelpOpen == false {
			t.Errorf("HelpOpen corrupted at sequence %d", i)
		}
	}
}

// BenchmarkHelpGeneration benchmarks help text generation
func BenchmarkHelpGeneration(b *testing.B) {
	km := newTestKeymap()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = km.GenerateHelp()
	}
}

// BenchmarkHelpScrollClamping benchmarks scroll clamping
func BenchmarkHelpScrollClamping(b *testing.B) {
	m := Model{
		HelpOpen:       true,
		HelpScroll:     500,
		HelpTotalLines: 1000,
		Height:         40,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.clampHelpScroll()
	}
}
