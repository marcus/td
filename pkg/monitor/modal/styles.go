package modal

import "charm.land/lipgloss/v2"

// Style mappings from sidecar to td monitor styles.
// These bridge the modal library to td's existing style system.

// Colors matching td's monitor/styles.go
var (
	// Primary colors
	Primary      = lipgloss.Color("212") // primaryColor
	Error        = lipgloss.Color("196") // errorColor
	Warning      = lipgloss.Color("214") // warningColor
	Info         = lipgloss.Color("45")  // cyan
	Muted        = lipgloss.Color("241") // mutedColor
	BgSecondary  = lipgloss.Color("235") // modal background
	TextMuted    = lipgloss.Color("241") // mutedColor
	BorderNormal = lipgloss.Color("240") // default border
)

// Button styles matching td's monitor/styles.go
var (
	Button = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("238")).
		Padding(0, 2)

	ButtonFocused = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(Primary).
			Bold(true).
			Padding(0, 2)

	ButtonHover = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("245")).
			Padding(0, 2)

	ButtonDanger = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("238")).
			Padding(0, 2)

	ButtonDangerFocused = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(Error).
				Bold(true).
				Padding(0, 2)

	ButtonDangerHover = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("203")).
				Padding(0, 2)
)

// Text styles
var (
	ModalTitle = lipgloss.NewStyle().Bold(true)
	MutedText  = lipgloss.NewStyle().Foreground(Muted)
	Body       = lipgloss.NewStyle() // Plain body text
)

// List styles for list sections
var (
	ListItemNormal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	ListItemSelected = lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255"))

	ListItemFocused = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("255")).
			Bold(true)

	ListCursor = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)
)
