package monitor

import (
	"fmt"
	"image/color"
	"reflect"

	"charm.land/bubbles/v2/textinput"
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

	status         map[models.Status]lipgloss.Style
	statusChart    map[models.Status]lipgloss.Style
	priority       map[models.Priority]lipgloss.Style
	typeIcon       map[models.Type]lipgloss.Style
	category       map[TaskListCategory]lipgloss.Style
	categoryHeader map[TaskListCategory]lipgloss.Style
	activityBadge  map[string]lipgloss.Style

	title                 lipgloss.Style
	subtle                lipgloss.Style
	help                  lipgloss.Style
	timestamp             lipgloss.Style
	sectionHeader         lipgloss.Style
	searchActive          lipgloss.Style
	searchEditing         lipgloss.Style
	searchBar             lipgloss.Style
	toast                 lipgloss.Style
	toastError            lipgloss.Style
	activeSession         lipgloss.Style
	handoffAlert          lipgloss.Style
	reviewAlert           lipgloss.Style
	updateAvailable       lipgloss.Style
	activityTableHeader   lipgloss.Style
	activityTableSelected lipgloss.Style
	statsTableLabel       lipgloss.Style
	kanbanTitle           lipgloss.Style
	kanbanHint            lipgloss.Style
	kanbanSeparator       lipgloss.Style
	kanbanBox             lipgloss.Style
	boardEditorHeader     lipgloss.Style
	errorText             lipgloss.Style

	selectionBackground string
}

var activityBadgeLabels = map[string]string{
	"log": "[LOG]", "action": "[ACT]", "comment": "[CMT]",
}

var categoryTagLabels = map[TaskListCategory]string{
	CategoryReviewable: "[REV]", CategoryReadyToClose: "[RTC]", CategoryNeedsRework: "[RWK]",
	CategoryInProgress: "[WIP]", CategoryReady: "[RDY]", CategoryPendingReview: "[PRV]",
	CategoryPendingOther: "[OTH]", CategoryBlocked: "[BLK]", CategoryClosed: "[CLS]",
}

