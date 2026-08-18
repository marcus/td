package monitor

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/modal"
)

func phase2TestTheme() Theme {
	return Theme{
		Primary: "#110001", Secondary: "#220002", Accent: "#330003",
		Success: "#440004", Warning: "#550005", Error: "#660006", Info: "#770007",
		ReadyToClose: "#440014", PendingReview: "#220012", PendingOther: "#bb001b",
		TextPrimary: "#880008", TextSecondary: "#990009", TextMuted: "#aa000a", TextSubtle: "#bb000b",
		TextSelection: "#cc000c", OnPrimary: "#dd000d", OnWarning: "#ee000e", OnError: "#ff000f",
		Background: "#010101", Surface: "#020202", SurfaceRaised: "#030303", Selection: "#040404",
		Border: "#050505", BorderMuted: "#060606", BorderActive: "#070707", Link: "#080808",
	}
}

func stylePrefix(rendered, marker string) string {
	if i := strings.Index(rendered, marker); i >= 0 {
		return rendered[:i]
	}
	return rendered
}

func colorFragment(rendered, marker string) string {
	prefix := stylePrefix(rendered, marker)
	return strings.TrimSuffix(strings.TrimPrefix(prefix, "\x1b["), "m")
}

func TestDefaultThemePreservesStandaloneSteelThread(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	styles := m.renderStyles()

	styleCases := []struct {
		name string
		got  string
		want string
	}{
		{"inactive panel", styles.panel.Render("panel"), panelStyle.Render("panel")},
		{"active panel", styles.activePanel.Render("panel"), activePanelStyle.Render("panel")},
		{"hover panel", styles.hoverPanel.Render("panel"), hoverPanelStyle.Render("panel")},
		{"divider hover", styles.dividerHoverPanel.Render("panel"), dividerHoverPanelStyle.Render("panel")},
		{"divider active", styles.dividerActivePanel.Render("panel"), dividerActivePanelStyle.Render("panel")},
		{"panel title", styles.panelTitle.Render("TASK LIST"), panelTitleStyle.Render("TASK LIST")},
		{"selection", m.highlightRow("task", 12), highlightRow("task", 12)},
	}
	for _, tt := range styleCases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("default %s output changed:\n got %q\nwant %q", tt.name, tt.got, tt.want)
			}
		})
	}

	for _, status := range []models.Status{
		models.StatusOpen,
		models.StatusInProgress,
		models.StatusBlocked,
		models.StatusInReview,
		models.StatusClosed,
	} {
		if got, want := m.formatStatus(status), formatStatus(status); got != want {
			t.Fatalf("default status %q output changed: got %q, want %q", status, got, want)
		}
	}
}

func TestPhase2DefaultWorkflowBucketsAndSearchPreserveStandaloneAppearance(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	styles := m.renderStyles()
	wantBucketColors := map[TaskListCategory]string{
		CategoryReadyToClose:  "78",
		CategoryPendingReview: "183",
		CategoryPendingOther:  "103",
	}
	for category, color := range wantBucketColors {
		got := styles.category[category].Render("X")
		want := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("X")
		if got != want {
			t.Errorf("default %s bucket style changed: got %q, want %q", category, got, want)
		}
		got = styles.categoryHeader[category].Render("X")
		want = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color(color)).Render("X")
		if got != want {
			t.Errorf("default %s bucket header changed: got %q, want %q", category, got, want)
		}
	}
	if got, want := m.SearchInput.Styles(), textinput.DefaultDarkStyles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default search styles changed:\n got %#v\nwant %#v", got, want)
	}
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	if err := m.SetTheme(DefaultTheme()); err != nil {
		t.Fatal(err)
	}
	if got, want := m.SearchInput.Styles(), textinput.DefaultDarkStyles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("restoring default theme did not restore default search styles:\n got %#v\nwant %#v", got, want)
	}
}

