package monitor

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/marcus/td/internal/models"
)

func phase4TestTheme() Theme {
	theme := phase2TestTheme()
	theme.Primary = "#D10203"
	theme.TextPrimary = "#E4E5E6"
	theme.TextMuted = "#A1A2A3"
	theme.SurfaceRaised = "#171819"
	theme.Backdrop = "#252627"
	return theme
}

func TestSetThemeRethemesOpenFormWithoutLosingState(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	fs := NewFormState(FormModeCreate, "td-parent")
	fs.Title = "draft title"
	fs.Description = "draft body"
	fs.ToggleExtended()
	_ = fs.Form.Init()
	_ = fs.Form.NextField()
	fs.Autofill = &AutofillState{Active: true, FieldKey: formKeyParent, Idx: 2, Query: "td"}
	fs.ButtonFocus = formButtonFocusCancel
	m.FormState = fs

	formBefore := fs.Form
	focusBefore := fs.focusedFieldKey()
	autofillBefore := fs.Autofill
	if err := m.SetTheme(phase4TestTheme()); err != nil {
		t.Fatal(err)
	}

	if fs.Form == formBefore {
		t.Fatal("live retheme did not rebuild fields with captured third-party styles")
	}
	if fs.Title != "draft title" || fs.Description != "draft body" || fs.Parent != "td-parent" {
		t.Fatalf("live retheme lost bound form values: %#v", fs)
	}
	if got := fs.focusedFieldKey(); got != focusBefore {
		t.Fatalf("live retheme changed focused field: got %q, want %q", got, focusBefore)
	}
	if fs.Autofill != autofillBefore || fs.Autofill.Idx != 2 || fs.ButtonFocus != formButtonFocusCancel {
		t.Fatal("live retheme changed autocomplete or button focus state")
	}
	primary := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(phase4TestTheme().Primary)).Render("X"), "X")
	if got := fs.Form.View(); !strings.Contains(got, primary) {
		t.Fatalf("open form did not repaint with model primary %q: %q", primary, got)
	}

	// Rebuilds after the retheme must retain the model theme too.
	fs.ToggleExtended()
	if got := fs.Form.View(); !strings.Contains(got, primary) {
		t.Fatal("form rebuild fell back to the third-party default theme")
	}
}

func TestThemeDrivesMarkdownAndRejectsStaleAsyncANSI(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	issue := &models.Issue{ID: "td-theme", Description: "## Heading\n\n`code`"}
	m.ModalStack = []ModalEntry{{IssueID: issue.ID, Issue: issue}}

	oldCmd := m.renderMarkdownAsync(issue.ID, issue.Description, "", 60)
	oldMsg := oldCmd().(MarkdownRenderedMsg)
	if err := m.SetTheme(phase4TestTheme()); err != nil {
		t.Fatal(err)
	}
	current := m.ModalStack[0].DescRender
	if current == "" || current == oldMsg.DescRender {
		t.Fatal("live retheme did not regenerate retained-source markdown")
	}
	if !strings.Contains(current, "38;2;209;2;3") {
		t.Fatalf("markdown does not contain the model primary color: %q", current)
	}

	updated, _ := m.Update(oldMsg)
	after := updated.(Model)
	if after.ModalStack[0].DescRender != current {
		t.Fatal("async markdown from an older theme replaced the regenerated cache")
	}
}

func TestSetThemeRegeneratesOpenNoteWithoutRebuildingModal(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 80, 30
	note := &models.Note{ID: "note-theme", Title: "Theme note", Content: "## Note heading"}
	m.NotesState = &NotesState{DetailNote: note, DetailRender: preRenderMarkdown(note.Content, 60, nil)}
	m.NotesModal = m.createNoteDetailModal()
	modalBefore := m.NotesModal

	if err := m.SetTheme(phase4TestTheme()); err != nil {
		t.Fatal(err)
	}
	if m.NotesModal != modalBefore {
		t.Fatal("live retheme rebuilt the open notes modal")
	}
	if !strings.Contains(m.NotesState.DetailRender, "38;2;209;2;3") {
		t.Fatalf("open note cache did not regenerate from retained source: %q", m.NotesState.DetailRender)
	}
	if got := m.NotesModal.Render(m.Width, m.Height, nil); !strings.Contains(got, "38;2;209;2;3") {
		t.Fatalf("open notes modal retained stale ANSI: %q", got)
	}
}

