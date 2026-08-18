package monitor

import (
	"fmt"
	"image/color"
	"reflect"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/marcus/td/internal/models"
)

// Theme is the host-neutral semantic palette used by the monitor. Color
// fields accept the same hexadecimal and ANSI color strings as lipgloss.Color.
// Empty fields inherit the corresponding value from DefaultTheme.
//
// SyntaxTheme and MarkdownTheme select the optional Chroma and Glamour themes.
// They are names rather than colors and may be left empty to use td's defaults.
type Theme struct {
	Primary   string
	Secondary string
	Accent    string

	Success string
	Warning string
	Error   string
	Info    string

	TextPrimary   string
	TextSecondary string
	TextMuted     string
	TextSubtle    string
	TextSelection string

	OnPrimary string
	OnWarning string
	OnError   string

	Background    string
	Surface       string
	SurfaceRaised string
	Selection     string

	Border       string
	BorderMuted  string
	BorderActive string

	Link string

	SyntaxTheme   string
	MarkdownTheme string
}

// DefaultTheme returns a fresh copy of td's standalone monitor palette.
func DefaultTheme() Theme {
	return Theme{
		Primary:       "212",
		Secondary:     "141",
		Accent:        "45",
		Success:       "42",
		Warning:       "214",
		Error:         "196",
		Info:          "45",
		TextPrimary:   "255",
		TextSecondary: "252",
		TextMuted:     "241",
		TextSubtle:    "244",
		TextSelection: "255",
		OnPrimary:     "255",
		OnWarning:     "0",
		OnError:       "255",
		Background:    "0",
		Surface:       "237",
		SurfaceRaised: "238",
		Selection:     "237",
		Border:        "240",
		BorderMuted:   "245",
		BorderActive:  "212",
		Link:          "45",
	}
}

// normalizedTheme validates every explicitly supplied color and overlays the
// supplied semantic slots on td's defaults. It constructs the complete result
// before returning so callers can apply a theme atomically.
func normalizedTheme(theme Theme) (Theme, error) {
	result := DefaultTheme()
	supplied := reflect.ValueOf(theme)
	normalized := reflect.ValueOf(&result).Elem()
	themeType := supplied.Type()

	for i := 0; i < supplied.NumField(); i++ {
		value := supplied.Field(i).String()
		if value == "" {
			continue
		}

		fieldName := themeType.Field(i).Name
		if fieldName != "SyntaxTheme" && fieldName != "MarkdownTheme" {
			if _, invalid := lipgloss.Color(value).(lipgloss.NoColor); invalid {
				return Theme{}, fmt.Errorf("invalid monitor theme color %s=%q", fieldName, value)
			}
		}

		normalized.Field(i).SetString(value)
	}

	return result, nil
}

func themeIsZero(theme Theme) bool {
	return theme == (Theme{})
}

// monitorStyles contains only model-owned, derived presentation state. The
// first phase intentionally migrates the visible panel/list/status steel
// thread; later phases extend this same value across the remaining renderers.
type monitorStyles struct {
	initialized bool

	panel              lipgloss.Style
	activePanel        lipgloss.Style
	hoverPanel         lipgloss.Style
	dividerHoverPanel  lipgloss.Style
	dividerActivePanel lipgloss.Style
	panelTitle         lipgloss.Style

	status map[models.Status]lipgloss.Style

	selectionBackground string
}

func newMonitorStyles(theme Theme) monitorStyles {
	color := func(value string) color.Color { return lipgloss.Color(value) }

	return monitorStyles{
		initialized: true,
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color(theme.Border)).
			Padding(0, 1),
		activePanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color(theme.BorderActive)).
			Padding(0, 1),
		hoverPanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color(theme.BorderMuted)).
			Padding(0, 1),
		dividerHoverPanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color(theme.Info)).
			Padding(0, 1),
		dividerActivePanel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color(theme.Warning)).
			Padding(0, 1),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Background(color(theme.Surface)).
			Foreground(color(theme.TextPrimary)).
			Padding(0, 1),
		status: map[models.Status]lipgloss.Style{
			models.StatusOpen:       lipgloss.NewStyle().Foreground(color(theme.Info)),
			models.StatusInProgress: lipgloss.NewStyle().Foreground(color(theme.Warning)),
			models.StatusBlocked:    lipgloss.NewStyle().Foreground(color(theme.Error)),
			models.StatusInReview:   lipgloss.NewStyle().Foreground(color(theme.Secondary)),
			models.StatusClosed:     lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		selectionBackground: ansi.NewStyle().BackgroundColor(color(theme.Selection)).String(),
	}
}

// renderStyles preserves the default appearance for legacy tests and callers
// that construct Model values directly instead of using a constructor.
func (m Model) renderStyles() monitorStyles {
	if m.styles.initialized {
		return m.styles
	}
	return newMonitorStyles(DefaultTheme())
}

func (m Model) formatStatus(status models.Status) string {
	style, ok := m.renderStyles().status[status]
	if !ok {
		return string(status)
	}
	return style.Render(string(status))
}

func (m Model) highlightRow(line string, width int) string {
	return highlightRowWithBackground(line, width, m.renderStyles().selectionBackground)
}