func newMonitorStyles(theme Theme) monitorStyles {
	color := func(value string) color.Color { return lipgloss.Color(value) }

	// Central semantic mapping for core monitor data:
	// statuses: open/info, in-progress/warning, blocked/error,
	// in-review/secondary, closed/muted (success in completion charts);
	// priorities: P0/error, P1/warning, P2/info, P3-P4/muted;
	// types: epic/primary, feature/success, bug/error, task/info,
	// chore/muted. Review buckets use the same roles below, so list,
	// board, and kanban modes cannot drift independently.
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
		statusChart: map[models.Status]lipgloss.Style{
			models.StatusOpen:       lipgloss.NewStyle().Foreground(color(theme.Info)),
			models.StatusInProgress: lipgloss.NewStyle().Foreground(color(theme.Warning)),
			models.StatusBlocked:    lipgloss.NewStyle().Foreground(color(theme.Error)),
			models.StatusInReview:   lipgloss.NewStyle().Foreground(color(theme.Secondary)),
			models.StatusClosed:     lipgloss.NewStyle().Foreground(color(theme.Success)),
		},
		priority: map[models.Priority]lipgloss.Style{
			models.PriorityP0: lipgloss.NewStyle().Foreground(color(theme.Error)).Bold(true),
			models.PriorityP1: lipgloss.NewStyle().Foreground(color(theme.Warning)),
			models.PriorityP2: lipgloss.NewStyle().Foreground(color(theme.Info)),
			models.PriorityP3: lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
			models.PriorityP4: lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		typeIcon: map[models.Type]lipgloss.Style{
			models.TypeEpic:    lipgloss.NewStyle().Foreground(color(theme.Primary)),
			models.TypeFeature: lipgloss.NewStyle().Foreground(color(theme.Success)),
			models.TypeBug:     lipgloss.NewStyle().Foreground(color(theme.Error)),
			models.TypeTask:    lipgloss.NewStyle().Foreground(color(theme.Info)),
			models.TypeChore:   lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		category: map[TaskListCategory]lipgloss.Style{
			CategoryReviewable:    lipgloss.NewStyle().Foreground(color(theme.Secondary)),
			CategoryReadyToClose:  lipgloss.NewStyle().Foreground(color(theme.Success)),
			CategoryNeedsRework:   lipgloss.NewStyle().Foreground(color(theme.Warning)),
			CategoryInProgress:    lipgloss.NewStyle().Foreground(color(theme.Info)),
			CategoryReady:         lipgloss.NewStyle().Foreground(color(theme.Success)),
			CategoryPendingReview: lipgloss.NewStyle().Foreground(color(theme.Secondary)),
			CategoryPendingOther:  lipgloss.NewStyle().Foreground(color(theme.TextSubtle)),
			CategoryBlocked:       lipgloss.NewStyle().Foreground(color(theme.Error)),
			CategoryClosed:        lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		categoryHeader: map[TaskListCategory]lipgloss.Style{
			CategoryReviewable:    lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Secondary)).Bold(true),
			CategoryReadyToClose:  lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Success)).Bold(true),
			CategoryNeedsRework:   lipgloss.NewStyle().Foreground(color(theme.Warning)),
			CategoryInProgress:    lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Info)).Bold(true),
			CategoryReady:         lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Success)).Bold(true),
			CategoryPendingReview: lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Secondary)).Bold(true),
			CategoryPendingOther:  lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.TextSubtle)).Bold(true),
			CategoryBlocked:       lipgloss.NewStyle().Foreground(color(theme.OnError)).Background(color(theme.Error)).Bold(true),
			CategoryClosed:        lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		activityBadge: map[string]lipgloss.Style{
			"log":     lipgloss.NewStyle().Foreground(color(theme.Success)),
			"action":  lipgloss.NewStyle().Foreground(color(theme.Secondary)),
			"comment": lipgloss.NewStyle().Foreground(color(theme.Info)),
		},
		title:                 lipgloss.NewStyle().Bold(true).Foreground(color(theme.TextPrimary)),
		subtle:                lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		help:                  lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		timestamp:             lipgloss.NewStyle().Foreground(color(theme.TextSubtle)),
		sectionHeader:         lipgloss.NewStyle().Bold(true).Foreground(color(theme.TextPrimary)).MarginTop(1),
		searchActive:          lipgloss.NewStyle().Foreground(color(theme.Warning)).Bold(true),
		searchEditing:         lipgloss.NewStyle().Foreground(color(theme.Primary)).Bold(true),
		searchBar:             lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(color(theme.Border)).Padding(0, 1),
		toast:                 lipgloss.NewStyle().Background(color(theme.Success)).Foreground(color(theme.OnWarning)).Bold(true),
		toastError:            lipgloss.NewStyle().Background(color(theme.Error)).Foreground(color(theme.OnError)).Bold(true),
		activeSession:         lipgloss.NewStyle().Foreground(color(theme.Info)),
		handoffAlert:          lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Success)).Bold(true),
		reviewAlert:           lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Secondary)).Bold(true),
		updateAvailable:       lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Warning)).Bold(true),
		activityTableHeader:   lipgloss.NewStyle().Bold(true).Foreground(color(theme.TextPrimary)),
		activityTableSelected: lipgloss.NewStyle().Background(color(theme.Selection)),
		statsTableLabel:       lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		kanbanTitle:           lipgloss.NewStyle().Bold(true).Foreground(color(theme.TextPrimary)),
		kanbanHint:            lipgloss.NewStyle().Foreground(color(theme.TextSubtle)),
		kanbanSeparator:       lipgloss.NewStyle().Foreground(color(theme.Border)),
		kanbanBox:             lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(theme.BorderActive)).Padding(0, 1),
		boardEditorHeader:     lipgloss.NewStyle().Bold(true).Foreground(color(theme.Primary)),
		errorText:             lipgloss.NewStyle().Foreground(color(theme.Error)),
		selectionBackground:   ansi.NewStyle().BackgroundColor(color(theme.Selection)).String(),
	}
}

func themedTextInputStyles(styles textinput.Styles, theme Theme) textinput.Styles {
	color := func(value string) color.Color { return lipgloss.Color(value) }
	styles.Focused.Text = lipgloss.NewStyle().Foreground(color(theme.TextPrimary))
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(color(theme.TextMuted))
	styles.Focused.Suggestion = lipgloss.NewStyle().Foreground(color(theme.TextMuted))
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(color(theme.Primary))
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(color(theme.TextSecondary))
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(color(theme.TextMuted))
	styles.Blurred.Suggestion = lipgloss.NewStyle().Foreground(color(theme.TextMuted))
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(color(theme.TextSubtle))
	styles.Cursor.Color = color(theme.Primary)
	return styles
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

func (m Model) formatPriority(priority models.Priority) string {
	style, ok := m.renderStyles().priority[priority]
	if !ok {
		return string(priority)
	}
	return style.Render(string(priority))
}

func (m Model) formatTypeIcon(issueType models.Type) string {
	icon, ok := typeIcons[issueType]
	if !ok {
		icon = "?"
	}
	style, ok := m.renderStyles().typeIcon[issueType]
	if !ok {
		return icon
	}
	return style.Render(icon)
}

func (m Model) formatActivityBadge(activityType string) string {
	label, ok := activityBadgeLabels[activityType]
	if !ok {
		return m.renderStyles().subtle.Render("[???]")
	}
	return m.renderStyles().activityBadge[activityType].Render(label)
}

func (m Model) highlightRow(line string, width int) string {
	return highlightRowWithBackground(line, width, m.renderStyles().selectionBackground)
}
