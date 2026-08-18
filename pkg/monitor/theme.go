package monitor

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/huh/v2"
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

	// Workflow bucket colors preserve distinctions that do not map cleanly
	// onto generic status roles.
	ReadyToClose  string
	PendingReview string
	PendingOther  string

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
	Backdrop      string // dimmed background content behind overlays

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
		ReadyToClose:  "78",
		PendingReview: "183",
		PendingOther:  "103",
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
		Backdrop:      "242",
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

// themeColorHex converts Lip Gloss' accepted ANSI and hex inputs to the hex
// form expected by Glamour/Chroma style primitives.
func themeColorHex(value string) string {
	r, g, b, _ := lipgloss.Color(value).RGBA()
	return fmt.Sprintf("#%02X%02X%02X", uint8(r>>8), uint8(g>>8), uint8(b>>8))
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
	modalSuccess          lipgloss.Style
	modalWarning          lipgloss.Style
	modalError            lipgloss.Style
	modalBreadcrumb       lipgloss.Style
	modalEpicFocused      lipgloss.Style
	modalSelected         lipgloss.Style
	modalParent           lipgloss.Style
	modalParentFocused    lipgloss.Style
	modalBlockedFocused   lipgloss.Style
	modalBlocksFocused    lipgloss.Style

	selectionBackground string
	selectionStyle      string
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
			CategoryReadyToClose:  lipgloss.NewStyle().Foreground(color(theme.ReadyToClose)),
			CategoryNeedsRework:   lipgloss.NewStyle().Foreground(color(theme.Warning)),
			CategoryInProgress:    lipgloss.NewStyle().Foreground(color(theme.Info)),
			CategoryReady:         lipgloss.NewStyle().Foreground(color(theme.Success)),
			CategoryPendingReview: lipgloss.NewStyle().Foreground(color(theme.PendingReview)),
			CategoryPendingOther:  lipgloss.NewStyle().Foreground(color(theme.PendingOther)),
			CategoryBlocked:       lipgloss.NewStyle().Foreground(color(theme.Error)),
			CategoryClosed:        lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		},
		categoryHeader: map[TaskListCategory]lipgloss.Style{
			CategoryReviewable:    lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Secondary)).Bold(true),
			CategoryReadyToClose:  lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.ReadyToClose)).Bold(true),
			CategoryNeedsRework:   lipgloss.NewStyle().Foreground(color(theme.Warning)),
			CategoryInProgress:    lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Info)).Bold(true),
			CategoryReady:         lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.Success)).Bold(true),
			CategoryPendingReview: lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.PendingReview)).Bold(true),
			CategoryPendingOther:  lipgloss.NewStyle().Foreground(color(theme.OnWarning)).Background(color(theme.PendingOther)).Bold(true),
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
		activityTableSelected: lipgloss.NewStyle().Foreground(color(theme.TextSelection)).Background(color(theme.Selection)),
		statsTableLabel:       lipgloss.NewStyle().Foreground(color(theme.TextMuted)),
		kanbanTitle:           lipgloss.NewStyle().Bold(true).Foreground(color(theme.TextPrimary)),
		kanbanHint:            lipgloss.NewStyle().Foreground(color(theme.TextSubtle)),
		kanbanSeparator:       lipgloss.NewStyle().Foreground(color(theme.Border)),
		kanbanBox:             lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(theme.BorderActive)).Padding(0, 1),
		boardEditorHeader:     lipgloss.NewStyle().Bold(true).Foreground(color(theme.Primary)),
		errorText:             lipgloss.NewStyle().Foreground(color(theme.Error)),
		modalSuccess:          lipgloss.NewStyle().Foreground(color(theme.Success)),
		modalWarning:          lipgloss.NewStyle().Foreground(color(theme.Warning)),
		modalError:            lipgloss.NewStyle().Foreground(color(theme.Error)),
		modalBreadcrumb:       lipgloss.NewStyle().Foreground(color(theme.TextSubtle)).Italic(true),
		modalEpicFocused:      lipgloss.NewStyle().Bold(true).Foreground(color(theme.Info)).MarginTop(1),
		modalSelected:         lipgloss.NewStyle().Background(color(theme.Selection)).Foreground(color(theme.TextSelection)),
		modalParent:           lipgloss.NewStyle().Foreground(color(theme.Primary)),
		modalParentFocused:    lipgloss.NewStyle().Background(color(theme.Selection)).Foreground(color(theme.Primary)).Bold(true),
		modalBlockedFocused:   lipgloss.NewStyle().Bold(true).Foreground(color(theme.Error)).MarginTop(1),
		modalBlocksFocused:    lipgloss.NewStyle().Bold(true).Foreground(color(theme.Info)).MarginTop(1),
		selectionBackground:   ansi.NewStyle().BackgroundColor(color(theme.Selection)).String(),
		selectionStyle:        ansi.NewStyle().ForegroundColor(color(theme.TextSelection)).BackgroundColor(color(theme.Selection)).String(),
	}
}