func TestSetThemeRethemesCapturedNoteLabelsAndMetadata(t *testing.T) {
	note := models.Note{
		ID: "note-colors", Title: "Captured note color marker", Content: "## Markdown marker",
		Pinned: true, Archived: true, CreatedAt: time.Now().Add(-2 * time.Hour), UpdatedAt: time.Now(),
	}

	t.Run("standalone defaults do not survive", func(t *testing.T) {
		m := NewModel(nil, "test", 0, "dev", t.TempDir())
		m.Width, m.Height = 90, 32
		m.NotesState = &NotesState{Notes: []models.Note{note}, DetailNote: &note, DetailRender: preRenderMarkdown(note.Content, 60, nil)}

		m.NotesModal = m.createNotesListModal()
		listBefore := m.NotesModal
		if err := m.SetTheme(phase4TestTheme()); err != nil {
			t.Fatal(err)
		}
		if m.NotesModal != listBefore {
			t.Fatal("live retheme rebuilt the notes list")
		}
		list := m.NotesModal.Render(m.Width, m.Height, nil)
		wantList := formatNoteListItemWithStyles(note, 66, m.renderStyles())
		if !strings.Contains(list, wantList) {
			t.Fatalf("notes list did not rebind label through current theme:\nwant fragment %q\nrender %q", wantList, list)
		}
		assertNoANSIColors(t, list, "38;5;255", "38;5;241")

		m.NotesModal = m.createNoteDetailModal()
		detailBefore := m.NotesModal
		// Return through DefaultTheme and then the hostile palette so the same
		// captured detail section has to shed standalone metadata ANSI.
		if err := m.SetTheme(DefaultTheme()); err != nil {
			t.Fatal(err)
		}
		if err := m.SetTheme(phase4TestTheme()); err != nil {
			t.Fatal(err)
		}
		if m.NotesModal != detailBefore {
			t.Fatal("live retheme rebuilt the note detail")
		}
		detail := m.NotesModal.Render(m.Width, m.Height, nil)
		wantMeta := formatNoteMetaWithStyles(&note, m.renderStyles())
		if !strings.Contains(detail, wantMeta) {
			t.Fatalf("note detail did not rebind metadata through current theme:\nwant fragment %q\nrender %q", wantMeta, detail)
		}
		assertNoANSIColors(t, detail, "38;5;255", "38;5;241")
	})

	t.Run("previous custom palette does not survive", func(t *testing.T) {
		m := NewModel(nil, "test", 0, "dev", t.TempDir())
		m.Width, m.Height = 90, 32
		oldTheme := phase4TestTheme()
		oldTheme.TextPrimary = "#111213"
		oldTheme.TextMuted = "#212223"
		oldTheme.Primary = "#313233"
		if err := m.SetTheme(oldTheme); err != nil {
			t.Fatal(err)
		}
		m.NotesState = &NotesState{Notes: []models.Note{note}, DetailNote: &note, DetailRender: preRenderMarkdown(note.Content, 60, m.MarkdownTheme)}
		m.NotesModal = m.createNotesListModal()
		oldFragments := []string{"38;2;17;18;19", "38;2;33;34;35", "38;2;49;50;51"}
		before := m.NotesModal.Render(m.Width, m.Height, nil)
		if !containsAny(before, oldFragments...) {
			t.Fatalf("test setup did not render the prior palette: %q", before)
		}

		if err := m.SetTheme(phase4TestTheme()); err != nil {
			t.Fatal(err)
		}
		list := m.NotesModal.Render(m.Width, m.Height, nil)
		assertNoANSIColors(t, list, oldFragments...)

		m.NotesModal = m.createNoteDetailModal()
		if err := m.SetTheme(oldTheme); err != nil {
			t.Fatal(err)
		}
		if err := m.SetTheme(phase4TestTheme()); err != nil {
			t.Fatal(err)
		}
		detail := m.NotesModal.Render(m.Width, m.Height, nil)
		assertNoANSIColors(t, detail, oldFragments...)
		if !strings.Contains(detail, "38;2;209;2;3") {
			t.Fatalf("note markdown did not repaint with the new palette: %q", detail)
		}
	})
}