func TestThemePartialOverlayUsesDefaults(t *testing.T) {
	got, err := normalizedTheme(Theme{
		Primary:       "#123456",
		TextSelection: "15",
		SyntaxTheme:   "monokai",
	})
	if err != nil {
		t.Fatalf("normalize partial theme: %v", err)
	}

	want := DefaultTheme()
	want.Primary = "#123456"
	want.TextSelection = "15"
	want.SyntaxTheme = "monokai"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partial theme overlay mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestModelThemesRenderIndependently(t *testing.T) {
	newThemedModel := func(theme Theme) Model {
		m := NewModel(nil, "test", 0, "dev", t.TempDir())
		m.Width = 48
		m.ActivePanel = PanelTaskList
		m.Cursor[PanelTaskList] = 0
		m.TaskListRows = []TaskListRow{{
			Issue: models.Issue{
				ID:       "td-theme",
				Title:    "model-local palette",
				Type:     models.TypeTask,
				Priority: models.PriorityP1,
				Status:   models.StatusInProgress,
			},
			Category: CategoryInProgress,
		}}
		if err := m.SetTheme(theme); err != nil {
			t.Fatalf("SetTheme: %v", err)
		}
		return m
	}

	first := newThemedModel(Theme{
		Border:       "#100000",
		BorderActive: "#110000",
		Selection:    "#220000",
		Warning:      "#330000",
		Surface:      "#440000",
	})
	firstPanel := first.renderTaskListPanel(8)
	firstInactive := first
	firstInactive.ActivePanel = PanelCurrentWork
	firstInactivePanel := firstInactive.renderTaskListPanel(8)
	firstStatus := first.renderKanbanCardLine(first.TaskListRows[0].Issue, 1, 30, false)

	second := newThemedModel(Theme{
		Border:       "#001000",
		BorderActive: "#001100",
		Selection:    "#002200",
		Warning:      "#003300",
		Surface:      "#004400",
	})
	secondPanel := second.renderTaskListPanel(8)
	secondInactive := second
	secondInactive.ActivePanel = PanelCurrentWork
	secondInactivePanel := secondInactive.renderTaskListPanel(8)
	secondStatus := second.renderKanbanCardLine(second.TaskListRows[0].Issue, 1, 30, false)

	if firstPanel == secondPanel {
		t.Fatal("two models with different panel/selection themes rendered identically")
	}
	if firstStatus == secondStatus {
		t.Fatal("two models with different status themes rendered identically")
	}
	if firstInactivePanel == secondInactivePanel {
		t.Fatal("two models with different inactive-panel themes rendered identically")
	}
	if !strings.Contains(firstPanel, first.renderStyles().selectionBackground) {
		t.Fatal("first task-list selection did not use its model-owned background")
	}
	if !strings.Contains(secondPanel, second.renderStyles().selectionBackground) {
		t.Fatal("second task-list selection did not use its model-owned background")
	}
	if got := first.renderTaskListPanel(8); got != firstPanel {
		t.Fatal("constructing the second model contaminated the first model's output")
	}
}

func TestEmbeddedOptionsRejectInvalidThemeBeforeOpeningDatabase(t *testing.T) {
	_, err := NewEmbeddedWithOptions(EmbeddedOptions{
		BaseDir: t.TempDir(), // deliberately not initialized
		Theme:   Theme{Primary: "not-a-color"},
	})
	if err == nil {
		t.Fatal("NewEmbeddedWithOptions accepted an invalid explicit color")
	}
	if !strings.Contains(err.Error(), "Primary") {
		t.Fatalf("constructor returned the wrong error before database setup: %v", err)
	}
}

func TestSetThemeRejectsInvalidColorAtomically(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	if err := m.SetTheme(Theme{
		BorderActive: "#123456",
		Selection:    "#234567",
		Warning:      "#345678",
		SyntaxTheme:  "monokai",
	}); err != nil {
		t.Fatalf("set initial valid theme: %v", err)
	}

	beforeTheme := m.theme
	beforePanel := m.renderStyles().activePanel.Render("panel")
	beforeSelection := m.renderStyles().selectionBackground
	beforeMarkdown := *m.MarkdownTheme

	err := m.SetTheme(Theme{
		BorderActive: "#abcdef",
		Selection:    "definitely-not-a-color",
		Warning:      "#fedcba",
		SyntaxTheme:  "dracula",
	})
	if err == nil {
		t.Fatal("SetTheme accepted an invalid explicit color")
	}
	if !strings.Contains(err.Error(), "Selection") {
		t.Fatalf("invalid color error does not identify the field: %v", err)
	}
	if m.theme != beforeTheme {
		t.Fatalf("invalid theme changed model theme:\n got %#v\nwant %#v", m.theme, beforeTheme)
	}
	if got := m.renderStyles().activePanel.Render("panel"); got != beforePanel {
		t.Fatal("invalid theme changed derived panel styles")
	}
	if got := m.renderStyles().selectionBackground; got != beforeSelection {
		t.Fatal("invalid theme changed derived selection style")
	}
	if m.MarkdownTheme == nil || *m.MarkdownTheme != beforeMarkdown {
		t.Fatal("invalid theme changed markdown compatibility state")
	}
}

func TestEmbeddedOptionsThemePrecedenceAndRendererCompatibility(t *testing.T) {
	type panelCall struct {
		content       string
		width, height int
		state         PanelState
	}
	var gotPanelCall panelCall
	panelRenderer := PanelRenderer(func(content string, width, height int, state PanelState) string {
		gotPanelCall = panelCall{content: content, width: width, height: height, state: state}
		return "custom-panel"
	})
	modalRenderer := ModalRenderer(func(content string, width, height int, modalType ModalType, depth int) string {
		return content
	})
	legacyMarkdown := &MarkdownThemeConfig{SyntaxTheme: "dracula", MarkdownTheme: "dark"}

	baseDir := t.TempDir()
	database, err := db.Initialize(baseDir)
	if err != nil {
		t.Fatalf("initialize embedded test database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close setup database: %v", err)
	}

	m, err := NewEmbeddedWithOptions(EmbeddedOptions{
		BaseDir:       baseDir,
		PanelRenderer: panelRenderer,
		ModalRenderer: modalRenderer,
		MarkdownTheme: legacyMarkdown,
		Theme: Theme{
			Primary:       "#123456",
			SyntaxTheme:   "monokai",
			MarkdownTheme: "light",
		},
	})
	if err != nil {
		t.Fatalf("NewEmbeddedWithOptions: %v", err)
	}
	defer m.Close()

	if m.PanelRenderer == nil || m.ModalRenderer == nil {
		t.Fatal("theme option displaced an existing renderer adapter")
	}
	m.Width = 48
	m.ActivePanel = PanelTaskList
	if got := m.wrapPanel("TASK LIST", "body", 9, PanelTaskList); got != "custom-panel" {
		t.Fatalf("custom panel renderer result = %q, want custom-panel", got)
	}
	if gotPanelCall.width != 48 || gotPanelCall.height != 9 || gotPanelCall.state != PanelStateActive {
		t.Fatalf("custom panel renderer arguments changed: %#v", gotPanelCall)
	}
	if !strings.Contains(gotPanelCall.content, "TASK LIST") || !strings.Contains(gotPanelCall.content, "body") {
		t.Fatalf("custom panel renderer content changed: %q", gotPanelCall.content)
	}
	if m.theme.Primary != "#123456" {
		t.Fatalf("embedded theme primary = %q, want supplied value", m.theme.Primary)
	}
	if m.theme.Warning != DefaultTheme().Warning {
		t.Fatalf("embedded partial theme did not inherit warning default: %q", m.theme.Warning)
	}
	if m.MarkdownTheme == legacyMarkdown {
		t.Fatal("legacy MarkdownTheme took precedence over Theme")
	}
	if m.MarkdownTheme == nil || m.MarkdownTheme.SyntaxTheme != "monokai" || m.MarkdownTheme.MarkdownTheme != "light" {
		t.Fatalf("markdown compatibility state did not derive from Theme: %#v", m.MarkdownTheme)
	}
}

func TestPhase2SemanticMappingsUseModelTheme(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	styles := m.renderStyles()
	searchStyles := m.SearchInput.Styles()

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"priority", m.formatPriority(models.PriorityP1), styles.priority[models.PriorityP1].Render("P1")},
		{"type", m.formatTypeIcon(models.TypeBug), styles.typeIcon[models.TypeBug].Render(typeIcons[models.TypeBug])},
		{"activity", m.formatActivityBadge("comment"), styles.activityBadge["comment"].Render("[CMT]")},
		{"review bucket", m.formatCategoryTag(CategoryReviewable), styles.category[CategoryReviewable].Render("[REV]")},
		{"pending bucket", m.formatCategoryTag(CategoryPendingReview), styles.category[CategoryPendingReview].Render("[PRV]")},
		{"closed bucket", m.formatCategoryTag(CategoryClosed), styles.category[CategoryClosed].Render("[CLS]")},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
	if got := searchStyles.Focused.Text.Render("query"); !strings.Contains(got, "\x1b[38;2;136;0;8m") {
		t.Fatalf("focused search text did not use TextPrimary: %q", got)
	}
	if got := searchStyles.Focused.Placeholder.Render("search"); !strings.Contains(got, "\x1b[38;2;170;0;10m") {
		t.Fatalf("focused search placeholder did not use TextMuted: %q", got)
	}
}

