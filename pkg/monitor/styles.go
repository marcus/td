package monitor

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

var (
	// Base colors
	primaryColor   = lipgloss.Color("212")
	secondaryColor = lipgloss.Color("141")
	mutedColor     = lipgloss.Color("241")
	successColor   = lipgloss.Color("42")
	warningColor   = lipgloss.Color("214")
	errorColor     = lipgloss.Color("196")

	// Panel styles
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	activePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(0, 1)

	// Hover style for inactive panels (subtle highlight when mouse over)
	hoverPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("245")).
			Padding(0, 1)

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1)

	// Text styles
	titleStyle     = lipgloss.NewStyle().Bold(true)
	subtleStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	helpStyle      = lipgloss.NewStyle().Foreground(mutedColor)
	timestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// Status styles
	statusStyles = map[models.Status]lipgloss.Style{
		models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		models.StatusInProgress: lipgloss.NewStyle().Foreground(warningColor),
		models.StatusBlocked:    lipgloss.NewStyle().Foreground(errorColor),
		models.StatusInReview:   lipgloss.NewStyle().Foreground(secondaryColor),
		models.StatusClosed:     lipgloss.NewStyle().Foreground(mutedColor),
	}

	// Status chart styles (slightly different colors for stats charts)
	statusChartStyles = map[models.Status]lipgloss.Style{
		models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		models.StatusInProgress: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		models.StatusBlocked:    lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		models.StatusInReview:   lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		models.StatusClosed:     lipgloss.NewStyle().Foreground(successColor),
	}

	// Priority styles
	priorityStyles = map[models.Priority]lipgloss.Style{
		models.PriorityP0: lipgloss.NewStyle().Foreground(errorColor).Bold(true),
		models.PriorityP1: lipgloss.NewStyle().Foreground(warningColor),
		models.PriorityP2: lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		models.PriorityP3: lipgloss.NewStyle().Foreground(mutedColor),
		models.PriorityP4: lipgloss.NewStyle().Foreground(mutedColor),
	}

	// Activity type badges
	logBadge     = lipgloss.NewStyle().Foreground(successColor)
	actionBadge  = lipgloss.NewStyle().Foreground(secondaryColor)
	commentBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))

	// Section headers
	sectionHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			MarginTop(1)

	// Selected row style - inverted colors for visibility
	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255"))

	// Highlight row style - background only, preserves text colors
	highlightRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237"))

	// Stats modal styles
	statsBarFilled  = "█"
	statsBarEmpty   = "░"
	statsTableLabel = lipgloss.NewStyle().Foreground(mutedColor)
	statsTableValue = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	statsSection    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).MarginTop(1)

	// Epic task styles
	epicTasksFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("45")). // Cyan when focused
				MarginTop(1)

	epicTaskSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255"))

	// Parent epic styles (shown at top of story/task modals)
	parentEpicStyle = lipgloss.NewStyle().
			Foreground(primaryColor) // Purple/magenta

	parentEpicFocusedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(primaryColor).
				Bold(true)

	// Blocked-by/blocks section styles
	blockedBySectionFocusedStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("196")). // Red when focused
					MarginTop(1)

	blockedBySelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255"))

	blocksSectionFocusedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("45")). // Cyan when focused
				MarginTop(1)

	blocksSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255"))

	// Breadcrumb style for stacked modals
	breadcrumbStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)

	// Toast styles for status messages
	toastStyle = lipgloss.NewStyle().
			Background(successColor).
			Foreground(lipgloss.Color("0")).
			Bold(true)

	toastErrorStyle = lipgloss.NewStyle().
			Background(errorColor).
			Foreground(lipgloss.Color("255")).
			Bold(true)

	// Type icon styles
	typeIconStyles = map[models.Type]lipgloss.Style{
		models.TypeEpic:    lipgloss.NewStyle().Foreground(lipgloss.Color("212")), // Purple/magenta
		models.TypeFeature: lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // Green
		models.TypeBug:     lipgloss.NewStyle().Foreground(lipgloss.Color("196")), // Red
		models.TypeTask:    lipgloss.NewStyle().Foreground(lipgloss.Color("45")),  // Cyan
		models.TypeChore:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")), // Gray
	}

	// Type icon symbols
	typeIcons = map[models.Type]string{
		models.TypeEpic:    "◆", // Diamond - container
		models.TypeFeature: "●", // Filled circle - new thing
		models.TypeBug:     "✗", // X mark - defect
		models.TypeTask:    "■", // Square - building block
		models.TypeChore:   "○", // Empty circle - routine
	}

	// Divider styles for drag-to-resize
	// Panel style when its bottom border is being hovered (divider hover)
	dividerHoverPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("45")). // Cyan
				Padding(0, 1)

	// Panel style when its bottom border is being dragged
	dividerActivePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("214")). // Orange/Yellow
				Padding(0, 1)

	// Button styles for interactive modal buttons
	buttonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("238")).
			Padding(0, 2)

	buttonFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(primaryColor). // Purple/Magenta
				Bold(true).
				Padding(0, 2)

	buttonHoverStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("245")).
				Padding(0, 2)

	// Danger button styles (for destructive actions like delete)
	buttonDangerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("238")).
				Padding(0, 2)

	buttonDangerFocusedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("255")).
					Background(errorColor). // Red
					Bold(true).
					Padding(0, 2)

	buttonDangerHoverStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("203")). // Lighter red
				Padding(0, 2)
)