func TestCustomHuhThemeReplacesEveryInheritedHelpColor(t *testing.T) {
	theme := phase4TestTheme()
	theme.Primary = "#010203"
	theme.TextMuted = "#040506"
	theme.TextSubtle = "#070809"
	styles := formTheme(theme).Theme(true)

	wantKey := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Primary)).Render("X")
	wantDesc := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextMuted)).Render("X")
	wantSeparator := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.TextSubtle)).Render("X")
	for name, got := range map[string]string{
		"short key": styles.Help.ShortKey.Render("X"), "full key": styles.Help.FullKey.Render("X"),
		"short description": styles.Help.ShortDesc.Render("X"), "full description": styles.Help.FullDesc.Render("X"),
		"short separator": styles.Help.ShortSeparator.Render("X"), "full separator": styles.Help.FullSeparator.Render("X"),
		"ellipsis": styles.Help.Ellipsis.Render("X"),
	} {
		want := wantSeparator
		if strings.Contains(name, "key") {
			want = wantKey
		} else if strings.Contains(name, "description") {
			want = wantDesc
		}
		if got != want {
			t.Errorf("Huh %s style = %q, want %q", name, got, want)
		}
	}

	fs := NewFormState(FormModeCreate, "")
	fs.setTheme(theme)
	_ = fs.Form.Init()
	view := fs.Form.View()
	assertNoANSIColors(t, view,
		"38;2;98;98;98", "38;2;74;74;74", "38;2;60;60;60", // Bubbles help defaults
		"38;2;68;71;90", "38;2;189;147;249", "38;2;241;250;140", "38;2;248;248;242", "38;2;98;114;164", // Dracula fields
	)
	if !strings.Contains(view, "38;2;4;5;6") {
		t.Fatalf("rendered Huh footer did not receive monitor description color: %q", view)
	}
}

func TestHostThemePrecedesHuhSelectFilterInitialization(t *testing.T) {
	theme := phase4TestTheme()
	issue := &models.Issue{
		ID: "td-filter", Title: "Filter theme", Type: models.TypeTask,
		Priority: models.PriorityP2, Status: models.StatusOpen,
	}
	cases := []struct {
		name, field string
		form        func() *FormState
	}{
		{"type", formKeyType, func() *FormState { return newFormStateWithTheme(FormModeCreate, "", theme) }},
		{"priority", formKeyPriority, func() *FormState { return newFormStateWithTheme(FormModeCreate, "", theme) }},
		{"status", formKeyStatus, func() *FormState {
			fs := newFormStateForEditWithTheme(issue, theme)
			fs.ShowExtended = true
			fs.buildForm()
			return fs
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.form()
			openHuhSelectFilter(t, fs, tt.field)
			view := fs.Form.View()
			if !strings.Contains(view, "38;2;51;0;3") || !strings.Contains(view, "38;2;228;229;230") {
				t.Fatalf("%s filter did not use host accent/text colors: %q", tt.field, view)
			}
			assertNoANSIColors(t, view, "38;2;241;250;140", "38;2;98;114;164")
		})
	}
}