func TestPhase2CoreRenderersUseContrastingModelPalettes(t *testing.T) {
	newModel := func(theme Theme) Model {
		m := NewModel(nil, "test", 0, "dev", t.TempDir())
		m.Width, m.Height = 96, 30
		m.ActivePanel = PanelTaskList
		m.SearchQuery = "theme"
		m.StatusMessage = "saved"
		m.LastRefresh = time.Now()
		m.TaskList = TaskListData{Reviewable: []models.Issue{{ID: "td-review", Type: models.TypeBug, Priority: models.PriorityP1, Status: models.StatusInReview}}}
		m.TaskListRows = BuildSwimlaneRows(m.TaskList)
		m.Activity = []ActivityItem{{Type: "comment", IssueID: "td-review", Message: "themed"}}
		m.BoardMode = BoardMode{Board: &models.Board{Name: "Theme"}, SwimlaneData: m.TaskList}
		if err := m.SetTheme(theme); err != nil {
			t.Fatal(err)
		}
		return m
	}

	dark := phase2TestTheme()
	light := phase2TestTheme()
	light.Primary, light.Secondary, light.Warning, light.Error, light.Info = "#f10101", "#f20202", "#f30303", "#f40404", "#f50505"
	light.TextPrimary, light.TextMuted, light.TextSubtle = "#111111", "#222222", "#333333"
	light.Selection, light.Border, light.BorderActive = "#eeeeee", "#dddddd", "#cccccc"

	first, second := newModel(dark), newModel(light)
	render := func(m Model) string {
		return strings.Join([]string{
			m.renderTaskListPanel(10),
			m.renderActivityPanel(8),
			m.renderSearchBar(),
			m.renderFooter(),
			m.renderKanbanView(),
		}, "\n")
	}
	firstOutput, secondOutput := render(first), render(second)
	if firstOutput == secondOutput {
		t.Fatal("contrasting model palettes rendered identical core monitor output")
	}
	for name, want := range map[string]string{
		"selection":     first.renderStyles().selectionStyle,
		"review":        stylePrefix(first.renderStyles().category[CategoryReviewable].Render("X"), "X"),
		"search":        stylePrefix(first.renderStyles().searchActive.Render("X"), "X"),
		"toast":         stylePrefix(first.renderStyles().toast.Render("X"), "X"),
		"kanban border": stylePrefix(first.renderStyles().kanbanBox.Render("X"), "╭"),
	} {
		if !strings.Contains(firstOutput, want) {
			t.Errorf("core output missing themed %s sequence %q", name, want)
		}
	}
	if strings.Contains(firstOutput, "\x1b[48;5;237m") {
		t.Fatal("core output leaked the legacy hardcoded selection background")
	}
	legacyPrefixes := map[string]string{
		"muted text":     stylePrefix(subtleStyle.Render("X"), "X"),
		"active search":  stylePrefix(searchQueryActiveStyle.Render("X"), "X"),
		"success toast":  stylePrefix(toastStyle.Render("X"), "X"),
		"kanban divider": stylePrefix(kanbanSepStyle.Render("X"), "X"),
		"bug icon":       stylePrefix(typeIconStyles[models.TypeBug].Render("X"), "X"),
	}
	for name, legacy := range legacyPrefixes {
		if legacy != "" && strings.Contains(firstOutput, legacy) {
			t.Errorf("core output leaked legacy td %s sequence %q", name, legacy)
		}
	}
}

