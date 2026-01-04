// Package monitor provides the TUI monitor for td.
package monitor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// DimStyle applies a dim gray color to background content behind modals.
// We strip existing ANSI codes and apply gray because SGR 2 (faint) doesn't
// reliably combine with existing color codes in most terminals.
var DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

// maxLineWidth returns the maximum visual width of the given lines.
func maxLineWidth(lines []string) int {
	maxWidth := 0
	for _, line := range lines {
		w := ansi.StringWidth(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

// dimLine strips ANSI codes and applies dim gray styling.
func dimLine(s string) string {
	return DimStyle.Render(ansi.Strip(s))
}

// compositeRow overlays modalLine onto bgLine at position modalStartX.
// Returns: dimmed-left-segment + modalLine + dimmed-right-segment
func compositeRow(bgLine, modalLine string, modalStartX, modalWidth, totalWidth int) string {
	var result strings.Builder

	// Strip ANSI from background for consistent dimming
	stripped := ansi.Strip(bgLine)
	bgWidth := ansi.StringWidth(stripped)

	// Left segment: dimmed background from 0 to modalStartX
	if modalStartX > 0 {
		// Use ansi.Truncate to get visual-width-based substring
		leftSeg := ansi.Truncate(stripped, modalStartX, "")
		leftWidth := ansi.StringWidth(leftSeg)
		result.WriteString(DimStyle.Render(leftSeg))
		// Pad if background is shorter than modal position
		if leftWidth < modalStartX {
			result.WriteString(strings.Repeat(" ", modalStartX-leftWidth))
		}
	}

	// Modal content (not dimmed)
	result.WriteString(modalLine)

	// Right segment: dimmed background after modal
	rightStartX := modalStartX + modalWidth
	if rightStartX < totalWidth && bgWidth > rightStartX {
		// Use ansi.Cut to get visual-width-based substring from position
		rightSeg := ansi.Cut(stripped, rightStartX, bgWidth)
		result.WriteString(DimStyle.Render(rightSeg))
	}

	return result.String()
}

// OverlayModal composites a modal on top of a dimmed background.
// The modal is centered, with dimmed background visible on all sides.
func OverlayModal(background, modal string, width, height int) string {
	bgLines := strings.Split(background, "\n")
	modalLines := strings.Split(modal, "\n")

	// Calculate modal dimensions and position
	modalWidth := maxLineWidth(modalLines)
	modalHeight := len(modalLines)
	startX := (width - modalWidth) / 2
	startY := (height - modalHeight) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	// Ensure we have enough background lines
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	// Build result with compositing
	result := make([]string, 0, height)
	for y := 0; y < height; y++ {
		bgLine := ""
		if y < len(bgLines) {
			bgLine = bgLines[y]
		}

		modalRowIdx := y - startY
		if modalRowIdx >= 0 && modalRowIdx < modalHeight {
			// Composite: dimmed-left + modal + dimmed-right
			result = append(result, compositeRow(bgLine, modalLines[modalRowIdx], startX, modalWidth, width))
		} else {
			// Pure dimmed background (above or below modal)
			result = append(result, dimLine(bgLine))
		}
	}

	return strings.Join(result, "\n")
}