func TestLiveRethemeRebuildsOpenSelectFiltersAndPreservesFormState(t *testing.T) {
	themeA := phase4TestTheme()
	themeB := phase4TestTheme()
	themeB.Primary = "#010203"
	themeB.Accent = "#0A0B0C"
	themeB.TextPrimary = "#0D0E0F"
	themeB.TextSecondary = "#101112"
	themeB.TextMuted = "#131415"
	themeB.TextSubtle = "#161718"

	issue := &models.Issue{
		ID: "td-live-filter", Title: "Existing draft", Type: models.TypeBug,
		Priority: models.PriorityP1, Status: models.StatusBlocked,
	}
	cases := []struct {
		name, field string
		form        func() *FormState
	}{
		{"create type", formKeyType, func() *FormState { return newFormStateWithTheme(FormModeCreate, "td-parent", themeA) }},
		{"create priority", formKeyPriority, func() *FormState { return newFormStateWithTheme(FormModeCreate, "td-parent", themeA) }},
		{"create points", formKeyPoints, func() *FormState {
			fs := newFormStateWithTheme(FormModeCreate, "td-parent", themeA)
			fs.ShowExtended = true
			fs.buildForm()
			return fs
		}},
		{"edit status", formKeyStatus, func() *FormState {
			fs := newFormStateForEditWithTheme(issue, themeA)
			fs.ShowExtended = true
			fs.buildForm()
			return fs
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.form()
			fs.Title = "stateful draft"
			fs.Type = string(models.TypeBug)
			fs.Priority = string(models.PriorityP1)
			fs.Description = "body in progress"
			fs.Labels = "theme, form"
			fs.Status = string(models.StatusBlocked)
			fs.Parent = "td-parent"
			fs.Points = "5"
			fs.Acceptance = "keep this"
			fs.Minor = true
			fs.Dependencies = "td-one, td-two"
			fs.Width = 73
			fs.ButtonFocus = formButtonFocusForm
			fs.ButtonHover = 2
			fs.Autofill = &AutofillState{Active: true, FieldKey: tt.field, Idx: 1, Query: "td"}
			fs.AutofillEpics = []AutofillItem{{ID: "td-epic", Title: "Epic"}}
			fs.AutofillAll = []AutofillItem{{ID: "td-one", Title: "One"}}

			openHuhSelectFilter(t, fs, tt.field)
			beforeView := fs.Form.View()
			if !strings.Contains(beforeView, "38;2;51;0;3") || !strings.Contains(beforeView, "38;2;228;229;230") {
				t.Fatalf("test setup did not render theme A filter colors: %q", beforeView)
			}
			before := snapshotFormState(fs)
			formBefore := fs.Form

			m := NewModel(nil, "test", 0, "dev", t.TempDir())
			if err := m.SetTheme(themeA); err != nil {
				t.Fatal(err)
			}
			m.FormState = fs
			if err := m.SetTheme(themeB); err != nil {
				t.Fatal(err)
			}

			if fs.Form == formBefore {
				t.Fatal("live retheme retained Huh fields with theme A filter snapshots")
			}
			if got := snapshotFormState(fs); !reflect.DeepEqual(got, before) {
				t.Fatalf("live retheme changed FormState-owned state:\n got %#v\nwant %#v", got, before)
			}
			if got := fs.focusedFieldKey(); got != tt.field {
				t.Fatalf("live retheme focus = %q, want %q", got, tt.field)
			}
			filtering := fs.Form.GetFocusedField().(interface{ GetFiltering() bool })
			if filtering.GetFiltering() {
				t.Fatal("live retheme retained inaccessible transient filter mode instead of safely resetting it")
			}

			// The transient query is intentionally reset, but the same select stays
			// focused and can immediately filter using the new captured palette.
			_, _ = fs.Form.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
			_, _ = fs.Form.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
			if !filtering.GetFiltering() {
				t.Fatal("rebuilt select did not re-enter filter mode")
			}
			view := fs.Form.View()
			if !strings.Contains(view, "38;2;10;11;12") || !strings.Contains(view, "38;2;13;14;15") {
				t.Fatalf("%s filter did not use theme B accent/text colors: %q", tt.field, view)
			}
			assertNoANSIColors(t, view,
				"38;2;209;2;3", "38;2;51;0;3", "38;2;228;229;230", "38;2;161;162;163", // theme A
				"38;2;68;71;90", "38;2;189;147;249", "38;2;241;250;140", // Dracula
				"38;2;248;248;242", "38;2;98;114;164",
			)
		})
	}
}

func TestLiveRethemeRestoresFocusedValidationError(t *testing.T) {
	fs := newFormStateWithTheme(FormModeCreate, "", phase4TestTheme())
	_ = fs.Form.Init()
	_, _ = fs.Form.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := fs.Form.GetFocusedField().Error(); !errors.Is(got, errTitleRequired) {
		t.Fatalf("test setup validation error = %v, want %v", got, errTitleRequired)
	}

	nextTheme := phase4TestTheme()
	nextTheme.Accent = "#0A0B0C"
	formBefore := fs.Form
	fs.setTheme(nextTheme)
	if fs.Form == formBefore {
		t.Fatal("live retheme did not rebuild form")
	}
	if got := fs.focusedFieldKey(); got != formKeyTitle {
		t.Fatalf("live retheme focus = %q, want title", got)
	}
	if got := fs.Form.GetFocusedField().Error(); !errors.Is(got, errTitleRequired) {
		t.Fatalf("live retheme validation error = %v, want %v", got, errTitleRequired)
	}
}

func snapshotFormState(fs *FormState) any {
	return struct {
		Mode                        FormMode
		IssueID, ParentID           string
		Title, Type, Priority       string
		Description, Labels, Status string
		ShowExtended, Minor         bool
		Parent, Points, Acceptance  string
		Dependencies                string
		ButtonFocus, ButtonHover    int
		Width                       int
		Autofill                    *AutofillState
		AutofillEpics, AutofillAll  []AutofillItem
	}{
		fs.Mode, fs.IssueID, fs.ParentID,
		fs.Title, fs.Type, fs.Priority,
		fs.Description, fs.Labels, fs.Status,
		fs.ShowExtended, fs.Minor,
		fs.Parent, fs.Points, fs.Acceptance,
		fs.Dependencies,
		fs.ButtonFocus, fs.ButtonHover,
		fs.Width,
		fs.Autofill,
		fs.AutofillEpics, fs.AutofillAll,
	}
}