func TestPhase2BoardEditorStatsAndEmptyStatesUseModelTheme(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	styles := m.renderStyles()

	m.BoardEditorPreview = &boardEditorPreviewData{Error: errors.New("bad query")}
	query := textarea.New()
	query.SetValue("bad")
	m.BoardEditorQueryInput = &query
	if got := m.renderBoardEditorQueryPreview(40); !strings.Contains(got, styles.errorText.Render("Error: bad query")) {
		t.Fatalf("board editor preview did not use themed error style: %q", got)
	}
	if got := m.renderBoardEditorTDQRef(40); !strings.Contains(got, styles.boardEditorHeader.Render("TDQ Quick Reference")) {
		t.Fatalf("board editor reference did not use themed header: %q", got)
	}

	m.StatsData = &StatsData{ExtendedStats: &models.ExtendedStats{ByStatus: map[models.Status]int{models.StatusBlocked: 2}}}
	stats := m.renderStatsContent(50)
	if !strings.Contains(stats, styles.sectionHeader.Render("STATUS BREAKDOWN")) ||
		!strings.Contains(stats, stylePrefix(styles.statusChart[models.StatusBlocked].Render("X"), "X")) {
		t.Fatalf("stats content did not use themed section/chart styles: %q", stats)
	}
	if legacy := stylePrefix(statusChartStyles[models.StatusBlocked].Render("X"), "X"); strings.Contains(stats, legacy) {
		t.Fatalf("stats content leaked legacy td blocked-chart sequence %q", legacy)
	}

	m.Width = 60
	empty := m.renderTaskListPanel(8)
	if !strings.Contains(empty, styles.subtle.Render("No tasks available")) {
		t.Fatalf("empty task-list state did not use themed muted text: %q", empty)
	}
}

