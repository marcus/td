package monitor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
)

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