func TestThemedFormConstructorPreservesStandaloneSelectFilter(t *testing.T) {
	standalone := NewFormState(FormModeCreate, "")
	themedDefault := newFormStateWithTheme(FormModeCreate, "", DefaultTheme())
	openHuhSelectFilter(t, standalone, formKeyType)
	openHuhSelectFilter(t, themedDefault, formKeyType)
	if got, want := themedDefault.Form.View(), standalone.Form.View(); got != want {
		t.Fatalf("host-aware constructor changed standalone select filtering:\n got %q\nwant %q", got, want)
	}
}

func openHuhSelectFilter(t *testing.T, fs *FormState, fieldKey string) {
	t.Helper()
	_ = fs.Form.Init()
	switch fieldKey {
	case formKeyType:
		_ = fs.Form.NextField()
	case formKeyPriority:
		_ = fs.Form.NextField()
		_ = fs.Form.NextField()
	case formKeyPoints:
		_ = fs.Form.NextGroup()
		_ = fs.Form.NextField()
	case formKeyStatus:
		_ = fs.Form.NextGroup()
		_ = fs.Form.NextGroup()
		_ = fs.Form.NextField()
		_ = fs.Form.NextField()
	default:
		t.Fatalf("unsupported select field %q", fieldKey)
	}
	if got := fs.focusedFieldKey(); got != fieldKey {
		t.Fatalf("focused field = %q, want %q", got, fieldKey)
	}
	_, _ = fs.Form.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	_, _ = fs.Form.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	filtering, ok := fs.Form.GetFocusedField().(interface{ GetFiltering() bool })
	if !ok || !filtering.GetFiltering() {
		t.Fatalf("%s select did not enter filter mode", fieldKey)
	}
}

func assertNoANSIColors(t *testing.T, content string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(content, fragment) {
			t.Errorf("render retained stale ANSI color %q: %q", fragment, content)
		}
	}
}

func containsAny(content string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(content, fragment) {
			return true
		}
	}
	return false
}

func TestThemeDrivesHelpAutocompleteAndOverlay(t *testing.T) {
	m := NewModel(nil, "test", 0, "dev", t.TempDir())
	m.Width, m.Height = 40, 8
	if err := m.SetTheme(phase4TestTheme()); err != nil {
		t.Fatal(err)
	}

	help := m.renderHelpText("MONITOR TUI - Key Bindings\n\nNAVIGATION:\n  Enter                Open")
	primary := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(phase4TestTheme().Primary)).Render("X"), "X")
	text := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(phase4TestTheme().TextPrimary)).Render("X"), "X")
	if !strings.Contains(help, primary) || !strings.Contains(help, text) {
		t.Fatalf("help text is not fully themed: %q", help)
	}

	m.FormState = NewFormState(FormModeCreate, "")
	m.FormState.Autofill = &AutofillState{Active: true, Filtered: []AutofillItem{{ID: "td-one", Title: "One"}}}
	if got := m.renderFormAutofillDropdown(); !strings.Contains(got, primary) {
		t.Fatalf("autocomplete selection did not use model primary: %q", got)
	}

	backdrop := colorFragment(lipgloss.NewStyle().Foreground(lipgloss.Color(phase4TestTheme().Backdrop)).Render("X"), "X")
	if got := m.overlayModal("background", "modal"); !strings.Contains(got, backdrop) {
		t.Fatalf("overlay did not use model backdrop: %q", got)
	}
}

func TestMarkdownThemeCompatibilityUsesCompleteThemeColors(t *testing.T) {
	theme := phase4TestTheme()
	theme.SyntaxTheme = "dracula"
	theme.MarkdownTheme = "dark"
	config := markdownThemeConfig(theme)
	if config == nil || config.Colors == nil {
		t.Fatal("complete theme did not produce semantic markdown colors")
	}
	if config.Colors.Primary != "#D10203" || config.Colors.Text != "#E4E5E6" || config.Colors.BgCode != "#020202" {
		t.Fatalf("semantic markdown palette = %#v", config.Colors)
	}
	if config.SyntaxTheme != "dracula" || config.MarkdownTheme != "dark" {
		t.Fatalf("named markdown compatibility selectors were lost: %#v", config)
	}
}

func TestCompleteDefaultThemePreservesStandaloneMarkdown(t *testing.T) {
	markdown := "# Heading\n\n### Three\n\n#### Four\n\n[link](https://example.com)\n\n```go\nfunc main() {}\n```"
	legacy := preRenderMarkdown(markdown, 72, nil)
	complete := preRenderMarkdown(markdown, 72, markdownThemeConfig(DefaultTheme()))
	if complete != legacy {
		t.Fatalf("complete default theme changed standalone markdown:\ncomplete %q\n  legacy %q", complete, legacy)
	}
}
