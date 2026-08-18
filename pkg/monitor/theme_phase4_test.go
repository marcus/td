package monitor

import (
	"strings"
	"testing"

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

func TestSetThemeRethemesOpenFormInPlace(t *testing.T) {
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

	if fs.Form != formBefore {
		t.Fatal("live retheme rebuilt the Huh form")
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
