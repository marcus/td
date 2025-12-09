package monitor

import (
	"github.com/charmbracelet/lipgloss"
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

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1)

	// Text styles
	titleStyle   = lipgloss.NewStyle().Bold(true)
	subtleStyle  = lipgloss.NewStyle().Foreground(mutedColor)
	helpStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	timestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	// Status styles
	statusStyles = map[models.Status]lipgloss.Style{
		models.StatusOpen:       lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		models.StatusInProgress: lipgloss.NewStyle().Foreground(warningColor),
		models.StatusBlocked:    lipgloss.NewStyle().Foreground(errorColor),
		models.StatusInReview:   lipgloss.NewStyle().Foreground(secondaryColor),
		models.StatusClosed:     lipgloss.NewStyle().Foreground(mutedColor),
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