func TestPhase2SelectionPreservesNestedForegrounds(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	foreground := m.renderStyles().typeIcon[models.TypeBug].Render("X")
	got := m.highlightRow(foreground+" plain", 20)
	if !strings.Contains(got, foreground[:strings.Index(foreground, "X")]) {
		t.Fatalf("selection removed nested foreground sequence: %q", got)
	}
	if strings.Count(got, m.renderStyles().selectionStyle) < 2 {
		t.Fatalf("selection foreground/background was not restored after nested ANSI: %q", got)
	}
	if strings.Contains(got, "\x1b[48;5;237m") {
		t.Fatal("selection used legacy ANSI 237 background")
	}
	combined := "\x1b[0;38;2;1;2;3m"
	got = m.highlightRow(combined+"nested\x1b[0m plain", 20)
	if !strings.Contains(got, combined+m.renderStyles().selectionBackground+"nested") {
		t.Fatalf("selection overrode a foreground set after a compound reset: %q", got)
	}
}

func TestPhase2HostilePaletteSelectionsUseReadablePlainTextAndKeepSemanticForegrounds(t *testing.T) {
	theme := phase2TestTheme()
	theme.Selection = "#111111"
	theme.Background = "#111111" // inherited terminal text would be unreadable here
	theme.TextPrimary = "#111111"
	theme.TextSelection = "#fafafa"

	issue := models.Issue{ID: "td-hostile", Title: "plain selected title", Type: models.TypeBug, Priority: models.PriorityP1, Status: models.StatusBlocked}
	newModel := func() Model {
		m := NewModel(nil, "test", 0, "dev", t.TempDir())
		m.Width, m.Height = 100, 30
		m.ActivePanel = PanelTaskList
		if err := m.SetTheme(theme); err != nil {
			t.Fatal(err)
		}
		return m
	}
	assertSelection := func(name, output string, m Model, semantic lipgloss.Style) {
		t.Helper()
		if !strings.Contains(output, m.renderStyles().selectionStyle) {
			t.Errorf("%s selection missing TextSelection foreground: %q", name, output)
		}
		semanticPrefix := stylePrefix(semantic.Render("X"), "X")
		if !strings.Contains(output, semanticPrefix) {
			t.Errorf("%s selection lost nested semantic foreground %q: %q", name, semanticPrefix, output)
		}
	}

	list := newModel()
	list.TaskList = TaskListData{Blocked: []models.Issue{issue}}
	list.TaskListRows = BuildSwimlaneRows(list.TaskList)
	assertSelection("list", list.renderTaskListPanel(9), list, list.renderStyles().typeIcon[models.TypeBug])

	board := newModel()
	board.TaskListMode = TaskListModeBoard
	board.BoardMode = BoardMode{
		Board:  &models.Board{Name: "Hostile"},
		Issues: []models.BoardIssueView{{Issue: issue, Category: string(CategoryBlocked)}},
	}
	assertSelection("board", board.renderTaskListBoardView(9), board, board.renderStyles().priority[models.PriorityP1])

	kanban := newModel()
	assertSelection("kanban", kanban.renderKanbanCardLine(issue, 0, 36, true), kanban, kanban.renderStyles().typeIcon[models.TypeBug])

	activity := newModel()
	activity.ActivePanel = PanelActivity
	activity.Activity = []ActivityItem{{Type: "comment", IssueID: issue.ID, Message: "plain selected message"}}
	activityOutput := activity.renderActivityPanel(8)
	selectedPrefix := stylePrefix(activity.renderStyles().activityTableSelected.Render("X"), "X")
	if !strings.Contains(activityOutput, selectedPrefix) {
		t.Errorf("activity selection missing TextSelection foreground: want %q in %q", selectedPrefix, activityOutput)
	}
	commentPrefix := stylePrefix(activity.renderStyles().activityBadge["comment"].Render("X"), "X")
	if !strings.Contains(activityOutput, commentPrefix) {
		t.Errorf("activity selection lost nested badge foreground %q: %q", commentPrefix, activityOutput)
	}
}

