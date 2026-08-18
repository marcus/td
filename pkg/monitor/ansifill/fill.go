// Package ansifill keeps a painted background solid behind nested styles.
//
// Lip Gloss applies a block background by emitting the background sequence at
// the start of a line and again around padding it adds itself. Any nested
// style inside the content ends with a reset, which also clears the block
// background, so every run of text after the first styled span renders on the
// terminal's default background instead of the modal surface. Re-applying the
// background after each SGR sequence closes those holes.
package ansifill

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Code returns the SGR sequence that sets the background to the given color
// value (any form Lip Gloss accepts). An empty value yields an empty code,
// which makes Lines a no-op.
func Code(value string) string {
	if value == "" {
		return ""
	}
	return ansi.NewStyle().BackgroundColor(lipgloss.Color(value)).String()
}

// Lines re-applies bgCode after every SGR sequence on every line so nested
// styles cannot punch holes in the surface behind text. When width is
// positive each line is also padded to that visual width with the background
// applied, which is what a host-owned chrome renderer needs: it pads short
// lines with unstyled spaces of its own otherwise.
func Lines(content, bgCode string, width int) string {
	if bgCode == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		filled := bgCode + sgrPattern.ReplaceAllStringFunc(line, func(sequence string) string {
			if !resetsBackground(sequence) {
				// The nested style paints its own background (a selected row,
				// a badge). Leave it alone; only restore the surface where a
				// sequence would drop back to the terminal default.
				return sequence
			}
			return sequence + bgCode
		})
		if width > lineWidth {
			filled += strings.Repeat(" ", width-lineWidth)
		}
		lines[i] = filled + "\x1b[0m"
	}
	return strings.Join(lines, "\n")
}

// resetsBackground reports whether an SGR sequence leaves the background at
// the terminal default: a full reset, an explicit 49, or any sequence that
// simply does not set one. Extended color parameters are consumed so a
// foreground component such as 38;2;44;0;5 is not mistaken for a background.
func resetsBackground(sequence string) bool {
	params := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
	if params == "" {
		return true
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "38", "48":
			background := parts[i] == "48"
			// Extended color: 5;n (256) or 2;r;g;b (truecolor).
			if i+1 < len(parts) && parts[i+1] == "5" {
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4
			} else {
				i++
			}
			if background {
				return false
			}
		case "40", "41", "42", "43", "44", "45", "46", "47",
			"100", "101", "102", "103", "104", "105", "106", "107":
			return false
		}
	}
	return true
}
