package modal

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// Theme is the semantic palette for a declarative modal. Empty fields inherit
// the standalone defaults. Theme values are copied into each Modal, so two
// modals can render with different palettes without shared mutable state.
type Theme struct {
	Primary       string
	Warning       string
	Error         string
	Info          string
	TextPrimary   string
	TextSecondary string
	TextMuted     string
	TextSelection string
	OnPrimary     string
	OnError       string
	Surface       string
	SurfaceRaised string
	Selection     string
	Border        string
	BorderMuted   string
}

func themedTextInputStyles(theme Theme) textinput.Styles {
	result := textinput.DefaultDarkStyles()
	if theme == DefaultTheme() {
		return result
	}
	result.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextPrimary))
	result.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Focused.Suggestion = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary))
	result.Blurred.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextSecondary))
	result.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Blurred.Suggestion = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Blurred.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Cursor.Color = lipgloss.Color(theme.Primary)
	return result
}

func themedTextareaStyles(theme Theme) textarea.Styles {
	result := textarea.DefaultDarkStyles()
	if theme == DefaultTheme() {
		return result
	}
	result.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextPrimary))
	result.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary))
	result.Blurred.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextSecondary))
	result.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Blurred.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted))
	result.Cursor.Color = lipgloss.Color(theme.Primary)
	return result
}

// DefaultTheme returns the palette historically used by the standalone modal
// package.
func DefaultTheme() Theme {
	return Theme{
		Primary:       "212",
		Warning:       "214",
		Error:         "196",
		Info:          "45",
		TextPrimary:   "255",
		TextSecondary: "252",
		TextMuted:     "241",
		TextSelection: "255",
		OnPrimary:     "255",
		OnError:       "255",
		Surface:       "235",
		SurfaceRaised: "238",
		Selection:     "237",
		Border:        "240",
		BorderMuted:   "245",
	}
}

func normalizeTheme(theme Theme) Theme {
	defaults := DefaultTheme()
	if theme.Primary == "" {
		theme.Primary = defaults.Primary
	}
	if theme.Warning == "" {
		theme.Warning = defaults.Warning
	}
	if theme.Error == "" {
		theme.Error = defaults.Error
	}
	if theme.Info == "" {
		theme.Info = defaults.Info
	}
	if theme.TextPrimary == "" {
		theme.TextPrimary = defaults.TextPrimary
	}
	if theme.TextSecondary == "" {
		theme.TextSecondary = defaults.TextSecondary
	}
	if theme.TextMuted == "" {
		theme.TextMuted = defaults.TextMuted
	}
	if theme.TextSelection == "" {
		theme.TextSelection = defaults.TextSelection
	}
	if theme.OnPrimary == "" {
		theme.OnPrimary = defaults.OnPrimary
	}
	if theme.OnError == "" {
		theme.OnError = defaults.OnError
	}
	if theme.Surface == "" {
		theme.Surface = defaults.Surface
	}
	if theme.SurfaceRaised == "" {
		theme.SurfaceRaised = defaults.SurfaceRaised
	}
	if theme.Selection == "" {
		theme.Selection = defaults.Selection
	}
	if theme.Border == "" {
		theme.Border = defaults.Border
	}
	if theme.BorderMuted == "" {
		theme.BorderMuted = defaults.BorderMuted
	}
	return theme
}

type styles struct {
	theme     Theme
	isDefault bool

	button              lipgloss.Style
	buttonFocused       lipgloss.Style
	buttonHover         lipgloss.Style
	buttonDanger        lipgloss.Style
	buttonDangerFocused lipgloss.Style
	buttonDangerHover   lipgloss.Style
	modalTitle          lipgloss.Style
	mutedText           lipgloss.Style
	body                lipgloss.Style
	listItemNormal      lipgloss.Style
	listItemSelected    lipgloss.Style
	listItemFocused     lipgloss.Style
	listCursor          lipgloss.Style
}

func newStyles(theme Theme) styles {
	theme = normalizeTheme(theme)
	color := func(value string) color.Color { return lipgloss.Color(value) }
	return styles{
		theme:     theme,
		isDefault: theme == DefaultTheme(),
		button: lipgloss.NewStyle().
			Foreground(color(theme.TextSecondary)).Background(color(theme.SurfaceRaised)).Padding(0, 2),
		buttonFocused: lipgloss.NewStyle().
			Foreground(color(theme.OnPrimary)).Background(color(theme.Primary)).Bold(true).Padding(0, 2),
		buttonHover: lipgloss.NewStyle().
			Foreground(color(theme.TextSelection)).Background(color(theme.BorderMuted)).Padding(0, 2),
		buttonDanger: lipgloss.NewStyle().
			Foreground(color(theme.TextSecondary)).Background(color(theme.SurfaceRaised)).Padding(0, 2),
		buttonDangerFocused: lipgloss.NewStyle().
			Foreground(color(theme.OnError)).Background(color(theme.Error)).Bold(true).Padding(0, 2),
		buttonDangerHover: lipgloss.NewStyle().
			Foreground(color(theme.OnError)).Background(color(theme.Error)).Padding(0, 2),
		modalTitle:       lipgloss.NewStyle().Bold(true),
		mutedText:        lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		body:             bodyStyle(theme),
		listItemNormal:   lipgloss.NewStyle().Foreground(color(theme.TextSecondary)),
		listItemSelected: lipgloss.NewStyle().Background(color(theme.Selection)).Foreground(color(theme.TextSelection)),
		listItemFocused:  lipgloss.NewStyle().Background(color(theme.Selection)).Foreground(color(theme.TextSelection)).Bold(true),
		listCursor:       lipgloss.NewStyle().Foreground(color(theme.Primary)).Bold(true),
	}
}

func bodyStyle(theme Theme) lipgloss.Style {
	if theme == DefaultTheme() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextPrimary))
}