func TestPhase3DeclarativeModalConstructionPathsUseModelTheme(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 100, 32
	theme := phase2TestTheme()
	if err := m.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	m.AllBoards = []models.Board{{Name: "Themed board", Query: "status = open"}}
	m.ConfirmIssueID, m.ConfirmTitle = "td-theme", "Delete themed issue"
	m.NotesState = &NotesState{}
	m.BoardEditorMode = "info"
	m.BoardEditorBoard = &models.Board{Name: "Builtin", IsBuiltin: true}

	cases := []struct {
		name      string
		build     func() *modal.Modal
		wantColor string
	}{
		{"default board picker", m.createBoardPickerModal, theme.Primary},
		{"danger confirmation", m.createDeleteConfirmModal, theme.Error},
		{"info sync", func() *modal.Modal { return m.buildSyncPromptListModal(nil) }, theme.Info},
		{"info board editor", m.createBoardEditorModal, theme.Info},
		{"notes", m.createNotesListModal, theme.Info},
		{"getting started", m.createGettingStartedModal, theme.Primary},
		{"help", m.createTDQHelpModal, theme.Info},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.build().Render(m.Width, m.Height, nil)
			borderPrefix := stylePrefix(lipgloss.NewStyle().Foreground(lipgloss.Color(tt.wantColor)).Render("X"), "X")
			bodyPrefix := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextPrimary)).Render("X"), "X")
			if !strings.Contains(got, borderPrefix) {
				t.Fatalf("modal missing themed variant border/title %q: %q", borderPrefix, got)
			}
			if !strings.Contains(got, bodyPrefix) {
				t.Fatalf("modal missing themed body text %q: %q", bodyPrefix, got)
			}
			if legacy := stylePrefix(lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render("X"), "X"); strings.Contains(got, legacy) {
				t.Fatalf("modal leaked legacy primary sequence %q", legacy)
			}
		})
	}
}

