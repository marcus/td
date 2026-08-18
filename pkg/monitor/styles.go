package monitor

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// Activity table column widths (fixed columns; message takes remaining space)
const (
	activityColTimeWidth    = 5  // "15:04" format
	activityColSessionWidth = 10 // truncated session ID
	activityColTypeWidth    = 5  // "[LOG]", "[ACT]", "[CMT]"
	activityColIssueWidth   = 11 // Issue ID like "td-abc123" + common suffix
	statsBarFilled          = "█"
	statsBarEmpty           = "░"
)

// Type icons are data, not presentation state. Their colors live in the
// model-owned monitorStyles value built from Theme.
var typeIcons = map[models.Type]string{
	models.TypeEpic:    "◆", // Diamond - container
	models.TypeFeature: "●", // Filled circle - new thing
	models.TypeBug:     "✗", // X mark - defect
	models.TypeTask:    "■", // Square - building block
	models.TypeChore:   "○", // Empty circle - routine
}

// ansiPattern matches ANSI escape sequences
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func highlightRowWithBackground(line string, width int, bgCode string) string {
	return highlightRowWithSelection(line, width, bgCode, bgCode)
}

func highlightRowWithSelection(line string, width int, selectionCode, bgCode string) string {
	reset := "\x1b[0m"

	// First, truncate if line is too wide (ANSI-aware truncation)
	lineWidth := lipgloss.Width(line)
	if lineWidth > width {
		// Truncate with ellipsis, leaving room for "..."
		line = ansi.Truncate(line, width-3, "...")
		lineWidth = lipgloss.Width(line)
	}

	// Preserve explicit nested foregrounds, but restore the selection foreground
	// whenever nested content resets to the terminal default. Background is
	// restored after every SGR so nested styles cannot punch holes in the row.
	line = ansiPattern.ReplaceAllStringFunc(line, func(sequence string) string {
		if sgrResetsForeground(sequence) {
			return sequence + selectionCode
		}
		return sequence + bgCode
	})

	// Plain text starts with the readable selection foreground/background pair.
	line = selectionCode + line

	// Pad to width if needed
	if lineWidth < width {
		line = line + strings.Repeat(" ", width-lineWidth)
	}

	return line + reset
}

func sgrResetsForeground(sequence string) bool {
	params := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
	if params == "" {
		return true
	}
	foregroundReset := false
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		param := parts[i]
		switch param {
		case "0", "39":
			foregroundReset = true
		case "38":
			foregroundReset = false
			// Skip indexed-color or RGB payloads so a zero color component is
			// not mistaken for a later SGR reset.
			if i+1 < len(parts) && parts[i+1] == "5" {
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4
			}
		case "48":
			if i+1 < len(parts) && parts[i+1] == "5" {
				i += 2
			} else if i+1 < len(parts) && parts[i+1] == "2" {
				i += 4
			}
		default:
			if len(param) == 2 && ((param[0] == '3' && param[1] >= '0' && param[1] <= '7') ||
				(param[0] == '9' && param[1] >= '0' && param[1] <= '7')) {
				foregroundReset = false
			}
		}
	}
	return foregroundReset
}
