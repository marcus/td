package monitor

import (
	"strings"
	"testing"
)

func TestMaxLineWidth(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected int
	}{
		{
			name:     "empty slice",
			lines:    []string{},
			expected: 0,
		},
		{
			name:     "single line",
			lines:    []string{"hello"},
			expected: 5,
		},
		{
			name:     "multiple lines",
			lines:    []string{"hi", "hello", "hey"},
			expected: 5,
		},
		{
			name:     "with ANSI codes",
			lines:    []string{"\x1b[31mred\x1b[0m", "plain"},
			expected: 5, // "plain" is 5, "red" is 3 (ANSI codes don't count)
		},
		{
			name:     "empty lines",
			lines:    []string{"", "", ""},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxLineWidth(tt.lines)
			if got != tt.expected {
				t.Errorf("maxLineWidth(%v) = %d, want %d", tt.lines, got, tt.expected)
			}
		})
	}
}

func TestDimLine(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedText string // Text content after ANSI stripping
	}{
		{
			name:         "plain text",
			input:        "hello world",
			expectedText: "hello world",
		},
		{
			name:         "with ANSI codes",
			input:        "\x1b[31mred text\x1b[0m",
			expectedText: "red text",
		},
		{
			name:         "empty string",
			input:        "",
			expectedText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dimLine(tt.input)
			// Result should contain the stripped text content
			if !strings.Contains(got, tt.expectedText) {
				t.Errorf("dimLine(%q) = %q, should contain %q", tt.input, got, tt.expectedText)
			}
			// Result should not contain original red color code (it should be stripped)
			if strings.Contains(tt.input, "\x1b[31m") && strings.Contains(got, "\x1b[31m") {
				t.Errorf("dimLine(%q) should strip original ANSI codes", tt.input)
			}
		})
	}
}

func TestCompositeRow(t *testing.T) {
	tests := []struct {
		name        string
		bgLine      string
		modalLine   string
		modalStartX int
		modalWidth  int
		totalWidth  int
	}{
		{
			name:        "modal in center",
			bgLine:      "AAAAAAAAAA",
			modalLine:   "MMM",
			modalStartX: 3,
			modalWidth:  3,
			totalWidth:  10,
		},
		{
			name:        "modal at start",
			bgLine:      "AAAAAAAAAA",
			modalLine:   "MMM",
			modalStartX: 0,
			modalWidth:  3,
			totalWidth:  10,
		},
		{
			name:        "modal at end",
			bgLine:      "AAAAAAAAAA",
			modalLine:   "MMM",
			modalStartX: 7,
			modalWidth:  3,
			totalWidth:  10,
		},
		{
			name:        "empty background",
			bgLine:      "",
			modalLine:   "MMM",
			modalStartX: 3,
			modalWidth:  3,
			totalWidth:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compositeRow(tt.bgLine, tt.modalLine, tt.modalStartX, tt.modalWidth, tt.totalWidth)
			// Modal content should be present in output
			if !strings.Contains(got, tt.modalLine) {
				t.Errorf("compositeRow() output should contain modal line %q, got %q", tt.modalLine, got)
			}
		})
	}
}

func TestOverlayModal(t *testing.T) {
	tests := []struct {
		name       string
		background string
		modal      string
		width      int
		height     int
	}{
		{
			name:       "simple overlay",
			background: "AAA\nBBB\nCCC",
			modal:      "X",
			width:      3,
			height:     3,
		},
		{
			name:       "larger background",
			background: "AAAAA\nBBBBB\nCCCCC\nDDDDD\nEEEEE",
			modal:      "MM\nMM",
			width:      5,
			height:     5,
		},
		{
			name:       "modal larger than background",
			background: "A",
			modal:      "MMMMM\nMMMMM\nMMMMM",
			width:      5,
			height:     3,
		},
		{
			name:       "empty background",
			background: "",
			modal:      "M",
			width:      3,
			height:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OverlayModal(tt.background, tt.modal, tt.width, tt.height)

			// Result should have correct number of lines
			lines := strings.Split(got, "\n")
			if len(lines) != tt.height {
				t.Errorf("OverlayModal() returned %d lines, want %d", len(lines), tt.height)
			}

			// Modal content should be present
			modalLines := strings.Split(tt.modal, "\n")
			for _, mLine := range modalLines {
				if !strings.Contains(got, mLine) {
					t.Errorf("OverlayModal() output should contain modal line %q", mLine)
				}
			}
		})
	}
}

func TestOverlayModalCentering(t *testing.T) {
	// Test that a small modal is centered in a larger area
	background := strings.Repeat("AAAAAAAAAA\n", 10)
	modal := "MMM"
	width := 10
	height := 10

	got := OverlayModal(background, modal, width, height)
	lines := strings.Split(got, "\n")

	// Modal should be vertically centered (line 4 or 5 for height 10, modal height 1)
	// startY = (10 - 1) / 2 = 4
	found := false
	for i, line := range lines {
		if strings.Contains(line, "MMM") {
			if i != 4 {
				t.Errorf("Modal should be on line 4 (centered), found on line %d", i)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("Modal content not found in output")
	}
}

func TestOverlayModalWithANSI(t *testing.T) {
	// Background with ANSI codes should be stripped and dimmed
	background := "\x1b[31mRED\x1b[0m\n\x1b[32mGREEN\x1b[0m\n\x1b[34mBLUE\x1b[0m"
	modal := "M"
	width := 5
	height := 3

	got := OverlayModal(background, modal, width, height)

	// Original color codes should not be in output (except for dim style)
	if strings.Contains(got, "\x1b[31m") {
		t.Error("OverlayModal() should strip original red color code")
	}
	if strings.Contains(got, "\x1b[32m") {
		t.Error("OverlayModal() should strip original green color code")
	}
	if strings.Contains(got, "\x1b[34m") {
		t.Error("OverlayModal() should strip original blue color code")
	}

	// Modal content should be present
	if !strings.Contains(got, "M") {
		t.Error("Modal content should be present in output")
	}
}