func themedTextInputStyles(theme Theme) textinput.Styles {
	styles := textinput.DefaultDarkStyles()
	if theme == DefaultTheme() {
		return styles
	}
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

func formTheme(theme Theme) huh.Theme {
	// The standalone monitor used Dracula before model themes existed. Return
	// the library theme itself so the default rendering remains byte-for-byte
	// compatible rather than approximating it from semantic slots.
	if themeIsZero(theme) || theme == DefaultTheme() {
		return huh.ThemeFunc(huh.ThemeDracula)
	}

	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		styles := huh.ThemeBase(isDark)
		color := func(value string) color.Color { return lipgloss.Color(value) }

		styles.Form.Base = styles.Form.Base.Foreground(color(theme.TextPrimary))
		styles.Group.Title = styles.Group.Title.Foreground(color(theme.Primary)).Bold(true)
		styles.Group.Description = styles.Group.Description.Foreground(color(theme.TextMuted))
		styles.FieldSeparator = styles.FieldSeparator.Foreground(color(theme.BorderMuted))

		focused := &styles.Focused
		focused.Base = focused.Base.BorderForeground(color(theme.BorderActive))
		focused.Card = focused.Base
		focused.Title = focused.Title.Foreground(color(theme.Primary)).Bold(true)
		focused.NoteTitle = focused.NoteTitle.Foreground(color(theme.Primary)).Bold(true)
		focused.Description = focused.Description.Foreground(color(theme.TextMuted))
		focused.ErrorIndicator = focused.ErrorIndicator.Foreground(color(theme.Error))
		focused.ErrorMessage = focused.ErrorMessage.Foreground(color(theme.Error))
		focused.Directory = focused.Directory.Foreground(color(theme.Link))
		focused.File = focused.File.Foreground(color(theme.TextPrimary))
		focused.SelectSelector = focused.SelectSelector.Foreground(color(theme.Accent))
		focused.NextIndicator = focused.NextIndicator.Foreground(color(theme.Accent))
		focused.PrevIndicator = focused.PrevIndicator.Foreground(color(theme.Accent))
		focused.Option = focused.Option.Foreground(color(theme.TextPrimary))
		focused.MultiSelectSelector = focused.MultiSelectSelector.Foreground(color(theme.Accent))
		focused.SelectedOption = focused.SelectedOption.Foreground(color(theme.Success))
		focused.SelectedPrefix = focused.SelectedPrefix.Foreground(color(theme.Success))
		focused.UnselectedOption = focused.UnselectedOption.Foreground(color(theme.TextPrimary))
		focused.UnselectedPrefix = focused.UnselectedPrefix.Foreground(color(theme.TextMuted))
		focused.FocusedButton = focused.FocusedButton.Foreground(color(theme.OnPrimary)).Background(color(theme.Primary)).Bold(true)
		focused.Next = focused.FocusedButton
		focused.BlurredButton = focused.BlurredButton.Foreground(color(theme.TextSecondary)).Background(color(theme.SurfaceRaised))
		focused.TextInput.Cursor = focused.TextInput.Cursor.Foreground(color(theme.Accent))
		focused.TextInput.CursorText = focused.TextInput.CursorText.Foreground(color(theme.TextSelection)).Background(color(theme.Selection))
		focused.TextInput.Placeholder = focused.TextInput.Placeholder.Foreground(color(theme.TextMuted))
		focused.TextInput.Prompt = focused.TextInput.Prompt.Foreground(color(theme.Accent))
		focused.TextInput.Text = focused.TextInput.Text.Foreground(color(theme.TextPrimary))

		styles.Blurred = *focused
		styles.Blurred.Base = focused.Base.BorderStyle(lipgloss.HiddenBorder()).BorderForeground(color(theme.BorderMuted))
		styles.Blurred.Card = styles.Blurred.Base
		styles.Blurred.Title = styles.Blurred.Title.Foreground(color(theme.TextSecondary)).Bold(false)
		styles.Blurred.NoteTitle = styles.Blurred.NoteTitle.Foreground(color(theme.TextSecondary)).Bold(false)
		styles.Blurred.Description = styles.Blurred.Description.Foreground(color(theme.TextSubtle))
		styles.Blurred.SelectSelector = lipgloss.NewStyle().SetString("  ")
		styles.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
		styles.Blurred.NextIndicator = lipgloss.NewStyle()
		styles.Blurred.PrevIndicator = lipgloss.NewStyle()
		styles.Blurred.TextInput.Prompt = styles.Blurred.TextInput.Prompt.Foreground(color(theme.TextMuted))
		styles.Blurred.TextInput.Text = styles.Blurred.TextInput.Text.Foreground(color(theme.TextSecondary))
		return styles
	})
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