// formatStatus renders a status with color
func formatStatus(s models.Status) string {
	style, ok := statusStyles[s]
	if !ok {
		return string(s)
	}
	return style.Render(string(s))
}

// formatPriority renders a priority with color
func formatPriority(p models.Priority) string {
	style, ok := priorityStyles[p]
	if !ok {
		return string(p)
	}
	return style.Render(string(p))
}

// formatTypeIcon renders a type icon with color
func formatTypeIcon(t models.Type) string {
	icon, ok := typeIcons[t]
	if !ok {
		icon = "?"
	}
	style, ok := typeIconStyles[t]
	if !ok {
		return icon
	}
	return style.Render(icon)
}

// formatActivityBadge renders an activity type badge
func formatActivityBadge(actType string) string {
	switch actType {
	case "log":
		return logBadge.Render("[LOG]")
	case "action":
		return actionBadge.Render("[ACT]")
	case "comment":
		return commentBadge.Render("[CMT]")
	default:
		return subtleStyle.Render("[???]")
	}
}

// ansiPattern matches ANSI escape sequences
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// renderButton renders a button with appropriate style based on focus and hover state.
// Parameters:
//   - label: the button text
//   - isFocused: true if this button has keyboard focus
//   - isHovered: true if mouse is hovering over this button
//   - isDanger: true for destructive actions (uses red when focused)
func renderButton(label string, isFocused, isHovered, isDanger bool) string {
	var style lipgloss.Style
	if isDanger {
		if isFocused {
			style = buttonDangerFocusedStyle
		} else if isHovered {
			style = buttonDangerHoverStyle
		} else {
			style = buttonDangerStyle
		}
	} else {
		if isFocused {
			style = buttonFocusedStyle
		} else if isHovered {
			style = buttonHoverStyle
		} else {
			style = buttonStyle
		}
	}
	return style.Render(label)
}

// renderButtonPair renders two buttons side by side with appropriate spacing.
// Returns the rendered buttons and their positions for hit-testing.
func renderButtonPair(leftLabel, rightLabel string, leftFocused, rightFocused, leftHovered, rightHovered, leftDanger, rightDanger bool) string {
	left := renderButton(leftLabel, leftFocused, leftHovered, leftDanger)
	right := renderButton(rightLabel, rightFocused, rightHovered, rightDanger)
	return left + "  " + right
}

// highlightRow applies selection highlight to entire row width, preserving text colors
func highlightRow(line string, width int) string {
	bgCode := "\x1b[48;5;237m" // Background color 237
	reset := "\x1b[0m"

	// First, truncate if line is too wide (ANSI-aware truncation)
	lineWidth := lipgloss.Width(line)
	if lineWidth > width {
		// Truncate with ellipsis, leaving room for "..."
		line = ansi.Truncate(line, width-3, "...")
		lineWidth = lipgloss.Width(line)
	}

	// Inject background after every ANSI escape sequence
	line = ansiPattern.ReplaceAllString(line, "${0}"+bgCode)

	// Prepend background at start
	line = bgCode + line

	// Pad to width if needed
	if lineWidth < width {
		line = line + strings.Repeat(" ", width-lineWidth)
	}

	return line + reset
}

// hoverRow applies a subtle hover highlight to a row (for mouse hover)
func hoverRow(line string, width int) string {
	bgCode := "\x1b[48;5;236m" // Background color 236 (slightly darker than highlight)
	reset := "\x1b[0m"

	// First, truncate if line is too wide (ANSI-aware truncation)
	lineWidth := lipgloss.Width(line)
	if lineWidth > width {
		line = ansi.Truncate(line, width-3, "...")
		lineWidth = lipgloss.Width(line)
	}

	// Inject background after every ANSI escape sequence
	line = ansiPattern.ReplaceAllString(line, "${0}"+bgCode)

	// Prepend background at start
	line = bgCode + line

	// Pad to width if needed
	if lineWidth < width {
		line = line + strings.Repeat(" ", width-lineWidth)
	}

	return line + reset
}