func TestPhase3LiveRethemePreservesOpenDeclarativeModalState(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 80, 24
	m.CloseConfirmIssueID, m.CloseConfirmTitle = "td-theme", "Retheme me"
	m.CloseConfirmInput.SetValue("keep this reason")
	m.CloseConfirmModal = m.createCloseConfirmModal()
	m.CloseConfirmModal.Render(m.Width, m.Height, nil)
	m.CloseConfirmModal.SetFocus("confirm")
	m.CloseConfirmModal.Scroll(3)
	beforeFocus := m.CloseConfirmModal.FocusedID()
	beforeScroll := m.CloseConfirmModal.ScrollOffset()
	before := m.CloseConfirmModal.Render(m.Width, m.Height, nil)
	m.CloseConfirmModal.Scroll(3)
	beforeScroll = m.CloseConfirmModal.ScrollOffset()

	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	if got := m.CloseConfirmModal.InputValue("reason"); got != "keep this reason" {
		t.Fatalf("retheme changed open modal input: %q", got)
	}
	if m.CloseConfirmModal.FocusedID() != beforeFocus || m.CloseConfirmModal.ScrollOffset() != beforeScroll {
		t.Fatalf("retheme changed focus/scroll: focus %q->%q scroll %d->%d", beforeFocus, m.CloseConfirmModal.FocusedID(), beforeScroll, m.CloseConfirmModal.ScrollOffset())
	}
	if after := m.CloseConfirmModal.Render(m.Width, m.Height, nil); after == before {
		t.Fatal("open declarative modal did not repaint")
	}
}

func TestPhase3LegacyNestedIssueAndHelpUseThemeInsideHostChrome(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 100, 32
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	var calls []ModalType
	m.ModalRenderer = func(content string, width, height int, modalType ModalType, depth int) string {
		calls = append(calls, modalType)
		return "HOST{" + content + "}"
	}
	m.ModalStack = []ModalEntry{{
		IssueID: "td-parent",
		Issue:   &models.Issue{ID: "td-parent", Title: "Parent", Type: models.TypeEpic, Priority: models.PriorityP1, Status: models.StatusInProgress, CreatedAt: time.Now()},
	}, {
		IssueID: "td-child",
		Issue:   &models.Issue{ID: "td-child", Title: "Child", Type: models.TypeBug, Priority: models.PriorityP0, Status: models.StatusBlocked, CreatedAt: time.Now()},
	}}
	issue := m.renderModal()
	if !strings.HasPrefix(issue, "HOST{") || !strings.Contains(issue, m.renderStyles().modalBreadcrumb.Render(m.ModalBreadcrumb())) {
		t.Fatalf("nested issue did not retain themed inner content inside host chrome: %q", issue)
	}
	if !strings.Contains(issue, m.formatTypeIcon(models.TypeBug)) || !strings.Contains(issue, m.formatIssueDetailStatus(models.StatusBlocked)) {
		t.Fatalf("nested issue leaked unthemed semantic content: %q", issue)
	}

	help := m.renderHelp()
	if !strings.HasPrefix(help, "HOST{") || !strings.Contains(help, m.renderStyles().subtle.Render("/:filter  j/k:scroll  Ctrl+d/u:½page  G/gg:end/start  ?/Esc:close")) {
		t.Fatalf("help did not retain themed inner content inside host chrome: %q", help)
	}
	if len(calls) != 2 || calls[0] != ModalTypeIssue || calls[1] != ModalTypeHelp {
		t.Fatalf("unexpected host chrome calls: %#v", calls)
	}
}

func TestPhase3DeclarativeModalHostRendererOwnsOuterChromeOnly(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 80, 24
	if err := m.SetTheme(phase2TestTheme()); err != nil {
		t.Fatal(err)
	}
	var gotType ModalType
	m.ModalRenderer = func(content string, width, height int, modalType ModalType, depth int) string {
		gotType = modalType
		if !strings.Contains(content, m.renderStyles().title.Render("")) && !strings.Contains(content, "SELECT BOARD") {
			t.Errorf("host did not receive modal-owned inner content: %q", content)
		}
		return "HOST-DECLARATIVE{" + content + "}"
	}
	m.AllBoards = []models.Board{{Name: "Host board"}}
	got := m.createBoardPickerModal().Render(m.Width, m.Height, nil)
	if !strings.HasPrefix(got, "HOST-DECLARATIVE{") || gotType != ModalTypeBoardPicker {
		t.Fatalf("declarative modal bypassed host outer chrome: type=%v output=%q", gotType, got)
	}
	innerPrefix := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.TextSelection)).Render("X"), "X")
	if !strings.Contains(got, innerPrefix) {
		t.Fatalf("host outer chrome replaced themed inner content: %q", got)
	}
}
