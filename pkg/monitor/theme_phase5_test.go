package monitor

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/marcus/td/internal/db"
	"github.com/marcus/td/internal/models"
	"github.com/marcus/td/pkg/monitor/modal"
)

func TestSetThemePreservesIntegratedLiveMonitorState(t *testing.T) {
	database := &db.DB{}
	m := NewModel(database, "session-theme", 7*time.Second, "dev", t.TempDir())
	m.Width, m.Height = 100, 34
	m.ActivePanel = PanelTaskList
	m.Cursor[PanelTaskList] = 3
	m.ScrollOffset[PanelTaskList] = 2
	m.SelectedID[PanelTaskList] = "td-selected"
	m.ScrollIndependent[PanelTaskList] = true
	m.PaneHeights = [3]float64{0.25, 0.5, 0.25}
	m.SearchMode = true
	m.SearchQuery = "status = open"
	m.IncludeClosed = true
	m.LastRefresh = time.Unix(100, 0)
	m.LastAutoSync = time.Unix(200, 0)
	m.AutoSyncInterval = time.Minute
	m.AutoSyncFunc = func() {}

	parent := &models.Issue{ID: "td-parent", Title: "Parent", Type: models.TypeEpic, Description: "## Parent"}
	child := &models.Issue{ID: "td-child", Title: "Child", Type: models.TypeTask, Description: "## Child", Acceptance: "- done"}
	m.ModalStack = []ModalEntry{
		{IssueID: parent.ID, Issue: parent, Scroll: 4, EpicTasksCursor: 1, TaskSectionFocused: true},
		{IssueID: child.ID, Issue: child, Scroll: 7, ParentEpic: parent, ParentEpicFocused: true, BlockedByCursor: 2},
	}

	form := newFormStateWithTheme(FormModeCreate, parent.ID, m.theme)
	form.Title = "unfinished title"
	form.Description = "unfinished body"
	form.ToggleExtended()
	_ = form.Form.Init()
	_ = form.Form.NextField()
	form.Autofill = &AutofillState{Active: true, FieldKey: formKeyParent, Idx: 2, Query: "td"}
	form.ButtonFocus = formButtonFocusCancel
	m.FormOpen = true
	m.FormState = form
	m.FormScrollOffset = 3

	note := &models.Note{ID: "note-1", Title: "Draft note", Content: "## Note"}
	m.NotesState = &NotesState{Notes: []models.Note{*note}, ListCursor: 1, ShowArchived: true, DetailNote: note, DetailRender: "stale"}

	openModal := m.newModal("Open dialog", ModalTypeHelp, modal.WithHints(false)).
		AddSection(modal.Buttons(modal.Btn(" First ", "first"), modal.Btn(" Second ", "second")))
	_ = openModal.Render(m.Width, m.Height, nil)
	openModal.SetFocus("second")
	openModal.SetScrollOffset(5)
	m.TDQHelpModal = openModal
	m.ShowTDQHelp = true

	formFocus := form.focusedFieldKey()
	autofill := form.Autofill
	beforeModal := m.TDQHelpModal
	beforeDB := m.DB
	beforeRefresh := m.RefreshInterval
	beforeNavigation := []any{
		m.ActivePanel, cloneMap(m.Cursor), cloneMap(m.ScrollOffset), cloneMap(m.SelectedID),
		cloneMap(m.ScrollIndependent), m.PaneHeights, m.SearchMode, m.SearchQuery, m.IncludeClosed,
	}
	beforePolling := []any{m.LastRefresh, m.LastAutoSync, m.AutoSyncInterval, m.AutoSyncFunc != nil}

	next := phase4TestTheme()
	if err := m.SetTheme(next); err != nil {
		t.Fatal(err)
	}

	if m.DB != beforeDB || m.DB != database || m.RefreshInterval != beforeRefresh {
		t.Fatal("SetTheme changed the database or refresh interval")
	}
	afterNavigation := []any{
		m.ActivePanel, cloneMap(m.Cursor), cloneMap(m.ScrollOffset), cloneMap(m.SelectedID),
		cloneMap(m.ScrollIndependent), m.PaneHeights, m.SearchMode, m.SearchQuery, m.IncludeClosed,
	}
	if !reflect.DeepEqual(afterNavigation, beforeNavigation) {
		t.Fatalf("SetTheme changed navigation/panel state:\n got %#v\nwant %#v", afterNavigation, beforeNavigation)
	}
	afterPolling := []any{m.LastRefresh, m.LastAutoSync, m.AutoSyncInterval, m.AutoSyncFunc != nil}
	if !reflect.DeepEqual(afterPolling, beforePolling) {
		t.Fatalf("SetTheme changed polling state: got %#v, want %#v", afterPolling, beforePolling)
	}

	if len(m.ModalStack) != 2 || m.ModalStack[0].Issue != parent || m.ModalStack[0].Scroll != 4 ||
		m.ModalStack[0].EpicTasksCursor != 1 || !m.ModalStack[0].TaskSectionFocused ||
		m.ModalStack[1].Issue != child || m.ModalStack[1].Scroll != 7 || m.ModalStack[1].ParentEpic != parent ||
		!m.ModalStack[1].ParentEpicFocused || m.ModalStack[1].BlockedByCursor != 2 {
		t.Fatalf("SetTheme changed nested issue modal state: %#v", m.ModalStack)
	}
	if m.TDQHelpModal != beforeModal || m.TDQHelpModal.FocusedID() != "second" || m.TDQHelpModal.ScrollOffset() != 5 {
		t.Fatal("SetTheme rebuilt or reset the open declarative modal")
	}
	if m.FormState.Title != "unfinished title" || m.FormState.Description != "unfinished body" ||
		m.FormState.Parent != parent.ID || m.FormState.focusedFieldKey() != formFocus ||
		m.FormState.Autofill != autofill || m.FormState.Autofill.Idx != 2 ||
		m.FormState.ButtonFocus != formButtonFocusCancel || m.FormScrollOffset != 3 {
		t.Fatalf("SetTheme changed form state: %#v", m.FormState)
	}
	if m.NotesState.DetailNote != note || m.NotesState.ListCursor != 1 || !m.NotesState.ShowArchived ||
		!strings.Contains(m.NotesState.DetailRender, "38;2;209;2;3") {
		t.Fatalf("SetTheme lost note state or failed to refresh note ANSI: %#v", m.NotesState)
	}

	primary := stylePrefix(lipgloss.NewStyle().Foreground(lipgloss.Color(next.Primary)).Render("X"), "X")
	if output := m.TDQHelpModal.Render(m.Width, m.Height, nil); !strings.Contains(output, primary) {
		t.Fatalf("open modal did not repaint with new theme sequence %q: %q", primary, output)
	}

	validTheme := m.theme
	validRevision := m.themeRevision
	validStyles := m.renderStyles().activePanel.Render("state")
	validMarkdown := *m.MarkdownTheme
	validModal := m.TDQHelpModal.Render(m.Width, m.Height, nil)
	invalid := next
	invalid.Primary = "definitely-not-a-color"
	if err := m.SetTheme(invalid); err == nil {
		t.Fatal("SetTheme accepted an invalid color")
	}
	if m.theme != validTheme || m.themeRevision != validRevision ||
		m.renderStyles().activePanel.Render("state") != validStyles || *m.MarkdownTheme != validMarkdown ||
		m.TDQHelpModal.Render(m.Width, m.Height, nil) != validModal {
		t.Fatal("invalid SetTheme partially changed monitor or child presentation state")
	}
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func TestSetThemeInvalidInputReturnsFieldContext(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	err := m.SetTheme(Theme{Warning: "broken"})
	if err == nil || !strings.Contains(err.Error(), "Warning") {
		t.Fatalf("invalid theme error lacks semantic field context: %v", err)
	}
}