func (m Model) formatIssueDetailStatus(status models.Status) string {
	styles := m.renderStyles()
	style, ok := styles.status[status]
	if !ok {
		return string(status)
	}
	if status == models.StatusClosed {
		style = styles.modalSuccess
	}
	return style.Bold(true).Render(string(status))
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

func (m Model) renderButton(label string, focused, hovered, danger bool) string {
	theme := m.themeOrDefault()
	foreground := theme.TextSecondary
	background := theme.SurfaceRaised
	bold := false
	if focused {
		foreground, background, bold = theme.OnPrimary, theme.Primary, true
	} else if hovered {
		foreground, background = theme.TextSelection, theme.BorderMuted
	}
	if danger && focused {
		foreground, background = theme.OnError, theme.Error
	} else if danger && hovered {
		foreground, background = theme.OnError, theme.Error
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(foreground)).
		Background(lipgloss.Color(background)).
		Bold(bold).
		Padding(0, 2).
		Render(label)
}

func (m Model) renderButtonPair(leftLabel, rightLabel string, leftFocused, rightFocused, leftHovered, rightHovered, leftDanger, rightDanger bool) string {
	left := m.renderButton(leftLabel, leftFocused, leftHovered, leftDanger)
	right := m.renderButton(rightLabel, rightFocused, rightHovered, rightDanger)
	return left + "  " + right
}

func (m Model) renderHelpLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	theme := m.themeOrDefault()
	if strings.HasSuffix(trimmed, ":") || strings.Contains(trimmed, "Key Bindings") || strings.Contains(trimmed, "Search Syntax") {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary)).Bold(true).Render(line)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextPrimary)).Render(line)
}

func (m Model) renderHelpText(content string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = m.renderHelpLine(lines[i])
	}
	return strings.Join(lines, "\n")
}

func (m Model) highlightRow(line string, width int) string {
	styles := m.renderStyles()
	return highlightRowWithSelection(line, width, styles.selectionStyle, styles.selectionBackground)
}
